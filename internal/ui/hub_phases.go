package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/mochlast/devcontainer-companion/internal/registry"
)

// FormConfig configures a hub form phase that can include optional loading
// before and after the form, all within a single AltScreen program.
type FormConfig struct {
	// LoadLabel and LoadFn run before the form. LoadFn returns the form title
	// and option definitions. If options is nil/empty, the form is skipped.
	LoadLabel string
	LoadFn    func() (string, map[string]registry.OptionDefinition, error)

	// PostLabel and PostFn run after the form completes (e.g. to apply a template).
	PostLabel string
	PostFn    func(opts map[string]any) error
}

// --- Messages ---

type formLoadDoneMsg struct {
	title   string
	options map[string]registry.OptionDefinition
	err     error
}

type formPostDoneMsg struct {
	err error
}

// --- Model ---

type formPhase int

const (
	formPhaseLoading formPhase = iota
	formPhaseForm
	formPhasePost
)

type hubFormModel struct {
	menuList   list.Model
	phase      formPhase
	loadLabel  string
	startLoad  tea.Cmd // runs LoadFn, set during construction
	form       *huh.Form
	formTitle  string
	stringVals map[string]*string
	boolVals   map[string]*bool
	postLabel  string
	postFn     func(opts map[string]any) error
	results    map[string]any
	cancelled  bool
	skipped    bool // true if no options to configure
	width      int
	height     int
}

func (m hubFormModel) Init() tea.Cmd {
	return m.startLoad
}

func (m hubFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case formLoadDoneMsg:
		if msg.err != nil || len(msg.options) == 0 {
			m.skipped = true
			return m, tea.Quit
		}
		// Build the form from loaded options
		stringVals, boolVals, fields := buildOptionFields(msg.options)
		groups := make([]*huh.Group, len(fields))
		for i, f := range fields {
			groups[i] = huh.NewGroup(f)
		}
		m.form = huh.NewForm(groups...)
		m.formTitle = msg.title
		m.stringVals = stringVals
		m.boolVals = boolVals
		m.phase = formPhaseForm
		// Size the form to the preview panel
		menuW := max(m.width/3, 30)
		previewW := m.width - menuW - 6
		initCmd := m.form.Init()
		sizeMsg := tea.WindowSizeMsg{Width: previewW, Height: m.height - 6}
		formModel, sizeCmd := m.form.Update(sizeMsg)
		m.form = formModel.(*huh.Form)
		return m, tea.Batch(initCmd, sizeCmd)

	case formPostDoneMsg:
		return m, tea.Quit

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		menuW := max(m.width/3, 30)
		m.menuList.SetWidth(menuW)
		m.menuList.SetHeight(m.height - 2)
		if m.phase == formPhaseForm && m.form != nil {
			previewW := m.width - menuW - 6
			formModel, cmd := m.form.Update(tea.WindowSizeMsg{Width: previewW, Height: m.height - 6})
			m.form = formModel.(*huh.Form)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancelled = true
			return m, tea.Quit
		}
		// During loading/post phases, ignore all other keys
		if m.phase != formPhaseForm {
			return m, nil
		}
	}

	// In form phase, forward all messages to the embedded form
	if m.phase == formPhaseForm && m.form != nil {
		formModel, cmd := m.form.Update(msg)
		m.form = formModel.(*huh.Form)

		if m.form.State == huh.StateAborted {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.form.State == huh.StateCompleted {
			m.results = collectOptionResults(m.stringVals, m.boolVals)
			// Run post-action if configured
			if m.postFn != nil {
				m.phase = formPhasePost
				m.menuList.SetWidth(max(m.width/3, 30))
				postFn := m.postFn
				results := m.results
				return m, func() tea.Msg {
					err := postFn(results)
					return formPostDoneMsg{err: err}
				}
			}
			return m, tea.Quit
		}
		return m, cmd
	}

	return m, nil
}

func (m hubFormModel) View() string {
	if m.cancelled || m.skipped {
		return ""
	}

	var content string
	var title string

	switch m.phase {
	case formPhaseLoading:
		content = previewBusyStyle.Render("⏳ " + m.loadLabel)
		title = "devcontainer.json"
	case formPhaseForm:
		if m.form != nil {
			content = m.form.View()
		}
		title = m.formTitle
	case formPhasePost:
		content = previewBusyStyle.Render("⏳ " + m.postLabel)
		title = "devcontainer.json"
	}

	return renderHubLayout(m.menuList, content, title, m.width, m.height)
}

// ShowHubForm displays a hub-layout form with optional loading before and after.
// If LoadFn returns no options, the form is skipped and nil is returned.
// Returns the configured option values, or nil if cancelled/skipped.
func ShowHubForm(ctx HubContext, cfg FormConfig) (map[string]any, error) {
	l := newHubMenuList(ctx.ProjectName, ctx.CLI, ctx.Dirty)
	loadFn := cfg.LoadFn

	m := hubFormModel{
		menuList:  l,
		phase:     formPhaseLoading,
		loadLabel: cfg.LoadLabel,
		postLabel: cfg.PostLabel,
		postFn:    cfg.PostFn,
		startLoad: func() tea.Msg {
			title, options, err := loadFn()
			return formLoadDoneMsg{title: title, options: options, err: err}
		},
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running form: %w", err)
	}

	fm := final.(hubFormModel)
	if fm.cancelled || fm.skipped {
		return nil, nil
	}
	return fm.results, nil
}

// --- Shared option field builders ---

func buildOptionFields(options map[string]registry.OptionDefinition) (map[string]*string, map[string]*bool, []huh.Field) {
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	stringVals := make(map[string]*string)
	boolVals := make(map[string]*bool)
	var fields []huh.Field

	for _, key := range keys {
		opt := options[key]
		fieldTitle := key
		if opt.Description != "" {
			fieldTitle = fmt.Sprintf("%s (%s)", key, opt.Description)
		}

		switch opt.Type {
		case "boolean":
			defaultVal := false
			if opt.Default != nil {
				switch v := opt.Default.(type) {
				case bool:
					defaultVal = v
				case string:
					defaultVal = v == "true"
				}
			}
			val := defaultVal
			boolVals[key] = &val
			fields = append(fields, huh.NewConfirm().
				Title(fieldTitle).
				Value(boolVals[key]))

		default:
			defaultStr := defaultToString(opt.Default)

			if len(opt.Enum) > 0 {
				val := defaultStr
				if val == "" && len(opt.Enum) > 0 {
					val = opt.Enum[0]
				}
				stringVals[key] = &val

				enumOpts := make([]huh.Option[string], len(opt.Enum))
				for i, e := range opt.Enum {
					enumOpts[i] = huh.NewOption(e, e)
				}

				fields = append(fields, huh.NewSelect[string]().
					Title(fieldTitle).
					Options(enumOpts...).
					Value(stringVals[key]))

			} else {
				val := defaultStr
				stringVals[key] = &val

				input := huh.NewInput().
					Title(fieldTitle).
					Value(stringVals[key])

				if len(opt.Proposals) > 0 {
					input = input.Placeholder(strings.Join(opt.Proposals, ", "))
				}

				fields = append(fields, input)
			}
		}
	}

	return stringVals, boolVals, fields
}

func collectOptionResults(stringVals map[string]*string, boolVals map[string]*bool) map[string]any {
	results := make(map[string]any)
	for key, ptr := range stringVals {
		results[key] = *ptr
	}
	for key, ptr := range boolVals {
		results[key] = strconv.FormatBool(*ptr)
	}
	return results
}
