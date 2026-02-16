package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mochlast/devcontainer-companion/internal/catalog"
)

// readmeFetchedMsg carries the result of an async README fetch.
type readmeFetchedMsg struct {
	sourceURL string
	content   string
	err       error
}

// readmePreview is a reusable component that shows a README in a scrollable viewport.
type readmePreview struct {
	visible   bool
	loading   bool
	sourceURL string
	viewport  viewport.Model
	width     int
	height    int
	errMsg    string
}

func newReadmePreview() readmePreview {
	return readmePreview{}
}

// Toggle opens or closes the preview. If opening for a new URL, returns a fetch command.
// While loading, a repeated toggle for the same URL is ignored to prevent accidental close.
func (p *readmePreview) Toggle(sourceURL string) tea.Cmd {
	if p.visible && p.sourceURL == sourceURL {
		if p.loading {
			return nil // ignore toggle while fetch is in-flight
		}
		p.visible = false
		return nil
	}

	if sourceURL == "" {
		p.visible = true
		p.loading = false
		p.sourceURL = ""
		p.errMsg = "No source URL available"
		p.viewport.SetContent(p.errMsg)
		return nil
	}

	needsFetch := p.sourceURL != sourceURL || p.errMsg != "" || p.viewport.TotalLineCount() == 0
	p.visible = true
	p.sourceURL = sourceURL

	if needsFetch {
		p.loading = true
		p.errMsg = ""
		p.viewport.SetContent("Loading README...")
		return fetchReadmeCmd(sourceURL)
	}

	return nil
}

// Close hides the preview panel.
func (p *readmePreview) Close() {
	p.visible = false
}

// HandleFetchResult processes the async fetch result.
func (p *readmePreview) HandleFetchResult(msg readmeFetchedMsg) {
	if msg.sourceURL != p.sourceURL {
		return
	}
	p.loading = false

	if msg.err != nil {
		p.errMsg = fmt.Sprintf("Error: %s", msg.err)
		p.viewport.SetContent(p.errMsg)
		return
	}

	rendered := renderMarkdown(msg.content, p.width)
	p.viewport.SetContent(rendered)
	p.viewport.GotoTop()
}

// SetSize updates the viewport dimensions.
func (p *readmePreview) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.viewport.Width = width
	p.viewport.Height = height
}

// Update handles viewport messages (scrolling).
func (p *readmePreview) Update(msg tea.Msg) tea.Cmd {
	if !p.visible {
		return nil
	}
	var cmd tea.Cmd
	p.viewport, cmd = p.viewport.Update(msg)
	return cmd
}

// View renders the preview panel.
func (p *readmePreview) View() string {
	if !p.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		PaddingLeft(1)

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("170")).
		Width(p.width - 2).
		Height(p.height - 3)

	title := titleStyle.Render("README Preview")
	body := borderStyle.Render(p.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}

// fetchReadmeCmd returns a tea.Cmd that fetches a README asynchronously.
func fetchReadmeCmd(sourceURL string) tea.Cmd {
	return func() tea.Msg {
		content, err := catalog.FetchReadme(sourceURL)
		return readmeFetchedMsg{
			sourceURL: sourceURL,
			content:   content,
			err:       err,
		}
	}
}

// renderMarkdown renders markdown content using glamour, falling back to raw text on error.
func renderMarkdown(content string, width int) string {
	if width < 10 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		return content
	}
	rendered, err := r.Render(content)
	if err != nil {
		return content
	}
	return rendered
}
