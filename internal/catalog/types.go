package catalog

import "strings"

// CatalogEntry represents a template or feature from the containers.dev catalog.
type CatalogEntry struct {
	Name       string `json:"name"`
	Maintainer string `json:"maintainer"`
	OciRef     string `json:"ociRef"`
	Version    string `json:"version"`
	SourceURL  string `json:"sourceURL,omitempty"`
}

// FilterValue returns the string used for fuzzy-filtering in the TUI picker.
func (e CatalogEntry) FilterValue() string {
	return e.Name + " " + e.Maintainer
}

// IsOfficial returns true if the OCI reference belongs to the official
// devcontainers collection (ghcr.io/devcontainers/).
func IsOfficial(ociRef string) bool {
	return strings.HasPrefix(ociRef, "ghcr.io/devcontainers/")
}
