package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/mochlast/devcontainer-companion/internal/devcontainer"
)

// settingKey identifies which setting to edit.
type settingKey string

const (
	skName             settingKey = "name"
	skRemoteUser       settingKey = "remoteUser"
	skShutdownAction   settingKey = "shutdownAction"
	skInit             settingKey = "init"
	skPrivileged       settingKey = "privileged"
	skForwardPorts     settingKey = "forwardPorts"
	skPostCreateCmd    settingKey = "postCreateCommand"
	skPostStartCmd     settingKey = "postStartCommand"
	skPostAttachCmd    settingKey = "postAttachCommand"
	skWaitFor          settingKey = "waitFor"
	skContainerEnv     settingKey = "containerEnv"
	skRemoteEnv        settingKey = "remoteEnv"
	skMounts           settingKey = "mounts"
	skCapAdd           settingKey = "capAdd"
	skRunArgs          settingKey = "runArgs"
	skBack             settingKey = "back"
)

type settingsMenuItem struct {
	key         settingKey
	label       string
	description string
	section     string
}

func (i settingsMenuItem) FilterValue() string { return i.label }
func (i settingsMenuItem) Title() string       { return i.label }
func (i settingsMenuItem) Description() string { return i.description }

type settingsDelegate struct{}

func (d settingsDelegate) Height() int                             { return 2 }
func (d settingsDelegate) Spacing() int                            { return 0 }
func (d settingsDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d settingsDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(settingsMenuItem)
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
		title = "> " + title
	} else {
		title = "  " + title
	}

	maxW := m.Width()
	fmt.Fprintf(w, "%s\n%s", titleStyle.MaxWidth(maxW).Render(title), descStyle.MaxWidth(maxW).Render(item.description))
}

type settingsModel struct {
	list     list.Model
	viewport viewport.Model
	config   map[string]any
	selected settingKey
	quitting bool
	width    int
	height   int
}

var settingsItems = []settingsMenuItem{
	{skName, "Name", "Display name for this devcontainer", "General"},
	{skRemoteUser, "Remote User", "User for tool connections (e.g. vscode)", "General"},
	{skShutdownAction, "Shutdown Action", "What to do when the IDE closes", "General"},
	{skInit, "Init Process", "Enable tini init for proper signal handling", "General"},
	{skPrivileged, "Privileged", "Needed for Docker-in-Docker", "General"},
	{skForwardPorts, "Forward Ports", "Comma-separated (e.g. 3000, 5432, 8080)", "Ports"},
	{skPostCreateCmd, "Post-Create Command", "Runs once after container creation", "Lifecycle"},
	{skPostStartCmd, "Post-Start Command", "Runs on every container start", "Lifecycle"},
	{skPostAttachCmd, "Post-Attach Command", "Runs on every IDE attach", "Lifecycle"},
	{skWaitFor, "Wait For", "Which command to wait for before connecting", "Lifecycle"},
	{skContainerEnv, "Container Env", "KEY=VALUE per line, set on container", "Environment"},
	{skRemoteEnv, "Remote Env", "KEY=VALUE per line, set for tools", "Environment"},
	{skMounts, "Mounts", "One per line, ${localWorkspaceFolder} for portability", "Advanced"},
	{skCapAdd, "Linux Capabilities", "Comma-separated (e.g. SYS_PTRACE)", "Advanced"},
	{skRunArgs, "Docker Run Args", "Comma-separated extra arguments", "Advanced"},
	{skBack, "Back", "Return to hub", ""},
}

func newSettingsModel(config map[string]any) settingsModel {
	items := make([]list.Item, len(settingsItems))
	for i, s := range settingsItems {
		items[i] = s
	}

	l := list.New(items, settingsDelegate{}, 30, 20)
	l.Title = "Edit Settings"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginLeft(2)

	return settingsModel{
		list:   l,
		config: config,
	}
}

func (m settingsModel) Init() tea.Cmd { return nil }

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.selected = skBack
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if item, ok := m.list.SelectedItem().(settingsMenuItem); ok {
				m.selected = item.key
				m.quitting = true
				return m, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *settingsModel) applyLayout() {
	menuW := max(m.width/3, 30)
	previewW := m.width - menuW

	m.list.SetWidth(menuW)
	m.list.SetHeight(m.height - 2)

	m.viewport.Width = previewW - 4
	m.viewport.Height = m.height - 4
	m.viewport.SetContent(m.renderPreview())
}

