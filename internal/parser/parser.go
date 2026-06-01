package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davidpaquet/claude-session-browser/internal/model"
)

// Parser handles parsing
type Parser struct{}

// NewParser creates a new parser
func NewParser() *Parser {
	return &Parser{}
}

// ListSessions returns basic session info without parsing content
func (p *Parser) ListSessions(claudeDir string) ([]model.SessionInfo, error) {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, err
	}

	var sessions []model.SessionInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		sessions = append(sessions, model.SessionInfo{
			ID:         model.GetSessionID(entry.Name()),
			FilePath:   filepath.Join(claudeDir, entry.Name()),
			LastActive: info.ModTime(), // Use file modification time
		})
	}

	return sessions, nil
}

// ListAllSessions walks every project sub-directory under rootDir and returns
// the combined session list. Each session records the encoded project directory
// it belongs to so the UI can distinguish sessions across projects.
func (p *Parser) ListAllSessions(rootDir string) ([]model.SessionInfo, error) {
	projects, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	var sessions []model.SessionInfo
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}

		projectDir := filepath.Join(rootDir, project.Name())
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			sessions = append(sessions, model.SessionInfo{
				ID:         model.GetSessionID(entry.Name()),
				FilePath:   filepath.Join(projectDir, entry.Name()),
				LastActive: info.ModTime(),
				Project:    project.Name(),
			})
		}
	}

	return sessions, nil
}

// ParseFullSession parses a single session with all details
func (p *Parser) ParseFullSession(filePath string) (*model.FullSession, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	session := &model.FullSession{
		ID:       model.GetSessionID(filePath),
		FilePath: filePath,
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var allLines []string
	var lastUserMessages []string
	messageCount := 0
	totalCost := 0.0

	// Read all lines
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		allLines = append(allLines, line)

		// Try to parse for basic info
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(line), &data); err == nil {
			// Count messages
			if msgType, ok := data["type"].(string); ok {
				if msgType == "user" || msgType == "assistant" {
					messageCount++
				}

				// Extract summary from dedicated summary line (preferred)
				if msgType == "summary" {
					if summary, ok := data["summary"].(string); ok && summary != "" {
						session.Summary = summary
					}
				}

				// Collect user messages for fallback summary
				if msgType == "user" {
					if msg, ok := data["message"].(map[string]interface{}); ok {
						if content, ok := msg["content"].(string); ok {
							content = strings.TrimSpace(content)
							if !strings.Contains(content, "system-reminder") {
								lastUserMessages = append(lastUserMessages, content)
							}
						}
					}
				}
			}

			// Get timestamp
			if ts, ok := data["timestamp"].(string); ok {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					session.LastActive = t
				}
			}

			// Get cost
			if cost, ok := data["costUSD"].(float64); ok {
				totalCost += cost
			}
		}

		// Flatten user/assistant content blocks into transcript entries.
		flattenLine(line, &session.Messages)
	}

	// Fallback: Set summary from last 3 user messages if no summary line was found
	if session.Summary == "" && len(lastUserMessages) > 0 {
		start := len(lastUserMessages) - 3
		if start < 0 {
			start = 0
		}
		summaryParts := []string{}
		for i := start; i < len(lastUserMessages); i++ {
			msg := lastUserMessages[i]
			if len(msg) > 150 {
				msg = msg[:147] + "..."
			}
			summaryParts = append(summaryParts, msg)
		}
		session.Summary = strings.Join(summaryParts, " | ")
	}

	// Get just the LAST raw message - complete and untruncated
	if len(allLines) > 0 {
		session.LastRawMessages = []string{allLines[len(allLines)-1]}
	}

	session.MessageCount = messageCount
	session.TotalCostUSD = totalCost

	return session, nil
}

// --- Transcript flattening -------------------------------------------------

