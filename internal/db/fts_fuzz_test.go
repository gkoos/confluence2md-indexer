package db

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzBuildMatchExpression guards the one place where free text becomes an FTS5
// expression. The assertions stay on the expression text, so no database is needed:
// whatever bytes arrive, the builder has to return a well-formed OR of quoted literals.
func FuzzBuildMatchExpression(f *testing.F) {
	for _, seed := range []string{
		// The inputs the issue names.
		"apple OR", `"apple`, "c++", "a:b", "NEAR(",
		// Cases the table test pins, replayed as seeds.
		"apple", "apple banana", `"apple banana"`, "appl*", "release-train",
		"apple:banana", "   ", "!!! ???",
		// Quoting and operator edges.
		`"`, `""`, `"a OR b"`, `a"b`, `"a b`, `a b"`,
		"*", "**", "a**", `"a"*`, "NEAR(apple banana)", "OR", "NOT",
		// Structure: more tokens than the bound, phrases, non-ASCII, control bytes.
		strings.Repeat("term ", 40), strings.Repeat(`"a b" `, 40),
		"日本語 テスト", "a\x00b", "\u00a0",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, query string) {
		expression := BuildMatchExpression(query)

		if trimmed := strings.TrimSpace(expression); trimmed != expression {
			t.Fatalf("expression %q has surrounding whitespace", expression)
		}
		if strings.Count(expression, `"`)%2 != 0 {
			t.Fatalf("expression %q has an unbalanced quote", expression)
		}
		if index := strings.IndexFunc(expression, unicode.IsControl); index >= 0 {
			t.Fatalf("expression %q carries the control rune at %d", expression, index)
		}
		if expression == "" {
			return
		}

		terms := splitExpressionTerms(expression)
		if len(terms) > maxMatchTerms {
			t.Fatalf("expression has %d terms, want at most %d", len(terms), maxMatchTerms)
		}
		for _, term := range terms {
			if !isQuotedLiteral(term) {
				t.Fatalf("expression %q holds the malformed term %q", expression, term)
			}
		}
	})
}

// splitExpressionTerms splits on the separator the builder emits, ignoring an OR that
// sits inside a quoted phrase: the query `"a OR b"` is one phrase, not three terms.
func splitExpressionTerms(expression string) []string {
	terms := make([]string, 0, 8)
	var current strings.Builder
	inQuote := false

	for index := 0; index < len(expression); index++ {
		switch {
		case expression[index] == '"':
			inQuote = !inQuote
			current.WriteByte(expression[index])
		case !inQuote && strings.HasPrefix(expression[index:], " OR "):
			terms = append(terms, current.String())
			current.Reset()
			index += len(" OR ") - 1
		default:
			current.WriteByte(expression[index])
		}
	}

	return append(terms, current.String())
}

// isQuotedLiteral reports whether term is one quoted literal, optionally marked as a
// prefix term. Exactly that shape is what keeps FTS5 syntax out of the expression.
func isQuotedLiteral(term string) bool {
	term = strings.TrimSuffix(term, "*")
	if len(term) < 2 || !strings.HasPrefix(term, `"`) || !strings.HasSuffix(term, `"`) {
		return false
	}

	return !strings.Contains(term[1:len(term)-1], `"`)
}
