package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/mochlast/devcontainer-companion/internal/catalog"
	"github.com/mochlast/devcontainer-companion/internal/devcontainer"
	"github.com/mochlast/devcontainer-companion/internal/feature"
	"github.com/mochlast/devcontainer-companion/internal/registry"
	"github.com/mochlast/devcontainer-companion/internal/template"
	"github.com/mochlast/devcontainer-companion/internal/ui"
)

// runHub is the main hub loop. It shows the dashboard, dispatches to the
// selected sub-flow, and loops back. Each sub-flow reads/writes config to
// disk, so the hub always shows the latest state. A dirty flag tracks whether
// config has changed since the last successful build. Build and Open run
// within the hub TUI itself.
func runHub(absFolder string, noCache bool) error {
	projectName := filepath.Base(absFolder)
	cli := template.DetectCLI()
	dirty := false

	// Clear the normal buffer so AltScreen transitions don't flash old content.
	fmt.Print("\033[2J\033[H")

	cb := ui.HubCallbacks{
		Build: func(noCache bool) (string, error) {
			return devcontainerBuild(absFolder, noCache)
		},
		Open: func() (string, error) {
			return devcontainerOpen(absFolder)
		},
		Preload: func(action ui.HubAction) (any, error) {
			switch action {
			case ui.HubActionTemplate:
				return catalog.GetTemplates(noCache)
			case ui.HubActionFeatures:
				return catalog.GetFeatures(noCache)
			default:
				return nil, nil
			}
		},
	}

	for {
		var config map[string]any
		if devcontainer.Exists(absFolder) {
			if c, _, err := devcontainer.ReadConfig(absFolder); err == nil {
				config = c
			}
		}

		action, newDirty, preloaded, err := ui.ShowHub(projectName, config, cli, dirty, cb)
		if err != nil {
			return err
		}
		dirty = newDirty

		ctx := ui.HubContext{
			ProjectName: projectName,
			Config:      config,
			CLI:         cli,
			Dirty:       dirty,
		}

		switch action {
		case ui.HubActionTemplate:
			err = runTemplateFlow(absFolder, projectName, noCache, ctx, preloaded)
			dirty = true
		case ui.HubActionFeatures:
			err = runFeaturesFlow(absFolder, noCache, ctx, preloaded)
			dirty = true
		case ui.HubActionExtensions:
			err = runExtensionsFlow(absFolder)
			dirty = true
		case ui.HubActionPlugins:
			err = runPluginsFlow(absFolder)
			dirty = true
		case ui.HubActionCustomizations:
			err = runCustomizationsFlow(absFolder)
			dirty = true
		case ui.HubActionExit:
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func runTemplateFlow(absFolder, projectName string, noCache bool, ctx ui.HubContext, preloaded any) error {
	// Use preloaded catalog data if available, otherwise load now
	var templates []catalog.CatalogEntry
	if preloaded != nil {
		templates = preloaded.([]catalog.CatalogEntry)
	} else {
		loaded, err := catalog.GetTemplates(noCache)
		if err != nil {
			return fmt.Errorf("loading template catalog: %w", err)
		}
		templates = loaded
	}

	// Pick a template
	selected, err := ui.PickTemplate(templates)
	if errors.Is(err, ui.ErrPickerCancelled) {
		return nil
	}
	if err != nil {
		return err
	}

	// No template selected → create empty
	if selected == nil {
		return template.CreateEmpty(absFolder, projectName)
	}

	// Fetch template metadata + configure options + apply — all in one TUI program
	ociRef := ui.FormatOciRefWithVersion(selected)

	if template.IsDevcontainerCLIAvailable() {
		_, err = ui.ShowHubForm(ctx, ui.FormConfig{
			LoadLabel: fmt.Sprintf("Loading options for %s...", selected.Name),
			LoadFn: func() (string, map[string]registry.OptionDefinition, error) {
				tmplDef, _, err := registry.FetchItemMetadata(ociRef)
				if err != nil {
					return "", nil, err
				}
				return fmt.Sprintf("Configure %s options:", selected.Name), tmplDef.Options, nil
			},
			PostLabel: "Applying template...",
			PostFn: func(opts map[string]any) error {
				return template.Apply(absFolder, ociRef, opts)
			},
		})
		if err != nil {
			return fmt.Errorf("configuring/applying template: %w", err)
		}
	} else {
		// No CLI available — just fetch metadata to show options form, then create empty
		_, err = ui.ShowHubForm(ctx, ui.FormConfig{
			LoadLabel: fmt.Sprintf("Loading options for %s...", selected.Name),
			LoadFn: func() (string, map[string]registry.OptionDefinition, error) {
				tmplDef, _, err := registry.FetchItemMetadata(ociRef)
				if err != nil {
					return "", nil, err
				}
				return fmt.Sprintf("Configure %s options:", selected.Name), tmplDef.Options, nil
			},
		})
		if err != nil {
			return err
		}
		return template.CreateEmpty(absFolder, projectName)
	}

	return nil
}

func runFeaturesFlow(absFolder string, noCache bool, ctx ui.HubContext, preloaded any) error {
	existingOpts := make(map[string]map[string]any)
	existingRefs := make(map[string]bool)
	if devcontainer.Exists(absFolder) {
		if config, _, err := devcontainer.ReadConfig(absFolder); err == nil {
			if feats, ok := config["features"].(map[string]any); ok {
				for ref, opts := range feats {
					bare := stripVersion(ref)
					existingRefs[bare] = true
					if m, ok := opts.(map[string]any); ok {
						existingOpts[bare] = m
					}
				}
			}
		}
	}

	// Use preloaded catalog data if available, otherwise load now
	var features []catalog.CatalogEntry
	if preloaded != nil {
		features = preloaded.([]catalog.CatalogEntry)
	} else {
		loaded, err := catalog.GetFeatures(noCache)
		if err != nil {
			return nil // non-fatal: catalog load failure
		}
		features = loaded
	}

	preSelected := make(map[string]bool)
	for _, entry := range features {
		if existingRefs[stripVersion(entry.OciRef)] {
			preSelected[entry.OciRef] = true
		}
	}

	// Pick features
	selected, err := ui.PickFeaturesWithSelection(features, preSelected)
	if errors.Is(err, ui.ErrPickerCancelled) {
		return nil
	}
	if err != nil {
		return err
	}

	// Configure each new feature
	var configs []feature.FeatureConfig
	for _, f := range selected {
		ociRef := ui.FormatFeatureOciRef(&f)
		bare := stripVersion(f.OciRef)

		// Keep existing configuration for previously selected features
		if prev, existed := existingOpts[bare]; existed {
			configs = append(configs, feature.FeatureConfig{OciRef: ociRef, Options: prev})
			continue
		}

		// Fetch metadata + configure options in one TUI program
		opts, err := ui.ShowHubForm(ctx, ui.FormConfig{
			LoadLabel: fmt.Sprintf("Loading options for %s...", f.Name),
			LoadFn: func() (string, map[string]registry.OptionDefinition, error) {
				_, featDef, err := registry.FetchItemMetadata(ociRef)
				if err != nil {
					return "", nil, err
				}
				return fmt.Sprintf("Configure %s options:", f.Name), featDef.Options, nil
			},
		})
		if err != nil {
			return err
		}
		configs = append(configs, feature.FeatureConfig{OciRef: ociRef, Options: opts})
	}

	// Write features
	if err := feature.ReplaceAll(absFolder, configs); err != nil {
		return fmt.Errorf("replacing features: %w", err)
	}
	return nil
}

func runExtensionsFlow(absFolder string) error {
	existing := extractStringList(absFolder, "customizations", "vscode", "extensions")

	selected, err := ui.PickExtensions(existing)
	if errors.Is(err, ui.ErrPickerCancelled) {
		return nil
	}
	if err != nil {
		return err
	}

	sort.Strings(selected)
	return writeCustomizationList(absFolder, selected, "vscode", "extensions")
}

func runPluginsFlow(absFolder string) error {
	existing := extractStringList(absFolder, "customizations", "jetbrains", "plugins")

	selected, err := ui.PickPlugins(existing)
	if errors.Is(err, ui.ErrPickerCancelled) {
		return nil
	}
	if err != nil {
		return err
	}

	sort.Strings(selected)
	return writeCustomizationList(absFolder, selected, "jetbrains", "plugins")
}

func runCustomizationsFlow(absFolder string) error {
	return ui.EditCustomizations(absFolder)
}

// --- helpers ---

func extractStringList(absFolder, topKey, ideKey, listKey string) map[string]bool {
	result := make(map[string]bool)
	if !devcontainer.Exists(absFolder) {
		return result
	}
	config, _, err := devcontainer.ReadConfig(absFolder)
	if err != nil {
		return result
	}
	top, _ := config[topKey].(map[string]any)
	ide, _ := top[ideKey].(map[string]any)
	items, _ := ide[listKey].([]any)
	for _, item := range items {
		if s, ok := item.(string); ok {
			result[s] = true
		}
	}
	return result
}

func writeCustomizationList(absFolder string, items []string, ideKey, listKey string) error {
	config, configPath, err := devcontainer.ReadConfig(absFolder)
	if err != nil {
		return err
	}

	customizations, _ := config["customizations"].(map[string]any)
	if customizations == nil {
		customizations = make(map[string]any)
	}
	ide, _ := customizations[ideKey].(map[string]any)
	if ide == nil {
		ide = make(map[string]any)
	}

	if len(items) > 0 {
		ide[listKey] = items
	} else {
		delete(ide, listKey)
	}

	if len(ide) > 0 {
		customizations[ideKey] = ide
	} else {
		delete(customizations, ideKey)
	}
	if len(customizations) > 0 {
		config["customizations"] = customizations
	} else {
		delete(config, "customizations")
	}

	return devcontainer.WriteConfig(configPath, config)
}
