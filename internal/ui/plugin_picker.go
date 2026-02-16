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

// pluginItem wraps a JetBrains Plugin for multi-select in the picker.
type pluginItem struct {
	plugin marketplace.Plugin
}

func (i pluginItem) FilterValue() string {
	return i.plugin.Name + " " + i.plugin.ID
}

func (i pluginItem) Title() string { return i.plugin.Name }

func (i pluginItem) Description() string {
	installs := formatInstallCount(i.plugin.Downloads)
	rating := fmt.Sprintf("%.1f", i.plugin.Rating)
	if i.plugin.Rating == 0 {
		rating = "-"
	}
	return fmt.Sprintf("%s  %s installs  ★ %s", i.plugin.ID, installs, rating)
}

// pluginDelegate renders plugin items with selection checkboxes.
type pluginDelegate struct {
	selectedItems map[string]bool
}

func (d pluginDelegate) Height() int                             { return 2 }
func (d pluginDelegate) Spacing() int                            { return 0 }
func (d pluginDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d pluginDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(pluginItem)
	if !ok {
		return
	}

	title := item.Title()
	desc := item.Description()

	isActive := index == m.Index()
	isChecked := d.selectedItems[item.plugin.ID]

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

// pluginSearchResultMsg carries JetBrains search results back to the model.
type pluginSearchResultMsg struct {
	plugins []marketplace.Plugin
	err     error
	query   string
}

// pluginReadmeFetchedMsg carries the result of an async plugin readme fetch.
type pluginReadmeFetchedMsg struct {
	pluginID string
	content  string
	err      error
}

// pluginPickerModel is the bubbletea model for JetBrains plugin picking with live search.
type pluginPickerModel struct {
	list          list.Model
	selectedItems map[string]bool
	confirmed     bool
	quitting      bool
	width         int
	height        int
	searchInput   string
	searching     bool
	lastQuery     string
	preview       readmePreview
}

func newPluginPicker(preSelected map[string]bool) pluginPickerModel {
	selectedItems := make(map[string]bool)
	if preSelected != nil {
		for k, v := range preSelected {
			selectedItems[k] = v
		}
	}

	delegate := pluginDelegate{selectedItems: selectedItems}

	// Show pre-selected plugins as initial items
	var initialItems []list.Item
	for id, checked := range selectedItems {
		if checked {
			initialItems = append(initialItems, pluginItem{
				plugin: marketplace.Plugin{
					ID:   id,
					Name: id,
				},
			})
		}
	}
	sort.Slice(initialItems, func(i, j int) bool {
		return initialItems[i].(pluginItem).plugin.ID < initialItems[j].(pluginItem).plugin.ID
	})

	l := list.New(initialItems, delegate, 80, 20)
	l.Title = "Search JetBrains plugins (type to search, Space to toggle, Enter to confirm)"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginLeft(2)

	l.KeyMap.ShowFullHelp = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "help"))
	l.KeyMap.CloseFullHelp = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "close help"))

	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "details")),
		}
	}

	return pluginPickerModel{
		list:          l,
		selectedItems: selectedItems,
		preview:       newReadmePreview(),
	}
}

func (m pluginPickerModel) Init() tea.Cmd {
	return nil
}

func (m *pluginPickerModel) applyLayout() {
	if m.preview.visible {
		listW := m.width / 3
		m.list.SetWidth(listW)
		m.list.SetHeight(m.height - 3)
		m.preview.SetSize(m.width-listW, m.height-2)
	} else {
		m.list.SetWidth(m.width)
		m.list.SetHeight(m.height - 3)
	}
}

