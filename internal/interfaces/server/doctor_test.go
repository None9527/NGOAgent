package server

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func TestKISummariesFromItems(t *testing.T) {
	got := kiSummariesFromItems([]apitype.KIInfo{
		{ID: "ki-1", Title: "First", Summary: "alpha"},
		{ID: "ki-2", Title: "Second", Summary: "beta"},
	})

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].ID != "ki-1" || got[0].Title != "First" || got[0].Summary != "alpha" {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if got[1].ID != "ki-2" || got[1].Title != "Second" || got[1].Summary != "beta" {
		t.Fatalf("got[1] = %#v", got[1])
	}
}
