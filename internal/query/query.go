package query

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

type Request struct {
	Text       string
	Mode       string
	Fusion     string
	Alpha      float64
	RRFK       int
	TopK       int
	Offset     int
	Limit      int
	CandidateK int
	Expand     int
	Filters    db.SearchFilters
}

type Result struct {
	Rank              int     `json:"rank"`
	ChunkID           string  `json:"chunkId"`
	DocumentID        string  `json:"documentId"`
	PageID            string  `json:"pageId"`
	Title             string  `json:"title"`
	LocalPath         string  `json:"localPath"`
	SpaceKey          string  `json:"spaceKey"`
	SourceURL         string  `json:"sourceUrl"`
	ChunkText         string  `json:"chunkText"`
	BaseChunkText     string  `json:"baseChunkText,omitempty"`
	ChunkIndex        int     `json:"chunkIndex"`
	ContextStartIndex int     `json:"contextStartIndex,omitempty"`
	ContextEndIndex   int     `json:"contextEndIndex,omitempty"`
	ContextChunkCount int     `json:"contextChunkCount,omitempty"`
	Lexical           float64 `json:"lexicalScore"`
	Vector            float64 `json:"vectorScore"`
	Fused             float64 `json:"fusedScore"`
	Fusion            string  `json:"fusion"`
}

func Run(ctx context.Context, database *sql.DB, provider embedding.Provider, req Request) ([]Result, int, error) {
	if database == nil {
		return nil, 0, fmt.Errorf("database is nil")
	}
	if provider == nil {
		provider = embedding.NewHashProvider(256)
	}

	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		return nil, 0, nil
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	if req.Limit <= 0 {
		req.Limit = req.TopK
	}
	if req.CandidateK <= 0 {
		req.CandidateK = 50
	}
	if req.Alpha < 0 || req.Alpha > 1 {
		req.Alpha = 0.70
	}
	if req.RRFK <= 0 {
		req.RRFK = 60
	}
	req.Filters.Candidate = req.CandidateK

	var lexical []db.Candidate
	var vector []db.Candidate
	var err error

	if req.Mode == "lexical" || req.Mode == "hybrid" {
		lexical, err = db.SearchLexical(ctx, database, req.Text, req.Filters)
		if err != nil {
			return nil, 0, fmt.Errorf("lexical search: %w", err)
		}
	}

	if req.Mode == "vector" || req.Mode == "hybrid" {
		vecs, err := provider.Embed(ctx, []string{req.Text})
		if err != nil {
			return nil, 0, fmt.Errorf("query embedding: %w", err)
		}
		if len(vecs) > 0 {
			vector, err = db.SearchVector(ctx, database, vecs[0], req.Filters)
			if err != nil {
				return nil, 0, fmt.Errorf("vector search: %w", err)
			}
		}
	}

	allResults := fuse(req, lexical, vector)
	if len(allResults) > req.TopK {
		allResults = allResults[:req.TopK]
	}
	total := len(allResults)

	start := min(req.Offset, total)
	end := min(start+req.Limit, total)

	results := append([]Result(nil), allResults[start:end]...)
	if req.Expand > 0 {
		if err := applyExpansion(ctx, database, results, req.Expand); err != nil {
			return nil, 0, fmt.Errorf("apply expansion: %w", err)
		}
	}
	for i := range results {
		results[i].Rank = start + i + 1
	}
	return results, total, nil
}

func applyExpansion(ctx context.Context, database *sql.DB, results []Result, expand int) error {
	for i := range results {
		window, err := db.FetchChunkWindow(ctx, database, results[i].DocumentID, results[i].ChunkIndex, expand)
		if err != nil {
			return err
		}
		if len(window) == 0 {
			continue
		}

		parts := make([]string, 0, len(window))
		results[i].BaseChunkText = results[i].ChunkText
		results[i].ContextStartIndex = window[0].ChunkIndex
		results[i].ContextEndIndex = window[len(window)-1].ChunkIndex
		results[i].ContextChunkCount = len(window)
		for _, item := range window {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
		results[i].ChunkText = strings.Join(parts, "\n\n")
	}

	return nil
}

func fuse(req Request, lexical, vector []db.Candidate) []Result {
	lexNorm := normalizeScores(lexical, func(c db.Candidate) float64 { return c.LexicalScoreRaw })
	vecNorm := normalizeScores(vector, func(c db.Candidate) float64 { return c.VectorScoreRaw })

	combined := map[string]*Result{}
	merge := func(c db.Candidate) *Result {
		if existing, ok := combined[c.ChunkID]; ok {
			return existing
		}
		r := &Result{
			ChunkID:    c.ChunkID,
			DocumentID: c.DocumentID,
			PageID:     c.PageID,
			Title:      c.Title,
			LocalPath:  c.LocalPath,
			SpaceKey:   c.SpaceKey,
			SourceURL:  c.SourceURL,
			ChunkText:  c.ChunkText,
			ChunkIndex: c.ChunkIndex,
		}
		combined[c.ChunkID] = r
		return r
	}

	for _, c := range lexical {
		r := merge(c)
		r.Lexical = lexNorm[c.ChunkID]
	}
	for _, c := range vector {
		r := merge(c)
		r.Vector = vecNorm[c.ChunkID]
	}

	if req.Mode == "lexical" {
		for _, r := range combined {
			r.Fused = r.Lexical
			r.Fusion = "lexical"
		}
	} else if req.Mode == "vector" {
		for _, r := range combined {
			r.Fused = r.Vector
			r.Fusion = "vector"
		}
	} else if req.Fusion == "rrf" {
		rrf(req, lexical, vector, combined)
	} else {
		for _, r := range combined {
			r.Fused = req.Alpha*r.Lexical + (1-req.Alpha)*r.Vector
			r.Fusion = "weighted"
		}
	}

	out := make([]Result, 0, len(combined))
	for _, r := range combined {
		if r.Fused <= 0 {
			continue
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Fused == out[j].Fused {
			return out[i].ChunkID < out[j].ChunkID
		}
		return out[i].Fused > out[j].Fused
	})

	return out
}

func rrf(req Request, lexical, vector []db.Candidate, combined map[string]*Result) {
	ranksL := rankMap(lexical)
	ranksV := rankMap(vector)
	for chunkID, r := range combined {
		score := 0.0
		if rank, ok := ranksL[chunkID]; ok {
			score += 1.0 / float64(req.RRFK+rank)
		}
		if rank, ok := ranksV[chunkID]; ok {
			score += 1.0 / float64(req.RRFK+rank)
		}
		r.Fused = score
		r.Fusion = "rrf"
	}
}

func rankMap(candidates []db.Candidate) map[string]int {
	m := map[string]int{}
	for i, c := range candidates {
		if _, exists := m[c.ChunkID]; !exists {
			m[c.ChunkID] = i + 1
		}
	}
	return m
}

func normalizeScores(candidates []db.Candidate, selector func(db.Candidate) float64) map[string]float64 {
	out := map[string]float64{}
	if len(candidates) == 0 {
		return out
	}
	min := selector(candidates[0])
	max := min
	for _, c := range candidates {
		v := selector(c)
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if max == min {
		for _, c := range candidates {
			out[c.ChunkID] = 1
		}
		return out
	}
	for _, c := range candidates {
		v := selector(c)
		out[c.ChunkID] = (v - min) / (max - min)
	}
	return out
}
