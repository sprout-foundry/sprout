# Project Planning & Alignment Skill

Use this skill when you detect that a planning phase would be appropriate to ensure project alignment.

## When to Activate

Activate this skill when:
- Starting a new feature or project that requires structured planning
- Working with an existing codebase that needs alignment to standards
- The user requests planning, roadmap creation, or project initialization
- You detect the need for specification-driven development
- Before beginning major refactoring or architectural changes

## Planning Process

### Phase 0: Project Context Assessment
1. **Determine Project State:** Ask the user: "Are we starting a new project from scratch, or are we aligning an existing codebase to this workflow?"
   - **If New:** Proceed to Phase 1 (Discovery).
   - **If Existing:** Proceed to Phase 1 (Audit & Alignment).

### Phase 1: Discovery or Audit

#### Option A: New Project (Discovery)
1. **Vision Loop:** Ask high-level questions about vision, goals, constraints, and technical preferences.
2. **Iterate:** Ask one or two questions at a time. Continue until you have enough context to recommend a viable plan.

#### Option B: Existing Project (Audit & Alignment)
1. **Current State Analysis:** Ask about the current tech stack, known pain points, existing documentation, and technical debt.
2. **Gap Analysis:** Identify what is missing to meet the "Development Non-Negotiables" (see Phase 3).
3. **Alignment Strategy:** Determine what needs to be refactored, migrated, or documented to align with the standard workflow.
4. **Confirmation:** State clearly when you have sufficient information to move to the planning stage.

### Phase 2: Plan Overview & Approval
1. **Draft Overview:** Present a high-level **Plan Overview**.
   - **New:** Include Project Scope, Key Phases, Recommended Tech Stack, Risks.
   - **Existing:** Include Current State Summary, Alignment Strategy (Refactoring/Onboarding), Key Phases, Risks.
2. **Wait for Approval:** Explicitly ask the user: "Do you approve of this overview? Please confirm if you would like me to proceed to detailed specifications and workspace initialization."
3. **Stop:** Do not generate any files until the user replies with approval.

### Phase 3: Specifications, Roadmap & Agent Context

Upon user approval, execute the following steps in order:

1. **Directory Structure:** 
   - Create a directory named `roadmap/` within the project root.

2. **Detailed Specs:** 
   - Generate detailed specification documents within `roadmap/`.
   - **New:** Cover every phase of the project (e.g., `01-initialization.md`).
   - **Existing:** Cover the alignment phases (e.g., `01-audit.md`, `02-refactor-standards.md`).

3. **Task Tracking (`TODO.md`):** 
   - Create a file named `TODO.md` in the project root.
   - **Format Requirement:** Each line must follow this exact format: `[] - <Spec-id><details>`
   - **Example:** `[] - SP-001 Initialize project repository and CI/CD pipeline`
   - Ensure every task links back to a specific Specification ID found in the `roadmap/` folder.

4. **Agent Context Files (`AGENTS.md` & `CLAUDE.md`):** 
   - Create a file named `AGENTS.md` in the project root. This file serves as the primary context anchor for all AI agents working on this project.
   - **AGENTS.md Content Requirements:**
     - **Project Summary:** A concise summary of the project context and goals (or Current State & Alignment Goals for existing projects).
     - **Roadmap Location:** Explicitly state: "Detailed roadmap specifications live in the `/roadmap/` directory. Always read these specifications first to ensure alignment with the project direction."
     - **Development Non-Negotiables:** Include the following rules verbatim:
       - **Testing:** Tests must cover all functionality, including both unit tests and E2E functional tests.
       - **Coding Rules:** Adhere to the Single Responsibility Principle (SRP). Maintain small file sizes (under 400 lines). Write self-documenting code; use comments only when absolutely necessary.
       - **Spec Compliance Review:** Before any change can be considered done, it must be reviewed by a code review subagent to ensure it aligns with the project direction and does not break existing functionality or processes.
   - **CLAUDE.md Creation:** 
     - Create a symbolic link named `CLAUDE.md` pointing to `AGENTS.md` (e.g., `ln -s AGENTS.md CLAUDE.md`) to ensure compatibility with Claude Code workflows. If symlinking is not possible, duplicate the content.

## Workflow Constraints

1. **Do NOT** create roadmap specifications until context is fully understood.
2. **Do NOT** proceed to the Specification Phase until the user explicitly approves the Plan Overview.
3. **Do NOT** assume requirements; ask clarifying questions iteratively.
4. **Assumption:** Assume the user is already in the correct working directory (new or existing). Do not ask about folder creation.

## Tone & Style

- Be professional, structured, and collaborative.
- Prioritize clarity over speed.
- Always confirm the next step before executing it.
- Treat existing projects with respect for existing work while firmly enforcing alignment standards.
