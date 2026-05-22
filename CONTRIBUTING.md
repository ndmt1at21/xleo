# Contributing to xleo

Thanks for your interest! Bug reports, fixes, and PRs are welcome.

## Reporting bugs

Open an issue with:
- The template `.xlsx` (or a minimal reproduction)
- The Go code you used to render it
- What you expected vs. what happened
- xleo version (`go list -m github.com/ndmt1at21/xleo`)

## Submitting a PR

1. Fork the repo and create a branch off `main`:
   ```bash
   git checkout -b fix/short-description
   ```
2. Make your change. Keep the diff focused — one concern per PR.
3. Run the full check locally:
   ```bash
   go vet ./...
   go test -race ./...
   ```
4. If you add a feature, add a test in the matching `*_test.go` file.
5. If your change touches the render hot path ([render.go](render.go) or
   [template.go](template.go)), run the perf check on a quiet machine:
   ```bash
   go test -run TestPerfTarget    # 200K rows; ~1s on modern hardware
   go test -bench=. -benchmem -run=^$ -benchtime=3x
   ```
   Don't worry if your laptop misses the 1s target by a bit — flag it in the
   PR if you see a *significant* regression vs. `main` on the same machine.
5. Commit with a clear message, push, open a PR against `main`.

## Code style

- `gofmt`-clean (your editor probably does this).
- Public API: keep it minimal. The four entry points (`ParseFile`, `ParseBytes`,
  `Render`, `RenderToFile`) plus `RegisterFunc` are the surface — additions
  need a justification in the PR.
- Performance matters here. If a change adds an allocation in the per-row hot
  path (`render.go`), include benchmark numbers before/after.

## Good first issues

Look for issues tagged `good first issue`. The "Limitations" section in
[README.md](README.md) also lists known gaps that are tractable contributions:

- Operators / conditionals in expressions
- `hrange` nesting inside another `hrange`
- Formula reference rewriting on row expansion

## Questions

Open a discussion or a draft PR — both are fine.
