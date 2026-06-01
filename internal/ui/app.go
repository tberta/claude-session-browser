package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/davidpaquet/claude-session-browser/internal/clipboard"
	"github.com/davidpaquet/claude-session-browser/internal/model"
	"github.com/davidpaquet/claude-session-browser/internal/parser"
	"github.com/davidpaquet/claude-session-browser/internal/search"
)

// SearchState represents the current search mode
type SearchState int

const (
	SearchStateNormal SearchState = iota  // No search active
	SearchStateInput                      // User is typing in search box
	SearchStateResults                    // User is navigating filtered results
)

// FocusedPane identifies which pane currently receives navigation keys.
type FocusedPane int

const (
	PaneFolders FocusedPane = iota
	PaneSessions
)

// SortMode is the field sessions are ordered by.
type SortMode int

const (
	SortLastActive SortMode = iota // by file modification time
	SortName                       // by session ID
	SortProject                    // by project, then recency
)

// sortModeCount is the number of sort modes, used for cycling.
const sortModeCount = 3

func (s SortMode) String() string {
	switch s {
	case SortName:
		return "Name"
	case SortProject:
		return "Project"
	default:
		return "Last Active"
	}
}

// folderEntry is one row in the folders pane. An entry with Project == "" is the
// synthetic "All" row that shows every session.
type folderEntry struct {
	Project string // encoded project directory name; "" for "All"
	Label   string // display label
	Count   int    // number of sessions in this folder
}

// Model is the app model
type Model struct {
	// Data
	sessions      []model.SessionInfo
	fullSession   *model.FullSession
	parser        *parser.Parser
	clipboardMgr  *clipboard.Manager
	claudeDir     string
	version       string

	// UI State
	width         int
	height        int
	selected      int
	scrollOffset  int
	loading       bool
	err           error

	// Folders pane
	focusedPane    FocusedPane
	folders        []folderEntry
	folderSelected int
	folderScroll   int
	selectedFolder string // "" == All; otherwise an encoded Project name
	initialFolder  string // encoded project to pre-select once sessions load
	forceAll       bool   // start on "All" regardless of initialFolder

	// Sort
	sortMode SortMode
	sortAsc  bool

	// Search State
	searchEngine     search.Engine
	searchState      SearchState
	searchInput      textinput.Model
	searchQuery      string
	searchResults    []search.SearchResult
	filteredSessions []model.SessionInfo

	// Status
	statusMsg     string
	statusTimer   time.Time
}

