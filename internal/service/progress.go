package service

// ProgressEvent is one step of an index run, for a caller that wants to show progress.
// Done and Total count documents across every source.
type ProgressEvent struct {
	// Event is "start" once the sources are read, "document" after each document is
	// stored, and "finished" when the run is complete.
	Event      string `json:"event"`
	SourceID   string `json:"sourceId,omitempty"`
	DocumentID string `json:"documentId,omitempty"`
	// Status is what happened to a document: inserted, updated, metadata or skipped.
	Status  string           `json:"status,omitempty"`
	Done    int              `json:"done,omitempty"`
	Total   int              `json:"total,omitempty"`
	Summary *ProgressSummary `json:"summary,omitempty"`
}

// ProgressSummary is the outcome a "finished" event carries.
type ProgressSummary struct {
	Inserted        int `json:"inserted"`
	Updated         int `json:"updated"`
	MetadataUpdates int `json:"metadata"`
	Skipped         int `json:"skipped"`
	Deleted         int `json:"deleted"`
	ChunkWrites     int `json:"chunkWrites"`
	EmbeddingWrites int `json:"embeddingWrites"`
}
