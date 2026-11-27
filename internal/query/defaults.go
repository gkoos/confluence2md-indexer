package query

// Built-in retrieval defaults shared by the command line and the public API, so a
// default changes in one place.
const (
	DefaultMode       = "hybrid"
	DefaultFusion     = "weighted"
	DefaultAlpha      = 0.70
	DefaultRRFK       = 60
	DefaultTopK       = 10
	DefaultCandidateK = 50
)
