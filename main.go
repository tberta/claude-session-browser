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

	var listAll bool
	flag.BoolVar(&listAll, "list-all-projects", false, "List sessions from every project under the Claude directory")
	flag.BoolVar(&listAll, "a", false, "List sessions from every project (shorthand)")

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

	// In single-project mode, narrow claudeDir down to the project matching the
	// current working directory. In all-projects mode we keep the root directory
	// and let the parser walk every project sub-directory.
	if !listAll {
		// Get current working directory and convert to Claude path format
		cwd, _ := os.Getwd()
		claudePath := convertToClaudePath(cwd)

		// Check if this project exists in the Claude directory
		projectPath := filepath.Join(claudeDir, claudePath)
		if _, err := os.Stat(projectPath); err == nil && hasJSONLFiles(projectPath) {
			// Found matching project for current directory
			claudeDir = projectPath
		} else {
			// No match for current directory, just use first project with JSONL files
			entries, err := os.ReadDir(claudeDir)
			if err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						testPath := filepath.Join(claudeDir, entry.Name())
						if hasJSONLFiles(testPath) {
							claudeDir = testPath
							break
						}
					}
				}
			}
		}
	}

	app := ui.NewApp(claudeDir, version, listAll)
	
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

func hasJSONLFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jsonl" {
			return true
		}
	}
	return false
}

func showHelp() {
	fmt.Println(`Claude Session Browser

A terminal user interface for browsing and resuming Claude Code sessions.

Usage:
  claude-session-browser [options]

Options:
  -d, --claude-dir PATH      Claude projects directory (default: ~/.claude/projects)
  -a, --list-all-projects    List sessions from every project, not just the current one
  -h, --help                 Show this help message

Environment Variables:
  CLAUDE_DIR              Alternative way to set Claude projects directory

Keyboard Shortcuts:
  ↑/↓, j/k               Navigate sessions
  Enter                  Copy resume command to clipboard
  r                      Refresh session list
  q                      Quit

Examples:
  # Run with default directory
  claude-session-browser

  # Specify custom Claude directory
  claude-session-browser --claude-dir ~/my-claude-projects

  # Browse sessions across all projects
  claude-session-browser --list-all-projects

  # Use environment variable
  export CLAUDE_DIR=~/my-claude-projects
  claude-session-browser`)
}