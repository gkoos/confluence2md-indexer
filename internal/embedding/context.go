package embedding

import (
	"fmt"
	"strings"
)

// Document context dimensions that may be prepended to the text that is embedded.
// The order below is canonical: it fixes both the header and the identity string, so
// the order a caller names them in cannot create a second vector space.
const (
	ContextTitle   = "title"
	ContextSpace   = "space"
	ContextSection = "section"
)

// AvailableContext lists the dimensions in canonical order.
var AvailableContext = []string{ContextTitle, ContextSpace, ContextSection}

// ParseContext turns a comma separated selection into its canonical form. An empty
// list means "no header", which is the default and keeps the vector space as it was.
// An unknown dimension is an error, so a typo cannot silently do nothing.
func ParseContext(list string) ([]string, error) {
	return NormalizeContext(strings.Split(list, ","))
}

// NormalizeContext validates a selection and returns it deduplicated and in canonical
// order. Hand-built options come through here too, so every caller that reaches a
// provider sees one selection for one behaviour.
func NormalizeContext(values []string) ([]string, error) {
	selected := make(map[string]bool, len(AvailableContext))
	for _, raw := range values {
		dimension := strings.ToLower(strings.TrimSpace(raw))
		if dimension == "" {
			continue
		}
		if !isContextDimension(dimension) {
			return nil, fmt.Errorf(
				"unknown embedding context %q (available: %s)",
				dimension, strings.Join(AvailableContext, ", "),
			)
		}
		selected[dimension] = true
	}

	selection := make([]string, 0, len(selected))
	for _, dimension := range AvailableContext {
		if selected[dimension] {
			selection = append(selection, dimension)
		}
	}

	return selection, nil
}

func isContextDimension(dimension string) bool {
	for _, available := range AvailableContext {
		if dimension == available {
			return true
		}
	}

	return false
}

// HasContext reports whether a selection includes one dimension.
func HasContext(selection []string, dimension string) bool {
	for _, selected := range selection {
		if selected == dimension {
			return true
		}
	}

	return false
}

// ContextFields holds the document metadata a context header can carry. Empty fields
// are skipped, so a page without a section still gets the lines it has.
type ContextFields struct {
	Title   string
	Space   string
	Section string
}

// DocumentText prepends the selected context lines to one chunk, so a short chunk keeps
// the page's subject when it is embedded. The stored chunk text is never changed: only
// the text handed to the provider goes through here, and query text never does.
//
// An empty selection, or a selection whose fields are all empty, returns text unchanged.
func DocumentText(selection []string, fields ContextFields, text string) string {
	if len(selection) == 0 {
		return text
	}

	lines := make([]string, 0, len(selection))
	for _, dimension := range selection {
		var value string
		switch dimension {
		case ContextTitle:
			value = fields.Title
		case ContextSpace:
			value = fields.Space
		case ContextSection:
			value = fields.Section
		}
		if value = strings.TrimSpace(value); value != "" {
			lines = append(lines, dimension+": "+value)
		}
	}
	if len(lines) == 0 {
		return text
	}

	return strings.Join(lines, "\n") + "\n\n" + text
}
