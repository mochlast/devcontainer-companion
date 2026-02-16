package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mochlast/devcontainer-companion/internal/marketplace"
)

// extensionItem wraps a marketplace Extension for multi-select in the picker.
type extensionItem struct {
	ext marketplace.Extension
}

func (i extensionItem) FilterValue() string {
	return i.ext.DisplayName + " " + i.ext.ID
}

func (i extensionItem) Title() string { return i.ext.DisplayName }

func (i extensionItem) Description() string {
	installs := formatInstallCount(i.ext.InstallCount)
	rating := fmt.Sprintf("%.1f", i.ext.Rating)
	if i.ext.Rating == 0 {
		rating = "-"
	}
	return fmt.Sprintf("%s  %s installs  ★ %s", i.ext.ID, installs, rating)
}

func formatInstallCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// extensionDelegate renders extension items with selection checkboxes.
type extensionDelegate struct {
	selectedItems map[string]bool
}

func (d extensionDelegate) Height() int                             { return 2 }
func (d extensionDelegate) Spacing() int                            { return 0 }
func (d extensionDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d extensionDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(extensionItem)
	if !ok {
		return
	}

	title := item.Title()
	desc := item.Description()

	isActive := index == m.Index()
	isChecked := d.selectedItems[item.ext.ID]

	checkbox := "[ ]"
	if isChecked {
		checkbox = "[x]"
	}

	titleStyle := lipgloss.NewStyle()
	descStyle := lipgloss.NewStyle().PaddingLeft(6).Faint(true)

	if isActive {
		titleStyle = titleStyle.Bold(true).Foreground(lipgloss.Color("170"))
		descStyle = descStyle.Foreground(lipgloss.Color("170"))
		title = fmt.Sprintf("> %s %s", checkbox, title)
	} else {
		title = fmt.Sprintf("  %s %s", checkbox, title)
	}

	maxW := m.Width()
	fmt.Fprintf(w, "%s\n%s", titleStyle.MaxWidth(maxW).Render(title), descStyle.MaxWidth(maxW).Render(desc))
}

// searchResultMsg carries marketplace search results back to the model.
type searchResultMsg struct {
	extensions []marketplace.Extension
	err        error
	query      string
	sortBy     marketplace.SortBy
}

// extReadmeFetchedMsg carries the result of an async extension README fetch.
type extReadmeFetchedMsg struct {
	extensionID string
	content     string
	err         error
}

// extensionPickerModel is the bubbletea model for extension picking with live search.
type extensionPickerModel struct {
	list          list.Model
	selectedItems map[string]bool
	confirmed     bool
	quitting      bool
	width         int
	height        int
	searchInput   string
	searching     bool
	lastQuery     string
	sortIndex     int
	sortOptions   []marketplace.SortOption
	preview       readmePreview
}

func newExtensionPicker(preSelected map[string]bool) extensionPickerModel {
	selectedItems := make(map[string]bool)
	if preSelected != nil {
		for k, v := range preSelected {
			selectedItems[k] = v
		}
	}

	delegate := extensionDelegate{selectedItems: selectedItems}

	// Show pre-selected extensions as initial items (with ID as display name)
	var initialItems []list.Item
	for id, checked := range selectedItems {
		if checked {
			initialItems = append(initialItems, extensionItem{
				ext: marketplace.Extension{
					ID:          id,
					DisplayName: id,
					Description: "(currently installed)",
				},
			})
		}
	}
	// Sort initial items alphabetically for stable display
	sort.Slice(initialItems, func(i, j int) bool {
		return initialItems[i].(extensionItem).ext.ID < initialItems[j].(extensionItem).ext.ID
	})

	l := list.New(initialItems, delegate, 80, 20)
	l.Title = "Search VS Code extensions (type to search, Space to toggle, Enter to confirm)"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false) // We handle search ourselves
	l.SetShowHelp(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginLeft(2)

	l.KeyMap.ShowFullHelp = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "help"))
	l.KeyMap.CloseFullHelp = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "close help"))

	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "details")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "sort")),
		}
	}

	return extensionPickerModel{
		list:          l,
		selectedItems: selectedItems,
		sortOptions:   marketplace.SortOptions(),
		sortIndex:     0,
		preview:       newReadmePreview(),
	}
}

