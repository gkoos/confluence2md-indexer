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
