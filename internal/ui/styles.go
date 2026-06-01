package ui

import (
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// zeroMarginDarkStyle returns glamour's dark style with the document margin
// removed so rendered markdown fills the narrow Details pane.
func zeroMarginDarkStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	zero := uint(0)
	s.Document.Margin = &zero
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	return s
}

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED")
	secondaryColor = lipgloss.Color("#10B981")
	mutedColor     = lipgloss.Color("#6B7280")
	errorColor     = lipgloss.Color("#EF4444")
	bgColor        = lipgloss.Color("#1F2937")
	selectedBg     = lipgloss.Color("#374151")

	// Text styles
	titleStyle = lipgloss.NewStyle().
		Foreground(primaryColor).
		Bold(true)

	errorStyle = lipgloss.NewStyle().
		Foreground(errorColor)

	infoStyle = lipgloss.NewStyle().
		Foreground(secondaryColor)

	mutedTextStyle = lipgloss.NewStyle().
		Foreground(mutedColor)
	
	highlightStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FBBF24")).
		Bold(true)

	// matchLineStyle highlights the current search-match line in the transcript.
	matchLineStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#78350F")).
		Foreground(lipgloss.Color("#FBBF24")).
		Bold(true)

	// List styles
	sessionListStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(mutedColor).
		Padding(1).
		MarginTop(1).
		MarginRight(1)

	// Folders pane (left-most); same chrome as the session list
	foldersListStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(mutedColor).
		Padding(1).
		MarginTop(1).
		MarginRight(1)

	sessionItemStyle = lipgloss.NewStyle().
		PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
		Background(selectedBg).
		Foreground(primaryColor).
		PaddingLeft(2)

	// Details pane
	detailsStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(mutedColor).
		Padding(1).
		MarginTop(1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
		Background(bgColor).
		Padding(0, 1)

	keyHelpStyle = lipgloss.NewStyle().
		Foreground(mutedColor)
)