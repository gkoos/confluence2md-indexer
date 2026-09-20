package indexer

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// frontMatter is the deterministic YAML block the crawler writes at the top of
// every exported page. It repeats part of metadata.json and adds fields that only
// exist there (is_seed, created_by, last_modified_by, comment_count, attachments),
// so it is used to fill gaps instead of overriding the JSON.
type frontMatter struct {
	PageID             string   `yaml:"page_id"`
	Title              string   `yaml:"title"`
	SourceURL          string   `yaml:"source_url"`
	CanonicalURL       string   `yaml:"canonical_url"`
	SpaceKey           string   `yaml:"space_key"`
	IsSeed             *bool    `yaml:"is_seed"`
	CrawledAt          string   `yaml:"crawled_at"`
	CreatedAt          string   `yaml:"created_at"`
	LastModifiedAt     string   `yaml:"last_modified_at"`
	CreatedBy          string   `yaml:"created_by"`
	LastModifiedBy     string   `yaml:"last_modified_by"`
	ConfluenceParentID string   `yaml:"confluence_parent_id"`
	CommentCount       int      `yaml:"comment_count"`
	Attachments        []string `yaml:"attachments"`
}

// parseFrontMatter reads the leading YAML block. A page without one, or with a block
// that does not parse, yields an empty result: front matter is a bonus source, never
// a reason to fail an index run.
func parseFrontMatter(content string) frontMatter {
	block, ok := frontMatterBlock(content)
	if !ok {
		return frontMatter{}
	}

	var parsed frontMatter
	if err := yaml.Unmarshal([]byte(block), &parsed); err != nil {
		return frontMatter{}
	}

	return parsed
}

// frontMatterBlock returns the text between the leading "---" line and the next one.
// Line endings are normalised first, so a file written with CRLF is read too.
func frontMatterBlock(content string) (string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", false
	}

	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return "", false
	}

	return content[4 : 4+end], true
}
