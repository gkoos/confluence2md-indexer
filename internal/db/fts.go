package db

import (
	"strings"
	"unicode"
)

// maxMatchTerms bounds how much work one query expression can ask for.
const maxMatchTerms = 32

// BuildMatchExpression turns free text into an FTS5 MATCH expression.
//
// Every whitespace-separated token becomes one literal term: characters that FTS5
// would read as syntax (column filters, boolean operators, unbalanced quotes) are
// removed first, so ordinary punctuation can neither turn a search into a syntax
// error nor filter on an index column by accident.
//
// Two forms are preserved on purpose:
//
//	"two words"  matches the words as an adjacent phrase
//	term*        matches the token as a prefix
//
// Terms are combined with OR. A document that matches more terms still ranks higher,
// because BM25 sums the contribution of every matched term, while a document that
// matches only one of them is still found. (A space-separated query used to be an
// implicit phrase, which required the words to be adjacent and hid most matches.)
//
// An empty result means the query held no searchable term; callers report that as
// "no lexical matches" instead of handing an empty MATCH to SQLite.
func BuildMatchExpression(query string) string {
	terms := make([]string, 0, 8)
	for _, token := range splitQueryTokens(query) {
		term := buildMatchTerm(token)
		if term == "" {
			continue
		}
		terms = append(terms, term)
		if len(terms) == maxMatchTerms {
			break
		}
	}

	return strings.Join(terms, " OR ")
}

// splitQueryTokens splits on whitespace while keeping "quoted phrases" together. An
// unbalanced quote is treated as an ordinary character, so a stray quote degrades
// into a plain term instead of an error.
func splitQueryTokens(query string) []string {
	tokens := make([]string, 0, 8)
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	inQuote := false
	for _, r := range query {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()

	return tokens
}

// buildMatchTerm turns one token into a quoted FTS5 term, or into an empty string
// when nothing searchable remains.
func buildMatchTerm(token string) string {
	token = strings.TrimSpace(token)
	prefix := strings.HasSuffix(token, "*")
	token = strings.Trim(strings.TrimSuffix(token, "*"), "\"")
	token = strings.TrimSpace(token)

	words := splitMatchWords(token)
	if len(words) == 0 {
		return ""
	}

	// Every term is quoted. A bare word that FTS5 reserves (AND, OR, NOT, NEAR) or
	// that looks like a column filter would otherwise be read as syntax, which is
	// how "apple OR" used to end in a SQLite parse error.
	term := `"` + strings.Join(words, " ") + `"`
	if prefix {
		term += "*"
	}

	return term
}

// splitMatchWords keeps the characters the FTS5 tokenizer keeps and splits on
// everything else, so "release-train" becomes ["release","train"] and "c++" becomes
// ["c"].
func splitMatchWords(token string) []string {
	words := make([]string, 0, 4)
	var current strings.Builder

	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}
