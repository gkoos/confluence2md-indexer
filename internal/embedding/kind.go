package embedding

// Kind distinguishes index-time text from query-time text.
//
// Asymmetric models (Cohere, Voyage, Jina, Gemini) produce vectors in different
// spaces for documents and queries, and prefix-style local models (E5, bge,
// nomic) expect different text prefixes. Providers that are symmetric ignore it.
type Kind int

const (
	// KindDocument is text that is written to the index.
	KindDocument Kind = iota
	// KindQuery is text that is searched with.
	KindQuery
)

func (k Kind) String() string {
	switch k {
	case KindQuery:
		return "query"
	case KindDocument:
		return "document"
	default:
		return "unknown"
	}
}
