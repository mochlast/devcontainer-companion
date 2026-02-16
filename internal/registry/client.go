package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client handles OCI registry HTTP interactions (token auth, manifests, blobs).
type Client struct {
	httpClient *http.Client
	tokens     map[string]string // cache: "registry/repo" -> token
}

// NewClient creates a new OCI registry client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
		tokens:     make(map[string]string),
	}
}

type tokenResponse struct {
	Token string `json:"token"`
}

type ociManifest struct {
	Layers []ociLayer `json:"layers"`
}

type ociLayer struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// GetToken fetches a bearer token for the given registry and repository.
func (c *Client) GetToken(registry, repository string) (string, error) {
	key := registry + "/" + repository
	if tok, ok := c.tokens[key]; ok {
		return tok, nil
	}

	// For ghcr.io, token endpoint is ghcr.io/token
	tokenURL := fmt.Sprintf("https://%s/token?scope=repository:%s:pull", registry, repository)

	resp, err := c.httpClient.Get(tokenURL)
	if err != nil {
		return "", fmt.Errorf("fetching token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	c.tokens[key] = tr.Token
	return tr.Token, nil
}

// GetManifest fetches the OCI manifest for a given repository and tag.
func (c *Client) GetManifest(registry, repository, tag string) (*ociManifest, error) {
	token, err := c.GetToken(registry, repository)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var manifest ociManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decoding manifest: %w", err)
	}

	return &manifest, nil
}

// GetBlob fetches a blob from the OCI registry, following redirects.
func (c *Client) GetBlob(registry, repository, digest string) ([]byte, error) {
	token, err := c.GetToken(registry, repository)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, digest)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching blob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("blob request failed (status %d): %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// ParseOciRef splits an OCI reference like "ghcr.io/devcontainers/templates/python:1"
// into registry, repository, and tag.
func ParseOciRef(ociRef string) (registry, repository, tag string, err error) {
	// Remove scheme if present
	ref := strings.TrimPrefix(ociRef, "oci://")

	// Split tag
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) == 2 {
		tag = parts[1]
	} else {
		tag = "latest"
	}

	// Split registry from repository
	fullPath := parts[0]
	slashIdx := strings.Index(fullPath, "/")
	if slashIdx == -1 {
		return "", "", "", fmt.Errorf("invalid OCI reference: %s", ociRef)
	}

	registry = fullPath[:slashIdx]
	repository = fullPath[slashIdx+1:]

	return registry, repository, tag, nil
}
