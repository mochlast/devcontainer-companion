package template

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Apply runs `devcontainer templates apply` to apply a template to a workspace folder.
// Uses cd instead of -w flag to work around a bug in the VS Code CLI (v0.442.0)
// where -w causes "paths[1] must be of type string" TypeError.
func Apply(workspaceFolder string, ociRef string, options map[string]any) error {
	if err := checkDevcontainerCLI(); err != nil {
		return err
	}

	args := []string{
		"templates", "apply",
		"-t", ociRef,
	}

	if len(options) > 0 {
		optJSON, err := json.Marshal(options)
		if err != nil {
			return fmt.Errorf("marshaling template options: %w", err)
		}
		args = append(args, "-a", string(optJSON))
	}

	cmd := exec.Command("devcontainer", args...)
	cmd.Dir = workspaceFolder
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("applying template: %w\nOutput: %s", err, string(output))
	}

	return nil
}

func checkDevcontainerCLI() error {
	_, err := exec.LookPath("devcontainer")
	if err != nil {
		return fmt.Errorf("devcontainer CLI not found in PATH — install via VS Code: Cmd+Shift+P → \"Dev Containers: Install devcontainer CLI\"")
	}
	return nil
}

// CLIInfo describes the available devcontainer CLI capabilities.
type CLIInfo struct {
	Installed bool // devcontainer binary found in PATH
	HasOpen   bool // supports 'devcontainer open' (VS Code CLI)
}

// DetectCLI probes the installed devcontainer CLI and returns its capabilities.
func DetectCLI() CLIInfo {
	if checkDevcontainerCLI() != nil {
		return CLIInfo{}
	}
	info := CLIInfo{Installed: true}

	// 'devcontainer open --help' exits 0 only on the VS Code-installed CLI.
	// The npm @devcontainers/cli doesn't have the 'open' subcommand.
	if err := exec.Command("devcontainer", "open", "--help").Run(); err == nil {
		info.HasOpen = true
	}

	return info
}

// IsDevcontainerCLIAvailable returns true if any devcontainer CLI is in PATH.
func IsDevcontainerCLIAvailable() bool {
	return checkDevcontainerCLI() == nil
}
