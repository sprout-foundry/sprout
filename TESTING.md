# Testing Pattern

This repository uses standard Go unit tests that do not interfere with regular builds:

- Unit tests live alongside code as files ending with `_test.go`.
- `go build ./...` never compiles `_test.go` files, so builds remain unaffected.
- Run unit tests with:
  - `go test ./...` (all unit tests)
  - `go test ./... -run <Regex>` (filter by name)

For heavier tests (integration/e2e) use build tags so they’re excluded by default:

- Put e2e tests in files named `*_e2e_test.go` with a build tag at the top:
  
  ```go
  //go:build e2e
  // +build e2e
  ```
  
- Run them explicitly: `go test -tags e2e ./...`.

Guidelines:
- Prefer table-driven tests.
- Use `t.TempDir()` for any filesystem interactions.
- Avoid network calls in unit tests; mock or stub dependencies.
- Keep fixtures inline or in `testdata/` directories; Go ignores `testdata/` during builds.

