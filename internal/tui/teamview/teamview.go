package teamview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── TeamViewModel — Bubbletea sub-model for swarm team panel ─────────────────
//
// Displays team status, teammate list with color indicators, and
// active/total counts. Can be toggled with Ctrl+T.

// TeammateStatus represents the display state of a single teammate.
type TeammateStatus struct {
	Name        string
	AgentID     string
	BackendType string // "in-process", "tmux"
	Status      string // "running", "idle", "stopped", "completed", "failed"
	Color       string // hex color for badge
	CurrentTool string
	TurnCount   int
}

// TeamViewModel is the team panel sub-model.
type TeamViewModel struct {
	teamName string
	members  []TeammateStatus
	visible  bool
	width    int
}

// New creates a new TeamViewModel.
func New() *TeamViewModel {
	return &TeamViewModel{
		visible: true,
		width:   30,
	}
}

// SetTeamName updates the displayed team name.
func (m *TeamViewModel) SetTeamName(name string) {
	m.teamName = name
}

// SetMembers replaces the member list.
func (m *TeamViewModel) SetMembers(members []TeammateStatus) {
	m.members = members
}

// Toggle toggles panel visibility.
func (m *TeamViewModel) Toggle() {
	m.visible = !m.visible
}

// IsVisible returns whether the panel is visible.
func (m *TeamViewModel) IsVisible() bool {
	return m.visible
}

// SetWidth sets the panel width.
func (m *TeamViewModel) SetWidth(w int) {
	if w > 0 {
		m.width = w
	}
}

// ActiveCount returns the number of active teammates.
func (m *TeamViewModel) ActiveCount() int {
	count := 0
	for _, ts := range m.members {
		if ts.Status == "running" || ts.Status == "idle" {
			count++
		}
	}
	return count
}

// View renders the team panel.
func (m *TeamViewModel) View() string {
	if !m.visible || m.teamName == "" {
		return ""
	}

	var sb strings.Builder

	// Header.
	header := fmt.Sprintf("  Team: %s (%d/%d active)",
		m.teamName, m.ActiveCount(), len(m.members))
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#45B7D1")).
		Width(m.width)
	sb.WriteString(headerStyle.Render(header))
	sb.WriteString("\n")

	// Separator.
	sb.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		Render(strings.Repeat("─", m.width)))
	sb.WriteString("\n")

	// Members.
	for _, ts := range m.members {
		sb.WriteString(renderMember(ts, m.width))
		sb.WriteString("\n")
	}

	// Wrap in a border.
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#555555")).
		Width(m.width).
		Padding(0, 1)

	return panelStyle.Render(sb.String())
}

// StatusLineInfo returns a compact string for the main status bar.
func (m *TeamViewModel) StatusLineInfo() string {
	if m.teamName == "" {
		return ""
	}
	return fmt.Sprintf("Team: %s (%d active)", m.teamName, m.ActiveCount())
}

func renderMember(ts TeammateStatus, _ int) string {
	// Status indicator dot.
	dot := statusDot(ts.Status, ts.Color)

	// Name and details.
	name := lipgloss.NewStyle().Bold(true).Render(ts.Name)

	detail := ts.Status
	if ts.CurrentTool != "" {
		detail = fmt.Sprintf("%s → %s", ts.Status, ts.CurrentTool)
	}
	if ts.TurnCount > 0 {
		detail += fmt.Sprintf(" (turn %d)", ts.TurnCount)
	}

	detailStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Italic(true)

	return fmt.Sprintf("  %s %s %s", dot, name, detailStyle.Render(detail))
}

func statusDot(status, hexColor string) string {
	c := lipgloss.Color("#888888")
	switch status {
	case "running":
		c = lipgloss.Color("#4ECDC4")
	case "idle":
		c = lipgloss.Color("#FFEAA7")
	case "stopped", "completed":
		c = lipgloss.Color("#888888")
	case "failed":
		c = lipgloss.Color("#FF6B6B")
	}
	if hexColor != "" {
		c = lipgloss.Color(hexColor)
	}
	return lipgloss.NewStyle().Foreground(c).Render("●")
}
