package catalog

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SourceURLToReadmeURL converts a GitHub tree URL to the raw README.md URL.
// Input:  https://github.com/{owner}/{repo}/tree/{branch}/{path}
// Output: https://raw.githubusercontent.com/{owner}/{repo}/{branch}/{path}/README.md
func SourceURLToReadmeURL(sourceURL string) string {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return ""
	}

	// Strip https://github.com/ prefix
	const prefix = "https://github.com/"
	if !strings.HasPrefix(sourceURL, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(sourceURL, prefix)

	// Expected format: {owner}/{repo}/tree/{branch}/{path...}
	parts := strings.SplitN(rest, "/", 5)
	if len(parts) < 5 || parts[2] != "tree" {
		return ""
	}

	owner := parts[0]
	repo := parts[1]
	branch := parts[3]
	path := parts[4]

	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s/README.md", owner, repo, branch, path)
}

// FetchReadme fetches the README content from the given source URL.
func FetchReadme(sourceURL string) (string, error) {
	readmeURL := SourceURLToReadmeURL(sourceURL)
	if readmeURL == "" {
		return "", fmt.Errorf("could not derive README URL from %q", sourceURL)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(readmeURL)
	if err != nil {
		return "", fmt.Errorf("fetching README: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching README: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading README: %w", err)
	}

	return string(body), nil
}
