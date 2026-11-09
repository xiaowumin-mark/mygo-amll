# Repository Guidelines

## Project Structure & Module Organization

This repository has no application source, tests, assets, or package manifests yet. For the first implementation, prefer `src/` for code, `tests/` for tests, and `assets/` for static resources, adapting to the chosen framework. Keep generated output and dependencies out of version control.

## Build, Test, and Development Commands

No build, test, or development commands are configured. When introducing a toolchain, document prerequisites and exact install, development, build, and test commands here. Do not assume `npm test` or other scripts exist.

- `git status --short`: inspect pending changes before committing.
- `git diff --check`: check tracked changes for whitespace errors.

## Coding Style & Naming Conventions

Use UTF-8 and the selected language's standard formatter. Add formatter configuration with the first source files. Use two spaces for YAML and nested Markdown lists. Choose descriptive names following framework conventions. Separate formatting-only changes from behavior changes.

## Testing Guidelines

No testing framework or coverage threshold exists yet. Add tests with the first executable functionality and document the test command. Follow framework test filename conventions. Cover normal behavior, edge cases, and regressions. Report validation that could not run.

## Commit & Pull Request Guidelines

There is no commit history establishing a message convention. Use concise, imperative subjects, such as `Add development setup`. Keep commits and pull requests focused. Describe the change, its purpose, and validation performed; link relevant issues and include screenshots for visible UI changes.

## Security & Configuration

Never commit credentials or local secrets. Ignore local configuration files and provide sanitized examples when configuration is introduced.

<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->
