# Agent instructions

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
- The templates and gotchas, if they use or depend on what changed. Make sure the templates still compile against the new API.

Before finishing, check that every exported identifier in those packages is covered by the skill, and that nothing in the skill refers to something that no longer exists.
