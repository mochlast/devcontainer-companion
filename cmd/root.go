package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mochlast/devcontainer-companion/internal/devcontainer"
	"github.com/mochlast/devcontainer-companion/internal/template"
)

var version = "dev"

var (
	workspaceFolder string
	noCache         bool
)

var rootCmd = &cobra.Command{
	Use:     "dcc",
	Short:   "Devcontainer CLI Companion",
	Long:    "dcc helps you create and configure devcontainers interactively.",
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		absFolder, err := filepath.Abs(workspaceFolder)
		if err != nil {
			return fmt.Errorf("resolving workspace folder: %w", err)
		}
		if !devcontainer.Exists(absFolder) {
			projectName := filepath.Base(absFolder)
			if err := template.CreateEmpty(absFolder, projectName); err != nil {
				return err
			}
		}
		return runHub(absFolder, noCache)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&workspaceFolder, "workspace-folder", "w", ".", "workspace folder path")
	rootCmd.PersistentFlags().BoolVar(&noCache, "no-cache", false, "bypass catalog cache")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
