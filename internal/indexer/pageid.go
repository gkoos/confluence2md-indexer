package indexer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// pageID is a Confluence page identifier as it appears in metadata.json.
//
// The crawler writes identifiers as JSON numbers ("confluence_parent_id": 12345)
// while older exports and hand-written fixtures may carry them as JSON strings.
// Both shapes decode to the same decimal text, so a representation change in one
// field cannot make an otherwise valid export unindexable — the same tolerance the
// YAML front matter path already has.
type pageID string

// UnmarshalJSON accepts a JSON number, a numeric string, or null.
func (id *pageID) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)

	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*id = ""
		return nil
	}

	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return fmt.Errorf("page id: %w", err)
		}
		*id = pageID(strings.TrimSpace(text))
		return nil
	}

	number, err := strconv.ParseInt(string(trimmed), 10, 64)
	if err != nil {
		return fmt.Errorf("page id: expected a number or string, got %s", trimmed)
	}

	*id = pageID(strconv.FormatInt(number, 10))
	return nil
}

// String returns the identifier as the text the index stores.
func (id pageID) String() string {
	return string(id)
}
