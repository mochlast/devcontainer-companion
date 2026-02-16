package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mochlast/devcontainer-companion/internal/template"
)

// HubAction represents the action selected from the hub menu.
// Build and Open are handled within the hub TUI and never returned to the caller.
type HubAction string

const (
	HubActionTemplate       HubAction = "template"
	HubActionFeatures       HubAction = "features"
	HubActionExtensions     HubAction = "extensions"
	HubActionPlugins        HubAction = "plugins"
	HubActionCustomizations HubAction = "customizations"
	HubActionBuild          HubAction = "build"
	HubActionOpen           HubAction = "open"
	HubActionExit           HubAction = "exit"
)

// HubCallbacks provides functions for actions handled within the hub TUI.
type HubCallbacks struct {
	Build   func(noCache bool) (string, error)
	Open    func() (string, error)
	Preload func(action HubAction) (any, error) // loads data before exiting for a sub-flow
}

// shortcutActions maps single-key shortcuts to their hub actions.
// CLI-dependent items (b/o) are added dynamically.
var shortcutActions = map[string]HubAction{
	"t": HubActionTemplate,
	"f": HubActionFeatures,
	"e": HubActionExtensions,
	"j": HubActionPlugins,
	"c": HubActionCustomizations,
}

type hubMenuItem struct {
	key         string
	label       string
	description string
	action      HubAction
}

func (i hubMenuItem) FilterValue() string { return i.label }
func (i hubMenuItem) Title() string       { return i.label }
func (i hubMenuItem) Description() string { return i.description }

type hubMenuDelegate struct{}

func (d hubMenuDelegate) Height() int                             { return 2 }
func (d hubMenuDelegate) Spacing() int                            { return 0 }
func (d hubMenuDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d hubMenuDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(hubMenuItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	titleStyle := lipgloss.NewStyle().PaddingLeft(2)
	descStyle := lipgloss.NewStyle().PaddingLeft(4).Faint(true)

	title := item.label
	if isSelected {
		titleStyle = titleStyle.Bold(true).Foreground(lipgloss.Color("170"))
		descStyle = descStyle.Foreground(lipgloss.Color("170"))
		title = fmt.Sprintf("> [%s] %s", item.key, title)
	} else {
		title = fmt.Sprintf("  [%s] %s", item.key, title)
	}

	maxW := m.Width()
	fmt.Fprintf(w, "%s\n%s", titleStyle.MaxWidth(maxW).Render(title), descStyle.MaxWidth(maxW).Render(item.description))
}

// cmdResultMsg is sent when an async command (build/open) completes.
type cmdResultMsg struct {
	kind    string // "build" or "open"
	success bool
	detail  string
}

// preloadDoneMsg is sent when a preload operation completes.
type preloadDoneMsg struct {
	value any
	err   error
}

type hubModel struct {
	list           list.Model
	viewport       viewport.Model
	config         map[string]any
	cli            template.CLIInfo
	dirty          bool
	callbacks      HubCallbacks
	actions        map[string]HubAction
	action         HubAction
	busy           bool
	busyLabel      string
	result         *cmdResultMsg
	preloadedData  any
	quitting       bool
	width          int
	height         int
}

// HubContext carries the state needed to render the hub layout across all phases.
type HubContext struct {
	ProjectName string
	Config      map[string]any
	CLI         template.CLIInfo
	Dirty       bool
}

// newHubMenuList creates the hub menu list for display. Used by the hub model
// and by phase models (loading, form) for visual continuity.
func newHubMenuList(projectName string, cli template.CLIInfo, dirty bool) list.Model {
	items := []list.Item{
		hubMenuItem{key: "t", label: "Set Template", description: "Choose or change the base template", action: HubActionTemplate},
		hubMenuItem{key: "f", label: "Add/Remove Features", description: "Manage devcontainer features", action: HubActionFeatures},
		hubMenuItem{key: "e", label: "VS Code Extensions", description: "Search & select VS Code extensions", action: HubActionExtensions},
		hubMenuItem{key: "j", label: "JetBrains Plugins", description: "Search & select JetBrains plugins", action: HubActionPlugins},
		hubMenuItem{key: "c", label: "Edit Settings", description: "Edit remoteUser, ports, commands, env", action: HubActionCustomizations},
	}

	if cli.Installed {
		buildDesc := "Build devcontainer image"
		if dirty {
			buildDesc = "Rebuild devcontainer image (config changed)"
		}
		items = append(items,
			hubMenuItem{key: "b", label: "Build", description: buildDesc, action: HubActionBuild},
		)
	}

	if cli.HasOpen {
		items = append(items,
			hubMenuItem{key: "o", label: "Open in VS Code", description: "Build, start & connect in VS Code", action: HubActionOpen},
		)
	}

	items = append(items,
		hubMenuItem{key: "q", label: "Exit", description: "Exit dcc", action: HubActionExit},
	)

	l := list.New(items, hubMenuDelegate{}, 30, 20)
	l.Title = fmt.Sprintf("dcc — %s", projectName)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginLeft(2)
	return l
}

// renderHubLayout renders the standard hub split-pane layout: menu on the left,
// preview content on the right inside a bordered box.
func renderHubLayout(menuList list.Model, previewContent, previewTitle string, width, height int) string {
	menuW := max(width/3, 30)
	previewW := width - menuW

	menuView := "\n" + menuList.View()
	menuClipped := lipgloss.NewStyle().Width(menuW).MaxWidth(menuW).Render(menuView)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		PaddingLeft(1).
		Render(previewTitle)

	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("170")).
		Width(previewW - 2).
		Height(height - 4).
		Render(previewContent)

	preview := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return lipgloss.JoinHorizontal(lipgloss.Top, menuClipped, preview)
}

