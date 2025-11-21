package iterator

import (
	"strings"
	"time"
)

// Visibility represents the visibility of the repositories.
type Visibility int

const (
	VisibilityNone Visibility = iota
	VisibilityPublic
	VisibilityPrivate
	VisibilityInternal
)

func (v Visibility) String() string {
	switch v {
	case VisibilityPublic:
		return "public"
	case VisibilityPrivate:
		return "private"
	case VisibilityInternal:
		return "internal"
	default:
		return ""
	}
}

// ArchiveCondition represents a condition to filter repositories by their archived status.
type ArchiveCondition int

const (
	IncludeArchived ArchiveCondition = iota
	OnlyArchived
	OmitArchived
)

// Source represents the source of the repositories.
type Source int

const (
	AllSources Source = iota
	OnlyForks
	OnlyNonForks
)

// SizeCondition represents a condition to filter repositories by size.
type SizeCondition int

const (
	All SizeCondition = iota
	NotEmpty
	OnlyEmpty
)

// Page represents the page number to fetch. If -1, it means all pages.
type Page int

// AllPages indicates to fetch all pages.
const AllPages Page = -1

// PageN returns a Page representing the given page number.
func PageN(n int) Page {
	return Page(n)
}

// SearchOptions are the options to search for repositories.
type SearchOptions struct {
	// Languages are the programming language of the repositories to search for e.g. Go.
	Languages []string
	// ArchiveCondition is the condition to apply to the archived repositories e.g. OnlyArchived.
	ArchiveCondition ArchiveCondition
	// Visibility is the visibility of the repositories to search for e.g. Public.
	Visibility Visibility
	// Source is the source of the repositories to search for e.g. OnlyForks.
	Source Source
	// PerPage is the number of repositories to fetch per page. The default is 100.
	PerPage int
	// Page is the page number to fetch. If passed -1, it will fetch all pages.
	Page Page
	// SizeCondition is the condition to apply to the size of the repositories e.g. NotEmpty.
	SizeCondition SizeCondition
	// FilterIn is a custom filter to apply to the repositories and decide what goes in.
	FilterIn func(Repository) bool
	// Cache the response, e.g. "3600s", "60m", "1h"
	Cache time.Duration
}

const (
	defaultPerPage = 100
	maxPerPage     = 1000
)

// MakeFilterIn creates a filter function based on the SearchOptions.
func (so SearchOptions) MakeFilterIn() func(Repository) bool {
	filters := []func(Repository) bool{}
	if so.FilterIn != nil {
		filters = append(filters, so.FilterIn)
	}

	if len(so.Languages) > 0 {
		filters = append(filters, func(r Repository) bool {
			for _, l := range so.Languages {
				if strings.EqualFold(l, r.Language) {
					return true
				}
			}

			return false
		})
	}

	switch so.ArchiveCondition {
	case OnlyArchived:
		filters = append(filters, func(r Repository) bool {
			return r.Archived
		})
	case OmitArchived:
		filters = append(filters, func(r Repository) bool {
			return !r.Archived
		})
	}

	switch so.Source {
	case OnlyForks:
		filters = append(filters, func(r Repository) bool {
			return r.Fork
		})
	case OnlyNonForks:
		filters = append(filters, func(r Repository) bool {
			return !r.Fork
		})
	}

	if so.Visibility != VisibilityNone {
		filters = append(filters, func(r Repository) bool {
			return r.Visibility == so.Visibility.String()
		})
	}

	switch so.SizeCondition {
	case NotEmpty:
		filters = append(filters, func(r Repository) bool {
			return r.Size > 0
		})
	case OnlyEmpty:
		filters = append(filters, func(r Repository) bool {
			return r.Size == 0
		})
	}

	return func(r Repository) bool {
		for _, filter := range filters {
			if !filter(r) {
				return false
			}
		}
		return true
	}
}
