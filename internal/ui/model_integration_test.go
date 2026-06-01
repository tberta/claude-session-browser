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
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
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

	// Focus folders (h moves left across panes), move up to "All".
	m = drive(m, key("h"))
	if m.focusedPane != PaneFolders {
		t.Fatalf("expected focus on folders after h")
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

func TestDetailsScrollAndFocus(t *testing.T) {
	sessions := []model.SessionInfo{
		{ID: "s1", FilePath: "/p/a/s1.jsonl", Project: "-p-a", LastActive: time.Now()},
	}
	m := NewApp("/p", "test", "", true) // forceAll → start on "All"
	m.width, m.height = 120, 30
	m = drive(m, sessionsLoadedMsg{sessions: sessions})

	// Inject a session with many message entries (no file I/O).
	entries := make([]model.Entry, 100)
	for i := range entries {
		entries[i] = model.Entry{Kind: model.KindUserText, Text: "line"}
	}
	m = drive(m, fullSessionLoadedMsg{session: &model.FullSession{
		ID: "s1", MessageCount: 100, Messages: entries,
	}})

	// Render once to build the transcript cache and set the viewport.
	_ = m.View()
	if len(m.transcriptLines) == 0 {
		t.Fatal("transcript cache not built")
	}

	// Enter focuses the Details pane.
	m = drive(m, key("enter"))
	if m.focusedPane != PaneDetails {
		t.Fatalf("expected PaneDetails after enter, got %v", m.focusedPane)
	}

	// Scroll down a few lines.
	for i := 0; i < 5; i++ {
		m = drive(m, key("j"))
	}
	_ = m.View()
	if m.detailsScroll == 0 {
		t.Fatal("expected detailsScroll > 0 after scrolling down")
	}

	// G jumps to bottom and clamps within bounds.
	m = drive(m, key("G"))
	_ = m.View()
	maxScroll := len(m.transcriptLines) - m.detailsViewport()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.detailsScroll != maxScroll {
		t.Fatalf("G: detailsScroll = %d, want clamped max %d", m.detailsScroll, maxScroll)
	}

	// g returns to top.
	m = drive(m, key("g"))
	if m.detailsScroll != 0 {
		t.Fatalf("g: detailsScroll = %d, want 0", m.detailsScroll)
	}

	// Enter no longer copies; status should be empty after the focus jump.
	m.statusMsg = ""
	m = drive(m, key("esc")) // back to Sessions
	if m.focusedPane != PaneSessions {
		t.Fatalf("expected PaneSessions after esc, got %v", m.focusedPane)
	}
	m = drive(m, key("enter")) // focuses Details, must not copy
	if m.statusMsg != "" {
		t.Fatalf("enter should not copy; statusMsg = %q", m.statusMsg)
	}

	// c copies the resume command (tolerate clipboard failure in headless CI).
	m = drive(m, key("c"))
	if m.statusMsg == "" {
		t.Fatal("c should set a status message (copied or copy-failed)")
	}
}

func TestDetailsScrollClampsWhenShort(t *testing.T) {
	m := NewApp("/p", "test", "", true)
	m.width, m.height = 120, 40
	m = drive(m, sessionsLoadedMsg{sessions: []model.SessionInfo{
		{ID: "s1", FilePath: "/p/a/s1.jsonl", Project: "-p-a", LastActive: time.Now()},
	}})
	// Few entries, tall pane → content shorter than viewport.
	m = drive(m, fullSessionLoadedMsg{session: &model.FullSession{
		ID: "s1", Messages: []model.Entry{{Kind: model.KindUserText, Text: "hi"}},
	}})
	m.focusedPane = PaneDetails
	m = drive(m, key("G"))
	_ = m.View()
	if m.detailsScroll != 0 {
		t.Fatalf("short content should clamp scroll to 0, got %d", m.detailsScroll)
	}
}

func TestSearchResultsTranscriptScroll(t *testing.T) {
	m := NewApp("/p", "test", "", true)
	m.width, m.height = 120, 30
	m = drive(m, sessionsLoadedMsg{sessions: []model.SessionInfo{
		{ID: "s1", FilePath: "/p/a/s1.jsonl", Project: "-p-a", LastActive: time.Now()},
	}})

	// Simulate being in search-results mode with a selected session transcript.
	entries := make([]model.Entry, 100)
	for i := range entries {
		entries[i] = model.Entry{Kind: model.KindAssistantText, Text: "line"}
	}
	m.fullSession = &model.FullSession{ID: "s1", Messages: entries}
	m.transcriptDirty = true
	m.searchQuery = "x"
	m.searchState = SearchStateResults
	m.focusedPane = PaneSessions
	_ = m.View() // build transcript + viewport

	// Enter focuses the transcript even while browsing results.
	m = drive(m, key("enter"))
	if m.focusedPane != PaneDetails {
		t.Fatalf("enter in results should focus Details, got %v", m.focusedPane)
	}

	// j scrolls the transcript (does not move to another result).
	for i := 0; i < 5; i++ {
		m = drive(m, key("j"))
	}
	_ = m.View()
	if m.detailsScroll == 0 {
		t.Fatal("expected transcript to scroll in results mode")
	}

	// First Esc leaves the transcript but keeps the search active.
	m = drive(m, key("esc"))
	if m.focusedPane != PaneSessions {
		t.Fatalf("esc should return to Sessions, got %v", m.focusedPane)
	}
	if m.searchState != SearchStateResults {
		t.Fatal("first esc should NOT clear the search")
	}

	// Second Esc clears the search.
	m = drive(m, key("esc"))
	if m.searchState != SearchStateNormal {
		t.Fatalf("second esc should clear search, state = %v", m.searchState)
	}
}

func TestTranscriptMatchNavigation(t *testing.T) {
	m := NewApp("/p", "test", "", true)
	m.width, m.height = 120, 30
	m = drive(m, sessionsLoadedMsg{sessions: []model.SessionInfo{
		{ID: "s1", FilePath: "/p/a/s1.jsonl", Project: "-p-a", LastActive: time.Now()},
	}})

	// A transcript where a few entries contain the query term "needle".
	entries := make([]model.Entry, 0, 20)
	for i := 0; i < 20; i++ {
		txt := "ordinary assistant line"
		if i == 4 || i == 11 || i == 17 {
			txt = "here is the needle you searched for"
		}
		entries = append(entries, model.Entry{Kind: model.KindAssistantText, Text: txt})
	}
	m.fullSession = &model.FullSession{ID: "s1", Messages: entries}
	m.searchQuery = "needle"
	m.searchState = SearchStateResults
	m.focusedPane = PaneSessions
	m.transcriptDirty = true
	_ = m.View() // builds transcript + matchLines

	if len(m.matchLines) < 3 {
		t.Fatalf("expected >=3 match lines, got %d", len(m.matchLines))
	}
	if m.currentMatch != -1 {
		t.Fatalf("currentMatch should start at -1, got %d", m.currentMatch)
	}

	// First 'n' selects a match and focuses the transcript.
	m = drive(m, key("n"))
	if m.focusedPane != PaneDetails {
		t.Fatalf("n should focus Details, got %v", m.focusedPane)
	}
	if m.currentMatch < 0 {
		t.Fatal("n should select a match")
	}
	first := m.currentMatch

	// Next 'n' advances to the following match.
	m = drive(m, key("n"))
	if m.currentMatch == first {
		t.Fatalf("second n should advance match (was %d)", first)
	}

	// 'N' goes back.
	m = drive(m, key("N"))
	if m.currentMatch != first {
		t.Fatalf("N should return to previous match %d, got %d", first, m.currentMatch)
	}

	// Wrap-around: from the first match, N wraps to the last.
	m.currentMatch = 0
	m = drive(m, key("N"))
	if m.currentMatch != len(m.matchLines)-1 {
		t.Fatalf("N from 0 should wrap to last, got %d", m.currentMatch)
	}
}

func TestHomeEndNavigation(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sessions := []model.SessionInfo{
		{ID: "s1", FilePath: "/p/a/s1.jsonl", Project: "-p-a", LastActive: t0},
		{ID: "s2", FilePath: "/p/a/s2.jsonl", Project: "-p-a", LastActive: t0.Add(time.Hour)},
		{ID: "s3", FilePath: "/p/a/s3.jsonl", Project: "-p-a", LastActive: t0.Add(2 * time.Hour)},
	}
	m := NewApp("/p", "test", "-p-a", false)
	m.width, m.height = 120, 40
	m = drive(m, sessionsLoadedMsg{sessions: sessions})

	// Sessions pane: End → last, Home → first.
	m.focusedPane = PaneSessions
	m = drive(m, key("end"))
	if m.selected != len(m.filteredSessions)-1 {
		t.Fatalf("End: selected = %d, want %d", m.selected, len(m.filteredSessions)-1)
	}
	m = drive(m, key("home"))
	if m.selected != 0 {
		t.Fatalf("Home: selected = %d, want 0", m.selected)
	}

	// g/G are terminal-independent aliases for Home/End in the list panes.
	m = drive(m, key("G"))
	if m.selected != len(m.filteredSessions)-1 {
		t.Fatalf("G: selected = %d, want %d", m.selected, len(m.filteredSessions)-1)
	}
	m = drive(m, key("g"))
	if m.selected != 0 {
		t.Fatalf("g: selected = %d, want 0", m.selected)
	}

	// Folders pane: End → last folder, Home → first ("All").
	m = drive(m, key("h")) // focus folders
	if m.focusedPane != PaneFolders {
		t.Fatalf("expected folders focus")
	}
	m = drive(m, key("end"))
	if m.folderSelected != len(m.folders)-1 {
		t.Fatalf("End: folderSelected = %d, want %d", m.folderSelected, len(m.folders)-1)
	}
	m = drive(m, key("home"))
	if m.folderSelected != 0 || m.selectedFolder != "" {
		t.Fatalf("Home: folderSelected=%d selectedFolder=%q, want 0/All", m.folderSelected, m.selectedFolder)
	}
}
