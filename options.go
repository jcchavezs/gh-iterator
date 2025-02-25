package iterator

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

type ArchiveCondition int

const (
	IncludeArchived ArchiveCondition = iota
	OnlyArchived
	OmitArchived
)

type Source int

const (
	AllSources Source = iota
	OnlyForks
	OnlyNonForks
)

type SizeCondition int

const (
	All SizeCondition = iota
	NotEmpty
	OnlyEmpty
)

type SearchOptions struct {
	Language         string
	ArchiveCondition ArchiveCondition
	Visibility       Visibility
	Source           Source
	PerPage          int
	Page             int
	SizeCondition    SizeCondition
	FilterIn         func(Repository) bool
}

const defaultPerPage = 100

func (so SearchOptions) MakeFilterIn() func(Repository) bool {
	filters := []func(Repository) bool{}
	if so.FilterIn != nil {
		filters = append(filters, so.FilterIn)
	}

	if so.Language != "" {
		filters = append(filters, func(r Repository) bool {
			return r.Language == so.Language
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
