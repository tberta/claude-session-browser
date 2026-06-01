package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidpaquet/claude-session-browser/internal/model"
)

// writeSession writes the given JSONL lines to a temp file and returns its path.
func writeSession(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFullSessionFlattensMessages(t *testing.T) {
	lines := []string{
		// user with plain string content
		`{"type":"user","timestamp":"2026-05-12T09:44:39.998Z","message":{"role":"user","content":"hello there"}}`,
		// assistant with thinking + text + tool_use(Bash)
		`{"type":"assistant","timestamp":"2026-05-12T09:44:44.326Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"let me think"},{"type":"text","text":"On it."},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la","description":"list files"}}]}}`,
		// user carrying a tool_result (array content, error)
		`{"type":"user","timestamp":"2026-05-12T09:44:45.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":[{"type":"text","text":"permission denied"}]}]}}`,
		// noise lines: must NOT produce entries
		`{"type":"summary","summary":"a summary"}`,
		`{"type":"file-history-snapshot","messageId":"x"}`,
		`{"type":"permission-mode","permissionMode":"auto"}`,
		`{"type":"system","subtype":"turn_duration","isMeta":false}`,
		// garbage line: must not break the parse
		`{bad json`,
	}
	path := writeSession(t, lines)

	p := NewParser()
	s, err := p.ParseFullSession(path)
	if err != nil {
		t.Fatalf("ParseFullSession errored: %v", err)
	}

	wantKinds := []model.EntryKind{
		model.KindUserText,
		model.KindThinking,
		model.KindAssistantText,
		model.KindToolUse,
		model.KindToolResult,
	}
	if len(s.Messages) != len(wantKinds) {
		t.Fatalf("got %d entries, want %d: %+v", len(s.Messages), len(wantKinds), s.Messages)
	}
	for i, want := range wantKinds {
		if s.Messages[i].Kind != want {
			t.Errorf("entry %d kind = %q, want %q", i, s.Messages[i].Kind, want)
		}
	}

	// Bash tool_use: name + command present in body.
	tool := s.Messages[3]
	if tool.ToolName != "Bash" {
		t.Errorf("tool name = %q, want Bash", tool.ToolName)
	}
	if !strings.Contains(tool.Text, "ls -la") {
		t.Errorf("tool body missing command: %q", tool.Text)
	}

	// tool_result: error flag + extracted text.
	res := s.Messages[4]
	if !res.IsError {
		t.Error("tool_result IsError = false, want true")
	}
	if !strings.Contains(res.Text, "permission denied") {
		t.Errorf("tool_result text = %q", res.Text)
	}

	// MessageCount counts user/assistant lines (3 here), unaffected by flattening.
	if s.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", s.MessageCount)
	}
}

func TestParseFullSessionEmptyAndNoise(t *testing.T) {
	// Only noise/meta → no transcript entries, no error.
	lines := []string{
		`{"type":"summary","summary":"x"}`,
		`{"type":"queue-operation","operation":"enqueue"}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"meta"}}`,
	}
	path := writeSession(t, lines)
	s, err := p().ParseFullSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Messages) != 0 {
		t.Fatalf("expected 0 entries, got %d: %+v", len(s.Messages), s.Messages)
	}
}

func p() *Parser { return NewParser() }
