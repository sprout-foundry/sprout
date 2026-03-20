# TODO: Security Validator

## Audit the command classification lists

- [ ] **Recoverable `rm -rf` targets**: Verify the current list covers common language/framework dependency and cache directories. Notable gaps to consider: `.mypy_cache`, `.ruff_cache`, `.pytest_cache`, `node_modules/.cache`, `dart_tool`, `.dart_tool`, `.pub-cache`, `bower_components`, `.bundle`, `.npm`, `.pnpm-store`
- [ ] **SAFE shell commands**: Audit whether all build/test/lint commands across common ecosystems are covered (Go, Node, Python, Rust, Java, Swift, Ruby, PHP, Dart/Flutter)
- [ ] **CAUTION patterns**: Consider adding `docker compose down` (removes containers but not volumes), `kubectl delete` (cluster state change), `terraform apply` (infra changes)
- [ ] **DANGEROUS gaps**: Consider `rm -rf .env*` (environment secrets), `DROP TABLE`, `TRUNCATE`, database write operations if accessed via shell

## Implement automated non-interactive evaluation

- [ ] **Replace CAUTION auto-allow with heuristic scoring**: Instead of blanket auto-allowing CAUTION in non-interactive mode, implement a lightweight scoring system that considers: (a) whether the path is within the workspace, (b) whether the operation is part of the current task context, (c) whether the operation has been approved before in this session
- [ ] **Session-level approval memory**: Track user-approved CAUTION operations and auto-approve identical operations in the same session without re-prompting
- [ ] **Context-aware classification**: Use the agent's current task/files context to determine if a `rm -rf` target is relevant (e.g., agent is working on a Go project and runs `rm -rf vendor/` — high confidence this is intentional)
- [ ] **Regex-based command expansion**: Support command alias patterns (e.g., `rmdir` → `rm -rf`, `gco` → `git checkout`) by resolving aliases before classification
- [ ] **Subcommand context awareness**: `git` tool's `branch_delete` is already DANGEROUS, but `shell_command: git branch -d feature-xyz` should also check if the branch has unmerged changes before escalating
