package embedding

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// identityPart is one optional identity marker, such as an endpoint or prefix tag.
type identityPart struct {
	key   string
	value string
}

// identityString builds the canonical provider identity, for example
// "openai:text-embedding-3-small@1536" or
// "openai-compatible:BAAI/bge-m3@1024+endpoint:e8a1c3d2+prefixes:5e6f7a8b".
//
// The identity doubles as the fingerprint persisted with every stored vector, so
// it must change whenever the vector space changes and must stay stable for
// operational settings such as batch size, timeout or retry count.
func identityString(id string, model string, dimension int, parts ...identityPart) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s:%s@%d", id, model, dimension)
	for _, part := range parts {
		if strings.TrimSpace(part.value) == "" {
			continue
		}
		fmt.Fprintf(&builder, "+%s:%s", part.key, part.value)
	}
	return builder.String()
}

// shortTag returns a short, stable, non-reversible tag for the given values.
//
// Raw endpoint URLs are never persisted: gateway deployments sometimes carry
// credentials in query parameters, and internal hostnames are not useful in
// output contracts.
func shortTag(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("%x", sum[:4])
}