func newHubModel(projectName string, config map[string]any, cli template.CLIInfo, dirty bool, cb HubCallbacks) hubModel {
	l := newHubMenuList(projectName, cli, dirty)

	actions := make(map[string]HubAction)
	for k, v := range shortcutActions {
		actions[k] = v
	}
	if cli.Installed {
		actions["b"] = HubActionBuild
	}
	if cli.HasOpen {
		actions["o"] = HubActionOpen
	}

	return hubModel{
		list:      l,
		config:    config,
		cli:       cli,
		dirty:     dirty,
		callbacks: cb,
		actions:   actions,
	}
}

func (m hubModel) Init() tea.Cmd { return nil }

func (m hubModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case preloadDoneMsg:
		if msg.err != nil {
			// Preload failed — return to idle
			m.busy = false
			m.result = &cmdResultMsg{kind: "preload", success: false, detail: msg.err.Error()}
			m.viewport.SetContent(m.renderPreview())
			return m, nil
		}
		m.preloadedData = msg.value
		m.quitting = true
		return m, tea.Quit

	case cmdResultMsg:
		m.busy = false
		m.result = &msg
		if msg.kind == "build" && msg.success {
			m.dirty = false
		}
		m.viewport.SetContent(m.renderPreview())
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		return m, nil

	case tea.KeyMsg:
		// Ignore all input while a command is running.
		if m.busy {
			return m, nil
		}

		// Dismiss result overlay on any key press.
		if m.result != nil {
			m.result = nil
			m.viewport.SetContent(m.renderPreview())
			return m, nil
		}

		key := msg.String()

		if key == "q" || key == "ctrl+c" {
			m.action = HubActionExit
			m.quitting = true
			return m, tea.Quit
		}

		if key == "enter" {
			item, ok := m.list.SelectedItem().(hubMenuItem)
			if !ok {
				return m, nil
			}
			return m.dispatchAction(item.action)
		}

		if action, ok := m.actions[key]; ok {
			return m.dispatchAction(action)
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// dispatchAction handles the selected action. Build and Open run async within
// the TUI. Actions with a Preload callback start loading before exiting.
// All other actions exit immediately.
func (m hubModel) dispatchAction(action HubAction) (tea.Model, tea.Cmd) {
	switch action {
	case HubActionBuild:
		return m.startBuild()
	case HubActionOpen:
		return m.startOpen()
	default:
		m.action = action
		// If a preload callback exists, run it before exiting.
		if m.callbacks.Preload != nil {
			preloadFn := m.callbacks.Preload
			m.busy = true
			m.busyLabel = "Loading..."
			m.viewport.SetContent(m.renderPreview())
			return m, func() tea.Msg {
				val, err := preloadFn(action)
				return preloadDoneMsg{value: val, err: err}
			}
		}
		m.quitting = true
		return m, tea.Quit
	}
}

func (m hubModel) startBuild() (tea.Model, tea.Cmd) {
	m.busy = true
	m.busyLabel = "Building devcontainer..."
	noCache := m.dirty
	if noCache {
		m.busyLabel = "Rebuilding devcontainer (no cache)..."
	}
	m.result = nil
	m.viewport.SetContent(m.renderPreview())
	buildFn := m.callbacks.Build
	return m, func() tea.Msg {
		output, err := buildFn(noCache)
		if err != nil {
			return cmdResultMsg{
				kind:    "build",
				success: false,
				detail:  filterBuildOutput(output, err),
			}
		}
		return cmdResultMsg{kind: "build", success: true}
	}
}

func (m hubModel) startOpen() (tea.Model, tea.Cmd) {
	m.busy = true
	m.busyLabel = "Opening in VS Code..."
	m.result = nil
	m.viewport.SetContent(m.renderPreview())
	openFn := m.callbacks.Open
	return m, func() tea.Msg {
		output, err := openFn()
		if err != nil {
			return cmdResultMsg{
				kind:    "open",
				success: false,
				detail:  fmt.Sprintf("%s\n%v", strings.TrimSpace(output), err),
			}
		}
		return cmdResultMsg{kind: "open", success: true}
	}
}

// filterBuildOutput extracts relevant error lines from devcontainer build output.
func filterBuildOutput(output string, err error) string {
	var b strings.Builder
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "ERROR:") ||
			strings.Contains(line, "did not complete successfully") ||
			strings.Contains(line, "exit code:") {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString(fmt.Sprintf("\n%v", err))
	return b.String()
}

func (m *hubModel) applyLayout() {
	menuW := max(m.width/3, 30)
	previewW := m.width - menuW

	m.list.SetWidth(menuW)
	m.list.SetHeight(m.height - 2)

	m.viewport.Width = previewW - 4
	m.viewport.Height = m.height - 4
	m.viewport.SetContent(m.renderPreview())
}

var (
	previewSuccessStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).PaddingLeft(1).PaddingTop(1)
	previewWarnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).PaddingLeft(1).PaddingTop(1)
	previewBusyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).PaddingLeft(1).PaddingTop(1)
	previewHintStyle    = lipgloss.NewStyle().Faint(true).PaddingLeft(1)
	previewDetailStyle  = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color("9"))
)

