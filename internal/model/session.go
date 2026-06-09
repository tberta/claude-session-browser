package model

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionInfo represents minimal session info for listing
type SessionInfo struct {
	ID         string
	FilePath   string
	LastActive time.Time
	Project    string // Encoded project directory name (empty for single-project listings)
}

// GetSessionID extracts the session ID from a filename
func GetSessionID(filename string) string {
	base := filepath.Base(filename)
	return strings.TrimSuffix(base, ".jsonl")
}

// EntryKind classifies a single rendered transcript block.
type EntryKind string

const (
	KindUserText      EntryKind = "user-text"
	KindAssistantText EntryKind = "assistant-text"
	KindThinking      EntryKind = "thinking"
	KindToolUse       EntryKind = "tool-use"
	KindToolResult    EntryKind = "tool-result"
)

// Entry is one content block flattened from a user/assistant message line. A
// single source JSONL line can produce several entries (one per content block).
type Entry struct {
	Kind        EntryKind
	Text        string    // markdown body: message/thinking text, tool input, or tool result
	ToolName    string    // tool name for KindToolUse; "" otherwise
	Timestamp   time.Time // line timestamp (zero if absent)
	IsSidechain bool      // line-level isSidechain (subagent output)
	IsError     bool      // tool_result is_error
}

// FullSession represents a fully parsed session
type FullSession struct {
	ID              string
	FilePath        string
	Summary         string
	LastActive      time.Time
	MessageCount    int
	TotalCostUSD    float64
	Cwd             string // working directory the session ran in (from the JSONL "cwd" field)
	LastRawMessages []string
	Messages        []Entry // ordered, flattened transcript for browsing
}

// GetResumeCommand returns the command to resume this session. When the session
// recorded a working directory, it is prefixed with a `cd` so the resumed
// session starts in the folder where the session originally took place.
func (s *FullSession) GetResumeCommand() string {
	resume := "claude --resume " + s.ID
	if s.Cwd == "" {
		return resume
	}
	return "cd " + formatCdPath(s.Cwd) + " && " + resume
}

// formatCdPath renders dir for use as a `cd` argument, collapsing the user's
// home directory to ~ for readability and quoting only when necessary. A
// leading ~/ is kept unquoted so the shell still performs tilde expansion.
func formatCdPath(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if dir == home {
			return "~"
		}
		if rest := strings.TrimPrefix(dir, home+"/"); rest != dir {
			return "~/" + shellQuote(rest)
		}
	}
	return shellQuote(dir)
}

// shellQuote returns s wrapped in single quotes when it contains characters that
// the shell would otherwise interpret, so paths with spaces or other special
// characters remain a single argument to `cd`.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`*?;&|<>()[]{}#~") {
		return s
	}
	// Wrap in single quotes, escaping any embedded single quotes.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}