package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/davidpaquet/claude-session-browser/internal/ui"
)

const version = "v0.2.0"

func main() {
	// Parse command line flags
	var claudeDir string
	flag.StringVar(&claudeDir, "claude-dir", "", "Claude projects directory (default: ~/.claude/projects)")
	flag.StringVar(&claudeDir, "d", "", "Claude projects directory (shorthand)")
	
	var help bool
	flag.BoolVar(&help, "help", false, "Show help")
	flag.BoolVar(&help, "h", false, "Show help (shorthand)")

	var forceAll bool
	flag.BoolVar(&forceAll, "list-all-projects", false, "Start on the \"All\" folder instead of the current directory's project")
	flag.BoolVar(&forceAll, "a", false, "Start on \"All\" (shorthand)")

	flag.Parse()
	
	// Show help if requested
	if help {
		showHelp()
		os.Exit(0)
	}
	
	// Set Claude directory
	if claudeDir == "" {
		claudeDir = os.Getenv("CLAUDE_DIR")
	}
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal("Failed to get home directory:", err)
		}
		claudeDir = filepath.Join(home, ".claude", "projects")
	}
	
	// Set CLAUDE_DIR environment variable for the app
	os.Setenv("CLAUDE_DIR", claudeDir)

	// Always scan every project under claudeDir; the folders pane lets the user
	// switch between them. Pre-select the project matching the current directory
	// so launching inside a known project shows just its sessions (unless -a
	// forces the "All" view).
	cwd, _ := os.Getwd()
	initialFolder := convertToClaudePath(cwd)

	app := ui.NewApp(claudeDir, version, initialFolder, forceAll)
	
	// Create the Bubble Tea program
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(), // Use alternate screen buffer
	)
	
	// Run the program
	if _, err := p.Run(); err != nil {
		log.Fatal("Error running program:", err)
	}
}

func convertToClaudePath(path string) string {
	// Convert filesystem path to Claude format. Claude Code encodes both path
	// separators and dots as dashes, e.g.
	//   "/home/tberta/.oh-my-zsh" -> "-home-tberta--oh-my-zsh"
	claudePath := strings.ReplaceAll(path, string(filepath.Separator), "-")
	claudePath = strings.ReplaceAll(claudePath, ".", "-")
	if !strings.HasPrefix(claudePath, "-") {
		claudePath = "-" + claudePath
	}
	return claudePath
}

func showHelp() {
	fmt.Println(`Claude Session Browser

A terminal user interface for browsing and resuming Claude Code sessions.

Usage:
  claude-session-browser [options]

The browser always scans every project under the Claude directory and shows a
Folders pane to switch between them. By default it pre-selects the project
matching the current working directory; use -a to start on "All" instead.

Options:
  -d, --claude-dir PATH      Claude projects directory (default: ~/.claude/projects)
  -a, --list-all-projects    Start on the "All" folder, ignoring the current directory
  -h, --help                 Show this help message

Environment Variables:
  CLAUDE_DIR              Alternative way to set Claude projects directory

Keyboard Shortcuts:
  ↑/↓, j/k               Navigate within the focused pane (scroll in Details)
  Tab / Shift+Tab        Cycle focus: Folders → Sessions → Details
  h / l                  Move focus left / right between panes
  Enter                  Open the transcript (focus Details to scroll messages)
  Esc                    Leave the transcript (focus Sessions)
  PgUp/PgDn, g/G         Page / jump top / bottom in the transcript
  t                      Toggle verbose transcript (thinking + full tool detail)
  c / y                  Copy resume command to clipboard
  s                      Cycle sort field (Last Active → Name → Project)
  S                      Toggle sort direction (ascending/descending)
  /                      Search session content
  r                      Refresh session list
  q                      Quit

Examples:
  # Run with default directory (pre-selects the current project's folder)
  claude-session-browser

  # Start on the All folder regardless of current directory
  claude-session-browser --list-all-projects

  # Specify custom Claude directory
  claude-session-browser --claude-dir ~/my-claude-projects

  # Use environment variable
  export CLAUDE_DIR=~/my-claude-projects
  claude-session-browser`)
}