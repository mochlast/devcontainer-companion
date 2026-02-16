package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const defaultTTL = 1 * time.Hour

type cachedCatalog struct {
	Entries   []CatalogEntry `json:"entries"`
	FetchedAt time.Time      `json:"fetchedAt"`
}

func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cache", "dcc")
	return dir, os.MkdirAll(dir, 0o755)
}

func cachePath(kind string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, kind+".json"), nil
}

// LoadCached loads cached catalog entries if the cache is still valid.
func LoadCached(kind string) ([]CatalogEntry, bool) {
	path, err := cachePath(kind)
	if err != nil {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var cached cachedCatalog
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}

	if time.Since(cached.FetchedAt) > defaultTTL {
		return nil, false
	}

	// Invalidate cache written before SourceURL was added
	if len(cached.Entries) > 0 && cached.Entries[0].SourceURL == "" {
		return nil, false
	}

	return cached.Entries, true
}

// SaveCache saves catalog entries to the cache.
func SaveCache(kind string, entries []CatalogEntry) error {
	path, err := cachePath(kind)
	if err != nil {
		return err
	}

	cached := cachedCatalog{
		Entries:   entries,
		FetchedAt: time.Now(),
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling cache: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// GetTemplates returns templates from cache or fetches them.
// Official devcontainers templates are sorted to the top.
func GetTemplates(noCache bool) ([]CatalogEntry, error) {
	if !noCache {
		if entries, ok := LoadCached("templates"); ok {
			return sortOfficialFirst(entries), nil
		}
	}

	entries, err := FetchTemplates()
	if err != nil {
		// Fallback to expired cache on error
		if cached, ok := LoadCached("templates"); ok {
			return sortOfficialFirst(cached), nil
		}
		return nil, err
	}

	_ = SaveCache("templates", entries)
	return sortOfficialFirst(entries), nil
}

// GetFeatures returns features from cache or fetches them.
// Official devcontainers features are sorted to the top.
func GetFeatures(noCache bool) ([]CatalogEntry, error) {
	if !noCache {
		if entries, ok := LoadCached("features"); ok {
			return sortOfficialFirst(entries), nil
		}
	}

	entries, err := FetchFeatures()
	if err != nil {
		// Fallback to expired cache on error
		if cached, ok := LoadCached("features"); ok {
			return sortOfficialFirst(cached), nil
		}
		return nil, err
	}

	_ = SaveCache("features", entries)
	return sortOfficialFirst(entries), nil
}

// sortOfficialFirst sorts entries so that official devcontainers entries
// (ghcr.io/devcontainers/) appear at the top, preserving relative order within
// each group.
func sortOfficialFirst(entries []CatalogEntry) []CatalogEntry {
	sort.SliceStable(entries, func(i, j int) bool {
		return IsOfficial(entries[i].OciRef) && !IsOfficial(entries[j].OciRef)
	})
	return entries
}