func (m pluginPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		return m, nil

	case pluginSearchResultMsg:
		m.searching = false
		if msg.err != nil || msg.query != m.lastQuery {
			return m, nil
		}
		items := make([]list.Item, 0, len(msg.plugins))
		for _, p := range msg.plugins {
			items = append(items, pluginItem{plugin: p})
		}
		if len(m.selectedItems) > 0 {
			sort.SliceStable(items, func(i, j int) bool {
				iSel := m.selectedItems[items[i].(pluginItem).plugin.ID]
				jSel := m.selectedItems[items[j].(pluginItem).plugin.ID]
				return iSel && !jSel
			})
		}
		m.list.SetItems(items)
		m.list.SetDelegate(pluginDelegate{selectedItems: m.selectedItems})
		return m, nil

	case pluginReadmeFetchedMsg:
		if msg.pluginID != m.preview.sourceURL {
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
		if m.preview.visible {
			switch msg.String() {
			case "?", "esc":
				m.preview.Close()
				m.applyLayout()
				return m, nil
			default:
				cmd := m.preview.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "?":
			if item, ok := m.list.SelectedItem().(pluginItem); ok {
				pluginID := item.plugin.ID
				m.preview.visible = true
				m.preview.loading = true
				m.preview.sourceURL = pluginID
				m.preview.errMsg = ""
				m.preview.viewport.SetContent("Loading plugin details...")
				m.applyLayout()
				return m, fetchPluginReadmeCmd(pluginID)
			}
			return m, nil

		case "enter":
			m.confirmed = true
			m.quitting = true
			return m, tea.Quit

		case " ":
			if item, ok := m.list.SelectedItem().(pluginItem); ok {
				id := item.plugin.ID
				m.selectedItems[id] = !m.selectedItems[id]
				m.list.SetDelegate(pluginDelegate{selectedItems: m.selectedItems})
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
				var items []list.Item
				for id, checked := range m.selectedItems {
					if checked {
						items = append(items, pluginItem{
							plugin: marketplace.Plugin{ID: id, Name: id},
						})
					}
				}
				sort.Slice(items, func(i, j int) bool {
					return items[i].(pluginItem).plugin.ID < items[j].(pluginItem).plugin.ID
				})
				m.list.SetItems(items)
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit

		default:
			if len(msg.Runes) > 0 {
				m.searchInput += string(msg.Runes)
				return m, m.triggerSearch()
			}
		}

		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *pluginPickerModel) triggerSearch() tea.Cmd {
	query := strings.TrimSpace(m.searchInput)
	if query == "" {
		return nil
	}
	m.lastQuery = query
	m.searching = true
	return tea.Tick(300*time.Millisecond, func(_ time.Time) tea.Msg {
		plugins, err := marketplace.SearchPlugins(query, 20)
		return pluginSearchResultMsg{plugins: plugins, err: err, query: query}
	})
}

func fetchPluginReadmeCmd(pluginID string) tea.Cmd {
	return func() tea.Msg {
		content, err := marketplace.FetchPluginReadme(pluginID)
		return pluginReadmeFetchedMsg{
			pluginID: pluginID,
			content:  content,
			err:      err,
		}
	}
}

func (m pluginPickerModel) View() string {
	if m.quitting {
		return ""
	}

	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("170"))
	faintStyle := lipgloss.NewStyle().Faint(true)

	searchLine := accentStyle.Render(fmt.Sprintf("  Search: %s", m.searchInput))
	searchLine += faintStyle.Render("_")
	if m.searching {
		searchLine += accentStyle.Render("  Searching...")
	}

	count := 0
	for _, v := range m.selectedItems {
		if v {
			count++
		}
	}

	status := ""
	if count > 0 {
		status = accentStyle.Render(fmt.Sprintf("\n  %d plugin(s) selected", count))
	}

	listView := "\n" + searchLine + "\n" + m.list.View() + status

	if m.preview.visible {
		listW := m.width / 3
		clipped := lipgloss.NewStyle().Width(listW).MaxWidth(listW).Render(listView)
		return lipgloss.JoinHorizontal(lipgloss.Top, clipped, m.preview.View())
	}

	return listView
}

// PickPlugins shows a multi-select plugin picker with live JetBrains Marketplace search.
func PickPlugins(preSelected map[string]bool) ([]string, error) {
	m := newPluginPicker(preSelected)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running plugin picker: %w", err)
	}

	result := finalModel.(pluginPickerModel)
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