func (m hubModel) renderPreview() string {
	// Busy state: show spinner-like message.
	if m.busy {
		return previewBusyStyle.Render("⏳ " + m.busyLabel)
	}

	// Command result overlay.
	if m.result != nil {
		return m.renderResult()
	}

	// Normal state: CLI warnings + config preview.
	var sections []string

	if !m.cli.Installed {
		warn := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1")).
			PaddingLeft(1).PaddingTop(1)
		hint := lipgloss.NewStyle().
			Faint(true).
			PaddingLeft(1)
		sections = append(sections,
			warn.Render("⚠ devcontainer CLI not installed"),
			hint.Render("Build and Open require the devcontainer CLI."),
			hint.Render("Install via VS Code: Cmd+Shift+P →"),
			hint.Render("  \"Dev Containers: Install devcontainer CLI\""),
			"",
		)
	} else if !m.cli.HasOpen {
		hint := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			PaddingLeft(1).PaddingTop(1)
		detail := lipgloss.NewStyle().
			Faint(true).
			PaddingLeft(1)
		sections = append(sections,
			hint.Render("ℹ Open in VS Code not available"),
			detail.Render("The npm @devcontainers/cli supports Build but not Open."),
			detail.Render("For Open, install via VS Code: Cmd+Shift+P →"),
			detail.Render("  \"Dev Containers: Install devcontainer CLI\""),
			"",
		)
	}

	if len(m.config) == 0 {
		sections = append(sections,
			lipgloss.NewStyle().Faint(true).PaddingLeft(1).PaddingTop(1).
				Render("No devcontainer.json yet\n\nSelect Set Template to get started"),
		)
	} else {
		data, err := json.MarshalIndent(m.config, "", "  ")
		if err != nil {
			sections = append(sections, fmt.Sprintf("Error: %v", err))
		} else {
			sections = append(sections, colorizeJSON(string(data)))
		}
	}

	return strings.Join(sections, "\n")
}

