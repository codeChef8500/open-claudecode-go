package teamview

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ── Teammate message and badge rendering ─────────────────────────────────────
//
// These functions produce styled strings for rendering teammate-related
// content in the chat viewport.

// RenderTeammateMessage renders a chat message from a teammate with color badge.
//
//	● [researcher] Found 42 Go files in the project.
func RenderTeammateMessage(name, hexColor, content string) string {
	c := lipgloss.Color("#45B7D1")
	if hexColor != "" {
		c = lipgloss.Color(hexColor)
	}

	dot := lipgloss.NewStyle().Foreground(c).Render("●")
	badge := lipgloss.NewStyle().
		Foreground(c).
		Bold(true).
		Render(fmt.Sprintf("[%s]", name))

	return fmt.Sprintf("%s %s %s", dot, badge, content)
}

// RenderPermissionBadge renders a teammate permission request badge.
//
//	⚠ [researcher] requests permission: Edit file.go
//	[y] Allow  [n] Deny
func RenderPermissionBadge(workerName, workerColor, toolName, desc string) string {
	c := lipgloss.Color("#FFEAA7")
	if workerColor != "" {
		c = lipgloss.Color(workerColor)
	}

	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Bold(true).Render("⚠")
	badge := lipgloss.NewStyle().Foreground(c).Bold(true).Render(fmt.Sprintf("[%s]", workerName))

	line1 := fmt.Sprintf("%s %s requests permission: %s", warn, badge, toolName)
	if desc != "" {
		line1 += fmt.Sprintf(" — %s", desc)
	}

	allowKey := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ECDC4")).
		Bold(true).
		Render("[y] Allow")
	denyKey := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF6B6B")).
		Bold(true).
		Render("[n] Deny")
	line2 := fmt.Sprintf("  %s  %s", allowKey, denyKey)

	return line1 + "\n" + line2
}

// RenderIdleNotification renders a teammate idle notification.
//
//	● [researcher] is idle, waiting for task
func RenderIdleNotification(name, hexColor string) string {
	c := lipgloss.Color("#FFEAA7")
	if hexColor != "" {
		c = lipgloss.Color(hexColor)
	}

	dot := lipgloss.NewStyle().Foreground(c).Render("●")
	badge := lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf("[%s]", name))

	return fmt.Sprintf("%s %s is idle, waiting for task",
		dot, badge)
}

// RenderTeammateSpawned renders a teammate spawn notification.
//
//	▶ Spawned teammate [researcher] (in-process)
func RenderTeammateSpawned(name, hexColor, backendType string) string {
	c := lipgloss.Color("#4ECDC4")
	if hexColor != "" {
		c = lipgloss.Color(hexColor)
	}

	arrow := lipgloss.NewStyle().Foreground(c).Render("▶")
	badge := lipgloss.NewStyle().Foreground(c).Bold(true).Render(fmt.Sprintf("[%s]", name))

	return fmt.Sprintf("%s Spawned teammate %s (%s)", arrow, badge, backendType)
}

// RenderTeammateShutdown renders a teammate shutdown notification.
//
//	■ Teammate [researcher] shut down: completed
func RenderTeammateShutdown(name, hexColor, reason string) string {
	c := lipgloss.Color("#888888")
	if hexColor != "" {
		c = lipgloss.Color(hexColor)
	}

	square := lipgloss.NewStyle().Foreground(c).Render("■")
	badge := lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf("[%s]", name))

	msg := fmt.Sprintf("%s Teammate %s shut down", square, badge)
	if reason != "" {
		msg += ": " + reason
	}
	return msg
}