// rawLine is a parse-only view of a JSONL line. message.content is kept raw so
// we can handle both string and array shapes without failing the whole line.
type rawLine struct {
	Type        string      `json:"type"`
	Message     *rawMessage `json:"message"`
	Timestamp   string      `json:"timestamp"`
	IsSidechain bool        `json:"isSidechain"`
	IsMeta      bool        `json:"isMeta"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// flattenLine decodes one JSONL line and appends its content blocks as Entry
// values. It is resilient: any decode failure or unknown shape yields no
// entries rather than an error, so a single bad line never breaks the parse.
func flattenLine(line string, out *[]model.Entry) {
	var rl rawLine
	if err := json.Unmarshal([]byte(line), &rl); err != nil {
		return
	}
	if rl.IsMeta || rl.Message == nil {
		return
	}

	ts, _ := time.Parse(time.RFC3339, rl.Timestamp)

	switch rl.Type {
	case "user":
		flattenUser(rl, ts, out)
	case "assistant":
		flattenAssistant(rl, ts, out)
	}
}

func flattenUser(rl rawLine, ts time.Time, out *[]model.Entry) {
	// content may be a plain string (older format) ...
	var s string
	if json.Unmarshal(rl.Message.Content, &s) == nil {
		if text := strings.TrimSpace(s); text != "" {
			*out = append(*out, model.Entry{
				Kind:        model.KindUserText,
				Text:        text,
				Timestamp:   ts,
				IsSidechain: rl.IsSidechain,
			})
		}
		return
	}

	// ... or an array of content blocks.
	var blocks []rawBlock
	if json.Unmarshal(rl.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if text := strings.TrimSpace(b.Text); text != "" {
				*out = append(*out, model.Entry{
					Kind:        model.KindUserText,
					Text:        text,
					Timestamp:   ts,
					IsSidechain: rl.IsSidechain,
				})
			}
		case "tool_result":
			*out = append(*out, model.Entry{
				Kind:        model.KindToolResult,
				Text:        toolResultText(b.Content),
				Timestamp:   ts,
				IsSidechain: rl.IsSidechain,
				IsError:     b.IsError,
			})
		}
	}
}

func flattenAssistant(rl rawLine, ts time.Time, out *[]model.Entry) {
	var blocks []rawBlock
	if json.Unmarshal(rl.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if text := strings.TrimSpace(b.Text); text != "" {
				*out = append(*out, model.Entry{
					Kind:        model.KindAssistantText,
					Text:        text,
					Timestamp:   ts,
					IsSidechain: rl.IsSidechain,
				})
			}
		case "thinking":
			if text := strings.TrimSpace(b.Thinking); text != "" {
				*out = append(*out, model.Entry{
					Kind:        model.KindThinking,
					Text:        text,
					Timestamp:   ts,
					IsSidechain: rl.IsSidechain,
				})
			}
		case "tool_use":
			*out = append(*out, model.Entry{
				Kind:        model.KindToolUse,
				ToolName:    b.Name,
				Text:        toolInputMarkdown(b.Name, b.Input),
				Timestamp:   ts,
				IsSidechain: rl.IsSidechain,
			})
		}
	}
}

// toolInputMarkdown renders a tool_use input as a markdown code block. Bash
// commands get a bash-highlighted block (plus an optional description line);
// everything else is shown as pretty-printed JSON.
func toolInputMarkdown(name string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	if name == "Bash" {
		var bash struct {
			Command     string `json:"command"`
			Description string `json:"description"`
		}
		if json.Unmarshal(input, &bash) == nil && bash.Command != "" {
			out := "```bash\n" + bash.Command + "\n```"
			if d := strings.TrimSpace(bash.Description); d != "" {
				out = "_" + d + "_\n\n" + out
			}
			return out
		}
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, input, "", "  "); err == nil {
		return "```json\n" + pretty.String() + "\n```"
	}
	return "```\n" + string(input) + "\n```"
}

// toolResultText extracts a tool_result's content, which may be a plain string
// or an array of {type:"text", text:...} blocks.
func toolResultText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []rawBlock
	if json.Unmarshal(content, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}