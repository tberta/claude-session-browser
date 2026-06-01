package ui

import (
	"testing"
	"time"

	"github.com/davidpaquet/claude-session-browser/internal/model"
)

func at(id, project string, t time.Time) model.SessionInfo {
	return model.SessionInfo{ID: id, Project: project, LastActive: t}
}

func ids(s []model.SessionInfo) []string {
	out := make([]string, len(s))
	for i := range s {
		out[i] = s[i].ID
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSortLastActive(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := []model.SessionInfo{
		at("old", "", t0),
		at("new", "", t0.Add(2*time.Hour)),
		at("mid", "", t0.Add(time.Hour)),
	}
	sortSessions(s, SortLastActive, false) // desc: newest first
	eq(t, ids(s), []string{"new", "mid", "old"})

	sortSessions(s, SortLastActive, true) // asc: oldest first
	eq(t, ids(s), []string{"old", "mid", "new"})
}

func TestSortName(t *testing.T) {
	now := time.Now()
	s := []model.SessionInfo{at("c", "", now), at("a", "", now), at("b", "", now)}
	sortSessions(s, SortName, false)
	eq(t, ids(s), []string{"a", "b", "c"})

	sortSessions(s, SortName, true)
	eq(t, ids(s), []string{"c", "b", "a"})
}

func TestSortProjectWithRecencyTiebreak(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := []model.SessionInfo{
		at("alpha-old", "-alpha", t0),
		at("beta", "-beta", t0),
		at("alpha-new", "-alpha", t0.Add(time.Hour)),
	}
	// Desc: groups A-Z by project; within a project, newest first.
	sortSessions(s, SortProject, false)
	eq(t, ids(s), []string{"alpha-new", "alpha-old", "beta"})
}

func TestSortStable(t *testing.T) {
	now := time.Now()
	// Equal keys (same time, mode LastActive) must preserve input order.
	s := []model.SessionInfo{at("x", "", now), at("y", "", now), at("z", "", now)}
	sortSessions(s, SortLastActive, false)
	eq(t, ids(s), []string{"x", "y", "z"})
}
