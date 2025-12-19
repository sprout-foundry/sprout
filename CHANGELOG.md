# Changelog

All notable changes to ledit will be documented in this file.

## [v0.9.2] - 2025-12-19

- Updates 2 files - Test response from mock provider (286f514)
- Updates 27 files - Test response from mock provider (470b8a6)
- Updates 46 files - Updates code formatting and removes unused tool summary logic (b1495df)
- Updates pkg/agent_providers/generic_provider_test.go - Updates provider tests (b7e7ce0)
- Updates 12 files - Updates DeepSeek provider support and custom provider handling (80d64fe)
- Merge remote-tracking branch 'origin/main' (bc68a00)
- Updates 3 files - Updates interactive mode detection logic (b4a5641)
- docs: Update changelog for v0.9.1 (0fcae62)

## [v0.9.1] - 2025-12-18

- Updates 11 files - Updates webui static files for go install support (0d74d40)
- docs: Update changelog for v0.9.0 (3d56cd1)

## [v0.9.0] - 2025-12-18

- Updates .github/workflows/release.yml - Updates release workflow (33288dc)
- Updates 3 files - Updates terminal suspension for cross platform support (f43e78a)
- Updates .github/workflows/build.yml - Updates build workflow (ef19b0a)
- Updates .github/workflows/build.yml - Updates GitHub workflow build (05349d1)
- Deletes integration_tests/test_new_user_initialization.sh - Deletes init test (7ae83e8)
- Updates integration_tests/test_new_user_initialization.sh - Updates CI test (176f315)
- Updates integration_tests/test_new_user_initialization.sh - Updates CI logs (7413928)
- Updates integration_tests/test_new_user_initialization.sh - Updates tests (adbfbdf)
- Updates 2 files - Updates test runner output and static file handling (77a8bf2)
- Updates 2 files - Updates integration tests with CI skips and options (f4b73f1)
- Updates integration_test_runner.py - Updates integration test runner output (e1021d7)
- Updates integration_tests/test_new_user_initialization.sh - Updates CI tests (5fd7d5d)
- [feat/markdown] Updates 3 files - Updates CI output with markdown formatting (655a2ee)
- Updates 5 files - Updates React component performance and structure (e4696b6)
- Updates 9 files - Updates UI components and CI configurations (42f37c3)
- Updates Makefile - Updates UI build to run in non-CI environment (88fbd46)
- Updates .github/workflows/build.yml - Updates GitHub Actions workflow pipeline (b5e45d4)
- Updates .github/workflows/build.yml - Updates GitHub build workflow (786db17)
- Updates 10 files - Updates testing and provider configurations (6ac2225)
- Updates 53 files - Updates input system with completion support (285dc2c)
- Updates 6 files - Updates terminal input handling and signal processing (e4a0ca8)
- Updates pkg/console/input_reader.go - Updates escape sequence handling (e66471a)
- Initial commit (8698769)
- Merge branch 'main' of github.com:alantheprice/ledit (6f6f6c6)
- feat: add input reader and update webui components (f59963b)
- Updates 17 files - Updates code review and agent completion logic (69e1845)
- Updates 16 files - Updates tool params, completion, provider config (51d9a62)
- Updates 16 files - Updates custom model providers and error handling (1a213e5)
- Updates 20 files - Updates context discovery and provider configuration (83872d0)
- Updates 7 files - Updates terminal with PTY support and resize handling (58ed14c)
- Adds 11 files - Adds conversation handler refactoring and display logic (1d330cf)
- Updates 4 files - Updates interrupt handling with pause resume functionality (3aef42f)
- Updates 10 files - Updates for LM Studio local auth, web UI API endpoints, and terminal improvements (96f7dad)
- Updates 25 files - Updates web UI with terminal and enhanced logging (bc9bb3f)
- Updates 9 files - Updates web UI port and adds sidebar component (e98eac1)
- feat: implement ChatGPT-style auto-scaling input and 3-view navigation system (c6ed5b5)
- Updates 2 files - Updates error handling for agent creation failures (262b8ed)
- Deletes 13 files - Deletes ZAI optimization summary and refactors logging (9711537)
- Updates 2 files - Updates gitignore and adds package json (363747e)
- Updates 16 files - Updates web UI with file editor and API enhancements (ab2d5a7)
- Updates 3 files - Updates conversation handler with enhanced logging (5799ec9)
- Updates cmd/agent_simple.go; Adds ledit_output.log; Updates pkg/agent/agent.go; Updates pkg/agent/api_client.go; Updates pkg/agent/blank_iteration_test.go; Updates pkg/agent/completion_policy.go; Updates pkg/agent/conversation.go; Updates pkg/agent/conversation_flow_test.go; Updates pkg/agent/conversation_handler.go; Updates pkg/agent/conversation_termination_test.go; Updates pkg/agent/tool_executor.go; Updates pkg/agent/utils.go; Adds pkg/console/terminal_manager.go; Updates pkg/webui/server.go - Updates ToolExecutor to publish tool execution events (29f1921)
- Updates 16 files - Updates tool call deduplication and UI improvements (aa4019d)
- Updates 2 files - Updates streaming to respect environment variable (65f1e16)
- Updates 113 files - Updates React web UI integration and removes legacy console components (d86f846)
- Updates 2 files - Updates dependencies in go modules (3c5f3ff)
- Updates 26 files - Updates provider system with generic configuration (d8f7ef6)
- Deletes 8 files - Deletes workspace package implementation files (1571a87)
- docs: Update changelog for v0.8.4 (75b635b)
- Clean up debug files and update VSCode settings (0a59bb8)
- Fix ZAI provider streaming completion detection (d74c642)
- Fix ZAI provider streaming issues and add ANSI sanitization (86fbe54)
- Updates pkg/agent/api_client.go - Updates streaming response logging and completion handling (c2d6532)
- Updates 2 files - Updates tests for LMStudio and fixes Go lint error (dff8e35)

## [v0.8.4] - 2025-10-26

- Updates cmd/agent.go - Updates streaming to respect environment variable (a5ab087)
- docs: Update changelog for v0.8.3 (a0f811b)

## [v0.8.3] - 2025-10-12

- Updates 5 files - Updates ZAI token pruning test case expectations (d766608)
- [feat/zai-integration] Updates 5 files - Updates debug logging and pruning (4c2f789)
- [feat/zai-integration] Updates 16 files - Updates security and pruning logic (acebdb5)
- [feat/zai-integration] Updates 12 files - Updates agent behavior and ZAI support (b230df7)
- [feat/zai-integration] Deletes 4 files - Deletes domain entities and services (6d3c021)
- [feat/zai-integration] Updates 4 files - Updates docs and removes math pkg (3e88c80)
- [feat/zai-integration] Updates 9 files - Updates workflow error handling (e71cd63)
- Updates pkg/agent_providers/zai.go - Updates ZAI provider with penalties (a914be5)
- Updates 10 files - Updates streaming and provider-specific optimizations (60cdd81)
- Updates 9 files - Updates provider support and console focus indicators (9ebecc8)
- Updates 3 files - Updates shell command output with status indicators (8c6bb2d)
- Merge remote-tracking branch 'origin/main' (195035e)
- Updates 5 files - Updates for Z.AI connection check and timeout configurations (0f74de6)
- Updates 14 files - Updates search limits and adds ZAI provider support (7b26345)
- docs: Update changelog for v0.8.0 (963479e)

## [v0.8.0] - 2025-10-09

- Initial release


## [v0.7.0] - 2025-09-29

- Initial release