// NewApp creates a new app. initialFolder is the encoded project name to
// pre-select once sessions load (matching the current directory); when forceAll
// is true the app starts on the "All" folder regardless.
func NewApp(claudeDir, version, initialFolder string, forceAll bool) *Model {
	// Initialize search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Search sessions..."
	searchInput.CharLimit = 100
	searchInput.Width = 30

	return &Model{
		parser:        parser.NewParser(),
		clipboardMgr:  clipboard.NewManager(),
		claudeDir:     claudeDir,
		version:       version,
		initialFolder: initialFolder,
		forceAll:      forceAll,
		focusedPane:   PaneSessions,
		sortMode:      SortLastActive,
		sortAsc:       false,
		loading:       true,
		width:         80,
		height:        24,
		searchInput:   searchInput,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.loadSessions()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
		
	case sessionsLoadedMsg:
		m.loading = false
		m.sessions = msg.sessions
		m.err = msg.err

		// Build the folders pane. m.sessions keeps its loaded order so the search
		// engine's positional indices stay valid; all ordering happens on copies
		// inside applyFilterAndSort.
		m.folders = deriveFolders(m.sessions)

		// Initialize search engine with sessions
		if len(m.sessions) > 0 {
			m.searchEngine = search.NewEngine(m.sessions)
		}

		// Resolve the initial folder selection: pre-select the current directory's
		// project when it has sessions, else fall back to "All".
		m.resolveInitialFolder()

		m.applyFilterAndSort()

		// Select first and load it
		m.selected = 0
		m.scrollOffset = 0
		return m, m.loadSelectedCmd()
		
	case fullSessionLoadedMsg:
		m.fullSession = msg.session
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
			m.statusTimer = time.Now()
		}
		return m, nil
		
	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil
		
	case searchCompleteMsg:
		// Ignore if search query has changed
		if msg.query != m.searchQuery {
			return m, nil
		}
		
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Search error: %v", msg.err)
			m.statusTimer = time.Now()
			return m, nil
		}
		
		// Store search results and recompute the filtered list (search results
		// composed with the active folder filter and current sort).
		m.searchResults = msg.results
		m.selected = 0
		m.scrollOffset = 0
		m.applyFilterAndSort()

		// Update status
		if len(m.filteredSessions) == 0 {
			m.statusMsg = fmt.Sprintf("No matches found for '%s'", m.searchQuery)
		} else {
			m.statusMsg = fmt.Sprintf("Found %d sessions matching '%s'", len(m.filteredSessions), m.searchQuery)
		}
		m.statusTimer = time.Now()

		return m, m.loadSelectedCmd()
		
	case tea.KeyMsg:
		// Handle based on current search state
		switch m.searchState {
		case SearchStateInput:
			// In search input mode
			switch msg.String() {
			case "esc":
				// Cancel search entirely
				m.clearSearch()
				return m, nil
			case "tab", "enter":
				// Exit input mode, enter results mode
				if m.searchQuery != "" {
					m.searchState = SearchStateResults
					m.searchInput.Blur()
				}
				return m, nil
			default:
				// Update search input
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.searchQuery = m.searchInput.Value()
				
				// Trigger async search
				if m.searchQuery != "" {
					m.statusMsg = "Searching..."
					m.statusTimer = time.Now()
					return m, tea.Batch(cmd, m.performSearchCmd())
				} else {
					// Clear search immediately if query is empty
					m.searchResults = nil
					m.statusMsg = ""
					m.applyFilterAndSort()
				}
				return m, cmd
			}
			
		case SearchStateResults:
			// In search results mode - handle navigation
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc":
				// Clear search and return to normal
				m.clearSearch()
				return m, nil
			case "/":
				// Return to search input mode
				m.searchState = SearchStateInput
				m.searchInput.Focus()
				return m, textinput.Blink
			case "up", "k":
				return m, m.moveSession(-1)
			case "down", "j":
				return m, m.moveSession(+1)
			case "enter":
				if m.fullSession != nil {
					cmd := m.fullSession.GetResumeCommand()
					if err := m.clipboardMgr.Copy(cmd); err != nil {
						m.statusMsg = fmt.Sprintf("Copy failed: %v", err)
					} else {
						m.statusMsg = "Copied to clipboard!"
					}
					m.statusTimer = time.Now()
					return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
						return clearStatusMsg{}
					})
				}
			case "r":
				m.loading = true
				m.clearSearch()
				return m, m.loadSessions()
			}
			
		default:
			// Normal mode - no search active
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit

			case "/":
				m.enterSearchMode()
				return m, textinput.Blink

			// Focus movement between panes
			case "tab", "shift+tab":
				m.cycleFocus()
				return m, nil
			case "h":
				if m.shouldShowFolders() {
					m.focusedPane = PaneFolders
				}
				return m, nil
			case "l":
				m.focusedPane = PaneSessions
				return m, nil

			// Sorting
			case "s":
				m.sortMode = (m.sortMode + 1) % sortModeCount
				m.applyFilterAndSort()
				return m, m.loadSelectedCmd()
			case "S":
				m.sortAsc = !m.sortAsc
				m.applyFilterAndSort()
				return m, m.loadSelectedCmd()

			// Navigation, routed by focused pane
			case "up", "k":
				if m.focusedPane == PaneFolders && m.shouldShowFolders() {
					return m, m.moveFolder(-1)
				}
				return m, m.moveSession(-1)

			case "down", "j":
				if m.focusedPane == PaneFolders && m.shouldShowFolders() {
					return m, m.moveFolder(+1)
				}
				return m, m.moveSession(+1)

			case "enter":
				if m.fullSession != nil {
					cmd := m.fullSession.GetResumeCommand()
					if err := m.clipboardMgr.Copy(cmd); err != nil {
						m.statusMsg = fmt.Sprintf("Copy failed: %v", err)
					} else {
						m.statusMsg = "Copied to clipboard!"
					}
					m.statusTimer = time.Now()
					// Clear the message after 2 seconds
					return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
						return clearStatusMsg{}
					})
				}

			case "r":
				m.loading = true
				m.clearSearch()
				return m, m.loadSessions()
			}
		}
	}
	
	return m, nil
}

