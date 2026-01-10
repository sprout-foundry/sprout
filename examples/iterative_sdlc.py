#!/usr/bin/env python3
"""
Production-Ready Iterative Agent-Driven SDLC Workflow

This script automates the full software development lifecycle with:
- Real evaluation (build status, test parsing, semantic assessment)
- Iterative SDLC cycles with feedback loops
- Context passing between phases
- Issue tracking and escalation
- Metrics and quantification

RECOMMENDED: For simple features, use plan mode directly:
    ledit plan "Build X"
    ledit plan --execute plan.md

Use this script for:
- Complex multi-phase projects needing orchestration
- Automated pipelines with real validation
- Projects requiring metrics and issue tracking
"""

import subprocess
import json
import os
import sys
import re
from dataclasses import dataclass, asdict, field
from typing import List, Dict, Optional, Tuple
from pathlib import Path
from datetime import datetime

# Color codes for terminal output
class Colors:
    GREEN = '\033[92m'
    RED = '\033[91m'
    YELLOW = '\033[93m'
    BLUE = '\033[94m'
    RESET = '\033[0m'
    BOLD = '\033[1m'

@dataclass
class Issue:
    """Represents a discovered issue during development."""
    phase: str
    severity: str  # blocker, critical, major, minor
    type: str  # build, test, code, design, requirement
    description: str
    evidence: str = ""
    timestamp: str = field(default_factory=lambda: datetime.now().isoformat())
    attempted_fixes: List[str] = field(default_factory=list)

@dataclass
class Metrics:
    """Metrics collected during development."""
    files_modified: int = 0
    lines_added: int = 0
    files_created: int = 0
    tests_passed: int = 0
    tests_failed: int = 0
    build_success: bool = False
    linter_errors: int = 0
    todos_created: int = 0
    todos_completed: int = 0

@dataclass
class TaskPhase:
    """Represents a phase in the iterative SDLC."""
    name: str
    prompt: str
    evaluation_type: str  # 'build', 'test', 'agent_eval', 'file_check', 'multi'
    completed: bool = False
    use_plan_mode: bool = False
    plan_file: Optional[str] = None
    output_file: Optional[str] = None  # For plan-based phases
    # Per-phase configuration
    system_prompt_file: Optional[str] = None  # Custom system prompt for this phase
    model: Optional[str] = None  # Override default model for this phase
    provider: Optional[str] = None  # Override default provider for this phase

@dataclass
class IterationResult:
    """Result of a single agent iteration."""
    phase: str
    iteration: int
    success: bool
    output: str
    metrics: Metrics
    issues: List[Issue]
    notes: str = ""
    should_revisit: List[str] = field(default_factory=list)  # Phases to revisit

