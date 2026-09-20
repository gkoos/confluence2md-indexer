package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultChunkSize    = 1200
	DefaultChunkOverlap = 200
)

type DocumentInput struct {
	ID          string
	PageID      string
	Title       string
	LocalPath   string
	SpaceKey    string
	SourceURL   string
	ModifiedAt  string
	ContentHash string
	Chunks      []ChunkInput
}

type ChunkInput struct {
	ID         string
	ChunkIndex int
	Text       string
	ChunkHash  string
}

func LoadDocuments(folder string, chunkSize int, overlap int) ([]DocumentInput, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 2
	}

	absFolder, meta, err := loadMetadata(folder)
	if err != nil {
		return nil, err
	}

	pageIDs := make([]string, 0, len(meta.Pages))
	for pageID := range meta.Pages {
		pageIDs = append(pageIDs, pageID)
	}
	sort.Strings(pageIDs)

	docs := make([]DocumentInput, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		page := meta.Pages[pageID]
		localPath := strings.TrimSpace(page.LocalPath)
		if localPath == "" {
			return nil, fmt.Errorf("metadata.pages[%s].local_path is empty", pageID)
		}

		mdPath := filepath.Join(absFolder, filepath.FromSlash(localPath))
		contentBytes, err := os.ReadFile(mdPath)
		if err != nil {
			return nil, fmt.Errorf("read markdown file %s: %w", mdPath, err)
		}

		clean := normalizeContent(stripFrontMatter(string(contentBytes)))
		sections := splitSections(clean)
		chunkTexts := chunkSections(sections, chunkSize, overlap)

		chunks := make([]ChunkInput, 0, len(chunkTexts))
		for i, text := range chunkTexts {
			chunks = append(chunks, ChunkInput{
				ID:         ChunkID(pageID, i),
				ChunkIndex: i,
				Text:       text,
				ChunkHash:  ContentHash(text),
			})
		}

		docs = append(docs, DocumentInput{
			ID:          pageID,
			PageID:      pageID,
			Title:       strings.TrimSpace(page.Title),
			LocalPath:   filepath.ToSlash(localPath),
			SpaceKey:    strings.TrimSpace(page.SpaceKey),
			SourceURL:   strings.TrimSpace(page.SourceURL),
			ModifiedAt:  strings.TrimSpace(page.LastModifiedAt),
			ContentHash: ContentHash(clean),
			Chunks:      chunks,
		})
	}

	return docs, nil
}

func loadMetadata(folder string) (string, metadataFile, error) {
	absFolder, err := filepath.Abs(strings.TrimSpace(folder))
	if err != nil {
		return "", metadataFile{}, fmt.Errorf("resolve folder %q: %w", folder, err)
	}
	metaPath := filepath.Join(absFolder, MetadataFileName)
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return "", metadataFile{}, fmt.Errorf("read metadata file %s: %w", metaPath, err)
	}
	var meta metadataFile
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return "", metadataFile{}, fmt.Errorf("parse metadata file %s: %w", metaPath, err)
	}
	return absFolder, meta, nil
}

func stripFrontMatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	idx := strings.Index(content[4:], "\n---\n")
	if idx < 0 {
		return content
	}
	start := 4 + idx + len("\n---\n")
	return content[start:]
}

func normalizeContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.TrimSpace(content)
}

func splitSections(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	sections := make([]string, 0, 8)
	var current []string

	flush := func() {
		if len(current) == 0 {
			return
		}
		section := strings.TrimSpace(strings.Join(current, "\n"))
		if section != "" {
			sections = append(sections, section)
		}
		current = nil
	}

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trim, "#")
		if isHeading && len(current) > 0 {
			flush()
		}
		current = append(current, line)
	}
	flush()

	if len(sections) == 0 {
		sections = append(sections, strings.TrimSpace(content))
	}

	return sections
}

func chunkSections(sections []string, chunkSize int, overlap int) []string {
	if len(sections) == 0 {
		return nil
	}

	out := make([]string, 0, len(sections))
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	for _, section := range sections {
		text := strings.TrimSpace(section)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) <= chunkSize {
			out = append(out, text)
			continue
		}
		for start := 0; start < len(runes); start += step {
			end := min(start+chunkSize, len(runes))
			chunk := strings.TrimSpace(string(runes[start:end]))
			if chunk != "" {
				out = append(out, chunk)
			}
			if end == len(runes) {
				break
			}
		}
	}

	return out
}
