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

## Choosing the function

| Need | Use |
|---|---|
| Only metadata (name, archived, language, visibility, size, pushed_at…) | `ViewRepositoriesInOrganization` |
| Look at a few known files (e.g. does `monorepo.yaml` exist, what does it contain) | `ViewRepositoriesInOrganization` + `github.ReadFile` (GitHub contents API, **no clone**) |
| Anything from the API only (`github.API`, `IsRepositoryArchived`) | `ViewRepositoriesInOrganization` |
| Need the working tree: grep many files, run tools (`govulncheck`, `npm`), walk directories | `RunForOrganization` (clones each repo) |
| Modify files, commit, push, open PRs | `RunForOrganization` (+ `github.*` git helpers) |
| Same, but a single repo | `RunForRepository` / `ViewRepository` |

Rule of thumb: **prefer `ViewRepositoriesInOrganization`**. Cloning is slow and heavy; reading 1–3 files through `github.ReadFile` is far cheaper. If you must clone but only need some paths, use `Options.CloningSubset`. Use `Options.CloneCacheKey` only if the same repo is cloned repeatedly in a run.

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

## Public API: package `iterator` (`github.com/jcchavezs/gh-iterator`)

### Functions
- `RunForOrganization(ctx, orgName, SearchOptions, Processor, RunOptions) (Result, error)`: clones every matching repo and runs the processor inside the clone (exec dir = clone dir).
- `RunForRepository(ctx, "owner/repo", Processor, RunOptions) error`: same for one repo (clones).
- `ViewRepositoriesInOrganization(ctx, orgName, SearchOptions, Processor, ViewRepositoriesOptions) (Result, error)`: lists matching repos and runs the processor **without cloning** (the execer is not in a clone; use `github.ReadFile`/`github.API`). The execer has `GH_REPO` set so `gh` commands target the repo.
- `ViewRepository(ctx, "owner/repo", Processor, ViewRepositoriesOptions) error`: same for one repo.
- `CloneCacheKeyFromString(s) CloneCacheKey`: constant cache key helper.
- `PageN(n) Page`, const `AllPages` (-1): page selection for `SearchOptions.Page`.
- `(SearchOptions).MakeFilterIn() func(Repository) bool`: the combined filter (useful in tests).

### Types
- `Processor = func(ctx context.Context, repository Repository, xr exec.Execer) error`. Returning an error aborts the whole run. `xr` runs in the clone with `Run*`; with `View*` there is no clone and `GH_REPO` is set to the repository.
- `Repository{Name ("owner/repo"), URL, SSHURL, DefaultBranchName, Archived, ArchivedAt, Language, Visibility, Fork, Size, PushedAt}`; methods `IsEmpty()`, `String()`.
- `Result{Found, Inspected, Processed}`: counts from the API / before filter / after filter.
- `SearchOptions`:
  - `Languages []string` (primary GitHub language, case-insensitive)
  - `ArchiveCondition`: `IncludeArchived` (default) | `OnlyArchived` | `OmitArchived`
  - `Visibility`: `VisibilityNone` (default) | `VisibilityPublic` | `VisibilityPrivate` | `VisibilityInternal`
  - `Source`: `AllSources` | `OnlyForks` | `OnlyNonForks`
  - `SizeCondition`: `All` (no filter, default) | `NotEmpty` (size > 0) | `OnlyEmpty` (size 0)
  - `PerPage int` (default 100), `Page` (`AllPages` to fetch everything)
  - `FilterIn func(Repository) bool` custom predicate (metadata only)
  - `Cache time.Duration` cache API response (e.g. `time.Hour`); handy while iterating on a program
- `RunOptions` (= `Options`): `UseHTTPS`, `CloneCacheKey`, `CloningSubset []string`, `NumberOfWorkers`, `LogHandler slog.Handler`, `ContextEnricher`, (`Debug` is deprecated: use `LogHandler`).
- `ViewRepositoriesOptions`: `NumberOfWorkers`, `LogHandler`.
- Const `GithubAPIVersion`: GitHub REST API version used in requests.
- `(Visibility).String()`: GitHub visibility name (empty for `VisibilityNone`). `(Repository).IsEmpty()` is `Size == 0`; `String()` is `Name`.

## Public API: package `github` (`github.com/jcchavezs/gh-iterator/github`)

All take the `xr exec.Execer` passed to the processor.

Read-only / API (work in both Run* and View*):
- `ReadFile(ctx, xr, repo, filePath) ([]byte, error)`: read a file at the repo root/path via the contents API on the default branch. Returns an error wrapping `os.ErrNotExist` when absent: check with `errors.Is(err, os.ErrNotExist)`. **`repo` is `repository.Name` (owner/repo).**
- `API(ctx, xr, path, args...) (string, error)`: raw `gh api <path>` call (extra `gh` args such as `--jq`, returns the output).
- `IsRepositoryArchived(ctx, xr, repoName) (bool, error)`
- `ErrOrGHAPIErr(apiResponsePayload, err) error`: turns a gh API error payload into a readable error.

Git/PR helpers (need a clone, so only meaningful with `RunForOrganization` / `RunForRepository`):
- `CurrentBranch(ctx, xr) (string, error)`
- `CheckoutNewBranch(ctx, xr, name) error`
- `AddFiles(ctx, xr, paths...) error`
- `HasChanges(ctx, xr) (bool, error)`; `ListChanges(ctx, xr) ([][2]string, error)`
- `Commit(ctx, xr, message, flags...) error`
- `Push(ctx, xr, branch, force PushOption) error` (`PushForce` | `PushNoForce`); `PushToRemote(ctx, xr, remote, branch, force) error`
- `ForkAndAddRemote(ctx, xr, remoteName) (func(branch string) string, error)`: for repos you cannot push to.
- `CreatePRIfNotExist(ctx, xr, PROptions{Title, Body, Draft, Head, Assignees}) (url string, isNew bool, err error)`

