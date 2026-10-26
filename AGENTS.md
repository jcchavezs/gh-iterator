# Agent instructions

## Verify every change

Before considering a change done, run:

```sh
make lint test build-examples
```

- `lint` runs `golangci-lint` (install it with `make install-tools` if missing).
- `test` runs the unit tests.
- `build-examples` compiles everything under `examples/`, which are separate `main` packages that break when the public API changes.

All three must pass. Fix the failures instead of skipping or silencing them (no `//nolint`, deleted tests or weakened assertions to get green). If a failure is unrelated to your change or cannot be fixed, say so explicitly in your final message with the failing output.

Run it again after your last edit, not only after an intermediate one, and report the result faithfully.

## Keep the `iterating-repositories` skill in sync with the public API

The skill at [.claude/skills/iterating-repositories/SKILL.md](.claude/skills/iterating-repositories/SKILL.md) documents the public API of the `iterator`, `github` and `exec` packages and tells agents which function to use when. It is only useful if it matches the code.

Whenever you change the public API, update the skill in the same change. This includes changes that only touch documentation, such as a doc comment on an exported identifier whose meaning, defaults or caveats change.

Public API changes include:

- Adding, removing or renaming an exported function, method, type, constant or variable in `iterator` (root package), `github/` or `exec/`.
- Changing a signature, a struct field (e.g. `SearchOptions`, `Options`, `ViewRepositoriesOptions`, `Repository`, `Result`, `PROptions`) or the `Processor` / `Execer` interfaces.
- Changing behavior callers rely on: defaults (workers, page size), error semantics (e.g. `ReadFile` wrapping `os.ErrNotExist`), whether a function clones, how logging is wired.
- Editing the doc comment of any exported identifier.

What to update in the skill:

- The "Public API" sections (function lists, types, options).
- The "Choosing the function" table, if the new or changed API affects which function fits which use case.
- The examples table and gotchas, if they use or depend on what changed. The programs under `examples/` must keep compiling (`make build-examples`), and the table must still describe what each one shows.

Before finishing, check that every exported identifier in those packages is covered by the skill, and that nothing in the skill refers to something that no longer exists.

## Keep the examples listed in the skill

The "Examples" section of the skill lists the programs under `examples/` so agents can use them as inspiration. Whenever you add, rename, remove or substantially change an example (a directory under `examples/`), update that table in the same change:

- Every directory under `examples/` must have a row with a link to its `main.go`, what it shows and which function it uses.
- No row may link to an example that no longer exists.
- If a new example shows a pattern not covered by the existing rows, say so in its row so agents know when to pick it.

Before finishing, compare `ls examples/` against the table.
