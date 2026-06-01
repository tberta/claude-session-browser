package ui

import (
	"testing"
	"time"

	"github.com/davidpaquet/claude-session-browser/internal/model"
)

func sess(id, project string) model.SessionInfo {
	return model.SessionInfo{ID: id, Project: project, LastActive: time.Now()}
}

func TestDeriveFoldersEmpty(t *testing.T) {
	folders := deriveFolders(nil)
	if len(folders) != 1 {
		t.Fatalf("expected 1 folder (All), got %d", len(folders))
	}
	if folders[0].Project != "" || folders[0].Label != "All" || folders[0].Count != 0 {
		t.Fatalf("unexpected All entry: %+v", folders[0])
	}
}

func TestDeriveFoldersSingleProject(t *testing.T) {
	// All sessions have Project == "" (single-project mode): only "All".
	sessions := []model.SessionInfo{sess("a", ""), sess("b", ""), sess("c", "")}
	folders := deriveFolders(sessions)
	if len(folders) != 1 {
		t.Fatalf("expected only All, got %d entries: %+v", len(folders), folders)
	}
	if folders[0].Count != 3 {
		t.Fatalf("expected All count 3, got %d", folders[0].Count)
	}
}

func TestDeriveFoldersMultiProject(t *testing.T) {
	sessions := []model.SessionInfo{
		sess("a", "-home-zeta"),
		sess("b", "-home-alpha"),
		sess("c", "-home-alpha"),
		sess("d", "-home-mid"),
	}
	folders := deriveFolders(sessions)

	// All + 3 distinct projects.
	if len(folders) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(folders), folders)
	}
	if folders[0].Label != "All" || folders[0].Count != 4 {
		t.Fatalf("All entry wrong: %+v", folders[0])
	}
	// Remaining sorted A-Z by label: alpha, mid, zeta.
	wantLabels := []string{"home-alpha", "home-mid", "home-zeta"}
	wantCounts := []int{2, 1, 1}
	for i, w := range wantLabels {
		f := folders[i+1]
		if f.Label != w {
			t.Errorf("folder %d label = %q, want %q", i+1, f.Label, w)
		}
		if f.Count != wantCounts[i] {
			t.Errorf("folder %d (%s) count = %d, want %d", i+1, w, f.Count, wantCounts[i])
		}
	}
}
