package indexer

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// maxFileSize is the largest file a files source indexes. A text file this size is almost
// always a log or a data dump, which would flood the index with chunks nobody searches for.
const maxFileSize = 8 << 20

// indexableExtensions are the file types a files source reads, compared in lower case.
// Everything else, including binaries, databases and configuration, is left alone.
var indexableExtensions = map[string]bool{".md": true, ".markdown": true, ".txt": true}

// sourceFile is one file a files source will index.
type sourceFile struct {
	// RelPath is the path below the source folder, with forward slashes.
	RelPath string
	AbsPath string
	ModTime time.Time
}

// scanFiles lists the Markdown and text files below a folder in path order, and counts the
// ones it left out for a reason worth reporting: a link, a file over the size limit.
//
// Hidden files and folders are skipped. Symbolic links are never followed or listed, so a
// link inside the folder cannot lead the walk outside it.
func scanFiles(root string) ([]sourceFile, int, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("source folder does not exist: %s", root)
		}
		return nil, 0, fmt.Errorf("stat source folder %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("source path is not a directory: %s", root)
	}

	var files []sourceFile
	skipped := 0

	err = filepath.WalkDir(root, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr
			}
			skipped++
			return nil
		}
		if p == root {
			return nil
		}

		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			skipped++
			return nil
		}
		if entry.IsDir() || !indexableExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}

		entryInfo, err := entry.Info()
		if err != nil || !entryInfo.Mode().IsRegular() || entryInfo.Size() > maxFileSize {
			skipped++
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			skipped++
			return nil
		}
		files = append(files, sourceFile{RelPath: filepath.ToSlash(rel), AbsPath: p, ModTime: entryInfo.ModTime()})

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("scan source folder %s: %w", root, err)
	}

	return files, skipped, nil
}

// PreflightSource checks a source with the preflight that fits its type.
func PreflightSource(source Source) (*PreflightSummary, error) {
	if source.Type == TypeFiles {
		return PreflightFiles(source.Folder)
	}

	return Preflight(source.Folder)
}

// PreflightFiles checks a files source folder and counts what it holds.
func PreflightFiles(folder string) (*PreflightSummary, error) {
	absFolder, err := filepath.Abs(strings.TrimSpace(folder))
	if err != nil {
		return nil, fmt.Errorf("resolve source folder %q: %w", folder, err)
	}

	files, skipped, err := scanFiles(absFolder)
	if err != nil {
		return nil, err
	}

	// An empty folder is far more likely a mistake than an intent to index nothing, and
	// indexing it would remove everything the source held before. A folder of crawler
	// output that lost its metadata.json looks like this too, so the message says so.
	if len(files) == 0 {
		return nil, fmt.Errorf("no Markdown or text files found in %s (a folder of crawler output needs a %s; the source type can be set with id:type=folder)", absFolder, MetadataFileName)
	}

	return &PreflightSummary{FolderPath: absFolder, PageCount: len(files), MarkdownChecked: len(files), SkippedFiles: skipped}, nil
}

// loadFiles reads a files source: one document per Markdown or text file, identified by its
// path below the folder. A file that is not text, is empty, or vanishes between the scan and
// the read is left out and counted, so one bad file cannot fail the run.
func loadFiles(source Source, chunkSize int, overlap int) (Corpus, error) {
	chunkSize, overlap = normalizeChunking(chunkSize, overlap)

	files, skipped, err := scanFiles(source.Folder)
	if err != nil {
		return Corpus{}, err
	}

	docs := make([]DocumentInput, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file.AbsPath)
		if err != nil || !isText(content) {
			skipped++
			continue
		}

		raw := strings.TrimPrefix(string(content), "\ufeff")
		isMarkdown := strings.ToLower(filepath.Ext(file.RelPath)) != ".txt"

		body := raw
		if isMarkdown {
			body = stripFrontMatter(raw)
		}
		clean := normalizeContent(body)
		if strings.TrimSpace(clean) == "" {
			skipped++
			continue
		}

		// Plain text has no headings: a '#' line in it is text, not a section.
		sections := []contentSection{{Text: strings.TrimSpace(clean)}}
		title := strings.TrimSuffix(path.Base(file.RelPath), path.Ext(file.RelPath))
		if isMarkdown {
			sections = splitSections(clean)
			if heading := firstHeading(clean); heading != "" {
				title = heading
			}
		}

		docID := source.ID + "/" + file.RelPath
		drafts := chunkSections(sections, chunkSize, overlap)
		chunks := make([]ChunkInput, 0, len(drafts))
		for i, draft := range drafts {
			chunks = append(chunks, ChunkInput{
				ID:         ChunkID(docID, i),
				ChunkIndex: i,
				Text:       draft.Text,
				ChunkHash:  ContentHash(draft.Text),
				Section:    draft.Section,
			})
		}

		docs = append(docs, DocumentInput{
			ID:          docID,
			PageID:      file.RelPath,
			SourceID:    source.ID,
			SourceType:  TypeFiles,
			Title:       title,
			LocalPath:   file.RelPath,
			ModifiedAt:  file.ModTime.UTC().Format(time.RFC3339),
			ContentHash: ContentHash(clean),
			Chunks:      chunks,
		})
	}

	return Corpus{Documents: docs, Crawl: CrawlInfo{PageCount: len(docs)}, Skipped: skipped}, nil
}

// isText reports whether bytes look like text: valid UTF-8 with no NUL byte. A NUL near the
// start is the usual sign of a binary file that happens to carry a text extension.
func isText(content []byte) bool {
	head := content
	if len(head) > 8000 {
		head = head[:8000]
	}

	return !bytes.Contains(head, []byte{0}) && utf8.Valid(content)
}

// firstHeading returns the title of the first heading outside a fenced code block.
func firstHeading(content string) string {
	fence := &fenceTracker{}
	for _, line := range strings.Split(content, "\n") {
		if fence.observe(line) || fence.open() {
			continue
		}
		if _, title, ok := parseHeading(line); ok && title != "" {
			return title
		}
	}

	return ""
}
