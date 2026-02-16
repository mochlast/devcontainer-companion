package marketplace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const galleryURL = "https://marketplace.visualstudio.com/_apis/public/gallery/extensionquery"

// SortBy represents the sort criterion for marketplace search results.
type SortBy int

const (
	SortByRelevance    SortBy = 0
	SortByInstalls     SortBy = 4
	SortByRating       SortBy = 6
	SortByName         SortBy = 2
	SortByPublished    SortBy = 5
	SortByUpdated      SortBy = 1
)

// SortOption pairs a display label with a SortBy value.
type SortOption struct {
	Label  string
	SortBy SortBy
}

// SortOptions returns the available sort options in display order.
func SortOptions() []SortOption {
	return []SortOption{
		{Label: "Installs", SortBy: SortByInstalls},
		{Label: "Rating", SortBy: SortByRating},
		{Label: "Name", SortBy: SortByName},
		{Label: "Published Date", SortBy: SortByPublished},
		{Label: "Updated Date", SortBy: SortByUpdated},
	}
}

// Extension represents a VS Code marketplace extension.
type Extension struct {
	ID           string // publisher.name identifier
	DisplayName  string
	Description  string
	InstallCount int64
	Rating       float64
}

// queryRequest is the JSON body for the marketplace API.
type queryRequest struct {
	Filters []queryFilter `json:"filters"`
	Flags   int           `json:"flags"`
}

type queryFilter struct {
	Criteria  []filterCriteria `json:"criteria"`
	PageSize  int              `json:"pageSize"`
	SortBy    int              `json:"sortBy"`
	SortOrder int              `json:"sortOrder"`
}

type filterCriteria struct {
	FilterType int    `json:"filterType"`
	Value      string `json:"value"`
}

// queryResponse models the relevant parts of the marketplace API response.
type queryResponse struct {
	Results []queryResult `json:"results"`
}

type queryResult struct {
	Extensions []extensionResult `json:"extensions"`
}

type extensionResult struct {
	Publisher    publisherResult   `json:"publisher"`
	ExtensionID string            `json:"extensionId"`
	Name        string            `json:"extensionName"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"shortDescription"`
	Statistics  []statisticResult `json:"statistics"`
	Versions    []versionResult   `json:"versions"`
}

type publisherResult struct {
	Name string `json:"publisherName"`
}

type statisticResult struct {
	Name  string  `json:"statisticName"`
	Value float64 `json:"value"`
}

type versionResult struct {
	Files []fileResult `json:"files"`
}

type fileResult struct {
	AssetType string `json:"assetType"`
	Source    string `json:"source"`
}

// Search queries the VS Code Marketplace for extensions matching the given term.
func Search(query string, pageSize int, sortBy SortBy) ([]Extension, error) {
	if pageSize <= 0 {
		pageSize = 20
	}

	sortOrder := 2 // descending
	if sortBy == SortByName {
		sortOrder = 1 // ascending for name
	}

	reqBody := queryRequest{
		Filters: []queryFilter{
			{
				Criteria: []filterCriteria{
					{FilterType: 10, Value: query}, // 10 = search text
					{FilterType: 8, Value: "Microsoft.VisualStudio.Code"},
				},
				PageSize:  pageSize,
				SortBy:    int(sortBy),
				SortOrder: sortOrder,
			},
		},
		Flags: 0x192, // IncludeAssetUri | IncludeInstallationTargets | IncludeSharedAccounts | IncludeVersions | IncludeStatistics
	}

	return doQuery(reqBody)
}

// FetchReadme fetches the README/detail content for a given extension ID (publisher.name).
func FetchReadme(extensionID string) (string, error) {
	parts := strings.SplitN(extensionID, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid extension ID %q", extensionID)
	}
	publisher, name := parts[0], parts[1]

	// Fetch the README asset directly from the gallery CDN
	url := fmt.Sprintf(
		"https://%s.gallery.vsassets.io/_apis/public/gallery/publisher/%s/extension/%s/latest/assetbyname/Microsoft.VisualStudio.Services.Content.Details",
		publisher, publisher, name,
	)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetching extension README: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching extension README: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading extension README: %w", err)
	}

	return string(body), nil
}

func doQuery(reqBody queryRequest) ([]Extension, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", galleryURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json;api-version=3.0-preview.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying marketplace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketplace returned status %d", resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result queryResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var extensions []Extension
	if len(result.Results) > 0 {
		for _, ext := range result.Results[0].Extensions {
			e := Extension{
				ID:          fmt.Sprintf("%s.%s", ext.Publisher.Name, ext.Name),
				DisplayName: ext.DisplayName,
				Description: ext.Description,
			}
			for _, stat := range ext.Statistics {
				switch stat.Name {
				case "install":
					e.InstallCount = int64(stat.Value)
				case "averagerating":
					e.Rating = stat.Value
				}
			}
			extensions = append(extensions, e)
		}
	}

	return extensions, nil
}
