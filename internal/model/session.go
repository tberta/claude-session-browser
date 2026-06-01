package model

import (
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
	LastRawMessages []string
	Messages        []Entry // ordered, flattened transcript for browsing
}

// GetResumeCommand returns the command to resume this session
func (s *FullSession) GetResumeCommand() string {
	return "claude --resume " + s.ID
}