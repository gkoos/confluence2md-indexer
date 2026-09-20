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

// Metadata is the crawler metadata stored with a document. Counts are derived from
// the link and attachment lists the crawler writes; a zero value means the field was
// not reported, which is why filters built on it are opt-in.
type Metadata struct {
	Host           string
	CanonicalURL   string
	Version        int
	Depth          int
	ParentID       string
	CreatedAt      string
	CrawledAt      string
	CreatedByName  string
	ModifiedByName string
	IsSeed         bool
	LinkIn         int
	LinkOut        int
	Attachments    int
	Comments       int
}

type DocumentInput struct {
	ID          string
	PageID      string
	Title       string
	LocalPath   string
	SpaceKey    string
	SourceURL   string
	ModifiedAt  string
	ContentHash string
	Metadata    Metadata
	Chunks      []ChunkInput
}

// MetadataFingerprint identifies every stored metadata value, including the ones
// that also feed the search index (title, space, URL, dates), so an index run can
// tell "nothing changed" from "metadata changed" from "content changed".
func (d DocumentInput) MetadataFingerprint() string {
	return ContentHash(fmt.Sprintf("%q|%q|%q|%q|%+v", d.Title, d.SpaceKey, d.SourceURL, d.ModifiedAt, d.Metadata))
}

// CrawlInfo describes the crawl a folder came from, as recorded by the crawler.
type CrawlInfo struct {
	StartedAt   string
	CompletedAt string
	SucceededAt string
	Mode        string
	SeedCount   int
	PageCount   int
}

// Corpus is everything one index run reads from a folder.
type Corpus struct {
	// Documents holds one entry per page, sorted by page id.
	Documents []DocumentInput
	// Crawl captures the crawl that produced the output.
	Crawl CrawlInfo
}

type ChunkInput struct {
	ID         string
	ChunkIndex int
	Text       string
	ChunkHash  string
	// Section is the heading breadcrumb the chunk sits under, for example
	// "Deployment > Rollback"; it is indexed for matching and is empty for text
	// above the first heading.
	Section string
}

// LoadDocuments returns just the documents, for callers that do not track crawl
// information.
func LoadDocuments(folder string, chunkSize int, overlap int) ([]DocumentInput, error) {
	corpus, err := LoadCorpus(folder, chunkSize, overlap)
	if err != nil {
		return nil, err
	}

	return corpus.Documents, nil
}

// LoadCorpus reads metadata.json, the markdown files it references, and the crawl
// information the metadata file carries.
func LoadCorpus(folder string, chunkSize int, overlap int) (Corpus, error) {
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
		return Corpus{}, err
	}

	seeds := make(map[string]bool, len(meta.SeedPageIDs))
	for _, id := range meta.SeedPageIDs {
		seeds[strings.TrimSpace(id)] = true
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
			return Corpus{}, fmt.Errorf("metadata.pages[%s].local_path is empty", pageID)
		}

		mdPath := filepath.Join(absFolder, filepath.FromSlash(localPath))
		contentBytes, err := os.ReadFile(mdPath)
		if err != nil {
			return Corpus{}, fmt.Errorf("read markdown file %s: %w", mdPath, err)
		}

		raw := string(contentBytes)
		clean := normalizeContent(stripFrontMatter(raw))
		sections := splitSections(clean)
		drafts := chunkSections(sections, chunkSize, overlap)

		chunks := make([]ChunkInput, 0, len(drafts))
		for i, draft := range drafts {
			chunks = append(chunks, ChunkInput{
				ID:         ChunkID(pageID, i),
				ChunkIndex: i,
				Text:       draft.Text,
				ChunkHash:  ContentHash(draft.Text),
				Section:    draft.Section,
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
			Metadata:    mergePageMetadata(pageID, page, parseFrontMatter(raw), seeds),
			Chunks:      chunks,
		})
	}

	return Corpus{
		Documents: docs,
		Crawl: CrawlInfo{
			StartedAt:   strings.TrimSpace(meta.CrawlStartedAt),
			CompletedAt: strings.TrimSpace(meta.LastCompletedCrawlCompletedAt),
			SucceededAt: strings.TrimSpace(meta.LastSuccessfulCrawlCompletedAt),
			Mode:        strings.TrimSpace(meta.LastCompletedCrawlMode),
			SeedCount:   len(meta.SeedPageIDs),
			PageCount:   len(docs),
		},
	}, nil
}