func (m *Model) View() string {
	if m.loading {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Loading sessions...")
	}
	
	if m.err != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit", m.err)))
	}
	
	// Calculate pane dimensions
	// Reserve space for status bar and search bar if active
	reservedHeight := 1 // status bar
	if m.searchState != SearchStateNormal {
		reservedHeight += 3 // search bar with border
	}
	availableHeight := m.height - reservedHeight

	// Build panes left-to-right: [Folders] | Sessions | Details.
	var panes []string
	remaining := m.width

	showFolders := m.shouldShowFolders()
	if !showFolders {
		// Folders pane hidden: keep focus on a visible pane.
		m.focusedPane = PaneSessions
	}
	if showFolders {
		const foldersWidth = 24
		panes = append(panes, m.renderFoldersPane(foldersWidth, availableHeight))
		remaining -= foldersWidth + 1 // account for MarginRight(1)
	}

	// Sessions pane width (mirrors the original fixed/adaptive sizing).
	sessionsWidth := 40
	if remaining < 80 {
		sessionsWidth = remaining / 2
	}
	rightWidth := remaining - sessionsWidth - 1

	panes = append(panes,
		m.renderSessionList(sessionsWidth, availableHeight),
		m.renderDetails(rightWidth, availableHeight),
	)

	// Join horizontally with no gap
	main := lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	
	// Add search bar if in search mode
	components := []string{main}
	if m.searchState != SearchStateNormal {
		searchBar := m.renderSearchBar()
		components = append(components, searchBar)
	}
	
	// Add status bar
	status := m.renderStatusBar()
	components = append(components, status)
	
	// Final layout
	return lipgloss.JoinVertical(
		lipgloss.Left,
		components...,
	)
}

func (m *Model) renderSessionList(width, height int) string {
	// Account for border, padding, and margins (1 border + 1 padding = 2 each side, +1 top margin)
	innerHeight := height - 5
	innerWidth := width - 4
	
	// Build content
	lines := []string{}
	arrow := "↓"
	if m.sortAsc {
		arrow = "↑"
	}
	sortLabel := fmt.Sprintf("%s %s", m.sortMode.String(), arrow)
	title := fmt.Sprintf("Sessions — %s", sortLabel)
	if m.searchState != SearchStateNormal {
		title = fmt.Sprintf("Sessions (%d) — %s", len(m.filteredSessions), sortLabel)
	}
	lines = append(lines, titleStyle.Render(title))
	lines = append(lines, "")
	
	// Calculate how many items we can show (minus title and blank line)
	itemsHeight := innerHeight - 2
	if itemsHeight < 1 {
		itemsHeight = 1
	}
	
	// Ensure scroll offset is valid
	maxScroll := len(m.filteredSessions) - itemsHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	
	// Render visible sessions
	visibleStart := m.scrollOffset
	visibleEnd := m.scrollOffset + itemsHeight
	if visibleEnd > len(m.filteredSessions) {
		visibleEnd = len(m.filteredSessions)
	}
	
	for i := visibleStart; i < visibleEnd; i++ {
		session := m.filteredSessions[i]
		
		// Format relative time
		timeStr := getRelativeTime(session.LastActive)
		
		// Truncate ID
		id := session.ID
		if len(id) > 24 {
			id = "..." + id[len(id)-21:]
		}
		
		// Add match indicator if searching
		matchIndicator := ""
		if m.searchQuery != "" {
			// Find match count for this session
			for _, result := range m.searchResults {
				if result.SessionID == session.ID {
					matchIndicator = fmt.Sprintf(" [%d]", len(result.Matches))
					break
				}
			}
		}
		
		// Format line to fit within inner width. In the "All" view prepend a
		// shortened project label so sessions from different projects are
		// distinguishable; when a specific folder is selected the label is
		// redundant and omitted.
		var line string
		if m.selectedFolder == "" && session.Project != "" {
			proj := shortenProject(session.Project)
			shortID := session.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			line = fmt.Sprintf("%-22s %-8s%s %s", proj, shortID, matchIndicator, timeStr)
		} else {
			line = fmt.Sprintf("%-24s%s %s", id, matchIndicator, timeStr)
		}
		if len(line) > innerWidth {
			line = line[:innerWidth]
		}
		
		// Apply selection style
		if i == m.selected {
			line = selectedItemStyle.Render(line)
		} else {
			line = sessionItemStyle.Render(line)
		}
		
		lines = append(lines, line)
	}
	
	// Pad to fill the inner height
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	
	// Join lines and apply container style. Highlight the border when focused.
	content := strings.Join(lines, "\n")
	style := sessionListStyle
	if m.focusedPane == PaneSessions {
		style = style.BorderForeground(primaryColor)
	}
	return style.
		Width(width).
		Height(height).
		Render(content)
}

