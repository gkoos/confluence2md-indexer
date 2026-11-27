package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Source types an index can read.
const (
	// TypeConfluence is a folder of crawler output with a metadata.json.
	TypeConfluence = "confluence"
	// TypeFiles is a plain folder of Markdown and text files.
	TypeFiles = "files"
)

// DefaultSourceID names the source created for a single folder given without an id.
const DefaultSourceID = "default"

// maxSourceIDLength keeps ids short enough to read in result paths and flags.
const maxSourceIDLength = 64

// sourceIDPattern keeps ids safe inside document ids, which join them to page keys
// with a slash, and inside a --source value, which joins them to a folder with '='.
var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Source is one folder an index is built from. Its id namespaces every document it
// contributes, so two sources can hold pages with the same key without colliding.
type Source struct {
	ID     string
	Type   string
	Folder string
}

// SingleSource is the source list for one folder passed without an id. Its type is
// detected from the folder when the list is normalised.
func SingleSource(folder string) []Source {
	return []Source{{ID: DefaultSourceID, Folder: folder}}
}

// ParseSourceSpec reads an "id=folder" or "id:type=folder" flag value. Without a type,
// the type is detected from the folder.
func ParseSourceSpec(spec string) (Source, error) {
	name, folder, found := strings.Cut(spec, "=")
	if !found {
		return Source{}, fmt.Errorf("source %q must be written id=folder or id:type=folder", spec)
	}

	id, sourceType, _ := strings.Cut(name, ":")

	return Source{ID: strings.TrimSpace(id), Type: strings.TrimSpace(sourceType), Folder: strings.TrimSpace(folder)}, nil
}

// detectType reads the type of a folder that was given none: a crawler output folder
// carries a metadata.json, and anything else is a plain folder of files.
func detectType(folder string) string {
	if info, err := os.Stat(filepath.Join(folder, MetadataFileName)); err == nil && !info.IsDir() {
		return TypeConfluence
	}

	return TypeFiles
}

// NormalizeSources checks every source, detects the type of those that have none, and
// returns the list with folders made absolute. An id may appear once.
func NormalizeSources(sources []Source) ([]Source, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one source is required")
	}

	normalized := make([]Source, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		source.ID = strings.TrimSpace(source.ID)
		source.Type = strings.TrimSpace(source.Type)
		source.Folder = strings.TrimSpace(source.Folder)

		switch {
		case source.ID == "":
			return nil, fmt.Errorf("a source has no id")
		case len(source.ID) > maxSourceIDLength:
			return nil, fmt.Errorf("source id %q is longer than %d characters", source.ID, maxSourceIDLength)
		case !sourceIDPattern.MatchString(source.ID):
			return nil, fmt.Errorf("source id %q may only use letters, digits, '.', '_' and '-', and must start with a letter or digit", source.ID)
		case seen[source.ID]:
			return nil, fmt.Errorf("source id %q is used more than once", source.ID)
		case source.Type != "" && source.Type != TypeConfluence && source.Type != TypeFiles:
			return nil, fmt.Errorf("source %q has unsupported type %q", source.ID, source.Type)
		case source.Folder == "":
			return nil, fmt.Errorf("source %q has no folder", source.ID)
		}
		seen[source.ID] = true

		absolute, err := filepath.Abs(source.Folder)
		if err != nil {
			return nil, fmt.Errorf("resolve folder of source %q: %w", source.ID, err)
		}
		source.Folder = absolute
		if source.Type == "" {
			source.Type = detectType(absolute)
		}

		normalized = append(normalized, source)
	}

	return normalized, nil
}