func (m settingsModel) renderPreview() string {
	if len(m.config) == 0 {
		return lipgloss.NewStyle().Faint(true).PaddingLeft(1).PaddingTop(1).
			Render("No settings configured yet")
	}

	// Show only the settings-relevant keys (not features/customizations)
	preview := make(map[string]any)
	for _, item := range settingsItems {
		key := string(item.key)
		if key == "back" {
			continue
		}
		if v, ok := m.config[key]; ok {
			preview[key] = v
		}
	}

	if len(preview) == 0 {
		return lipgloss.NewStyle().Faint(true).PaddingLeft(1).PaddingTop(1).
			Render("All settings at defaults")
	}

	data, err := json.MarshalIndent(preview, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return colorizeJSON(string(data))
}

func (m settingsModel) View() string {
	if m.quitting {
		return ""
	}

	menuW := max(m.width/3, 30)
	previewW := m.width - menuW

	menuView := "\n" + m.list.View()
	menuClipped := lipgloss.NewStyle().Width(menuW).MaxWidth(menuW).Render(menuView)

	previewTitle := lipgloss.NewStyle().
		Bold(true).Foreground(lipgloss.Color("170")).PaddingLeft(1).
		Render("Current Settings")

	previewBody := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("170")).
		Width(previewW - 2).Height(m.height - 4).
		Render(m.viewport.View())

	preview := lipgloss.JoinVertical(lipgloss.Left, previewTitle, previewBody)
	return lipgloss.JoinHorizontal(lipgloss.Top, menuClipped, preview)
}

// showSettingsMenu displays the settings submenu and returns which setting to edit.
func showSettingsMenu(config map[string]any) (settingKey, error) {
	m := newSettingsModel(config)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return skBack, fmt.Errorf("running settings menu: %w", err)
	}
	return final.(settingsModel).selected, nil
}

// EditCustomizations runs the settings submenu loop. Each iteration shows the
// menu, lets the user pick a setting, edits it with a small huh form, writes
// back to disk, and loops.
func EditCustomizations(absFolder string) error {
	for {
		config, configPath, err := devcontainer.ReadConfig(absFolder)
		if err != nil {
			return err
		}

		key, err := showSettingsMenu(config)
		if err != nil {
			return err
		}
		if key == skBack {
			return nil
		}

		changed, err := editSetting(config, key)
		if err != nil {
			return err
		}
		if changed {
			if err := devcontainer.WriteConfig(configPath, config); err != nil {
				return err
			}
		}
	}
}

// editSetting opens a small huh form for a single setting. Returns true if the
// config was modified.
func editSetting(config map[string]any, key settingKey) (bool, error) {
	switch key {
	case skName:
		return editStringField(config, "name", "Name", "Display name for this devcontainer")
	case skRemoteUser:
		return editStringField(config, "remoteUser", "Remote User", "User for tool connections (e.g. vscode)")
	case skShutdownAction:
		return editSelectField(config, "shutdownAction", "Shutdown Action",
			[]string{"none", "stopContainer"}, "none")
	case skInit:
		return editBoolField(config, "init", "Init Process", "Enable tini init for proper signal handling")
	case skPrivileged:
		return editBoolField(config, "privileged", "Privileged", "Needed for Docker-in-Docker")
	case skForwardPorts:
		return editPortsField(config)
	case skPostCreateCmd:
		return editStringField(config, "postCreateCommand", "Post-Create Command", "Runs once after container creation (e.g. npm install)")
	case skPostStartCmd:
		return editStringField(config, "postStartCommand", "Post-Start Command", "Runs on every container start")
	case skPostAttachCmd:
		return editStringField(config, "postAttachCommand", "Post-Attach Command", "Runs on every IDE attach")
	case skWaitFor:
		return editSelectField(config, "waitFor", "Wait For",
			[]string{"updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand"},
			"updateContentCommand")
	case skContainerEnv:
		return editEnvField(config, "containerEnv", "Container Env", "KEY=VALUE per line, set on Docker container")
	case skRemoteEnv:
		return editEnvField(config, "remoteEnv", "Remote Env", "KEY=VALUE per line, set for tool processes")
	case skMounts:
		return editTextSliceField(config, "mounts", "Mounts", "One per line, use ${localWorkspaceFolder} for portability")
	case skCapAdd:
		return editCSVField(config, "capAdd", "Linux Capabilities", "Comma-separated (e.g. SYS_PTRACE)")
	case skRunArgs:
		return editCSVField(config, "runArgs", "Docker Run Args", "Comma-separated extra docker run arguments")
	}
	return false, nil
}

// --- single-field editors ---

func editStringField(config map[string]any, key, title, desc string) (bool, error) {
	val := getString(config, key)
	before := val

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(title).Description(desc).Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing %s: %w", key, err)
	}

	val = strings.TrimSpace(val)
	if val == before {
		return false, nil
	}
	setString(config, key, val)
	return true, nil
}

func editBoolField(config map[string]any, key, title, desc string) (bool, error) {
	val := getBool(config, key)
	before := val

	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Description(desc).Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing %s: %w", key, err)
	}

	if val == before {
		return false, nil
	}
	setBool(config, key, val)
	return true, nil
}

