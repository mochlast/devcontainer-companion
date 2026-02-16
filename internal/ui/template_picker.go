package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mochlast/devcontainer-companion/internal/catalog"
)

const emptyTemplateName = "[Empty Template]"

// templateItem wraps a CatalogEntry for the bubbles/list component.
type templateItem struct {
	entry    catalog.CatalogEntry
	isEmpty  bool
}

func (i templateItem) FilterValue() string {
	if i.isEmpty {
		return emptyTemplateName
	}
	return i.entry.FilterValue()
}

func (i templateItem) Title() string {
	if i.isEmpty {
		return emptyTemplateName
	}
	return i.entry.Name
}

func (i templateItem) Description() string {
	if i.isEmpty {
		return "Start with a minimal Ubuntu-based devcontainer"
	}
	return fmt.Sprintf("%s  %s", i.entry.Maintainer, i.entry.OciRef)
}

// templateDelegate renders template items in the list.
type templateDelegate struct{}

func (d templateDelegate) Height() int                             { return 2 }
func (d templateDelegate) Spacing() int                            { return 0 }
func (d templateDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d templateDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(templateItem)
	if !ok {
		return
	}

	title := item.Title()
	desc := item.Description()

	isSelected := index == m.Index()

	titleStyle := lipgloss.NewStyle().PaddingLeft(2)
	descStyle := lipgloss.NewStyle().PaddingLeft(4).Faint(true)

	if isSelected {
		titleStyle = titleStyle.Bold(true).Foreground(lipgloss.Color("170"))
		descStyle = descStyle.Foreground(lipgloss.Color("170"))
		title = "> " + title
	} else {
		title = "  " + title
	}

	maxW := m.Width()
	fmt.Fprintf(w, "%s\n%s", titleStyle.MaxWidth(maxW).Render(title), descStyle.MaxWidth(maxW).Render(desc))
}

// templatePickerModel is the bubbletea model for the template picker.
type templatePickerModel struct {
	list     list.Model
	selected *templateItem
	quitting bool
	preview  readmePreview
	width    int
	height   int
}

func newTemplatePicker(entries []catalog.CatalogEntry) templatePickerModel {
	items := make([]list.Item, 0, len(entries)+1)

	// Add empty template as first item
	items = append(items, templateItem{isEmpty: true})

	for _, e := range entries {
		items = append(items, templateItem{entry: e})
	}

	l := list.New(items, templateDelegate{}, 80, 20)
	l.Title = "Select a devcontainer template"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Filter = officialFirstFilterFunc(items)
	l.SetShowHelp(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginLeft(2)

	// Rebind help toggle from ? to h to free ? for README preview
	l.KeyMap.ShowFullHelp = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "help"))
	l.KeyMap.CloseFullHelp = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "close help"))

	// Add ? as additional help key info
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "README")),
		}
	}

	return templatePickerModel{
		list:    l,
		preview: newReadmePreview(),
	}
}

func (m templatePickerModel) Init() tea.Cmd {
	return nil
}

func (m *templatePickerModel) applyLayout() {
	if m.preview.visible {
		listW := m.width / 3
		m.list.SetWidth(listW)
		m.list.SetHeight(m.height - 2)
		m.preview.SetSize(m.width-listW, m.height-2)
	} else {
		m.list.SetWidth(m.width)
		m.list.SetHeight(m.height - 2)
	}
}

func (m templatePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		return m, nil

	case readmeFetchedMsg:
		m.preview.HandleFetchResult(msg)
		return m, nil

	case tea.KeyMsg:
		// Don't intercept keys when already filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "?":
			sourceURL := ""
			if item, ok := m.list.SelectedItem().(templateItem); ok && !item.isEmpty {
				sourceURL = item.entry.SourceURL
			}
			cmd := m.preview.Toggle(sourceURL)
			m.applyLayout()
			return m, cmd

		case "esc":
			if m.preview.visible {
				m.preview.Close()
				m.applyLayout()
				return m, nil
			}

		case "enter":
			if !m.preview.visible {
				if item, ok := m.list.SelectedItem().(templateItem); ok {
					m.selected = &item
					m.quitting = true
					return m, tea.Quit
				}
			}
			return m, nil

		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

		// When preview is open, only scroll viewport; swallow everything else
		if m.preview.visible {
			cmd := m.preview.Update(msg)
			return m, cmd
		}

		// Auto-start filtering on printable character input
		if len(msg.Runes) > 0 && msg.Runes[0] != '/' && m.list.FilterState() == list.Unfiltered {
			filterMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
			m.list, _ = m.list.Update(filterMsg)
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m templatePickerModel) View() string {
	if m.quitting && m.selected != nil {
		return ""
	}
	listView := "\n" + m.list.View()
	if m.preview.visible {
		listW := m.width / 3
		clipped := lipgloss.NewStyle().Width(listW).MaxWidth(listW).Render(listView)
		return lipgloss.JoinHorizontal(lipgloss.Top, clipped, m.preview.View())
	}
	return listView
}

// PickTemplate shows a fuzzy-finder to select a devcontainer template.
// Returns the selected CatalogEntry, or nil if "Empty Template" was chosen.
// Returns an error if the user quit without selecting.
func PickTemplate(entries []catalog.CatalogEntry) (*catalog.CatalogEntry, error) {
	m := newTemplatePicker(entries)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running template picker: %w", err)
	}

	result := finalModel.(templatePickerModel)
	if result.selected == nil {
		return nil, fmt.Errorf("no template selected")
	}

	if result.selected.isEmpty {
		return nil, nil
	}

	return &result.selected.entry, nil
}

// FormatOciRefWithVersion creates a versioned OCI reference for display.
func FormatOciRefWithVersion(entry *catalog.CatalogEntry) string {
	ref := entry.OciRef
	if entry.Version != "" && !strings.Contains(ref, ":") {
		ref += ":" + entry.Version
	}
	return ref
}
