package marketplace

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const jetbrainsAPIBase = "https://plugins.jetbrains.com/api"

// Plugin represents a JetBrains Marketplace plugin.
type Plugin struct {
	ID          string // xmlId used in devcontainer.json (e.g. "com.intellij.python")
	Name        string
	Description string
	Downloads   int64
	Rating      float64
}

// jetbrainsSearchResponse models the JetBrains search API response.
type jetbrainsSearchResponse struct {
	Plugins []jetbrainsPlugin `json:"plugins"`
}

type jetbrainsPlugin struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	XMLId     string  `json:"xmlId"`
	Preview   string  `json:"preview"`
	Downloads int64   `json:"downloads"`
	Rating    float64 `json:"rating"`
}

// SearchPlugins queries the JetBrains Marketplace for plugins matching the given term.
func SearchPlugins(query string, pageSize int) ([]Plugin, error) {
	if pageSize <= 0 {
		pageSize = 20
	}

	params := url.Values{
		"search":       {query},
		"max":          {strconv.Itoa(pageSize)},
		"isIDERequest": {"false"},
	}

	reqURL := fmt.Sprintf("%s/searchPlugins?%s", jetbrainsAPIBase, params.Encode())

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("querying JetBrains marketplace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JetBrains marketplace returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result jetbrainsSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var plugins []Plugin
	for _, p := range result.Plugins {
		if p.XMLId == "" {
			continue
		}
		plugins = append(plugins, Plugin{
			ID:          p.XMLId,
			Name:        p.Name,
			Description: p.Preview,
			Downloads:   p.Downloads,
			Rating:      p.Rating,
		})
	}

	return plugins, nil
}

// FetchPluginReadme fetches the description/readme for a JetBrains plugin by its numeric ID.
// The xmlId is used to look up the plugin first, then fetch its description.
func FetchPluginReadme(xmlID string) (string, error) {
	// First resolve xmlId to numeric ID
	params := url.Values{
		"search":       {xmlID},
		"max":          {"1"},
		"isIDERequest": {"false"},
	}
	reqURL := fmt.Sprintf("%s/searchPlugins?%s", jetbrainsAPIBase, params.Encode())

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("looking up plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("JetBrains API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var result jetbrainsSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Plugins) == 0 {
		return "", fmt.Errorf("plugin %q not found", xmlID)
	}

	numericID := result.Plugins[0].ID

	// Fetch the full plugin description
	descURL := fmt.Sprintf("%s/plugins/%d", jetbrainsAPIBase, numericID)
	resp2, err := client.Get(descURL)
	if err != nil {
		return "", fmt.Errorf("fetching plugin details: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching plugin details: status %d", resp2.StatusCode)
	}

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return "", fmt.Errorf("reading plugin details: %w", err)
	}

	var detail struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Preview     string `json:"preview"`
	}
	if err := json.Unmarshal(body2, &detail); err != nil {
		return "", fmt.Errorf("parsing plugin details: %w", err)
	}

	// Description is HTML, but glamour can handle basic HTML via markdown rendering
	// Return preview + description
	content := fmt.Sprintf("# %s\n\n%s\n\n---\n\n%s", detail.Name, detail.Preview, detail.Description)
	return content, nil
}