// mergePageMetadata combines what metadata.json says about a page with what its front
// matter says. The JSON wins where both carry a value, because it is the crawler's
// own record, while front matter fills the gaps it leaves (is_seed, authors,
// attachments, comment count, created_at) for whatever a page happens to have.
func mergePageMetadata(pageID string, page pageRecord, front frontMatter, seeds map[string]bool) Metadata {
	attachments := len(page.Attachments)
	if attachments == 0 {
		attachments = len(front.Attachments)
	}

	comments := page.CommentCount
	if comments == 0 {
		comments = front.CommentCount
	}

	// seed_page_ids is authoritative when the crawler wrote it; a crawler that does
	// not write it still marks seeds in the front matter.
	isSeed := seeds[pageID]
	if len(seeds) == 0 && front.IsSeed != nil {
		isSeed = *front.IsSeed
	}

	return Metadata{
		Host:           strings.TrimSpace(page.Host),
		CanonicalURL:   firstNonEmptyString(page.CanonicalURL, front.CanonicalURL),
		Version:        page.Version,
		Depth:          page.Depth,
		ParentID:       firstNonEmptyString(page.ConfluenceParentID, front.ConfluenceParentID),
		CreatedAt:      firstNonEmptyString(page.CreatedAt, front.CreatedAt),
		CrawledAt:      firstNonEmptyString(page.CrawledAt, front.CrawledAt),
		CreatedByName:  firstNonEmptyString(page.CreatedByName, front.CreatedBy),
		ModifiedByName: firstNonEmptyString(page.LastModifiedByName, front.LastModifiedBy),
		IsSeed:         isSeed,
		LinkIn:         len(page.IncomingLinks),
		LinkOut:        len(page.OutgoingLinks),
		Attachments:    attachments,
		Comments:       comments,
	}
}

// firstNonEmptyString returns the first value that carries something other than
// whitespace.
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
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
	content = strings.ReplaceAll(content, "\r\n", "\n")
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

// contentSection is one heading-delimited part of a page together with the heading
// path that leads to it.
type contentSection struct {
	Text       string
	Breadcrumb string
}

// chunkDraft is one chunk before it is stored, carrying the breadcrumb of the
// section it came from.
type chunkDraft struct {
	Text    string
	Section string
}

// heading is one entry of the heading stack a breadcrumb is built from.
type heading struct {
	level int
	title string
}

func splitSections(content string) []contentSection {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	sections := make([]contentSection, 0, 8)
	headings := make([]heading, 0, 4)
	fence := &fenceTracker{}
	var current []string

	flush := func() {
		if len(current) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(current, "\n"))
		if text != "" {
			sections = append(sections, contentSection{Text: text, Breadcrumb: breadcrumbOf(headings)})
		}
		current = nil
	}

	for _, line := range lines {
		// A fenced code block is content, so a '#' inside it must not split the
		// section or enter the breadcrumb.
		if fence.observe(line) {
			current = append(current, line)
			continue
		}
		if !fence.open() {
			if level, title, isHeading := parseHeading(line); isHeading {
				flush()
				headings = pushHeading(headings, level, title)
			}
		}
		current = append(current, line)
	}
	flush()

	if len(sections) == 0 {
		if text := strings.TrimSpace(content); text != "" {
			sections = append(sections, contentSection{Text: text})
		}
	}

	return sections
}