class SDLCManager:
    """Production-ready SDLC workflow manager."""

    def __init__(self, config: Dict):
        self.config = config
        self.project_name = config['project_name']
        self.base_dir = Path(config.get('base_dir', '.'))
        self.phases = self._create_phases()
        self.results: List[IterationResult] = []
        self.max_iterations = config.get('max_iterations_per_phase', 5)
        self.max_global_iterations = config.get('max_global_iterations', 20)
        self.global_iteration = 0
        self.ledit_cmd = config.get('ledit_cmd', './ledit')
        self.agent_flags = config.get('agent_flags', [])
        self.issues: List[Issue] = []
        self.phase_context: Dict[str, Dict] = {}  # Context passed between phases
        self.current_metrics = Metrics()

    def _create_phases(self) -> List[TaskPhase]:
        """Create SDLC phases from configuration."""
        phases = []
        for phase_data in self.config['phases']:
            phases.append(TaskPhase(
                name=phase_data['name'],
                prompt=phase_data['prompt'],
                evaluation_type=phase_data.get('evaluation_type', 'agent_eval'),
                use_plan_mode=phase_data.get('use_plan_mode', False),
                plan_file=phase_data.get('plan_file'),
                output_file=phase_data.get('output_file'),
                system_prompt_file=phase_data.get('system_prompt_file'),
                model=phase_data.get('model'),
                provider=phase_data.get('provider')
            ))
        return phases

    def run_ledit_agent(self, prompt: str, system_prompt_file: Optional[str] = None,
                         model: Optional[str] = None, provider: Optional[str] = None) -> Tuple[str, int]:
        """Execute ledit agent. Returns (output, exit_code)."""
        cmd = [self.ledit_cmd, 'agent', '--no-stream', '--no-web-ui']

        # Add custom system prompt if specified
        if system_prompt_file:
            cmd.extend(['--system-prompt', system_prompt_file])

        # Add model/provider if specified
        if model:
            cmd.extend(['--model', model])
        if provider:
            cmd.extend(['--provider', provider])

        cmd.extend(self.agent_flags)
        cmd.append(prompt)

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                cwd=str(self.base_dir),
                timeout=self.config.get('agent_timeout', 600)
            )
            return (result.stdout + result.stderr, result.returncode)

        except subprocess.TimeoutExpired:
            return ("TIMEOUT", -1)
        except Exception as e:
            return (f"ERROR: {e}", -1)

    def run_ledit_plan(self, prompt: str, plan_file: str) -> Tuple[bool, str]:
        """Execute ledit plan mode. Returns (success, plan_path or error)."""
        cmd = [self.ledit_cmd, 'plan'] + self.agent_flags + [prompt, '--output', plan_file]

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                cwd=str(self.base_dir)
            )

            # Check if plan file was created
            if os.path.exists(plan_file):
                return (True, plan_file)
            else:
                return (False, result.stderr or result.stdout or "Plan file not created")

        except Exception as e:
            return (False, f"ERROR: {e}")

    def execute_plan(self, plan_file: str) -> Tuple[str, int]:
        """Execute a saved plan. Returns (output, exit_code)."""
        cmd = [self.ledit_cmd, 'plan', '--execute', plan_file] + self.agent_flags

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                cwd=str(self.base_dir),
                timeout=self.config.get('agent_timeout', 900)  # Longer for execution
            )
            return (result.stdout + result.stderr, result.returncode)

        except subprocess.TimeoutExpired:
            return ("TIMEOUT", -1)
        except Exception as e:
            return (f"ERROR: {e}", -1)

    def evaluate_build(self) -> Tuple[bool, Metrics, List[Issue]]:
        """Run build and evaluate with real output parsing."""
        print(f"\n{Colors.BLUE} evaluating build...{Colors.RESET}")

        # Run go build
        result = subprocess.run(
            ['go', 'build', './...'],
            capture_output=True,
            text=True,
            cwd=str(self.base_dir)
        )

        metrics = Metrics()
        issues = []

        if result.returncode == 0:
            metrics.build_success = True
            print(f"{Colors.GREEN}✓ Build successful{Colors.RESET}")
            return (True, metrics, issues)

        # Parse build errors
        print(f"{Colors.RED}✗ Build failed{Colors.RESET}")
        stderr = result.stderr + result.stdout

        # Extract compilation errors
        error_pattern = re.compile(r'([^:]+):(\d+):(\d+):\s*(.+)')
        for match in error_pattern.finditer(stderr):
            file_path, line, col, error_msg = match.groups()
            issues.append(Issue(
                phase='build',
                severity='blocker',
                type='build',
                description=f"Compilation error: {error_msg}",
                evidence=f"{file_path}:{line}:{col}"
            ))

        if not issues:
            issues.append(Issue(
                phase='build',
                severity='blocker',
                type='build',
                description=f"Build failed: {stderr[:200]}",
                evidence=stderr[:500]
            ))

        return (False, metrics, issues)

    def evaluate_tests(self) -> Tuple[bool, Metrics, List[Issue]]:
        """Run tests and parse results."""
        print(f"\n{Colors.BLUE}Running tests...{Colors.RESET}")

        # Run go test with json output
        result = subprocess.run(
            ['go', 'test', './...', '-json', '-v'],
            capture_output=True,
            text=True,
            cwd=str(self.base_dir),
            timeout=self.config.get('test_timeout', 300)
        )

        metrics = Metrics()
        issues = []

        if result.returncode == 0 and 'PASS' in result.stdout:
            print(f"{Colors.GREEN}✓ All tests passed{Colors.RESET}")
            metrics.tests_passed = result.stdout.count('"Action":"pass"')
            return (True, metrics, issues)

        # Parse test failures
        stderr = result.stderr + result.stdout

        # Count passes and fails
        metrics.tests_passed = stderr.count('"Action":"pass"')
        metrics.tests_failed = stderr.count('"Action":"fail"')

        # Extract failure details
        fail_pattern = re.compile(r'"Test":"([^"]+)".*?"Action":"fail".*?"Output":"([^"]*)"')
        for match in fail_pattern.finditer(stderr):
            test_name = match.group(1)
            output = match.group(2).replace('\\n', '\n')[:200]

            if metrics.tests_failed < 5:  # Track top 5 failures
                issues.append(Issue(
                    phase='testing',
                    severity='critical',
                    type='test',
                    description=f"Test failure: {test_name}",
                    evidence=output
                ))

        print(f"{Colors.RED}✗ {metrics.tests_failed} test(s) failed{Colors.RESET}")

        return (False, metrics, issues)

    def _check_plan_adherence(self, phase: TaskPhase, agent_output: str, agent_files_changed: List[str] = None) -> List[Issue]:
        """Check if agent is adhering to plan, not adding scope creep."""
        if not phase.plan_file or not os.path.exists(phase.plan_file):
            return []

        with open(phase.plan_file, 'r') as f:
            plan_content = f.read()

        issues = []

        # Check for DEFER todos (good scope management)
        defer_pattern = re.compile(r'add_todos.*DEFER:', re.IGNORECASE)
        if defer_pattern.search(agent_output):
            # Agent is properly deferring improvements - this is GOOD
            return []

        # Check for scope expansion signals
        # These patterns suggest agent is adding beyond plan scope
        expansion_patterns = [
            r'I discovered.*so [Ii][\']',
            r'Realized that.*also needs',
            r'Better approach.*adding',
            r'Should also implement',
            r'While implementing.*added',
        ]
        for pattern in expansion_patterns:
            if pattern.search(agent_output, re.IGNORECASE):
                issues.append(Issue(
                    phase=phase.name,
                    severity='BLOCKER',
                    type='scope_creep',
                    description="Agent appears to be expanding scope beyond plan.md",
                    evidence="Detected scope expansion language in agent output",
                ))
                break

        # If agent created todos for items not in plan
        # This is harder to detect, but we can warn if MANY new todos appear
        todo_pattern = re.compile(r'add_todos.*\{.*\}', re.DOTALL)
        todos_created = len(todo_pattern.findall(agent_output))
        if todos_created > 3:  # Threshold for suspicious activity
            # Might be many legitimate todos, but flag as potential issue
            pass

        return issues

    def evaluate_with_agent(self, output: str, phase_name: str, criteria: List[str]) -> Tuple[bool, List[Issue]]:
        """Use agent to semantically evaluate output."""
        print(f"\n{Colors.BLUE}Semantic evaluation by agent...{Colors.RESET}")

        criteria_str = '\n'.join(f"- {c}" for c in criteria)
        eval_prompt = f"""
Evaluate the following output for the '{phase_name}' phase.

SUCCESS CRITERIA:
{criteria_str}

OUTPUT TO EVALUATE:
---
{output[:3000]}
---

Respond with a JSON object:
{{
    "success": true/false,
    "confidence": 0.0-1.0,
    "issues": [
        {{"severity": "blocker/critical/major/minor", "type": "code/design/requirement", "description": "..."}}
    ],
    "notes": "Brief explanation"
}}
Only respond with the JSON, nothing else.
"""

        response, exit_code = self.run_ledit_agent(eval_prompt)

        # Try to parse JSON response
        json_match = re.search(r'\{[^{}]*"success"[^{}]*\}', response, re.DOTALL)
        if json_match:
            try:
                eval_result = json.loads(json_match.group(0))
                issues = []
                for issue_data in eval_result.get('issues', []):
                    issues.append(Issue(
                        phase=phase_name,
                        severity=issue_data.get('severity', 'major'),
                        type=issue_data.get('type', 'code'),
                        description=issue_data.get('description', '')
                    ))
                return (eval_result.get('success', False), issues)
            except json.JSONDecodeError:
                pass

        # Fallback: simple keyword check
        success = any(c.lower() in output.lower() for c in criteria[:3])
        return (success, [])

    def evaluate_review_with_linters(self, phase_name: str = 'review') -> Tuple[bool, Metrics, List[Issue]]:
        """Run code review tools and parse output into structured issues."""
        print(f"\n{Colors.BLUE}Running code review analysis...{Colors.RESET}\n")

        metrics = Metrics()
        issues = []

        # Run go vet
        print("  Running go vet...")
        vet_result = subprocess.run(
            ['go', 'vet', './...'],
            capture_output=True,
            text=True,
            cwd=str(self.base_dir),
            timeout=60
        )

        if vet_result.returncode != 0:
            vet_issues = self._parse_go_vet(vet_result.stdout + vet_result.stderr)
            issues.extend(vet_issues)
            metrics.linter_errors += len(vet_issues)
            print(f"    {Colors.YELLOW}{len(vet_issues)} vet issues found{Colors.RESET}")
        else:
            print(f"    {Colors.GREEN}✓ go vet passed{Colors.RESET}")

        # Try gofmt/linting if available
        fmt_result = subprocess.run(
            ['gofmt', '-l', './...'],
            capture_output=True,
            text=True,
            cwd=str(self.base_dir),
            timeout=30
        )

        if fmt_result.returncode != 0:
            formatted_files = fmt_result.stdout.strip().split('\n')
            for file_path in formatted_files[:10]:  # Limit to top 10
                if file_path.strip():
                    issues.append(Issue(
                        phase=phase_name,
                        severity='minor',
                        type='code_quality',
                        description=f"File not formatted: {file_path}",
                        evidence=file_path
                    ))
            print(f"    {Colors.YELLOW}{len(formatted_files)} files need formatting{Colors.RESET}")
            metrics.linter_errors += len(formatted_files)
        else:
            print(f"    {Colors.GREEN}✓ All files formatted{Colors.RESET}")

        # Assess success based on linter results
        success = len([i for i in issues if i.severity == 'blocker']) == 0

        print(f"\n{Colors.BLUE}Review complete.{Colors.RESET}")
        return (success, metrics, issues)

    def _parse_go_vet(self, output: str) -> List[Issue]:
        """Parse go vet output into structured issues."""
        issues = []
        lines = output.split('\n')

        for line in lines:
            # Pattern: path/file.go:line:column: error message
            vet_pattern = re.compile(r'([^:]+):(\d+):(\d+):\s*(.+)')

            for match in vet_pattern.finditer(line):
                file_path, line_no, col_no, message = match.groups()
                severity = 'critical' if 'shadow' in message.lower() else 'major'
                issues.append(Issue(
                    phase='review',
                    severity=severity,
                    type='code_quality',
                    description=message.strip()[:200],
                    evidence=f"{file_path}:{line_no}:{col_no}"
                ))

        return issues

    def evaluate_file_changes(self, metrics: Metrics) -> Tuple[bool, List[Issue]]:
        """Evaluate file changes and metrics."""
        print(f"\n{Colors.BLUE}Analyzing file changes...{Colors.RESET}")

        # Run git status to check changes
        git_result = subprocess.run(
            ['git', 'status', '--short'],
            capture_output=True,
            text=True,
            cwd=str(self.base_dir)
        )

        if git_result.returncode == 0:
            lines = git_result.stdout.strip().split('\n')
            metrics.files_modified = sum(1 for line in lines if line.strip())

            # Count created files
            metrics.files_created = sum(1 for line in lines if line.strip().startswith('??'))

        print(f"  Files modified: {metrics.files_modified}")
        print(f"  Files created: {metrics.files_created}")

        # Check if any changes made
        success = metrics.files_modified > 0 or metrics.files_created > 0

        if not success:
            issues = [Issue(
                phase='implementation',
                severity='blocker',
                type='code',
                description="No files were modified or created"
            )]
            return (False, issues)

        return (True, [])

    def evaluate_phase(self, phase: TaskPhase, output: str) -> Tuple[bool, Metrics, List[Issue], List[str]]:
        """Evaluate phase output based on evaluation type."""
        metrics = Metrics()
        issues = []
        should_revisit = []

        eval_type = phase.evaluation_type

        if eval_type == 'build':
            success, metrics, issues = self.evaluate_build()
            if not success:
                should_revisit.append('implementation')

        elif eval_type == 'test':
            success, metrics, issues = self.evaluate_tests()
            if not success:
                should_revisit.append('implementation')
                should_revisit.append('testing')

        elif eval_type == 'file_check':
            success, issues = self.evaluate_file_changes(metrics)
            # Combine with plan content if available
            if phase.plan_file and os.path.exists(phase.plan_file):
                with open(phase.plan_file, 'r') as f:
                    plan_content = f.read()
                    # Check if tasks mentioned in plan are addressed
                    success = len(issues) == 0

        elif eval_type == 'multi':
            # Run multiple evaluations
            build_ok, build_metrics, build_issues = self.evaluate_build()
            metrics.build_success = build_ok
            issues.extend(build_issues)

            test_ok, test_metrics, test_issues = self.evaluate_tests()
            metrics.tests_passed = test_metrics.tests_passed
            metrics.tests_failed = test_metrics.tests_failed
            issues.extend(test_issues)

            success = build_ok and test_ok
            if not success:
                should_revisit.append('implementation')
                should_revisit.append('testing')

            # Pass review issues to next phase automatically
            self.issues.extend(issues)

        # Check for plan adherence / scope creep
        phase_adherence_issues = self._check_plan_adherence(phase, output)
        if phase_adherence_issues:
            print(f"\n{Colors.RED}🚨 Scope CREEP DETECTED{Colors.RESET}")
            for issue in phase_adherence_issues:
                print(f"  {Colors.RED}{issue.severity.upper()}{Colors.RESET}: {issue.description}")

            should_revisit.extend(['planning'])
            issues.extend(phase_adherence_issues)

        return (success, metrics, issues, should_revisit)

    def run_phase(self, phase: TaskPhase, context: Optional[Dict] = None) -> IterationResult:
        """Run a phase with iteration and proper evaluation."""
        print(f"\n{Colors.BOLD}{'='*70}{Colors.RESET}")
        print(f"{Colors.BOLD}# PHASE: {phase.name.upper()}{Colors.RESET}")
        print(f"{Colors.BOLD}{'='*70}{Colors.RESET}\n")

        iteration = 0
        last_output = ""
        phase_issues: List[Issue] = []
        phase_metrics = Metrics()
        should_revisit: List[str] = []

        while iteration < self.max_iterations and self.global_iteration < self.max_global_iterations:
            iteration += 1
            self.global_iteration += 1

            print(f"\n{Colors.BLUE}--- Iteration {iteration} / {self.max_iterations} (Global: {self.global_iteration} ---{Colors.RESET}\n")

            # Build enhanced prompt with context
            prompt = self._build_enhanced_prompt(phase, iteration, last_output, phase_issues, context)

            output = ""
            exit_code = 0

            # Execute based on phase type
            if phase.use_plan_mode:
                # Planning phase
                if phase.plan_file:
                    success, result = self.run_ledit_plan(prompt, phase.plan_file)
                    if success:
                        # Read the plan content
                        with open(phase.plan_file, 'r') as f:
                            output = f.read()
                        print(f"{Colors.GREEN}✓ Plan created: {phase.plan_file}{Colors.RESET}")
                    else:
                        print(f"{Colors.RED}✗ Plan creation failed: {result}{Colors.RESET}")
                        output = result
                        exit_code = 1
                else:
                    output, exit_code = self.run_ledit_agent(prompt,
                                                            system_prompt_file=phase.system_prompt_file,
                                                            model=phase.model,
                                                            provider=phase.provider)
            else:
                # Standard agent phase
                output, exit_code = self.run_ledit_agent(prompt,
                                                        system_prompt_file=phase.system_prompt_file,
                                                        model=phase.model,
                                                        provider=phase.provider)

            last_output = output
            phase_metrics = Metrics()

            # Evaluate the output
            success, metrics, issues, revisit_list = self.evaluate_phase(phase, output)
            phase_metrics = metrics
            phase_issues.extend(issues)
            should_revisit = revisit_list

            # Print evaluation results
            print(f"\n{Colors.BOLD}Evaluation Results:{Colors.RESET}")
            print(f"  Success: {Colors.GREEN if success else Colors.RED}{success}{Colors.RESET}")

            if issues:
                print(f"  Issues: {len(issues)}")
                for i, issue in enumerate(issues[:5], 1):
                    color = Colors.RED if issue.severity in ['blocker', 'critical'] else Colors.YELLOW
                    print(f"    {i}. {color}{issue.severity.upper()}{Colors.RESET}: {issue.type} - {issue.description}")

            if should_revisit:
                print(f"  Revisit phases: {', '.join(should_revisit)}")

            # Check success criteria
            if success:
                print(f"\n{Colors.GREEN}✨ Phase '{phase.name}' completed successfully!{Colors.RESET}")
                phase.completed = True
                return IterationResult(
                    phase=phase.name,
                    iteration=iteration,
                    success=True,
                    output=output,
                    metrics=phase_metrics,
                    issues=[],
                    notes=f"Completed in {iteration} iteration(s)"
                )
            else:
                print(f"\n{Colors.YELLOW}⚠ Phase '{phase.name}' incomplete.{Colors.RESET}")
                blocker_issues = [i for i in issues if i.severity == 'blocker']
                if blocker_issues:
                    print(f"  {Colors.RED}BLOCKER: Cannot proceed.{Colors.RESET}")
                    break

        # Phase failed after max iterations
        print(f"\n{Colors.RED}✗ Phase '{phase.name}' reached max iterations.{Colors.RESET}")

        # Check if should escalate
        if phase.name in self.config.get('critical_phases', []):
            print(f"{Colors.RED}CRITICAL PHASE FAILED - Stopping workflow{Colors.RESET}")

        return IterationResult(
            phase=phase.name,
            iteration=iteration,
            success=False,
            output=last_output,
            metrics=phase_metrics,
            issues=phase_issues,
            notes=f"Max iterations reached",
            should_revisit=should_revisit
        )

    def _build_enhanced_prompt(self, phase: TaskPhase, iteration: int, previous_output: str,
                             issues: List[Issue], context: Optional[Dict]) -> str:
        """Build an enhanced prompt with SDLC-mode structured context."""
        base_prompt = phase.prompt

        # Always include SDLC context header
        enhancement = "\n\n" + "="*70 + "\n"
        enhancement += "## SDLC WORKFLOW CONTEXT\n"
        enhancement += "="*70 + "\n"
        enhancement += f"\n**Current Phase:** {phase.name.upper()}\n"
        enhancement += f"**Iteration:** {iteration} of {self.max_iterations}\n"
        enhancement += f"**Time remaining:** {self.max_iterations - iteration} iterations in this phase\n"

        # Add metrics from previous iteration
        if iteration > 1:
            enhancement += "\n---\n"
            enhancement += "## Previous Iteration Metrics\n\n"
            # Count issues by severity
            severity_counts = {}
            for issue in issues:
                severity_counts[issue.severity] = severity_counts.get(issue.severity, 0) + 1

            if severity_counts:
                enhancement += f"**Issues remaining:** {sum(severity_counts.values())}\n"
                for severity in ['blocker', 'critical', 'major', 'minor']:
                    if severity in severity_counts:
                        color_symbol = {'blocker': '🔴', 'critical': '🟠', 'major': '🟡', 'minor': '🟢'}
                        enhancement += f"  - {color_symbol[severity]} **{severity.upper()}**: {severity_counts[severity]}\n"

        # Add issues by severity
        if issues:
            enhancement += "\n---\n"
            enhancement += f"## Previous Iteration Failures (Iteration {iteration - 1})\n\n"

            # Group by severity
            blocker_issues = [i for i in issues if i.severity == 'blocker']
            critical_issues = [i for i in issues if i.severity == 'critical']
            major_issues = [i for i in issues if i.severity == 'major']
            minor_issues = [i for i in issues if i.severity == 'minor']

            if blocker_issues:
                enhancement += "### 🔴 BLOCKER Issues (Must Fix to Proceed)\n\n"
                for idx, issue in enumerate(blocker_issues, 1):
                    enhancement += f"\n{idx}. **{issue.type}: {issue.description}**\n"
                    if issue.evidence:
                        # Format evidence with code block if it looks like code
                        if issue.evidence.strip().startswith(('pkg/', 'src/', 'main.', 'lib/')):
                            enhancement += f"\n```\n{issue.evidence}\n```\n"
                        else:
                            enhancement += f"   **Evidence:** {issue.evidence[:200]}\n"
                    enhancement += f"   **Required Fix:** {self._get_fix_suggestion(issue)}\n"
                    enhancement += f"   **Priority:** P0 (blocks workflow)\n"

            if critical_issues:
                enhancement += "\n### 🟠 CRITICAL Issues\n\n"
                for idx, issue in enumerate(critical_issues, 1):
                    enhancement += f"\n{idx}. **{issue.type}: {issue.description}**\n"
                    if issue.evidence:
                        enhancement += f"   **Evidence:** {issue.evidence[:150]}\n"
                    enhancement += f"   **Required Fix:** {self._get_fix_suggestion(issue)}\n"
                    enhancement += f"   **Priority:** P1 (must resolve before phase completion)\n"

            if major_issues:
                enhancement += "\n### 🟡 MAJOR Issues\n\n"
                for idx, issue in enumerate(major_issues[:3], 1):
                    enhancement += f"{idx}. {issue.type}: {issue.description}\n"
                    enhancement += f"   **Priority:** P2 (fix before phase completion)\n"

            if minor_issues and (iteration >= self.max_iterations - 1):
                enhancement += "\n### 🟢 MINOR Issues\n\n"
                for issue in minor_issues[:3]:
                    enhancement += f"- {issue.type}: {issue.description}\n"

        # Add success criteria
        enhancement += "\n---\n"
        enhancement += "## Success Criteria for This Iteration\n\n"
        success_criteria = self._get_phase_success_criteria(phase)
        for criterion in success_criteria:
            enhancement += f"- {criterion}\n"

        # Add phase-specific guidance
        enhancement += "\n---\n"
        enhancement += "## Phase-Specific Guidance\n\n"
        phase_guidance = self._get_phase_guidance(phase)
        enhancement += phase_guidance

        # Add conversation memory note
        enhancement += "\n---\n"
        enhancement += "## Conversation Memory\n\n"
        enhancement += "You are being called in a subprocess with fresh context.\n"
        enhancement += "Each iteration receives ONLY the prompt provided above.\n"
        enhancement += "You DON'T have access to previous conversation, tool results, or files.\n"
        enhancement += "\n**This means:**\n"
        enhancement += "- Check tool outputs before assuming success\n"
        enhancement += "- Read files completely - don't assume they're unchanged\n"
        enhancement += "- Previous errors in this prompt are ALL the context you have\n"
        enhancement += "- When in doubt, read relevant code to verify\n"

        if issues:
            enhancement += "\n**Before starting new work:**\n"
            enhancement += "Review the issues above. You must fix ALL BLOCKER and CRITICAL issues before proceeding. Do not add new features until existing issues are resolved.\n"

        return base_prompt + enhancement

    def _get_fix_suggestion(self, issue: Issue) -> str:
        """Suggest appropriate fix based on issue type."""
        if issue.type == 'build':
            return "Fix compilation error (check imports, types, logic)"
        elif issue.type == 'test':
            return "Fix test or implementation causing failure"
        elif issue.type == 'code':
            return "Fix code issue, refactor if needed"
        else:
            return "Resolve the issue"

    def _get_phase_success_criteria(self, phase: TaskPhase) -> List[str]:
        """Get success criteria based on phase type."""
        if phase.evaluation_type == 'build':
            return [
                "Zero compilation errors (go build exit code 0)"
            ]
        elif phase.evaluation_type == 'test':
            return [
                "All tests passing",
                "No critical regression failures"
            ]
        elif phase.evaluation_type == 'file_check':
            return [
                "Plan file created and valid",
                "Sections complete (requirements, design, implementation tasks)"
            ]
        elif phase.evaluation_type == 'multi':
            return [
                "Zero build errors (exit code 0)",
                "All tests passing",
                "Core functionality implemented"
            ]
        else:  # agent_eval
            return [
                "Core requirements implemented",
                "Ready for next phase"
            ]

    def _get_phase_guidance(self, phase: TaskPhase) -> str:
        """Get phase-specific behavioral guidance."""
        if phase.name == 'planning':
            return """
**Planning Phase Behaviors:**
- Ask clarifying questions if requirements are unclear
- Use tools (read_file, search_files) to understand codebase
- Create structured plan with tasks, dependencies, risks
- Don't implement any code

**What NOT to do:**
- Don't write or modify code
- Don't run tests
- Don't refactor existing code
"""

        if phase.name == 'implementation':
            return """
**Implementation Phase Behaviors:**
1. Fix BLOCKER issues FIRST - build errors before new features
2. Add tests concurrently with implementation
3. Validate after each significant change (go build)
4. Create todos for tasks to track progress
5. Don't refactor unless fixing a blocker

**Error Fixing Order:**
1. Build errors (imports → types → logic)
2. Test failures
3. Code issues

**What NOT to do:**
- Don't add new features not in plan
- Don't refactor "just because"
- Don't skip tests
- Don't write documentation
"""

        if phase.name == 'testing':
            return """
**Testing Phase Behaviors:**
- Fix test failures, not implementation
- Add missing test coverage
- Don't refactor existing code
- Run tests before final response
- Report detailed test metrics

**Test Fixing Strategy:**
1. Read test file to understand assertions
2. Identify root cause (bug vs bad test)
3. Fix ONE test failure at a time
4. Re-run that specific test to verify

**What NOT to do:**
- Don't implement new features
- Don't refactor working code
- Don't change test without understanding failure
"""

        # Default guidance
        return """
1. Track progress with todos
2. Validate builds frequently
3. Use tools efficiently (batch operations)
4. Be decisive - don't over-analyze
"""

    def run_workflow(self) -> List[IterationResult]:
        """Run the complete SDLC workflow with feedback loops."""
        print(f"\n{Colors.BOLD}{'='*70}{Colors.RESET}")
        print(f"{Colors.BOLD}PRODUCTION SDLC WORKFLOW: {self.project_name}{Colors.RESET}")
        print(f"{Colors.BOLD}{'='*70}{Colors.RESET}")
        print(f"\nPhases: {len(self.phases)}")
        print(f"Max iterations per phase: {self.max_iterations}")
        print(f"Max global iterations: {self.max_global_iterations}")

        # Track which phases need revisiting
        phases_to_revisit = []
        context = {}

        # Main SDLC cycle - may revisit phases
        while self.global_iteration < self.max_global_iterations:
            for phase in self.phases:
                if phase.name in [r.phase for r in self.results if r.success]:
                    # Phase already completed
                    continue

                result = self.run_phase(phase, context)
                self.results.append(result)
                self.issues.extend(result.issues)

                # Update context with phase output
                if phase.output_file and os.path.exists(phase.output_file):
                    with open(phase.output_file, 'r') as f:
                        context[phase.name] = f.read()

                # Track phases to revisit
                phases_to_revisit.extend(result.should_revisit)
                phases_to_revisit = list(set(phases_to_revisit))  # Deduplicate

                # Critical phase failure - stop
                if not result.success and phase.name in self.config.get('critical_phases', []):
                    break

            # If we have phases to revisit, cycle back
            if phases_to_revisit:
                print(f"\n{Colors.YELLOW}Revisiting phases: {', '.join(phases_to_revisit)}{Colors.RESET}")

                # Reset completed status for revisiting phases
                for result in self.results:
                    if result.phase in phases_to_revisit:
                        # Find the phase and reset
                        for p in self.phases:
                            if p.name == result.phase:
                                p.completed = False

                phases_to_revisit = []
            elif all(p.completed for p in self.phases):
                # All phases completed
                break
            else:
                # Some in-progress but no explicit revisit - continue
                pass

        self._save_progress()
        self._print_summary()
        return self.results

    def _save_progress(self):
        """Save workflow progress."""
        progress_file = self.base_dir / "sdlc_progress.json"

        progress_data = {
            'project_name': self.project_name,
            'global_iteration': self.global_iteration,
            'completed_phases': [r.phase for r in self.results if r.success],
            'results': [asdict(r) for r in self.results],
            'issues': [asdict(i) for i in self.issues]
        }

        with open(progress_file, 'w') as f:
            json.dump(progress_data, f, indent=2)

        print(f"\n{Colors.BLUE}💾 Progress saved to {progress_file}{Colors.RESET}")

    def _print_summary(self):
        """Print workflow summary."""
        print(f"\n{Colors.BOLD}{'='*70}{Colors.RESET}")
        print(f"{Colors.BOLD}WORKFLOW SUMMARY{Colors.RESET}")
        print(f"{Colors.BOLD}{'='*70}{Colors.RESET}\n")

        completed = len([r for r in self.results if r.success])
        total_phases = len(self.phases)

        print(f"Phases completed: {completed}/{total_phases}")
        print(f"Global iterations: {self.global_iteration}")

        # Metrics summary
        all_metrics = [r.metrics for r in self.results if r.metrics]
        total_files_modified = sum(m.files_modified for m in all_metrics)
        total_tests_passed = sum(m.tests_passed for m in all_metrics)
        total_tests_failed = sum(m.tests_failed for m in all_metrics)

        print(f"\n{Colors.BOLD}Metrics:{Colors.RESET}")
        print(f"  Files modified: {total_files_modified}")
        print(f"  Tests passed: {total_tests_passed}")
        print(f"  Tests failed: {total_tests_failed}")

        # Issues summary
        if self.issues:
            print(f"\n{Colors.BOLD}Issues ({len(self.issues)}):{Colors.RESET}")
            severity_counts = {}
            for issue in self.issues:
                severity_counts[issue.severity] = severity_counts.get(issue.severity, 0) + 1

            for severity, count in sorted(severity_counts.items()):
                color = Colors.RED if severity in ['blocker', 'critical'] else Colors.YELLOW
                print(f"  {color}{severity.upper()}{Colors.RESET}: {count}")

        # Exit status
        if all(p.completed for p in self.phases):
            print(f"\n{Colors.GREEN}{Colors.BOLD}🎉 All phases completed successfully!{Colors.RESET}")
        else:
            print(f"\n{Colors.YELLOW}⚠ Some phases did not complete{Colors.RESET}")


