package ui

import (
	"sort"

	"github.com/charmbracelet/bubbles/list"
	"github.com/mochlast/devcontainer-companion/internal/catalog"
)

// ociRefItem is implemented by list items backed by a catalog entry.
type ociRefItem interface {
	ociRef() string
}

func (i templateItem) ociRef() string { return i.entry.OciRef }
func (i featureItem) ociRef() string  { return i.entry.OciRef }

// officialFirstFilterFunc returns a list.FilterFunc that fuzzy-matches like the
// default filter but sorts official devcontainers entries (ghcr.io/devcontainers/)
// to the top of the results.
func officialFirstFilterFunc(items []list.Item) list.FilterFunc {
	return func(term string, targets []string) []list.Rank {
		ranks := list.DefaultFilter(term, targets)
		sort.SliceStable(ranks, func(i, j int) bool {
			iOfficial := isOfficialItem(items[ranks[i].Index])
			jOfficial := isOfficialItem(items[ranks[j].Index])
			return iOfficial && !jOfficial
		})
		return ranks
	}
}

func isOfficialItem(item list.Item) bool {
	if r, ok := item.(ociRefItem); ok {
		return catalog.IsOfficial(r.ociRef())
	}
	return false
}
