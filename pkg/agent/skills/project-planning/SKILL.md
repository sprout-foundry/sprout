---
name: project-planning
description: Structured planning and project initialization workflow. Use when starting a new project, setting up a new codebase, or creating a project plan.
---

# Project Planning & Setup

Use this skill when setting up a new project or creating a structured project plan.

## When to Activate

- Starting a new project from scratch
- Initializing a new codebase with proper structure
- Creating a project roadmap or development plan
- Setting up CI/CD, testing, and development workflows for a new repository

## Project Setup Workflow

### Phase 1: Discovery
1. Ask about project goals, tech stack preferences, and constraints
2. Determine language, framework, build system, and testing approach
3. Identify target platform(s) and deployment model

### Phase 2: Scaffolding
1. Create project directory structure following language conventions
2. Initialize version control (git init, .gitignore)
3. Set up build configuration (Makefile, package.json, go.mod, Cargo.toml, etc.)
4. Create initial source files with hello-world entry point
5. Add a README.md with project description, setup instructions, and usage

### Phase 3: Quality Infrastructure
1. Set up linting and formatting (golangci-lint, eslint, ruff, etc.)
2. Configure testing framework with an initial test
3. Add CI/CD configuration (GitHub Actions, etc.)
4. Create .gitignore appropriate for the language/toolchain

### Phase 4: Documentation & Agent Context
1. Create AGENTS.md with:
   - Project summary and goals
   - Build, test, and run commands
   - Architecture overview
   - Development conventions
2. Create symlink CLAUDE.md → AGENTS.md for Claude Code compatibility

## Required Output
- Working project scaffold that builds and passes tests
- README.md with setup and usage instructions
- AGENTS.md with project context for AI-assisted development
- All boilerplate committed and ready for feature development
