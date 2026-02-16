package feature

import (
	"github.com/mochlast/devcontainer-companion/internal/devcontainer"
)

// FeatureConfig represents a feature with its OCI reference and configured options.
type FeatureConfig struct {
	OciRef  string
	Options map[string]any
}

// ReplaceAll reads the devcontainer.json, replaces the entire features map,
// and writes it back. Features not present in the given list are removed.
func ReplaceAll(workspaceFolder string, features []FeatureConfig) error {
	config, configPath, err := devcontainer.ReadConfig(workspaceFolder)
	if err != nil {
		return err
	}

	featuresMap := make(map[string]any)
	for _, f := range features {
		if len(f.Options) > 0 {
			featuresMap[f.OciRef] = f.Options
		} else {
			featuresMap[f.OciRef] = map[string]any{}
		}
	}

	config["features"] = featuresMap

	return devcontainer.WriteConfig(configPath, config)
}
