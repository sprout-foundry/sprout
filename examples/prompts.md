# Example Prompts

Example prompts for common sprout workflows.

## Todo Process

Look through the list of todos in TODO.md and pick one to address. Then use subagents to Build, test then review the solution. If issues come back in the review, continue the build → test → review process until it is ready for release. Once it is ready, mark it complete in the TODO.md file.

## Code Review

Review the changes in my current git diff. Focus on security issues, error handling, and test coverage. Use the code_reviewer persona.

## Bug Investigation

I'm getting a panic when I run `make test ./pkg/webui/...`. The error mentions "nil pointer dereference in handleWebSocket". Find the root cause and fix it.

## Feature Implementation

Implement a new CLI flag `--risk-profile` that accepts readonly, cautious, default, permissive, or unrestricted. Wire it to the existing security classifier. Write tests for the flag parsing and the profile-to-rules mapping. Use the coder persona for implementation, then tester for tests.

## Refactoring

The file `pkg/agent/conversation_handler.go` is over 800 lines. Extract the tool execution logic into a separate `pkg/agent/tool_executor.go` without changing any behavior. Use the refactor persona and verify all existing tests still pass.

## Research

Investigate how the embedding system works in this codebase — what providers exist, how they're configured, and what the current HNSW index format looks like. Also research whether sqlite-vec would be a viable replacement for the current JSONL store.

## Multi-File Change

Add a new subagent persona called "security_auditor" that focuses on OWASP Top 10 vulnerabilities. Create the prompt file in `pkg/agent/prompts/subagent_prompts/security_auditor.md`, register it in the persona factory, add it to the README, and write a test that verifies the persona loads correctly.
