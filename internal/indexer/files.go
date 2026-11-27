package indexer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	ModTime time.Time
}

// scanFiles lists the Markdown and text files below a folder in path order, and counts the
// ones it left out for a reason worth reporting: a link, a file over the size limit.
//
// Hidden files and folders are skipped. Symbolic links are never followed or listed, so a
// link inside the folder cannot lead the walk outside it.
//
// An error is only tolerated for something that has disappeared since the walk reached it.
// Any other failure, such as an unreadable subfolder, stops the scan: a listing that
// silently lacks part of the tree would make the index treat the missing documents as
// deleted and remove them.
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
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
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
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entryInfo.Mode().IsRegular() || entryInfo.Size() > maxFileSize {
			skipped++
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, sourceFile{RelPath: filepath.ToSlash(rel), ModTime: entryInfo.ModTime()})

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
// path below the folder. A file that is not text, is empty or is over the size limit is left
// out and counted, and one that vanished between the scan and the read is simply gone.
//
// Every other read failure stops the run. Leaving the file out would drop its document from
// the corpus, and the index removes documents the corpus no longer holds, so a permission
// or I/O error would delete stored documents. A corpus with no documents at all is
// rejected for the same reason: a folder emptied or made unreadable since the preflight
// must not wipe what the source held.
//
// Files are opened through an os.Root on the source folder, which refuses any path that
// resolves outside it, symbolic links included. The scan already skips links, so this
// only matters if one is swapped in afterwards, and it keeps the containment from
// depending on that earlier check.
func loadFiles(source Source, chunkSize int, overlap int) (Corpus, error) {
	chunkSize, overlap = normalizeChunking(chunkSize, overlap)

	files, skipped, err := scanFiles(source.Folder)
	if err != nil {
		return Corpus{}, err
	}

	root, err := os.OpenRoot(source.Folder)
	if err != nil {
		return Corpus{}, fmt.Errorf("open source folder %s: %w", source.Folder, err)
	}
	defer func() { _ = root.Close() }()

	docs := make([]DocumentInput, 0, len(files))
	for _, file := range files {
		content, err := readConfined(root, file.RelPath)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case errors.Is(err, errSkipFile):
			skipped++
			continue
		case err != nil:
			return Corpus{}, fmt.Errorf("read %s in source folder %s: %w", file.RelPath, source.Folder, err)
		}
		if !isText(content) {
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

	if len(docs) == 0 {
		return Corpus{}, fmt.Errorf("no indexable text files in source folder %s (%d files were empty, not text, or over the size limit)", source.Folder, skipped)
	}

	return Corpus{Documents: docs, Crawl: CrawlInfo{PageCount: len(docs)}, Skipped: skipped}, nil
}

// errSkipFile marks a file that is left out on purpose: not a regular file, or too large.
var errSkipFile = errors.New("file is not indexable")

// readConfined reads one file below a root without leaving it. The size is checked on the
// opened handle and the read is capped, so a file that grew since the scan cannot be read
// whole either.
func readConfined(root *os.Root, rel string) ([]byte, error) {
	file, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errSkipFile
	}

	content, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxFileSize {
		return nil, errSkipFile
	}

	return content, nil
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
