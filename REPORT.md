# DX Audit Report — go-cache

## 1. CLAUDE.md — helpful for a new developer?

**Overall: Yes.** Covers project purpose, commands, architecture, testing conventions, commit conventions, and environment. A new developer can orient quickly.

**Minor gaps:**

- `cache.go:1` package doc reads "file-based cache for GitHub API responses" — misleading; the package is general-purpose and the GitHub helpers are a small subset.
- `GitHubReposKey` / `GitHubRepoKey` helpers are not mentioned in CLAUDE.md (only discoverable by reading the source).
- `docs/architecture.md:182` shows a stale `fmt.Errorf` example in the path-traversal section; the code now uses `coreerr.E()`.

## 2. Test coverage

```
forge.lthn.ai/core/go-cache  coverage: 70.1% of statements

cache.go:New               83.3%
cache.go:Path              70.0%
cache.go:Get               75.0%
cache.go:Set               66.7%
cache.go:Delete            66.7%
cache.go:Clear             66.7%
cache.go:Age               70.0%
cache.go:GitHubReposKey     0.0%   ← untested
cache.go:GitHubRepoKey      0.0%   ← untested
```

Uncovered paths: both GitHub key helpers, path-traversal rejection in `Path()`, and error injection branches in `Set`/`Delete`/`Clear` (e.g. `EnsureDir` failing, `Write` failing, `Delete` failing).

## 3. Error handling — `coreerr.E()` vs `fmt.Errorf`

**Clean.** No `fmt.Errorf` in source code. All errors use `coreerr.E()`.

One stale documentation example: `docs/architecture.md:182` shows `fmt.Errorf` in an inline code snippet — documentation only, not executed.

## 4. File I/O — `go-io` vs `os.ReadFile` / `os.WriteFile`

**Clean.** All storage operations delegated to `coreio.Medium`. The `os` package is imported only for:
- `os.Getwd()` — to derive the default `baseDir` when none is provided (not a file read/write).
- `os.ErrNotExist` — used as a sentinel to distinguish "file not found" from other read errors.

Neither `os.ReadFile` nor `os.WriteFile` appears anywhere in the codebase.