# ============================================================================
# CONFIGURATION
# ============================================================================

TASK_CONFIG = {
    'project_name': 'example-feature',
    'base_dir': '.',
    'max_iterations_per_phase': 5,
    'max_global_iterations': 50,
    'ledit_cmd': './ledit',
    'agent_timeout': 600,
    'test_timeout': 300,
    'critical_phases': ['planning', 'implementation'],
    'phases': [
        {
            'name': 'planning',
            'use_plan_mode': True,
            'plan_file': 'project-plan.md',
            'evaluation_type': 'file_check',
            'prompt': (
                "Create a detailed implementation plan. "
                "Use tools (read_file, search_files) to understand the codebase. "
                "Break down work into specific, actionable tasks with dependencies. "
                "Include: requirements, solution design, implementation tasks, "
                "testing strategy, and risk assessment."
            ),
            'output_file': 'project-plan.md',
            # Per-phase configuration options:
            # 'system_prompt_file': 'prompts/planning_system_prompt.md',  # Custom system prompt for planning
            # 'model': 'openai:gpt-4o',  # Use best-in-class model for planning
            # 'provider': 'openai',
        },
        {
            'name': 'implementation',
            'use_plan_mode': False,
            'evaluation_type': 'multi',  # Build + test
            'prompt': (
                "Implement the project-plan.md tasks. "
                "Create todos for each task using add_todos. "
                "Mark todos complete as you implement each task. "
                "Write clean, production-ready code with error handling. "
                "After each significant change, run 'go build' to verify compilation."
            ),
            # Per-phase configuration options:
            # 'system_prompt_file': 'prompts/implementation_system_prompt.md',  # Custom system prompt for implementation
            # 'model': 'deepseek:deepseek-v3',  # Use cheaper model for implementation
            # 'provider': 'deepseek',
        },
        {
            'name': 'testing',
            'use_plan_mode': False,
            'evaluation_type': 'test',
            'prompt': (
                "Ensure comprehensive tests exist for all implemented code. "
                "Run tests with 'go test ./... -v'. "
                "If tests fail, analyze the failures and fix the implementation. "
                "Ensure test coverage is adequate."
            ),
            # Per-phase configuration options:
            # 'system_prompt_file': 'prompts/testing_system_prompt.md',  # Custom system prompt for testing
            # 'model': 'deepseek:deepseek-cheaper-model',  # Use cheaper model for testing
        }
    ]
}


