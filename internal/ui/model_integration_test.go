package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/davidpaquet/claude-session-browser/internal/model"
)

// key builds a KeyMsg for a single rune or named key.
func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// drive feeds a message and returns the model (commands are intentionally
// discarded; we only assert state + that View() does not panic).
func drive(m *Model, msg tea.Msg) *Model {
	next, _ := m.Update(msg)
	return next.(*Model)
}

func TestModelLoadFolderSortSearchFlow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sessions := []model.SessionInfo{
		{ID: "s1", FilePath: "/p/a/s1.jsonl", Project: "-p-a", LastActive: t0},
		{ID: "s2", FilePath: "/p/b/s2.jsonl", Project: "-p-b", LastActive: t0.Add(time.Hour)},
		{ID: "s3", FilePath: "/p/a/s3.jsonl", Project: "-p-a", LastActive: t0.Add(2 * time.Hour)},
	}

	m := NewApp("/p", "test", "-p-a", false)
	m.width, m.height = 120, 40

	// Load sessions.
	m = drive(m, sessionsLoadedMsg{sessions: sessions})

	// Folders: All + 2 projects.
	if len(m.folders) != 3 {
		t.Fatalf("expected 3 folders, got %d: %+v", len(m.folders), m.folders)
	}
	// initialFolder "-p-a" should be pre-selected, filtering to s1, s3.
	if m.selectedFolder != "-p-a" {
		t.Fatalf("expected selectedFolder -p-a, got %q", m.selectedFolder)
	}
	if len(m.filteredSessions) != 2 {
		t.Fatalf("expected 2 sessions for -p-a, got %d", len(m.filteredSessions))
	}
	// Default sort is LastActive desc → s3 (newest) first.
	if m.filteredSessions[0].ID != "s3" {
		t.Fatalf("expected s3 first, got %s", m.filteredSessions[0].ID)
	}

	// View must render without panic.
	if out := m.View(); !strings.Contains(out, "Folders") || !strings.Contains(out, "Sessions") {
		t.Fatalf("View missing panes:\n%s", out)
	}

	// Focus folders, move down to "All".
	m = drive(m, key("tab"))
	if m.focusedPane != PaneFolders {
		t.Fatalf("expected focus on folders after tab")
	}
	m = drive(m, key("up")) // clamp at top (All is index 0)
	if m.selectedFolder != "" {
		t.Fatalf("expected All selected, got %q", m.selectedFolder)
	}
	if len(m.filteredSessions) != 3 {
		t.Fatalf("expected 3 sessions in All, got %d", len(m.filteredSessions))
	}

	// Cycle sort to Name, ascending order check.
	m = drive(m, key("s")) // LastActive -> Name
	if m.sortMode != SortName {
		t.Fatalf("expected SortName, got %v", m.sortMode)
	}
	if m.filteredSessions[0].ID != "s1" {
		t.Fatalf("expected s1 first by Name desc-default? got %s", m.filteredSessions[0].ID)
	}

	// Toggle direction.
	m = drive(m, key("S"))
	if !m.sortAsc {
		t.Fatalf("expected ascending after S")
	}

	// Render again at narrow width: folders pane hidden, no panic, focus forced.
	m.width = 50
	_ = m.View()
	if m.focusedPane != PaneSessions {
		t.Fatalf("expected focus forced to Sessions when folders hidden")
	}
}
