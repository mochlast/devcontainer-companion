package devcontainer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"
)

// ReadConfig reads and parses the devcontainer.json from a workspace folder.
// Supports JSONC (JSON with comments).
func ReadConfig(workspaceFolder string) (map[string]any, string, error) {
	configPath := filepath.Join(workspaceFolder, ".devcontainer", "devcontainer.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, configPath, fmt.Errorf("reading devcontainer.json: %w", err)
	}

	// Strip JSONC comments
	cleanJSON := jsonc.ToJSON(data)

	var config map[string]any
	if err := json.Unmarshal(cleanJSON, &config); err != nil {
		return nil, configPath, fmt.Errorf("parsing devcontainer.json: %w", err)
	}

	return config, configPath, nil
}

// WriteConfig writes a config map to the given path as formatted JSON with 2-space indent.
// If the file already exists, the key ordering from the existing file is preserved.
// New keys are appended in alphabetical order.
func WriteConfig(path string, config map[string]any) error {
	// Read existing file to preserve key ordering.
	var order *keyOrder
	if existing, err := os.ReadFile(path); err == nil {
		order = extractKeyOrder(jsonc.ToJSON(existing))
	}

	data := marshalOrdered(config, order)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing devcontainer.json: %w", err)
	}

	return nil
}

// Exists checks if a .devcontainer directory exists in the workspace folder.
func Exists(workspaceFolder string) bool {
	_, err := os.Stat(filepath.Join(workspaceFolder, ".devcontainer"))
	return err == nil
}