def main():
    print(f"{Colors.BOLD}")
    print("+====================================================================+")
    print("|              PRODUCTION AGENT-DRIVEN SDLC WORKFLOW                  |")
    print("|                                                                      |")
    print("|  This script automates the full SDLC with:                           |")
    print("|  * Real evaluation (build, test, semantic)                          |")
    print("|  * Iterative feedback cycles                                         |")
    print("|  * Issue tracking and escalation                                      |")
    print("|  * Comprehensive metrics                                             |")
    print("+====================================================================+")
    print(Colors.RESET)

    if not os.path.exists(TASK_CONFIG['ledit_cmd']):
        print(f"{Colors.YELLOW}Warning: ledit not found at '{TASK_CONFIG['ledit_cmd']}'{Colors.RESET}")
        response = input("Continue anyway? (y/n): ")
        if response.lower() != 'y':
            sys.exit(1)

    print("\nPress Enter to start workflow, or Ctrl+C to cancel...")
    try:
        input()
    except KeyboardInterrupt:
        print("\n\nWorkflow cancelled.")
        sys.exit(0)

    workflow = SDLCManager(TASK_CONFIG)
    results = workflow.run_workflow()

    sys.exit(0 if all(p.completed for p in workflow.phases) else 1)


if __name__ == '__main__':
    main()
