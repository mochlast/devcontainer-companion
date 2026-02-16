package template

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CreateEmpty creates a minimal .devcontainer/devcontainer.json in the given workspace folder.
func CreateEmpty(workspaceFolder string, projectName string) error {
	devcontainerDir := filepath.Join(workspaceFolder, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0o755); err != nil {
		return fmt.Errorf("creating .devcontainer directory: %w", err)
	}

	config := map[string]any{
		"name":  projectName,
		"image": "mcr.microsoft.com/devcontainers/base:ubuntu",
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing devcontainer.json: %w", err)
	}

	return nil
}