**Never push, open PRs or otherwise mutate remote repositories unless the user explicitly asked for it.** Listing/reporting tasks must stay read-only.

## Package `exec` (`github.com/jcchavezs/gh-iterator/exec`): the `Execer`

- `Run(ctx, cmd, args...) (Result, error)` / `RunX(...) (string, error)` (X = error on non-zero exit). `Result{Stdout, Stderr, ExitCode, Cancelled}`, `Result.TrimStdout()`.
- `RunWithStdin` / `RunWithStdinX`
- `WithEnv(kv...)`, `WithLogFields(kv...)`, `Sub(subpath)`, `GenerateFS() afero.Fs` (filesystem rooted at the clone: use it to read files in `RunForOrganization`)
- `Log(ctx, level, msg, fields...)`, `DebugShell(ctx)`
- Helpers: `TrimStdout`, `GetStderr`, `StderrNotEmpty`, `NewExecer`, `NewExecerWithLogger`, `NewExecErr(msg, stderr, exitCode)` (nil if exit code is 0; stderr retrievable with `GetStderr`).

With `ViewRepositories*` the execer's dir is not a clone: don't read local files, use `github.ReadFile`.

## Templates

### A. Metadata + file existence (no clone)

"List all non-archived repositories in `atko-cic` that contain a `monorepo.yaml` in the root."

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
)

func main() {
	const org = "atko-cic"

	logger := newLogger() // see "Logging and progress"
	logger.Info("starting", "org", org)
	start := time.Now()

	var (
		mu    sync.Mutex
		found []string
		done  atomic.Int64
	)

	res, err := iterator.ViewRepositoriesInOrganization(
		context.Background(), org,
		iterator.SearchOptions{
			ArchiveCondition: iterator.OmitArchived,
			SizeCondition:    iterator.NotEmpty,
			Page:             iterator.AllPages,
		},
		func(ctx context.Context, repo iterator.Repository, xr exec.Execer) error {
			xr.Log(ctx, slog.LevelInfo, "checking repository", "repository", repo.Name, "n", done.Add(1))

			_, err := github.ReadFile(ctx, xr, repo.Name, "monorepo.yaml")
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}

			mu.Lock()
			found = append(found, repo.Name)
			mu.Unlock()
			xr.Log(ctx, slog.LevelInfo, "match", "repository", repo.Name)
			return nil
		},
		iterator.ViewRepositoriesOptions{LogHandler: logger.Handler()},
	)
	if err != nil {
		log.Fatal(err)
	}

	sort.Strings(found)
	for _, name := range found {
		fmt.Println(name)
	}
	logger.Info("done", "found", res.Found, "inspected", res.Inspected, "processed", res.Processed, "matches", len(found), "elapsed", time.Since(start))
}
```

Template A omits `newLogger()` for brevity: copy it from "Logging and progress" into every program (and add `log/slog` imports as needed).

### B. Parse file contents (no clone)

"List repos where root `monorepo.yaml` or `project.yaml` has `.build.language == node`." Same skeleton as A; the processor becomes:

```go
type buildConfig struct {
	Build struct {
		Language string `yaml:"language"`
	} `yaml:"build"`
}

// uses xr.Log for warnings; xr is the processor's execer
func hasNodeBuild(ctx context.Context, xr exec.Execer, repo iterator.Repository) (string, bool, error) {
	for _, f := range []string{"monorepo.yaml", "project.yaml"} {
		b, err := github.ReadFile(ctx, xr, repo.Name, f)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", false, err
		}

		var cfg buildConfig
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			// a malformed file in one repo should not abort the whole run
			xr.Log(ctx, slog.LevelWarn, "malformed file", "repository", repo.Name, "file", f, "error", err)
			continue
		}
		if cfg.Build.Language == "node" {
			return f, true, nil
		}
	}
	return "", false, nil
}
```

Import `"gopkg.in/yaml.v3"`. It is already in `go.sum` (currently marked `// indirect`, which still builds); if `go run` complains, run `go mod tidy`/`go get gopkg.in/yaml.v3` and tell the user go.mod changed.

### C. Needs the working tree (clone)

Use `iterator.RunForOrganization(ctx, org, searchOpts, processor, iterator.Options{CloningSubset: []string{"monorepo.yaml"}})`; inside the processor `xr` runs in the clone, so `xr.RunX(ctx, "grep", ...)` or `afero.ReadFile(xr.GenerateFS(), "monorepo.yaml")` work. Same locking rules apply.

## Gotchas

- Returning an error from the processor stops the entire run. Treat "not found" and per-repo parse problems as non-errors (log at Warn); return only for unexpected failures (auth, rate limit).
- Print results to stdout and logs to stderr so output can be piped.
- Archived repos are included by default, so set `ArchiveCondition: iterator.OmitArchived` when the user says "non archived"/"active". Empty repos make `ReadFile` fail with 404-ish errors, so `SizeCondition: iterator.NotEmpty` is usually wise.
- Big orgs: set `Cache: time.Hour` in `SearchOptions` while iterating on the program to avoid refetching the listing, and `Page: iterator.AllPages` to get every repo.
- `ReadFile` only reads the default branch and returns raw content; for directories or listings use `github.API(ctx, xr, "/repos/"+repo.Name+"/contents/<dir>", "--jq", ".[].name")`.
