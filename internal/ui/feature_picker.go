package ui

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mochlast/devcontainer-companion/internal/catalog"
)

// ErrPickerCancelled is returned when the user quits a picker with q or Ctrl+C.
var ErrPickerCancelled = errors.New("cancelled")

// featureItem wraps a CatalogEntry for multi-select in the feature picker.
type featureItem struct {
	entry    catalog.CatalogEntry
	selected bool
}

func (i featureItem) FilterValue() string { return i.entry.FilterValue() }
func (i featureItem) Title() string       { return i.entry.Name }
func (i featureItem) Description() string {
	return fmt.Sprintf("%s  %s", i.entry.Maintainer, i.entry.OciRef)
}

// featureDelegate renders feature items with selection checkboxes.
type featureDelegate struct {
	selectedItems map[string]bool
}

func (d featureDelegate) Height() int                             { return 2 }
func (d featureDelegate) Spacing() int                            { return 0 }
func (d featureDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d featureDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(featureItem)
	if !ok {
		return
	}

	title := item.Title()
	desc := item.Description()

	isActive := index == m.Index()
	isChecked := d.selectedItems[item.entry.OciRef]

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

// featurePickerModel is the bubbletea model for multi-select feature picking.
type featurePickerModel struct {
	list          list.Model
	selectedItems map[string]bool
	confirmed     bool
	quitting      bool
	preview       readmePreview
	width         int
	height        int
}

func newFeaturePicker(entries []catalog.CatalogEntry, preSelected map[string]bool) featurePickerModel {
	items := make([]list.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, featureItem{entry: e})
	}

	selectedItems := make(map[string]bool)
	if preSelected != nil {
		for k, v := range preSelected {
			selectedItems[k] = v
		}
	}

	// Pin pre-selected items to the top of the list
	if len(selectedItems) > 0 {
		sort.SliceStable(items, func(i, j int) bool {
			iSel := selectedItems[items[i].(featureItem).entry.OciRef]
			jSel := selectedItems[items[j].(featureItem).entry.OciRef]
			return iSel && !jSel
		})
	}

	delegate := featureDelegate{selectedItems: selectedItems}

	l := list.New(items, delegate, 80, 20)
	l.Title = "Select features (Space to toggle, Enter to confirm)"
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

	return featurePickerModel{
		list:          l,
		selectedItems: selectedItems,
		preview:       newReadmePreview(),
	}
}

func (m featurePickerModel) Init() tea.Cmd {
	return nil
}

func (m *featurePickerModel) applyLayout() {
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

func (m featurePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if item, ok := m.list.SelectedItem().(featureItem); ok {
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

		case " ":
			if m.preview.visible {
				return m, nil
			}
			// Toggle selection by OCI ref (index is unreliable when filtered)
			if item, ok := m.list.SelectedItem().(featureItem); ok {
				ref := item.entry.OciRef
				m.selectedItems[ref] = !m.selectedItems[ref]
				m.list.SetDelegate(featureDelegate{selectedItems: m.selectedItems})
			}
			return m, nil

		case "enter":
			if !m.preview.visible {
				m.confirmed = true
				m.quitting = true
				return m, tea.Quit
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

func (m featurePickerModel) View() string {
	if m.quitting {
		return ""
	}

	// Show count of selected items
	count := 0
	for _, v := range m.selectedItems {
		if v {
			count++
		}
	}

	status := ""
	if count > 0 {
		status = lipgloss.NewStyle().
			MarginLeft(2).
			Foreground(lipgloss.Color("170")).
			Render(fmt.Sprintf("\n  %d feature(s) selected", count))
	}

	listView := "\n" + m.list.View() + status
	if m.preview.visible {
		listW := m.width / 3
		clipped := lipgloss.NewStyle().Width(listW).MaxWidth(listW).Render(listView)
		return lipgloss.JoinHorizontal(lipgloss.Top, clipped, m.preview.View())
	}
	return listView
}

// PickFeatures shows a multi-select fuzzy-finder for devcontainer features.
// Returns the selected CatalogEntries.
func PickFeatures(entries []catalog.CatalogEntry) ([]catalog.CatalogEntry, error) {
	return PickFeaturesWithSelection(entries, nil)
}

// PickFeaturesWithSelection shows a multi-select fuzzy-finder for devcontainer features
// with optional pre-selected entries. The preSelected map keys are unversioned OCI refs.
func PickFeaturesWithSelection(entries []catalog.CatalogEntry, preSelected map[string]bool) ([]catalog.CatalogEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	m := newFeaturePicker(entries, preSelected)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running feature picker: %w", err)
	}

	result := finalModel.(featurePickerModel)
	if !result.confirmed {
		return nil, ErrPickerCancelled
	}

	var selected []catalog.CatalogEntry
	for _, item := range result.list.Items() {
		if fi, ok := item.(featureItem); ok && result.selectedItems[fi.entry.OciRef] {
			selected = append(selected, fi.entry)
		}
	}

	return selected, nil
}

// FormatFeatureOciRef creates a versioned OCI reference for a feature.
func FormatFeatureOciRef(entry *catalog.CatalogEntry) string {
	ref := entry.OciRef
	if entry.Version != "" && !strings.Contains(ref, ":") {
		ref += ":" + entry.Version
	}
	return ref
}