func editSelectField(config map[string]any, key, title string, options []string, defaultVal string) (bool, error) {
	val := getString(config, key)
	if val == "" {
		val = defaultVal
	}
	before := val

	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Options(opts...).Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing %s: %w", key, err)
	}

	if val == before {
		return false, nil
	}
	setDefault(config, key, val, defaultVal)
	return true, nil
}

func editPortsField(config map[string]any) (bool, error) {
	val := joinPorts(config)
	before := val

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Forward Ports").
			Description("Comma-separated (e.g. 3000, 5432, 8080)").
			Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing forwardPorts: %w", err)
	}

	if strings.TrimSpace(val) == strings.TrimSpace(before) {
		return false, nil
	}
	parsePorts(config, "forwardPorts", val)
	return true, nil
}

func editEnvField(config map[string]any, key, title, desc string) (bool, error) {
	val := joinEnv(config, key)
	before := val

	form := huh.NewForm(huh.NewGroup(
		huh.NewText().Title(title).Description(desc).Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing %s: %w", key, err)
	}

	if strings.TrimSpace(val) == strings.TrimSpace(before) {
		return false, nil
	}
	parseEnv(config, key, val)
	return true, nil
}

func editTextSliceField(config map[string]any, key, title, desc string) (bool, error) {
	val := joinSlice(config, key)
	before := val

	form := huh.NewForm(huh.NewGroup(
		huh.NewText().Title(title).Description(desc).Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing %s: %w", key, err)
	}

	if strings.TrimSpace(val) == strings.TrimSpace(before) {
		return false, nil
	}
	parseLines(config, key, val)
	return true, nil
}

func editCSVField(config map[string]any, key, title, desc string) (bool, error) {
	val := joinCSV(config, key)
	before := val

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(title).Description(desc).Value(&val),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("editing %s: %w", key, err)
	}

	if strings.TrimSpace(val) == strings.TrimSpace(before) {
		return false, nil
	}
	parseCSV(config, key, val)
	return true, nil
}

// --- config read helpers ---

func getString(config map[string]any, key string) string {
	s, _ := config[key].(string)
	return s
}

func getBool(config map[string]any, key string) bool {
	b, _ := config[key].(bool)
	return b
}

func joinPorts(config map[string]any) string {
	ports, _ := config["forwardPorts"].([]any)
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		switch v := p.(type) {
		case float64:
			parts = append(parts, strconv.Itoa(int(v)))
		default:
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(parts, ", ")
}

func joinEnv(config map[string]any, key string) string {
	env, _ := config[key].(map[string]any)
	lines := make([]string, 0, len(env))
	for k, v := range env {
		lines = append(lines, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(lines, "\n")
}

func joinSlice(config map[string]any, key string) string {
	arr, _ := config[key].([]any)
	lines := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

func joinCSV(config map[string]any, key string) string {
	arr, _ := config[key].([]any)
	parts := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// --- config write helpers ---

func setString(config map[string]any, key, val string) {
	if val == "" {
		delete(config, key)
	} else {
		config[key] = val
	}
}

func setBool(config map[string]any, key string, val bool) {
	if val {
		config[key] = true
	} else {
		delete(config, key)
	}
}

func setDefault(config map[string]any, key, val, defaultVal string) {
	if val != defaultVal {
		config[key] = val
	} else {
		delete(config, key)
	}
}

func parsePorts(config map[string]any, key, val string) {
	if strings.TrimSpace(val) == "" {
		delete(config, key)
		return
	}
	var ports []any
	for _, p := range strings.Split(val, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			ports = append(ports, n)
		} else {
			ports = append(ports, p)
		}
	}
	if len(ports) == 0 {
		delete(config, key)
	} else {
		config[key] = ports
	}
}

func parseEnv(config map[string]any, key, val string) {
	if strings.TrimSpace(val) == "" {
		delete(config, key)
		return
	}
	env := make(map[string]any)
	for _, line := range strings.Split(val, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	if len(env) == 0 {
		delete(config, key)
	} else {
		config[key] = env
	}
}

func parseLines(config map[string]any, key, val string) {
	if strings.TrimSpace(val) == "" {
		delete(config, key)
		return
	}
	var items []any
	for _, line := range strings.Split(val, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	if len(items) == 0 {
		delete(config, key)
	} else {
		config[key] = items
	}
}

func parseCSV(config map[string]any, key, val string) {
	if strings.TrimSpace(val) == "" {
		delete(config, key)
		return
	}
	var items []any
	for _, p := range strings.Split(val, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			items = append(items, p)
		}
	}
	if len(items) == 0 {
		delete(config, key)
	} else {
		config[key] = items
	}
}
