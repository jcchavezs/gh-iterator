---
name: iterating-repositories
description: Use when the user wants to list, inspect, audit, report on, or change many GitHub repositories of an organization (e.g. "list all non archived repos in org X that have a monorepo.yaml", "repos whose project.yaml has build.language == node", "open a PR in every repo"). Generates and runs a throwaway Go program in `.programs/<name>` built on the gh-iterator packages.
allowed-tools: Read, Write, Edit, Bash(go run ./.programs/*), Bash(go vet ./.programs/*), Bash(go build ./.programs/*), Bash(go mod tidy:*), Bash(gh auth status:*), Bash(mkdir -p .programs/*)
---

# Iterating repositories with gh-iterator

Generate a small Go program under `.programs/<name>/main.go` that calls the `iterator` package (`github.com/jcchavezs/gh-iterator`), then run it with `go run ./.programs/<name> [args]`.

The directory is inside the repo's Go module, so it can import `iterator`, `iterator/github` and `iterator/exec` directly (dot-prefixed dirs are skipped by `./...`, so they never break `go build ./...` / `go test ./...`). Programs are throwaway: make sure `.programs/` is in `.gitignore` (add it if missing) and never commit them unless asked.

Requirements: `gh` CLI authenticated (`gh auth status`) with access to the org.

## Workflow

1. Turn the request into: org (or single repo), repository-level filters (`SearchOptions`), and the per-repo question/action.
2. Pick the iteration function (decision table below).
3. Pick a short kebab-case `<name>` describing the task (e.g. `monorepo-node-repos`), write `.programs/<name>/main.go`.
4. `go vet ./.programs/<name>` then `go run ./.programs/<name> <org>`.
5. Report the output (and the path of the program so it can be re-run). Org name and similar inputs should be CLI args or constants, not prompts.
6. Design a dry run mode in your program to preview actions without making changes e.g. never push or create a PR unless the user confirms.
7. Execute the program only after reviewing and confirming the inputs.

## Choosing the function

| Need | Use |
|---|---|
| Only metadata (name, archived, language, visibility, size, pushed_at…) | `ViewRepositoriesInOrganization` |
| Look at a few known files (e.g. does `monorepo.yaml` exist, what does it contain) | `ViewRepositoriesInOrganization` + `github.ReadFile` (GitHub contents API, **no clone**) |
| Anything from the API only (`github.API`, `IsRepositoryArchived`) | `ViewRepositoriesInOrganization` |
| Need the working tree: grep many files, run tools (`govulncheck`, `npm`), walk directories | `RunForOrganization` (clones each repo) |
| Modify files, commit, push, open PRs | `RunForOrganization` (+ `github.*` git helpers) |
| Same, but a single repo | `RunForRepository` / `ViewRepository` |

Rule of thumb: **prefer `ViewRepositoriesInOrganization`**. Cloning is slow and heavy; reading 1–3 files through `github.ReadFile` is far cheaper. If you must clone but only need some paths, use `Options.CloningSubset`. Clones are shallow (`CloneDepth`, default 1 commit); set `CloneDepth: -1` if the processor needs history (`git log`, `blame`). Use `Options.CloneCacheKey` only if the same repo is cloned repeatedly in a run.

**Be mindful of the language.** If the task is tied to a language or ecosystem (Go version, `go.mod`, npm, Python deps, Gradle…), set `SearchOptions.Languages` (GitHub's primary language, case-insensitive, e.g. `[]string{"Go"}`) so you don't fetch files from thousands of irrelevant repos. Pick the GitHub language names that cover the ecosystem (Node → `JavaScript`, `TypeScript`; JVM → `Java`, `Kotlin`). The detected language is only a heuristic (monorepos and polyglot repos can be labeled differently), so if the user wants exhaustive results (e.g. "all repos with `build.language == node`" read from a config file), do **not** filter by language; say that you didn't and why. When you do filter, mention it in the final report.

Both org-level functions process repos concurrently (default 10 workers, `NumberOfWorkers`), so **guard shared state (results slice, map, printing) with a `sync.Mutex`** and sort before printing for deterministic output.

## Logging and progress

Programs can take minutes on big orgs, so always show progress. Use `log/slog` unless the user asks for another logging library, with the default level **Info**, writing to **stderr** (stdout is reserved for results). Let the level be overridable with an env var (e.g. `LOG_LEVEL=debug`).

The iterator discards its own logs unless you pass `LogHandler` in `Options`/`ViewRepositoriesOptions`, so pass the same handler to get its messages (pagination, cloning, ...). Inside the processor log through the execer: `xr.Log(ctx, slog.LevelInfo, "msg", "repository", repo.Name)`.

```go
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		_ = level.UnmarshalText([]byte(v)) // keeps Info if invalid
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
```

What to log (Info): the start (org, filters), one line per repo processed with a counter (`processed` via `atomic.Int64`), one line per match, and a final summary with `Result` counts and elapsed time. Debug: per-file lookups, skipped repos. Warn: recoverable per-repo problems (malformed file). Don't log secrets or whole file contents.

```go
logger := newLogger()
var done atomic.Int64
start := time.Now()
logger.Info("starting", "org", org)
// in the processor:
//   n := done.Add(1)
//   xr.Log(ctx, slog.LevelInfo, "checked repository", "repository", repo.Name, "n", n)
// pass LogHandler: logger.Handler() in the options
logger.Info("done", "found", res.Found, "inspected", res.Inspected, "processed", res.Processed, "elapsed", time.Since(start))
```

## Public API:

<!-- Generate: Godoc begins -->
### Package `github.com/jcchavezs/gh-iterator`

```go
import "github.com/jcchavezs/gh-iterator"
```

## Usage

```go
const (

	// GithubAPIVersion is the GitHub REST API version sent in API requests.
	GithubAPIVersion = "2022-11-28"
)
```

#### func  RunForRepository

```go
func RunForRepository(ctx context.Context, repoName string, processor Processor, opts RunOptions) error
```
RunForRepository clones a single repository (owner/repo) and runs the processor
on it.

#### func  ViewRepository

```go
func ViewRepository(ctx context.Context, repoName string, processor Processor, opts ViewRepositoriesOptions) error
```
ViewRepository runs the processor for a single repository (owner/repo) without
cloning it.

#### type ArchiveCondition

```go
type ArchiveCondition int
```

ArchiveCondition represents a condition to filter repositories by their archived
status.

```go
const (
	// IncludeArchived applies no filter on archived repositories (default).
	IncludeArchived ArchiveCondition = iota
	OnlyArchived
	OmitArchived
)
```

#### type CloneCacheKey

```go
type CloneCacheKey func(repository Repository) string
```

CloneCacheKey is a function to generate a cache key for a repository clone.

#### func  CloneCacheKeyFromString

```go
func CloneCacheKeyFromString(s string) CloneCacheKey
```
CloneCacheKeyFromString returns a CloneCacheKey that always returns s.

#### type ListOptions

```go
type ListOptions struct {
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers.
	NumberOfWorkers int

	// Log handler
	LogHandler slog.Handler
}
```


#### type Options

```go
type Options struct {
	// UseHTTPS is a flag to use HTTPS instead of SSH to clone the repositories.
	UseHTTPS bool
	// CloneCacheKey is a key to identify the cached version of a repository clone to be used
	// in an execution. This is beneficial when during the same execution a repository is cloned
	// many times reducing the execution time by cloning once and copying the same repository locally.
	// If the key is empty, no cache will be used.
	CloneCacheKey CloneCacheKey
	// CloningSubset is a list of files or directories to clone to avoid cloning the whole repository.
	// it is helpful on big repositories to speed up the process.
	CloningSubset []string
	// CloneDepth is the number of commits to fetch when cloning a repository. By default (0) it
	// fetches only the latest commit (depth 1), a negative value fetches the whole history.
	CloneDepth int
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers. Not used by RunForRepository.
	NumberOfWorkers int
	// Debug is a flag to print debug information.
	//
	// Deprecated: use LogHandler.
	Debug bool
	// Log handler
	LogHandler slog.Handler
	// ContextEnricher is a function to enrich the context before processing a repository.
	ContextEnricher func(context.Context, Repository) context.Context
}
```

Options are the options to run the iterator.

#### type Page

```go
type Page int
```

Page represents the page number to fetch. If -1, it means all pages.

```go
const AllPages Page = -1
```
AllPages indicates to fetch all pages.

#### func  PageN

```go
func PageN(n int) Page
```
PageN returns a Page representing the given page number.

#### type Processor

```go
type Processor func(ctx context.Context, repository Repository, xr exec.Execer) error
```

Processor is the function that process a repository. - ctx is the context to
cancel the processing. - repository is the representation of the repository. -
xr is an exec.Execer to run commands. With the Run* functions it runs in the
clone, with the View* functions there is no clone and `GH_REPO` is set to the
repository. Returning an error aborts the whole run.

#### type Repository

```go
type Repository struct {
	Name              string    `json:"full_name"`
	URL               string    `json:"clone_url"`
	SSHURL            string    `json:"ssh_url"`
	DefaultBranchName string    `json:"default_branch"`
	Archived          bool      `json:"archived"`
	ArchivedAt        time.Time `json:"archived_at"`
	Language          string    `json:"language"`
	Visibility        string    `json:"visibility"`
	Fork              bool      `json:"fork"`
	Size              int       `json:"size"`
	PushedAt          time.Time `json:"pushed_at"`
}
```

Repository represents a GitHub repository

#### func (Repository) IsEmpty

```go
func (r Repository) IsEmpty() bool
```
IsEmpty reports whether the repository has no content (size 0).

#### func (Repository) String

```go
func (r Repository) String() string
```
String returns the full name (owner/repo).

#### type Result

```go
type Result struct {
	// Found is the total number of repositories found i.e. the total number of
	// repositories retrieved from the API.
	Found int
	// Inspected is the total number of repositories inspected before the filtering.
	Inspected int
	// Processed is the total number of repositories processed after the filtering.
	Processed int
}
```

Result holds the result from running the iterator for an organization.

#### func  ListForOrganization

```go
func ListForOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, callback func(ctx context.Context, xr iteratorexec.Execer, repository string) error, opts ListOptions) (Result, error)
```
ListForOrganization lists the repositories for the given organization and
processes them concurrently using the provided callback function. It returns a
Result struct with the number of repositories found and inspected, or an error
if any occurs during the process.

#### func  RunForOrganization

```go
func RunForOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts RunOptions) (Result, error)
```
RunForOrganization runs the processor concurrently for the matching repositories
of an organization, cloning each one first. Use ViewRepositoriesInOrganization
if no clone is needed.

Repositories are processed while the listing is still being fetched, so
Result.Found only reflects the repositories retrieved before the run ended.

#### func  ViewRepositoriesInOrganization

```go
func ViewRepositoriesInOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts ViewRepositoriesOptions) (Result, error)
```
ViewRepositoriesInOrganization runs the processor concurrently for the matching
repositories of an organization without cloning them: the execer is not in a
clone, use github.ReadFile or github.API to inspect them. It is much faster than
RunForOrganization.

#### type RunOptions

```go
type RunOptions = Options
```

RunOptions are the options to run the iterator.

#### type SearchOptions

```go
type SearchOptions struct {
	// Languages are the programming language of the repositories to search for e.g. Go.
	Languages []string
	// ArchiveCondition is the condition to apply to the archived repositories e.g. OnlyArchived.
	ArchiveCondition ArchiveCondition
	// Visibility is the visibility of the repositories to search for e.g. Public.
	Visibility Visibility
	// Source is the source of the repositories to search for e.g. OnlyForks. It is applied by the
	// GitHub API, so repositories left out are not counted in Result.Found.
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
```

SearchOptions are the options to search for repositories.

#### func (SearchOptions) MakeFilterIn

```go
func (so SearchOptions) MakeFilterIn() func(Repository) bool
```
MakeFilterIn creates a filter function based on the SearchOptions.

#### type SizeCondition

```go
type SizeCondition int
```

SizeCondition represents a condition to filter repositories by size.

```go
const (
	// All applies no size filter (default).
	All SizeCondition = iota
	// NotEmpty keeps repositories with size > 0.
	NotEmpty
	// OnlyEmpty keeps repositories with size 0.
	OnlyEmpty
)
```

#### type Source

```go
type Source int
```

Source represents the source of the repositories.

```go
const (
	// AllSources applies no filter on forks (default).
	AllSources Source = iota
	OnlyForks
	OnlyNonForks
)
```

#### type ViewRepositoriesOptions

```go
type ViewRepositoriesOptions struct {
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers.
	NumberOfWorkers int

	// Log handler
	LogHandler slog.Handler
}
```

ViewRepositoriesOptions are the options to run the View* functions.

#### type Visibility

```go
type Visibility int
```

Visibility represents the visibility of the repositories.

```go
const (
	// VisibilityNone applies no visibility filter.
	VisibilityNone Visibility = iota
	VisibilityPublic
	VisibilityPrivate
	VisibilityInternal
)
```

#### func (Visibility) String

```go
func (v Visibility) String() string
```
String returns the GitHub visibility name, empty for VisibilityNone.

### Package `github.com/jcchavezs/gh-iterator/github`

```go
import "github.com/jcchavezs/gh-iterator/github"
```

## Usage

#### func  API

```go
func API(ctx context.Context, xr iteratorexec.Execer, path string, args ...string) (string, error)
```
API calls `gh api <path>` with the given extra gh args (e.g. "--jq", ".name")
and returns the output.

#### func  AddFiles

```go
func AddFiles(ctx context.Context, xr iteratorexec.Execer, paths ...string) error
```
AddFiles adds file contents to the index.

#### func  CheckoutNewBranch

```go
func CheckoutNewBranch(ctx context.Context, xr iteratorexec.Execer, name string) error
```
CheckoutNewBranch creates and checks out a new branch.

#### func  Commit

```go
func Commit(ctx context.Context, xr iteratorexec.Execer, message string, flags ...string) error
```
Commit records changes to the repository

#### func  CreatePRIfNotExist

```go
func CreatePRIfNotExist(ctx context.Context, xr iteratorexec.Execer, opts PROptions) (string, bool, error)
```
CreatePRIfNotExist on GitHub and returns: - The PR URL - Whether the PR is new
or not - An error if occurred.

#### func  CurrentBranch

```go
func CurrentBranch(ctx context.Context, xr iteratorexec.Execer) (string, error)
```
CurrentBranch returns the current branch

#### func  ErrOrGHAPIErr

```go
func ErrOrGHAPIErr(apiResponsePayload string, err error) error
```
ErrOrGHAPIErr unmarshals the response payload and if it success return the GH
API error, otherwise returns the generic error.

#### func  ForkAndAddRemote

```go
func ForkAndAddRemote(ctx context.Context, xr iteratorexec.Execer, remoteName string) (func(branchName string) string, error)
```
ForkAndAddRemote a repository and add the remote to the local git config. It
returns a function that given a branch name returns the head reference to be
used in the PR creation (i.e., username:branchName). Important: if you name the
remote as 'upstream', gh CLI might get confused when creating PRs.

#### func  HasChanges

```go
func HasChanges(ctx context.Context, xr iteratorexec.Execer) (bool, error)
```
HasChanges returns true if files are changed in the working tree status

#### func  IsRepositoryArchived

```go
func IsRepositoryArchived(ctx context.Context, repoName string, xr iteratorexec.Execer) (bool, error)
```
IsRepositoryArchived checks if the repository is archived by querying the GitHub
API.

#### func  ListChanges

```go
func ListChanges(ctx context.Context, xr iteratorexec.Execer) ([][2]string, error)
```
ListChanges return a list of changes in the working tree status

#### func  Push

```go
func Push(ctx context.Context, xr iteratorexec.Execer, branchName string, force PushOption) error
```
Push updates remote refs along with associated objects

#### func  PushToRemote

```go
func PushToRemote(ctx context.Context, xr iteratorexec.Execer, remoteName string, branchName string, force PushOption) error
```
PushToRemote updates remote refs along with associated objects

#### func  ReadFile

```go
func ReadFile(ctx context.Context, xr iteratorexec.Execer, repo string, filePath string) ([]byte, error)
```
ReadFile returns the content of filePath in repo (owner/repo) from its default
branch via the GitHub API, without cloning. If the file does not exist the error
wraps os.ErrNotExist.

#### type PROptions

```go
type PROptions struct {
	// Title for the pull request
	Title string
	// Body for the pull request
	Body string
	// Draft will open the pull request as draft when true
	Draft bool
	// The branch that contains commits for your pull request
	Head string
	// Assignees' logins for the pull request. If the PR already exists, the assignees will be added to the existing ones.
	Assignees []string
}
```

PROptions contains the options for creating a pull request. It is used in
CreatePRIfNotExist.

#### type PushOption

```go
type PushOption bool
```

PushOption represents the options for pushing changes to the remote repository.

```go
const (
	// PushForce pushes with --force.
	PushForce PushOption = true
	// PushNoForce pushes without --force.
	PushNoForce PushOption = false
)
```

### Package `github.com/jcchavezs/gh-iterator/exec`

```go
import "github.com/jcchavezs/gh-iterator/exec"
```

## Usage

#### func  GetStderr

```go
func GetStderr(err error) (string, bool)
```
GetStderr returns the stderr from a RunX error

#### func  NewExecErr

```go
func NewExecErr(message string, stderr string, exitCode int) error
```
NewExecErr creates an error carrying stderr and the exit code, retrievable with
GetStderr. It returns nil if exitCode is 0.

#### func  StderrNotEmpty

```go
func StderrNotEmpty(stderr string, ok bool) (string, bool)
```
StderrNotEmpty checks if the stderr is truly not empty

#### func  TrimStdout

```go
func TrimStdout(o string, err error) (string, error)
```
TrimStdout for convenience as RunX does not return a result where you can get
the Result.TrimStdout but instead the stdout.

#### type Execer

```go
type Execer interface {
	// Run executes a command in the execer's directory (the clone, with Run* functions).
	Run(ctx context.Context, command string, args ...string) (Result, error)
	// RunX is like Run but returns stdout and an error if the exit code is non zero.
	RunX(ctx context.Context, command string, args ...string) (string, error)
	// RunWithStdin is like Run but feeds stdin to the command.
	RunWithStdin(ctx context.Context, stdin io.Reader, command string, args ...string) (Result, error)
	// RunWithStdinX is like RunX but feeds stdin to the command.
	RunWithStdinX(ctx context.Context, stdin io.Reader, command string, args ...string) (string, error)
	// Log logs a message with the given level and fields
	Log(ctx context.Context, level slog.Level, msg string, fields ...any)

	// DebugShell starts a shell session in the execer's directory
	DebugShell(ctx context.Context)

	// WithEnv creates a child execer with added env variables
	WithEnv(kv ...string) Execer
	// WithLogFields creates a child execer with added log fields
	WithLogFields(kvFields ...any) Execer
	// Sub creates a new execer in an existing subpath.
	Sub(subpath string) (Execer, error)
	// GenerateFS returns a FS object relative to the exec dir to interact with
	GenerateFS() afero.Fs
}
```

Execer defines an interface to execute commands in a given directory with
optional environment variables and logging capabilities.

#### func  NewExecer

```go
func NewExecer(dir string) Execer
```
NewExecer creates a new execer

#### func  NewExecerWithLogger

```go
func NewExecerWithLogger(dir string, logger *slog.Logger) Execer
```
NewExecerWithLogger creates a new execer with a logger

#### type Result

```go
type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Cancelled bool
}
```

Result holds the result from a command run

#### func (Result) TrimStdout

```go
func (r Result) TrimStdout() string
```
TrimStdout returns the content of stdout removing the trailing new lines.
<!-- Generate: Godoc ends -->

## Examples

Runnable programs in [examples/](https://github.com/jcchavezs/gh-iterator/tree/main/examples/) show the library in use. Read the one closest to the task and adapt it instead of starting from scratch. They are `main` packages with the org or repo as an optional first CLI arg; copy the shape, not the details.

<!-- Generate: Examples table begins -->
| Example | Shows |
|---|---|
| [examples/cachedrepo/main.go](https://github.com/jcchavezs/gh-iterator/blob/main/examples/cachedrepo/main.go) | This example clones a repository 100 times with RunForRepository. If the CACHE_KEY env var is passed with a non empty value it clones the repository once to a cache (CloneCacheKey) and reuses copies of it. If the same value is passed again to a new execution it will reuse the cache. |
| [examples/govulncheck/main.go](https://github.com/jcchavezs/gh-iterator/blob/main/examples/govulncheck/main.go) | This example runs govulncheck in all Go repositories of an organization with RunForOrganization. It shows running a tool in each clone (xr.Run), the Languages filter, a LogHandler and writing a report file. |
| [examples/listrepos/main.go](https://github.com/jcchavezs/gh-iterator/blob/main/examples/listrepos/main.go) | This example lists all repositories of an organization with ListForOrganization and checks if each one has a dependabot manifest by reading .github/dependabot.yml with github.ReadFile (no clone, os.ErrNotExist means missing). It also shows AllPages and counting with a sync.Mutex. |
| [examples/singlefile/main.go](https://github.com/jcchavezs/gh-iterator/blob/main/examples/singlefile/main.go) | This example checks if README.md exists in all repositories of an organization with RunForOrganization. To speed up the work it clones only that file with CloningSubset, combined with OnlyNonForks, OmitArchived and NotEmpty. |
<!-- Generate: Examples table ends -->

Whatever the example, a program still needs the logging and progress setup above and a `sync.Mutex` around shared state.

## Gotchas

- Returning an error from the processor stops the entire run. Treat "not found" and per-repo parse problems as non-errors (log at Warn); return only for unexpected failures (auth, rate limit).
- Print results to stdout and logs to stderr so output can be piped.
- Archived repos are included by default, so set `ArchiveCondition: iterator.OmitArchived` when the user says "non archived"/"active". Empty repos make `ReadFile` fail with 404-ish errors, so `SizeCondition: iterator.NotEmpty` is usually wise.
- Big orgs: set `Cache: time.Hour` in `SearchOptions` while iterating on the program to avoid refetching the listing, and `Page: iterator.AllPages` to get every repo.
- `ReadFile` only reads the default branch and returns raw content; for directories or listings use `github.API(ctx, xr, "/repos/"+repo.Name+"/contents/<dir>", "--jq", ".[].name")`.