func (m extensionPickerModel) Init() tea.Cmd {
	return nil
}

func (m *extensionPickerModel) applyLayout() {
	if m.preview.visible {
		listW := m.width / 3
		m.list.SetWidth(listW)
		m.list.SetHeight(m.height - 4)
		m.preview.SetSize(m.width-listW, m.height-2)
	} else {
		m.list.SetWidth(m.width)
		m.list.SetHeight(m.height - 4)
	}
}

func (m extensionPickerModel) currentSortBy() marketplace.SortBy {
	if m.sortIndex >= 0 && m.sortIndex < len(m.sortOptions) {
		return m.sortOptions[m.sortIndex].SortBy
	}
	return marketplace.SortByInstalls
}

func (m extensionPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		return m, nil

	case searchResultMsg:
		m.searching = false
		if msg.err != nil || msg.query != m.lastQuery || msg.sortBy != m.currentSortBy() {
			return m, nil
		}
		items := make([]list.Item, 0, len(msg.extensions))
		for _, ext := range msg.extensions {
			items = append(items, extensionItem{ext: ext})
		}
		// Pin selected items to the top of results
		if len(m.selectedItems) > 0 {
			sort.SliceStable(items, func(i, j int) bool {
				iSel := m.selectedItems[items[i].(extensionItem).ext.ID]
				jSel := m.selectedItems[items[j].(extensionItem).ext.ID]
				return iSel && !jSel
			})
		}
		m.list.SetItems(items)
		m.list.SetDelegate(extensionDelegate{selectedItems: m.selectedItems})
		return m, nil

	case extReadmeFetchedMsg:
		if msg.extensionID != m.preview.sourceURL {
			return m, nil
		}
		m.preview.loading = false
		if msg.err != nil {
			m.preview.errMsg = fmt.Sprintf("Error: %s", msg.err)
			m.preview.viewport.SetContent(m.preview.errMsg)
			return m, nil
		}
		rendered := renderMarkdown(msg.content, m.preview.width)
		m.preview.viewport.SetContent(rendered)
		m.preview.viewport.GotoTop()
		return m, nil

	case tea.KeyMsg:
		// When preview is open, handle preview-specific keys first
		if m.preview.visible {
			switch msg.String() {
			case "?":
				m.preview.Close()
				m.applyLayout()
				return m, nil
			case "esc":
				m.preview.Close()
				m.applyLayout()
				return m, nil
			default:
				// Scroll viewport
				cmd := m.preview.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "?":
			if item, ok := m.list.SelectedItem().(extensionItem); ok {
				extID := item.ext.ID
				if m.preview.visible && m.preview.sourceURL == extID {
					m.preview.Close()
					m.applyLayout()
					return m, nil
				}
				m.preview.visible = true
				m.preview.loading = true
				m.preview.sourceURL = extID
				m.preview.errMsg = ""
				m.preview.viewport.SetContent("Loading extension details...")
				m.applyLayout()
				return m, fetchExtensionReadmeCmd(extID)
			}
			return m, nil

		case "tab":
			m.sortIndex = (m.sortIndex + 1) % len(m.sortOptions)
			// Re-trigger search with new sort
			return m, m.forceSearch()

		case "shift+tab":
			m.sortIndex = (m.sortIndex - 1 + len(m.sortOptions)) % len(m.sortOptions)
			return m, m.forceSearch()

		case "enter":
			m.confirmed = true
			m.quitting = true
			return m, tea.Quit

		case " ":
			if item, ok := m.list.SelectedItem().(extensionItem); ok {
				id := item.ext.ID
				m.selectedItems[id] = !m.selectedItems[id]
				m.list.SetDelegate(extensionDelegate{selectedItems: m.selectedItems})
			}
			return m, nil

		case "backspace":
			if len(m.searchInput) > 0 {
				m.searchInput = m.searchInput[:len(m.searchInput)-1]
				return m, m.triggerSearch()
			}
			return m, nil

		case "esc":
			if m.searchInput != "" {
				m.searchInput = ""
				m.lastQuery = ""
				// Restore pre-selected items view
				var items []list.Item
				for id, checked := range m.selectedItems {
					if checked {
						items = append(items, extensionItem{
							ext: marketplace.Extension{
								ID:          id,
								DisplayName: id,
								Description: "(currently installed)",
							},
						})
					}
				}
				sort.Slice(items, func(i, j int) bool {
					return items[i].(extensionItem).ext.ID < items[j].(extensionItem).ext.ID
				})
				m.list.SetItems(items)
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit

		default:
			// Printable characters go to search input
			if len(msg.Runes) > 0 {
				m.searchInput += string(msg.Runes)
				return m, m.triggerSearch()
			}
		}

		// Pass navigation keys to list
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// triggerSearch returns a debounced search command.
func (m *extensionPickerModel) triggerSearch() tea.Cmd {
	query := strings.TrimSpace(m.searchInput)
	if query == "" {
		return nil
	}
	m.lastQuery = query
	m.searching = true
	sortBy := m.currentSortBy()
	return tea.Tick(300*time.Millisecond, func(_ time.Time) tea.Msg {
		exts, err := marketplace.Search(query, 20, sortBy)
		return searchResultMsg{extensions: exts, err: err, query: query, sortBy: sortBy}
	})
}

// forceSearch re-triggers the current search immediately (used when sort changes).
func (m *extensionPickerModel) forceSearch() tea.Cmd {
	query := strings.TrimSpace(m.searchInput)
	if query == "" {
		return nil
	}
	m.lastQuery = query
	m.searching = true
	sortBy := m.currentSortBy()
	return func() tea.Msg {
		exts, err := marketplace.Search(query, 20, sortBy)
		return searchResultMsg{extensions: exts, err: err, query: query, sortBy: sortBy}
	}
}

// fetchExtensionReadmeCmd returns a tea.Cmd that fetches an extension README asynchronously.
func fetchExtensionReadmeCmd(extensionID string) tea.Cmd {
	return func() tea.Msg {
		content, err := marketplace.FetchReadme(extensionID)
		return extReadmeFetchedMsg{
			extensionID: extensionID,
			content:     content,
			err:         err,
		}
	}
}

func (m extensionPickerModel) View() string {
	if m.quitting {
		return ""
	}

	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("170"))
	faintStyle := lipgloss.NewStyle().Faint(true)

	// Search line
	searchLine := accentStyle.Render(fmt.Sprintf("  Search: %s", m.searchInput))
	searchLine += faintStyle.Render("_")
	if m.searching {
		searchLine += accentStyle.Render("  Searching...")
	}

	// Sort indicator
	sortLabel := ""
	if m.sortIndex >= 0 && m.sortIndex < len(m.sortOptions) {
		var labels []string
		for i, opt := range m.sortOptions {
			if i == m.sortIndex {
				labels = append(labels, accentStyle.Bold(true).Render(opt.Label))
			} else {
				labels = append(labels, faintStyle.Render(opt.Label))
			}
		}
		sortLabel = fmt.Sprintf("  Sort: %s", strings.Join(labels, faintStyle.Render(" | ")))
	}

	// Selection count
	count := 0
	for _, v := range m.selectedItems {
		if v {
			count++
		}
	}

	status := ""
	if count > 0 {
		status = accentStyle.Render(fmt.Sprintf("\n  %d extension(s) selected", count))
	}

	listView := "\n" + searchLine + "\n" + sortLabel + "\n" + m.list.View() + status

	if m.preview.visible {
		listW := m.width / 3
		clipped := lipgloss.NewStyle().Width(listW).MaxWidth(listW).Render(listView)
		return lipgloss.JoinHorizontal(lipgloss.Top, clipped, m.preview.View())
	}

	return listView
}

// PickExtensions shows a multi-select extension picker with live marketplace search.
func PickExtensions(preSelected map[string]bool) ([]string, error) {
	m := newExtensionPicker(preSelected)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running extension picker: %w", err)
	}

	result := finalModel.(extensionPickerModel)
	if !result.confirmed {
		return nil, ErrPickerCancelled
	}

	var selected []string
	for id, checked := range result.selectedItems {
		if checked {
			selected = append(selected, id)
		}
	}

	return selected, nil
}
