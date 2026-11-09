# Repository Guidelines

## Scope and Source of Truth

Build an embeddable Go lyric library with mygo Native UI and custom drawing,
using AMLL as the behavioral reference. Performance and bounded memory are
acceptance criteria. Read `docs/implementation.md`, `docs/architecture.md`,
`docs/performance.md`, and `docs/upstreams.md` before changing these boundaries.
Distinguish implemented capabilities from proposals and unmeasured targets.

The current implementation contains `lyric`, `timeline`, a console example,
and an experimental Native UI spike in `examples/native`. See `docs/m1-native.md`
for validation gaps; there is no stable Native component API. Format parsers and
dynamic backgrounds remain planned. Do not introduce empty packages or describe
the spike as a finished renderer.

## Structure and Dependencies

- Module: `github.com/xiaowumin-mark/mygo-amll`; Go 1.27.3+.
- Use conventional Go package directories without an extra `src/` layer.
- Keep pure data/time packages independent of mygo, audio playback, and I/O.
- Tests live beside code as `*_test.go`; runnable integrations in `examples/`.
- `ref/` and `.codegraph/` are ignored local references/tool state, not dependencies.
- Keep mygo at its verified pinned version for the Native spike; use only public
  APIs, never upstream `internal/*` or private resource methods.
- License is AGPL-3.0-only. Preserve upstream attribution and review file-level
  licenses before importing code, shaders, or assets.

## Commands and Formatting

- `python scripts/check.py`: gofmt check, vet, race tests, title checker tests,
  and tracked whitespace validation. Python 3.11+ is required.
- `python scripts/check.py --no-race`: local fallback only when the C toolchain
  is unavailable; CI must still run race checks.
- `gofmt -w lyric timeline examples`: format the current Go sources.
- `go run ./examples/timeline`: run the UI-independent example.
- `go test ./... -run '^$' -bench . -benchmem -count 5`: collect core benchmarks.
- `git status --short` and `git diff --check`: inspect pending changes.

Use UTF-8, LF, gofmt, and standard-library tests. YAML and nested Markdown lists
use two spaces. Follow `.editorconfig` and `docs/development.md`. Do not assume
npm, a graph environment, or a package manager is required for the pure core.

## Correctness, Performance, and Ownership

Document timing units, half-open intervals, slice ownership, and concurrency.
Preserve overlapping lines and seek correctness; active playback and persistent
visual highlighting are different states. Reusable query buffers belong to the
caller. Never expose mutable backing slices from immutable documents.

Keep parsing, I/O, shaping, shader compilation, and full-document traversal out
of the per-frame hot path. Budget caches by bytes and entries, define invalidation
and cancellation, and release old document references on song changes. Draw may
run multiple times per frame; do not advance business state there. UI resources
belong to the UI thread. Explicitly distinguish UTF-8 byte, rune, and UTF-16 indices.

Tests cover normal, edge, error, overlap, and seek behavior. Use synthetic or
authorized fixtures and deterministic clocks. For performance changes include
before/after benchmark evidence and allocation results. Real window/GPU/RSS
measurements cannot be replaced by pure Go or headless CPU tests.

## Contributions and Automation

Follow `CONTRIBUTING.md`, `GOVERNANCE.md`, and `docs/commit-convention.md`.
Use Conventional Commit PR titles and squash merge. Public APIs, resource models,
and major dependencies require a design discussion. Keep changes focused;
separate formatting-only changes from behavior changes.

GitHub Actions are pinned to full commits and PR checks are read-only. Do not
execute PR code through privileged triggers. Release-please maintains the release
PR, tags, and changelog; do not fabricate a published version or bypass review.
See `docs/releases.md` for manual GitHub settings that YAML cannot enable.

Never commit credentials, local secrets, reference repositories, generated output,
or profiling artifacts. Report any check or platform validation that could not run.

<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->