func (m hubModel) renderResult() string {
	var sections []string

	if m.result.success {
		switch m.result.kind {
		case "build":
			sections = append(sections, previewSuccessStyle.Render("✓ Devcontainer built successfully"))
		case "open":
			sections = append(sections, previewSuccessStyle.Render("✓ VS Code opened"))
		}
	} else {
		switch m.result.kind {
		case "build":
			sections = append(sections,
				previewWarnStyle.Render("⚠ Build failed"),
				"",
				previewDetailStyle.Render(m.result.detail),
				"",
				previewHintStyle.Render("This usually means a feature's install script failed."),
				previewHintStyle.Render("Try removing the problematic feature and rebuilding."),
			)
		case "open":
			sections = append(sections,
				previewWarnStyle.Render("⚠ Failed to open VS Code"),
				"",
				previewDetailStyle.Render(m.result.detail),
			)
		}
	}

	sections = append(sections, "", previewHintStyle.Render("Press any key to continue"))
	return strings.Join(sections, "\n")
}

func (m hubModel) View() string {
	if m.quitting {
		return ""
	}
	return renderHubLayout(m.list, m.viewport.View(), "devcontainer.json", m.width, m.height)
}

// ShowHub displays the hub dashboard and returns the selected action.
// Build and Open are handled within the TUI and never returned.
// Returns the selected action, the updated dirty flag, and any preloaded data.
func ShowHub(projectName string, config map[string]any, cli template.CLIInfo, dirty bool, cb HubCallbacks) (HubAction, bool, any, error) {
	m := newHubModel(projectName, config, cli, dirty, cb)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return HubActionExit, dirty, nil, fmt.Errorf("running hub: %w", err)
	}
	fm := final.(hubModel)
	return fm.action, fm.dirty, fm.preloadedData, nil
}

// --- JSON colorization ---

var (
	jsonKeyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	jsonStrStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	jsonNumStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	jsonBoolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	jsonNullStyle = lipgloss.NewStyle().Faint(true)
)

func colorizeJSON(s string) string {
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}

		trimmed := strings.TrimSpace(line)
		indent := line[:len(line)-len(trimmed)]

		if idx := strings.Index(trimmed, `":`); idx >= 0 {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				b.WriteString(indent)
				b.WriteString(jsonKeyStyle.Render(strings.TrimSpace(parts[0])))
				b.WriteString(": ")
				b.WriteString(colorizeValue(strings.TrimSpace(parts[1])))
				continue
			}
		}

		b.WriteString(indent)
		b.WriteString(colorizeValue(trimmed))
	}
	return b.String()
}

func colorizeValue(val string) string {
	bare := strings.TrimRight(val, ",")
	switch {
	case strings.HasPrefix(bare, `"`):
		return jsonStrStyle.Render(val)
	case bare == "true" || bare == "false":
		return jsonBoolStyle.Render(val)
	case bare == "null":
		return jsonNullStyle.Render(val)
	case len(bare) > 0 && (bare[0] >= '0' && bare[0] <= '9' || bare[0] == '-'):
		return jsonNumStyle.Render(val)
	default:
		return val
	}
}
