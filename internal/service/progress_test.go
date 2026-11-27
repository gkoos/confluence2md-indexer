package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

func TestIndexReportsProgress(t *testing.T) {
	ctx := context.Background()
	sources := []indexer.Source{
		{ID: "first", Folder: writeSourceFolder(t, "banana text")},
		{ID: "second", Folder: writeSourceFolder(t, "cherry text")},
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")

	run := func() []ProgressEvent {
		var events []ProgressEvent
		_, err := Index(ctx, IndexRequest{
			Sources:  sources,
			DBPath:   dbPath,
			Progress: func(event ProgressEvent) { events = append(events, event) },
		})
		if err != nil {
			t.Fatalf("index: %v", err)
		}
		return events
	}

	events := run()
	if len(events) != 4 {
		t.Fatalf("got %d events, want start, two documents and finished: %+v", len(events), events)
	}
	if events[0].Event != "start" || events[0].Total != 2 {
		t.Fatalf("first event = %+v, want start with total 2", events[0])
	}
	for i, source := range []string{"first", "second"} {
		event := events[i+1]
		if event.Event != "document" || event.SourceID != source || event.DocumentID != source+"/1" ||
			event.Status != "inserted" || event.Done != i+1 || event.Total != 2 {
			t.Fatalf("document event %d = %+v", i, event)
		}
	}
	finished := events[3]
	if finished.Event != "finished" || finished.Summary == nil || finished.Summary.Inserted != 2 || finished.Summary.Deleted != 0 {
		t.Fatalf("finished event = %+v", finished)
	}

	// A second run over unchanged sources reports each document as skipped.
	again := run()
	if again[1].Status != "skipped" || again[3].Summary.Skipped != 2 || again[3].Summary.Inserted != 0 {
		t.Fatalf("second run = %+v", again)
	}
}
