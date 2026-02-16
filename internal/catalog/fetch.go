package catalog

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

const (
	templatesURL = "https://containers.dev/templates"
	featuresURL  = "https://containers.dev/features"
)

// FetchTemplates fetches and parses the template catalog from containers.dev.
func FetchTemplates() ([]CatalogEntry, error) {
	return fetchCatalog(templatesURL)
}

// FetchFeatures fetches and parses the feature catalog from containers.dev.
func FetchFeatures() ([]CatalogEntry, error) {
	return fetchCatalog(featuresURL)
}

func fetchCatalog(url string) ([]CatalogEntry, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}

	return parseHTML(string(body))
}

// parseHTML extracts CatalogEntry items from the HTML table on containers.dev.
// The table has class "tg" and 4 columns: Name, Maintainer, Reference, Version.
func parseHTML(htmlContent string) ([]CatalogEntry, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	var entries []CatalogEntry
	table := findTable(doc)
	if table == nil {
		return nil, fmt.Errorf("could not find catalog table in HTML")
	}

	rows := findElements(table, "tr")
	for _, row := range rows {
		cells := findElements(row, "td")
		if len(cells) < 4 {
			continue
		}

		// Skip header row (uses <b> tags inside <td>)
		if hasElement(cells[0], "b") {
			continue
		}

		entry := CatalogEntry{
			Name:       extractText(cells[0]),
			Maintainer: extractText(cells[1]),
			OciRef:     extractText(cells[2]),
			Version:    extractText(cells[3]),
			SourceURL:  extractFirstHref(cells[0]),
		}
		if entry.Name != "" && entry.OciRef != "" {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// findTable finds the <table id="collectionTable"> element,
// falling back to the last <table class="tg"> if not found.
func findTable(n *html.Node) *html.Node {
	// Prefer table with id="collectionTable"
	if t := findTableByAttr(n, "id", "collectionTable"); t != nil {
		return t
	}
	// Fallback: find last table with class "tg" (the first is often a wrapper)
	tables := findAllTablesByClass(n, "tg")
	if len(tables) > 0 {
		return tables[len(tables)-1]
	}
	return nil
}

func findTableByAttr(n *html.Node, key, val string) *html.Node {
	if n.Type == html.ElementNode && n.Data == "table" {
		for _, attr := range n.Attr {
			if attr.Key == key && attr.Val == val {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findTableByAttr(c, key, val); result != nil {
			return result
		}
	}
	return nil
}

func findAllTablesByClass(n *html.Node, class string) []*html.Node {
	var results []*html.Node
	if n.Type == html.ElementNode && n.Data == "table" {
		for _, attr := range n.Attr {
			if attr.Key == "class" && strings.Contains(attr.Val, class) {
				results = append(results, n)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		results = append(results, findAllTablesByClass(c, class)...)
	}
	return results
}

// findElements finds all direct/nested child elements with the given tag.
func findElements(n *html.Node, tag string) []*html.Node {
	var results []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			results = append(results, c)
		} else {
			// Check inside tbody etc.
			results = append(results, findElements(c, tag)...)
		}
	}
	return results
}

// extractText extracts all text content from a node, trimming whitespace.
func extractText(n *html.Node) string {
	var sb strings.Builder
	extractTextRecursive(n, &sb)
	return strings.TrimSpace(sb.String())
}

func extractTextRecursive(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractTextRecursive(c, sb)
	}
}

// extractFirstHref finds the href attribute of the first <a> element in the node tree.
func extractFirstHref(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "a" {
		for _, attr := range n.Attr {
			if attr.Key == "href" {
				return attr.Val
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if href := extractFirstHref(c); href != "" {
			return href
		}
	}
	return ""
}

// hasElement checks if a node contains a child element with the given tag.
func hasElement(n *html.Node, tag string) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return true
		}
		if hasElement(c, tag) {
			return true
		}
	}
	return false
}
