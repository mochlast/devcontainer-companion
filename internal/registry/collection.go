package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// CollectionMetadata represents the devcontainer-collection.json from an OCI registry.
type CollectionMetadata struct {
	Templates []TemplateDefinition `json:"templates"`
	Features  []FeatureDefinition  `json:"features"`
}

// TemplateDefinition represents a template within a collection.
type TemplateDefinition struct {
	ID          string                    `json:"id"`
	Version     string                    `json:"version"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Options     map[string]OptionDefinition `json:"options"`
}

// FeatureDefinition represents a feature within a collection.
type FeatureDefinition struct {
	ID          string                    `json:"id"`
	Version     string                    `json:"version"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Options     map[string]OptionDefinition `json:"options"`
}

// OptionDefinition represents an option for a template or feature.
type OptionDefinition struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Default     any      `json:"default"`
	Proposals   []string `json:"proposals,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// collectionCache caches fetched collection metadata.
var (
	collectionCache   = make(map[string]*CollectionMetadata)
	collectionCacheMu sync.Mutex
)

// FetchCollectionMetadata fetches the devcontainer-collection.json for a given OCI reference.
// The ociRef should point to any template/feature in the collection; the function
// resolves the collection root automatically.
func FetchCollectionMetadata(ociRef string) (*CollectionMetadata, error) {
	// Extract collection base from the OCI reference.
	// e.g., "ghcr.io/devcontainers/templates/python:1" → collection is "ghcr.io/devcontainers/templates"
	collectionBase := extractCollectionBase(ociRef)

	collectionCacheMu.Lock()
	if cached, ok := collectionCache[collectionBase]; ok {
		collectionCacheMu.Unlock()
		return cached, nil
	}
	collectionCacheMu.Unlock()

	client := NewClient()

	registry, repository, tag, err := ParseOciRef(collectionBase + ":latest")
	if err != nil {
		return nil, fmt.Errorf("parsing collection OCI ref: %w", err)
	}

	// The devcontainer-collection.json is stored as a special tag "latest" on the collection root
	// But actually, each individual template/feature has its own manifest with a layer containing
	// devcontainer-template.json or devcontainer-feature.json.
	// The collection metadata is at the collection root with tag "latest".
	manifest, err := client.GetManifest(registry, repository, tag)
	if err != nil {
		return nil, fmt.Errorf("fetching collection manifest: %w", err)
	}

	// Find the layer with the collection metadata
	for _, layer := range manifest.Layers {
		blob, err := client.GetBlob(registry, repository, layer.Digest)
		if err != nil {
			continue
		}

		// Try to extract devcontainer-collection.json from the tgz
		metadata, err := extractCollectionJSON(blob)
		if err != nil {
			continue
		}

		collectionCacheMu.Lock()
		collectionCache[collectionBase] = metadata
		collectionCacheMu.Unlock()

		return metadata, nil
	}

	return nil, fmt.Errorf("devcontainer-collection.json not found in %s", collectionBase)
}

// FetchItemMetadata fetches metadata for a specific template or feature from its OCI reference.
// It looks for devcontainer-template.json or devcontainer-feature.json in the item's OCI layers.
func FetchItemMetadata(ociRef string) (*TemplateDefinition, *FeatureDefinition, error) {
	client := NewClient()

	registry, repository, tag, err := ParseOciRef(ociRef)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing OCI ref: %w", err)
	}

	manifest, err := client.GetManifest(registry, repository, tag)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching manifest for %s: %w", ociRef, err)
	}

	for _, layer := range manifest.Layers {
		blob, err := client.GetBlob(registry, repository, layer.Digest)
		if err != nil {
			continue
		}

		// Try to extract from tgz
		tmpl, feat, err := extractItemJSON(blob)
		if err != nil {
			continue
		}
		if tmpl != nil || feat != nil {
			return tmpl, feat, nil
		}
	}

	return nil, nil, fmt.Errorf("metadata not found in %s", ociRef)
}

// extractCollectionJSON extracts devcontainer-collection.json from a tgz blob.
func extractCollectionJSON(blob []byte) (*CollectionMetadata, error) {
	return extractJSONFromTgz[CollectionMetadata](blob, "devcontainer-collection.json")
}

// extractItemJSON tries to extract devcontainer-template.json or devcontainer-feature.json.
func extractItemJSON(blob []byte) (*TemplateDefinition, *FeatureDefinition, error) {
	tmpl, err := extractJSONFromTgz[TemplateDefinition](blob, "devcontainer-template.json")
	if err == nil && tmpl != nil {
		return tmpl, nil, nil
	}

	feat, err := extractJSONFromTgz[FeatureDefinition](blob, "devcontainer-feature.json")
	if err == nil && feat != nil {
		return nil, feat, nil
	}

	return nil, nil, fmt.Errorf("no metadata JSON found in blob")
}

// extractJSONFromArchive extracts a named JSON file from a tar or tgz blob.
func extractJSONFromTgz[T any](blob []byte, filename string) (*T, error) {
	// Try gzip first, fall back to plain tar
	var reader io.Reader
	gzr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		// Not gzip — treat as plain tar
		reader = bytes.NewReader(blob)
	} else {
		defer gzr.Close()
		reader = gzr
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Match the filename (may be prefixed with ./)
		name := strings.TrimPrefix(header.Name, "./")
		if name == filename {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			var result T
			if err := json.Unmarshal(data, &result); err != nil {
				return nil, err
			}
			return &result, nil
		}
	}

	return nil, fmt.Errorf("%s not found in archive", filename)
}

// extractCollectionBase extracts the collection base path from an OCI reference.
// "ghcr.io/devcontainers/templates/python:1" → "ghcr.io/devcontainers/templates"
func extractCollectionBase(ociRef string) string {
	ref := strings.TrimPrefix(ociRef, "oci://")
	// Remove tag
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}
	// Remove last path segment (the specific template/feature ID)
	if idx := strings.LastIndex(ref, "/"); idx != -1 {
		ref = ref[:idx]
	}
	return ref
}