// fenceTracker follows fenced code blocks so headings inside them stay content.
//
// The rules follow CommonMark closely enough for exported wiki pages: a fence opens
// with three or more backticks or tildes (indentation of up to three spaces is
// allowed, which trimming covers) and closes with a run of the same character that is
// at least as long and carries nothing else. An unclosed fence runs to the end of the
// document, so its remaining text is never read as headings.
type fenceTracker struct {
	// marker is the fence character while a fence is open, and zero otherwise.
	marker byte
	// length counts the characters that opened the current fence.
	length int
}

// observe feeds one line to the tracker and reports whether the line is a fence
// delimiter, which is never a heading or a breadcrumb entry.
func (f *fenceTracker) observe(line string) bool {
	trim := strings.TrimSpace(line)

	char, count, ok := fenceRun(trim)
	if !ok {
		return false
	}

	if f.marker == 0 {
		f.marker, f.length = char, count
		return true
	}
	if char != f.marker || count < f.length {
		return false
	}
	// A closing fence holds nothing but the run itself; anything else is content.
	if strings.TrimSpace(trim[count:]) != "" {
		return false
	}

	f.marker, f.length = 0, 0
	return true
}

// open reports whether a fence is still running.
func (f *fenceTracker) open() bool {
	return f.marker != 0
}

// fenceRun reports the leading fence run of a trimmed line.
func fenceRun(trim string) (byte, int, bool) {
	if trim == "" {
		return 0, 0, false
	}

	char := trim[0]
	if char != '`' && char != '~' {
		return 0, 0, false
	}

	count := 0
	for count < len(trim) && trim[count] == char {
		count++
	}
	if count < 3 {
		return 0, 0, false
	}
	// A backtick fence cannot carry a backtick in its info string, which is what keeps
	// an inline "```" in prose from opening a block.
	if char == '`' && strings.ContainsRune(trim[count:], '`') {
		return 0, 0, false
	}

	return char, count, true
}

// parseHeading reports whether a line is an ATX heading and returns its level and
// title. Callers skip lines inside a fence: this rule alone would read a comment in a
// code block as a heading. It keeps the section-splitting behaviour this package
// always used, where any line whose first non-space character is '#' can open a
// section.
func parseHeading(line string) (int, string, bool) {
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "#") {
		return 0, "", false
	}

	level := 0
	for level < len(trim) && trim[level] == '#' {
		level++
	}
	if level > 6 {
		level = 6
	}

	// A title that starts with extra '#' or spaces would read badly in a
	// breadcrumb, so both are trimmed.
	title := strings.TrimSpace(trim[level:])
	title = strings.TrimSpace(strings.TrimLeft(title, "#"))

	return level, title, true
}

// pushHeading keeps the heading stack in document order: a heading closes every
// heading at its own level or deeper. A heading without a title closes them too but
// contributes nothing to the breadcrumb.
func pushHeading(stack []heading, level int, title string) []heading {
	for len(stack) > 0 && stack[len(stack)-1].level >= level {
		stack = stack[:len(stack)-1]
	}
	if title == "" {
		return stack
	}

	return append(stack, heading{level: level, title: title})
}

// breadcrumbOf joins the heading stack, for example "Deployment > Rollback".
func breadcrumbOf(stack []heading) string {
	titles := make([]string, 0, len(stack))
	for _, item := range stack {
		if item.title != "" {
			titles = append(titles, item.title)
		}
	}

	return strings.Join(titles, " > ")
}

func chunkSections(sections []contentSection, chunkSize int, overlap int) []chunkDraft {
	if len(sections) == 0 {
		return nil
	}

	out := make([]chunkDraft, 0, len(sections))
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	for _, section := range sections {
		text := strings.TrimSpace(section.Text)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) <= chunkSize {
			out = append(out, chunkDraft{Text: text, Section: section.Breadcrumb})
			continue
		}
		for start := 0; start < len(runes); start += step {
			end := min(start+chunkSize, len(runes))
			chunk := strings.TrimSpace(string(runes[start:end]))
			if chunk != "" {
				out = append(out, chunkDraft{Text: chunk, Section: section.Breadcrumb})
			}
			if end == len(runes) {
				break
			}
		}
	}

	return out
}