// renderFoldersPane renders the left-most pane listing project folders (with
// session counts) plus the synthetic "All" entry.
func (m *Model) renderFoldersPane(width, height int) string {
	innerHeight := height - 5
	innerWidth := width - 4
	if innerHeight < 1 {
		innerHeight = 1
	}

	lines := []string{titleStyle.Render("Folders"), ""}

	itemsHeight := innerHeight - 2
	if itemsHeight < 1 {
		itemsHeight = 1
	}

	// Clamp scroll.
	maxScroll := len(m.folders) - itemsHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.folderScroll > maxScroll {
		m.folderScroll = maxScroll
	}
	if m.folderScroll < 0 {
		m.folderScroll = 0
	}

	visibleStart := m.folderScroll
	visibleEnd := m.folderScroll + itemsHeight
	if visibleEnd > len(m.folders) {
		visibleEnd = len(m.folders)
	}

	for i := visibleStart; i < visibleEnd; i++ {
		f := m.folders[i]
		count := fmt.Sprintf("%d", f.Count)
		labelWidth := innerWidth - len(count) - 1
		if labelWidth < 1 {
			labelWidth = 1
		}
		label := f.Label
		if len(label) > labelWidth {
			label = label[:labelWidth]
		}
		line := fmt.Sprintf("%-*s %s", labelWidth, label, count)
		if len(line) > innerWidth {
			line = line[:innerWidth]
		}
		if i == m.folderSelected {
			line = selectedItemStyle.Render(line)
		} else {
			line = sessionItemStyle.Render(line)
		}
		lines = append(lines, line)
	}

	for len(lines) < innerHeight {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	style := foldersListStyle
	if m.focusedPane == PaneFolders {
		style = style.BorderForeground(primaryColor)
	}
	return style.
		Width(width).
		Height(height).
		Render(content)
}

func (m *Model) renderDetails(width, height int) string {
	// Account for border, padding, and margins (1 border + 1 padding = 2 each side, +1 top margin)
	innerHeight := height - 5
	innerWidth := width - 4
	
	if innerHeight < 1 || innerWidth < 1 {
		return detailsStyle.Width(width).Height(height).Render("")
	}
	
	lines := []string{}
	
	if m.fullSession == nil {
		lines = append(lines, "Select a session...")
		// Pad to fill height
		for len(lines) < innerHeight {
			lines = append(lines, "")
		}
		content := strings.Join(lines, "\n")
		return detailsStyle.Width(width).Height(height).Render(content)
	}
	
	// Build content
	lines = append(lines, titleStyle.Render("Session Details"))
	lines = append(lines, "")
	
	// Basic info
	lines = append(lines, fmt.Sprintf("ID: %s", m.fullSession.ID))
	lines = append(lines, fmt.Sprintf("Messages: %d", m.fullSession.MessageCount))
	lines = append(lines, fmt.Sprintf("Cost: $%.4f", m.fullSession.TotalCostUSD))
	lines = append(lines, "")
	
	// Summary
	if m.fullSession.Summary != "" {
		lines = append(lines, "Summary:")
		wrapped := wrapText(m.fullSession.Summary, innerWidth-2)
		for _, line := range wrapped {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	}
	
	// Show search matches if searching
	if m.searchQuery != "" {
		// Find matches for current session
		var currentMatches []search.Match
		for _, result := range m.searchResults {
			if result.SessionID == m.fullSession.ID {
				currentMatches = result.Matches
				break
			}
		}
		
		if len(currentMatches) > 0 {
			lines = append(lines, fmt.Sprintf("Search Matches (%d):", len(currentMatches)))
			lines = append(lines, strings.Repeat("─", innerWidth-2))
			
			// Show up to 5 matches
			shown := 0
			for _, match := range currentMatches {
				if shown >= 5 {
					lines = append(lines, fmt.Sprintf("  ... and %d more matches", len(currentMatches)-shown))
					break
				}
				
				// Use context if available, otherwise fall back to text
				displayText := match.Context
				if displayText == "" {
					displayText = strings.TrimSpace(match.Text)
				}
				
				// Ensure it fits within width
				if len(displayText) > innerWidth-4 {
					displayText = displayText[:innerWidth-7] + "..."
				}
				
				lines = append(lines, fmt.Sprintf("  %s", displayText))
				shown++
			}
			lines = append(lines, "")
		}
	}
	
	// Resume command
	lines = append(lines, "Resume:")
	cmd := m.fullSession.GetResumeCommand()
	if len(cmd)+2 > innerWidth {
		cmd = cmd[:innerWidth-5] + "..."
	}
	lines = append(lines, infoStyle.Render("  "+cmd))
	lines = append(lines, "")
	
	// Check remaining space for JSON
	usedLines := len(lines)
	remainingLines := innerHeight - usedLines - 2 // -2 for JSON header
	
	if remainingLines > 3 { // Only show JSON if we have decent space
		lines = append(lines, "Last Raw Message (Complete):")
		lines = append(lines, "")
		
		if len(m.fullSession.LastRawMessages) > 0 {
			// Pretty print JSON with limited lines
			var prettyJSON bytes.Buffer
			rawMsg := m.fullSession.LastRawMessages[0]
			if err := json.Indent(&prettyJSON, []byte(rawMsg), "", "  "); err == nil {
				jsonLines := strings.Split(prettyJSON.String(), "\n")
				shown := 0
				for _, line := range jsonLines {
					if shown >= remainingLines-1 {
						lines = append(lines, mutedTextStyle.Render("  ... (more)"))
						break
					}
					if len(line) > innerWidth-2 {
						line = line[:innerWidth-5] + "..."
					}
					lines = append(lines, mutedTextStyle.Render("  "+line))
					shown++
				}
			}
		}
	}
	
	// Ensure we don't exceed inner height
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}
	
	// Pad to fill height
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	
	content := strings.Join(lines, "\n")
	return detailsStyle.Width(width).Height(height).Render(content)
}

func (m *Model) renderStatusBar() string {
	var leftText string

	// Show status message if present, otherwise show key hints
	statusDuration := 3 * time.Second
	// Show ripgrep warning for longer
	if strings.Contains(m.statusMsg, "ripgrep") {
		statusDuration = 10 * time.Second
	}
	if m.statusMsg != "" && time.Since(m.statusTimer) < statusDuration {
		leftText = m.statusMsg
	} else if m.searchState == SearchStateInput {
		leftText = "[Tab/Enter] Navigate results  [Esc] Cancel  Type to search..."
	} else if m.searchState == SearchStateResults {
		leftText = "[↑↓] Navigate  [/] Edit search  [Esc] Clear search  [Enter] Copy"
	} else {
		leftText = "[↑↓/jk] Nav  [Tab/h/l] Pane  [s] Sort  [S] Dir  [Enter] Copy  [/] Search  [r] Refresh  [q] Quit"
	}

	// Create left and right content sections
	leftStyle := keyHelpStyle.Width(m.width - lipgloss.Width(m.version) - 2)
	rightStyle := keyHelpStyle.Align(lipgloss.Right)

	leftContent := leftStyle.Render(leftText)
	rightContent := rightStyle.Render(m.version)

	// Join horizontally with bottom alignment
	content := lipgloss.JoinHorizontal(lipgloss.Bottom, leftContent, rightContent)

	return statusBarStyle.Width(m.width).Render(content)
}

func (m *Model) renderSearchBar() string {
	// Different styles for focused vs unfocused
	var borderColor lipgloss.Color
	var statusText string
	
	if m.searchState == SearchStateInput {
		// Focused - bright purple border
		borderColor = lipgloss.Color("#9B59B6")
		statusText = ""
	} else {
		// Unfocused - dimmed border
		borderColor = lipgloss.Color("#4B5563")
		if m.searchQuery != "" && len(m.filteredSessions) == 0 {
			statusText = " (no matches)"
		} else if len(m.filteredSessions) > 0 {
			statusText = fmt.Sprintf(" (%d matches)", len(m.filteredSessions))
		}
	}
	
	searchStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(m.width - 2)
	
	searchIcon := "🔍 "
	var prompt string
	
	if m.searchState == SearchStateInput {
		// Show cursor when focused
		prompt = searchIcon + "Search: " + m.searchInput.View()
	} else {
		// Show static text when unfocused
		prompt = searchIcon + "Search: " + m.searchQuery + statusText
		if m.searchState == SearchStateResults {
			prompt += " [Press / to edit]"
		}
	}
	
	return searchStyle.Render(prompt)
}

func (m *Model) ensureVisible() {
	// Calculate actual visible items (accounting for title and padding)
	innerHeight := m.height - 1 - 5 // -1 for status, -5 for borders/padding/margins
	itemsHeight := innerHeight - 2  // -2 for title and blank line
	
	if itemsHeight < 1 {
		itemsHeight = 1
	}
	
	// Adjust scroll to keep selection visible
	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
	} else if m.selected >= m.scrollOffset + itemsHeight {
		m.scrollOffset = m.selected - itemsHeight + 1
	}
	
	// Ensure scroll offset is valid
	maxScroll := len(m.filteredSessions) - itemsHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// applyFilterAndSort recomputes filteredSessions from the full session set,
// applying the active search, the selected folder filter, and the current sort.
// It is the single source of truth for what the Sessions pane shows; m.sessions
// is never reordered (the search engine indexes into it positionally).
func (m *Model) applyFilterAndSort() {
	var base []model.SessionInfo
	if m.searchQuery != "" && m.searchResults != nil {
		base = make([]model.SessionInfo, 0, len(m.searchResults))
		for _, r := range m.searchResults {
			if r.SessionIndex < len(m.sessions) {
				base = append(base, m.sessions[r.SessionIndex])
			}
		}
	} else {
		base = make([]model.SessionInfo, len(m.sessions))
		copy(base, m.sessions)
	}

	if m.selectedFolder != "" {
		filtered := base[:0]
		for _, s := range base {
			if s.Project == m.selectedFolder {
				filtered = append(filtered, s)
			}
		}
		base = filtered
	}

	sortSessions(base, m.sortMode, m.sortAsc)
	m.filteredSessions = base
	m.clampSessionCursor()
}

// clampSessionCursor pulls the session selection/scroll back into range after
// the filtered list changes size.
func (m *Model) clampSessionCursor() {
	if m.selected > len(m.filteredSessions)-1 {
		m.selected = len(m.filteredSessions) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// loadSelectedCmd returns a command to load the currently selected session, or
// clears the detail pane when the list is empty.
func (m *Model) loadSelectedCmd() tea.Cmd {
	if len(m.filteredSessions) == 0 {
		m.fullSession = nil
		return nil
	}
	return m.loadFullSession(m.filteredSessions[m.selected].FilePath)
}

// moveSession moves the Sessions-pane cursor by delta and loads the new selection.
func (m *Model) moveSession(delta int) tea.Cmd {
	n := len(m.filteredSessions)
	if n == 0 {
		return nil
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected > n-1 {
		m.selected = n-1
	}
	m.ensureVisible()
	return m.loadFullSession(m.filteredSessions[m.selected].FilePath)
}

// moveFolder moves the Folders-pane cursor by delta, switches the active folder
// filter, and reloads the (now filtered) session list.
func (m *Model) moveFolder(delta int) tea.Cmd {
	n := len(m.folders)
	if n == 0 {
		return nil
	}
	m.folderSelected += delta
	if m.folderSelected < 0 {
		m.folderSelected = 0
	}
	if m.folderSelected > n-1 {
		m.folderSelected = n-1
	}
	m.ensureFolderVisible()
	m.selectedFolder = m.folders[m.folderSelected].Project
	m.selected = 0
	m.scrollOffset = 0
	m.applyFilterAndSort()
	return m.loadSelectedCmd()
}

// cycleFocus toggles focus between the Folders and Sessions panes. It is a no-op
// when the Folders pane is hidden.
func (m *Model) cycleFocus() {
	if !m.shouldShowFolders() {
		m.focusedPane = PaneSessions
		return
	}
	if m.focusedPane == PaneFolders {
		m.focusedPane = PaneSessions
	} else {
		m.focusedPane = PaneFolders
	}
}

// shouldShowFolders reports whether the folders pane is rendered given the
// current terminal width and the available folder rows.
func (m *Model) shouldShowFolders() bool {
	return m.width >= 60 && len(m.folders) > 0
}

// ensureFolderVisible adjusts folderScroll to keep folderSelected on screen.
func (m *Model) ensureFolderVisible() {
	innerHeight := m.height - 1 - 5
	itemsHeight := innerHeight - 2
	if itemsHeight < 1 {
		itemsHeight = 1
	}
	if m.folderSelected < m.folderScroll {
		m.folderScroll = m.folderSelected
	} else if m.folderSelected >= m.folderScroll+itemsHeight {
		m.folderScroll = m.folderSelected - itemsHeight + 1
	}
	maxScroll := len(m.folders) - itemsHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.folderScroll > maxScroll {
		m.folderScroll = maxScroll
	}
	if m.folderScroll < 0 {
		m.folderScroll = 0
	}
}

// resolveInitialFolder sets the initial folder selection after sessions load.
// It selects the current directory's project when it has sessions (and forceAll
// is not set); otherwise it selects the synthetic "All" entry.
func (m *Model) resolveInitialFolder() {
	target := 0 // default to "All"
	if !m.forceAll && m.initialFolder != "" {
		for i, f := range m.folders {
			if f.Project == m.initialFolder {
				target = i
				break
			}
		}
	}
	m.folderSelected = target
	m.folderScroll = 0
	m.selectedFolder = m.folders[target].Project
}

func (m *Model) loadSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.parser.ListAllSessions(m.claudeDir)
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

func (m *Model) loadFullSession(filePath string) tea.Cmd {
	return func() tea.Msg {
		session, err := m.parser.ParseFullSession(filePath)
		return fullSessionLoadedMsg{session: session, err: err}
	}
}

// Messages
type sessionsLoadedMsg struct {
	sessions []model.SessionInfo
	err      error
}

type fullSessionLoadedMsg struct {
	session *model.FullSession
	err     error
}

type clearStatusMsg struct{}

type searchCompleteMsg struct {
	results []search.SearchResult
	query   string
	err     error
}

// Helper functions
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{}
	}
	
	lines := []string{}
	currentLine := ""
	
	for _, word := range words {
		if currentLine == "" {
			currentLine = word
		} else if len(currentLine+" "+word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	
	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	
	return lines
}

// shortenProject turns an encoded project directory name into a compact label
// for display. The encoding is lossy (both "/" and "." map to "-"), so this is
// a best-effort: drop the leading dash and keep the trailing portion, which is
// usually the most identifying part of the path.
func shortenProject(encoded string) string {
	proj := strings.TrimPrefix(encoded, "-")
	const maxLen = 22
	if len(proj) > maxLen {
		proj = "…" + proj[len(proj)-(maxLen-1):]
	}
	return proj
}

// deriveFolders groups sessions by their Project and returns the folders-pane
// rows. The first row is always the synthetic "All" entry. Distinct non-empty
// projects follow, sorted A-Z by their display label. Single-project data (every
// Project == "") yields just the "All" row.
func deriveFolders(sessions []model.SessionInfo) []folderEntry {
	counts := map[string]int{}
	var order []string
	for _, s := range sessions {
		if s.Project == "" {
			continue
		}
		if _, ok := counts[s.Project]; !ok {
			order = append(order, s.Project)
		}
		counts[s.Project]++
	}

	sort.Slice(order, func(i, j int) bool {
		return shortenProject(order[i]) < shortenProject(order[j])
	})

	folders := make([]folderEntry, 0, len(order)+1)
	folders = append(folders, folderEntry{Project: "", Label: "All", Count: len(sessions)})
	for _, proj := range order {
		folders = append(folders, folderEntry{
			Project: proj,
			Label:   shortenProject(proj),
			Count:   counts[proj],
		})
	}
	return folders
}

// sortSessions orders s in place by the given mode and direction. asc==false
// (the default) sorts most-relevant-first: newest, or A-Z reversed.
func sortSessions(s []model.SessionInfo, mode SortMode, asc bool) {
	less := func(i, j int) bool {
		switch mode {
		case SortName:
			return s[i].ID < s[j].ID
		case SortProject:
			if s[i].Project != s[j].Project {
				return s[i].Project < s[j].Project
			}
			return s[i].LastActive.After(s[j].LastActive)
		default: // SortLastActive
			return s[i].LastActive.After(s[j].LastActive)
		}
	}
	sort.SliceStable(s, func(i, j int) bool {
		if asc {
			return less(j, i)
		}
		return less(i, j)
	})
}

func getRelativeTime(t time.Time) string {
	diff := time.Since(t)
	
	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		minutes := int(diff.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else if diff < 30*24*time.Hour {
		weeks := int(diff.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	} else if diff < 365*24*time.Hour {
		months := int(diff.Hours() / (24 * 30))
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	} else {
		years := int(diff.Hours() / (24 * 365))
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}

// Search helper methods
func (m *Model) enterSearchMode() {
	// Check if ripgrep is available
	if !m.checkRipgrep() {
		m.statusMsg = "Warning: ripgrep (rg) not found. Install it for search to work."
		m.statusTimer = time.Now()
		// Still enter search mode but user is warned
	}
	
	m.searchState = SearchStateInput
	m.searchInput.Focus()
	m.searchInput.SetValue(m.searchQuery) // Keep existing query if any
}

func (m *Model) checkRipgrep() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

func (m *Model) clearSearch() {
	m.searchState = SearchStateNormal
	m.searchInput.Blur()
	m.searchInput.SetValue("")
	m.searchQuery = ""
	m.searchResults = nil
	// Reset to the current folder's sessions (folder filter + sort survive).
	m.selected = 0
	m.scrollOffset = 0
	m.applyFilterAndSort()
}

func (m *Model) performSearchCmd() tea.Cmd {
	return func() tea.Msg {
		if m.searchEngine == nil || m.searchQuery == "" {
			return searchCompleteMsg{
				results: []search.SearchResult{},
				query:   m.searchQuery,
				err:     nil,
			}
		}
		
		// Perform FULL TEXT SEARCH across all session content
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		results, err := m.searchEngine.Search(ctx, m.searchQuery, search.SearchTypeContent)
		
		return searchCompleteMsg{
			results: results,
			query:   m.searchQuery,
			err:     err,
		}
	}
}