package cmd

import (
	"os/exec"
	"strings"
)

// stripVersion removes the version tag from an OCI reference.
// e.g. "ghcr.io/devcontainers/features/java:1" -> "ghcr.io/devcontainers/features/java"
func stripVersion(ref string) string {
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		return ref[:idx]
	}
	return ref
}

// devcontainerBuild runs 'devcontainer build'. If noCache is true, Docker layer
// cache is skipped for a full rebuild.
func devcontainerBuild(folder string, noCache bool) (string, error) {
	args := []string{"build", "--workspace-folder", folder}
	if noCache {
		args = append(args, "--no-cache")
	}
	cmd := exec.Command("devcontainer", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// devcontainerOpen runs 'devcontainer open' to build, start, and connect VS Code.
func devcontainerOpen(folder string) (string, error) {
	cmd := exec.Command("devcontainer", "open", folder)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
