package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const MetadataFileName = "metadata.json"

type metadataFile struct {
	Pages map[string]pageRecord `json:"pages"`
	// Root fields describe the crawl that produced the pages. They are stored with
	// the index so stats can report how stale it is.
	SeedPageIDs                    []string `json:"seed_page_ids"`
	CrawlStartedAt                 string   `json:"crawl_started_at"`
	LastCompletedCrawlCompletedAt  string   `json:"last_completed_crawl_completed_at"`
	LastCompletedCrawlMode         string   `json:"last_completed_crawl_mode"`
	LastSuccessfulCrawlCompletedAt string   `json:"last_successful_crawl_completed_at"`
}

// pageRecord is one entry of metadata.json. Only the fields the indexer stores are
// decoded; the crawler writes more, and unknown keys are ignored on purpose so a
// newer crawler keeps working with an older indexer.
type pageRecord struct {
	ID                 string   `json:"id"`
	Host               string   `json:"host"`
	LocalPath          string   `json:"local_path"`
	Title              string   `json:"title"`
	SpaceKey           string   `json:"space_key"`
	Version            int      `json:"version"`
	Depth              int      `json:"depth"`
	CrawledAt          string   `json:"crawled_at"`
	CreatedAt          string   `json:"created_at"`
	LastModifiedAt     string   `json:"last_modified_at"`
	SourceURL          string   `json:"source_url"`
	CanonicalURL       string   `json:"canonical_url"`
	ConfluenceParentID string   `json:"confluence_parent_id"`
	CreatedByName      string   `json:"created_by_name"`
	LastModifiedByName string   `json:"last_modified_by_name"`
	OutgoingLinks      []string `json:"outgoing_links"`
	IncomingLinks      []string `json:"incoming_links"`
	Attachments        []string `json:"attachments"`
	CommentCount       int      `json:"comment_count"`
	FetchError         string   `json:"fetch_error"`
}

type PreflightSummary struct {
	FolderPath      string `json:"folderPath"`
	MetadataPath    string `json:"metadataPath"`
	PageCount       int    `json:"pageCount"`
	MarkdownChecked int    `json:"markdownChecked"`
}

func Preflight(folder string) (*PreflightSummary, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = "."
	}

	absFolder, err := filepath.Abs(folder)
	if err != nil {
		return nil, fmt.Errorf("resolve input folder %q: %w", folder, err)
	}

	folderInfo, err := os.Stat(absFolder)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("input folder does not exist: %s", absFolder)
		}
		return nil, fmt.Errorf("stat input folder %s: %w", absFolder, err)
	}
	if !folderInfo.IsDir() {
		return nil, fmt.Errorf("input path is not a directory: %s", absFolder)
	}

	metaPath := filepath.Join(absFolder, MetadataFileName)
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("missing required file %s", metaPath)
		}
		return nil, fmt.Errorf("read metadata file %s: %w", metaPath, err)
	}

	var meta metadataFile
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("parse metadata file %s: %w", metaPath, err)
	}

	if len(meta.Pages) == 0 {
		return nil, fmt.Errorf("metadata file %s has empty pages corpus", metaPath)
	}

	checked := 0
	for pageID, page := range meta.Pages {
		localPath := strings.TrimSpace(page.LocalPath)
		if localPath == "" {
			return nil, fmt.Errorf("metadata.pages[%s].local_path is empty", pageID)
		}

		mdPath := filepath.Join(absFolder, filepath.FromSlash(localPath))
		mdInfo, err := os.Stat(mdPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("markdown file referenced by metadata.pages[%s].local_path not found: %s", pageID, mdPath)
			}
			return nil, fmt.Errorf("stat markdown file %s: %w", mdPath, err)
		}
		if mdInfo.IsDir() {
			return nil, fmt.Errorf("metadata.pages[%s].local_path points to a directory, expected markdown file: %s", pageID, mdPath)
		}

		f, err := os.Open(mdPath)
		if err != nil {
			return nil, fmt.Errorf("open markdown file %s: %w", mdPath, err)
		}
		_ = f.Close()
		checked++
	}

	return &PreflightSummary{
		FolderPath:      absFolder,
		MetadataPath:    metaPath,
		PageCount:       len(meta.Pages),
		MarkdownChecked: checked,
	}, nil
}
