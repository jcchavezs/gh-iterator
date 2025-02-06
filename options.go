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

type SearchOptions struct {
	Language         string
	ArchiveCondition ArchiveCondition
	Visibility       Visibility
	Source           Source
	Limit            int
}
