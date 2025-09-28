#!/usr/bin/env python3
"""
End-to-End Test Runner for ledit
Tests complete user workflows with real AI models
"""

import os
import sys
import subprocess
import argparse
import json
from pathlib import Path

TEST_DIR = "e2e_tests"

def main():
    parser = argparse.ArgumentParser(description="Run ledit end-to-end tests with real AI models")
    parser.add_argument("-t", "--test", type=int, help="Run specific test by number")
    parser.add_argument("-l", "--list", action="store_true", help="List available tests")
    parser.add_argument("-y", "--yes", action="store_true", help="Run all tests without confirmation")
    parser.add_argument(
        "-m",
        "--model",
        default="openrouter:qwen/qwen3-coder-30b-a3b-instruct",
        help="Model to use (default: openrouter:qwen/qwen3-coder-30b-a3b-instruct)",
    )
    parser.add_argument(
        "--non-interactive",
        action="store_true",
        help="Run only non-interactive, no-network smoke tests",
    )
    args = parser.parse_args()

    # Validate model (basic sanity)
    if args.model == "test:test":
        print("ERROR: E2E tests require a real AI model, not test:test")
        print("Tip: using default model: openrouter:qwen/qwen3-coder-30b-a3b-instruct")
        args.model = "openrouter:qwen/qwen3-coder-30b-a3b-instruct"

    # Find test directory
    script_dir = Path(__file__).parent
    test_path = script_dir / TEST_DIR
    
    if not test_path.exists():
        print(f"Error: {TEST_DIR} directory not found")
        sys.exit(1)

    # Discover tests
    tests = sorted([f for f in test_path.glob("*.sh") if f.is_file()])
    if args.non_interactive:
        # Whitelist of non-interactive, no-network tests
        ni_set = {"test_ui_smoke.sh"}
        tests = [f for f in tests if f.name in ni_set]
    
    if args.list:
        header = "Available non-interactive tests:" if args.non_interactive else "Available end-to-end tests (require real AI models):"
        print(header)
        for i, test in enumerate(tests, 1):
            print(f"{i}: {test.stem}")
        sys.exit(0)

    if not args.test:
        print("Running ALL e2e tests with model:", args.model)
        print("This may take a while and consume API credits!")
        if not args.yes:
            response = input("Continue? (y/N): ")
            if response.lower() != 'y':
                print("Aborted")
                sys.exit(0)
        # Run all tests
        run_all_tests(script_dir, tests, args.model)
    else:
        # Run specific test
        if args.test < 1 or args.test > len(tests):
            print(f"Error: Test number must be between 1 and {len(tests)}")
            sys.exit(1)
        
        test_file = tests[args.test - 1]
        print(f"\nRunning e2e test: {test_file.stem}")
        print(f"Using model: {args.model}")
        print("-" * 50)
        
        exit_code = run_single_test(script_dir, test_file, args.model)
        sys.exit(exit_code)

def run_single_test(script_dir, test_file, model):
    """Run a single test and return exit code"""
    # Build ledit if needed
    build_result = subprocess.run(["go", "build", "-o", "ledit"], 
                                cwd=str(script_dir),
                                capture_output=True, text=True)
    if build_result.returncode != 0:
        print(f"Build failed: {build_result.stderr}")
        return 1
        
    # Create temp directory for test
    test_dir = script_dir / "testing" / test_file.stem
    test_dir.mkdir(parents=True, exist_ok=True)
    
    # Ensure test-local binary exists (some scripts use ./ledit)
    built_bin = script_dir / "ledit"
    local_bin = test_dir / "ledit"
    try:
        if built_bin.exists():
            import shutil
            shutil.copy2(str(built_bin), str(local_bin))
    except Exception as e:
        print(f"Warning: could not prepare local ledit binary: {e}")

    # Run the test
    env = os.environ.copy()
    env["PATH"] = f"{script_dir}:{env.get('PATH', '')}"
    
    try:
        result = subprocess.run(
            ["bash", str(test_file), model],
            cwd=str(test_dir),
            capture_output=True,
            text=True,
            timeout=600,  # 10 minute timeout for e2e tests
            env=env
        )
        
        # Print result
        print("\n" + "-" * 50)
        if result.returncode == 0:
            print(f"✅ PASSED: {test_file.stem}")
        else:
            print(f"❌ FAILED: {test_file.stem}")
            print("\nTest output:")
            print(result.stdout)
            if result.stderr:
                print("\nError output:")
                print(result.stderr)
        
        return result.returncode
        
    except subprocess.TimeoutExpired:
        print(f"❌ TIMEOUT: {test_file.stem}")
        return 1
    finally:
        # Cleanup
        import shutil
        if test_dir.parent.exists():
            shutil.rmtree(test_dir.parent, ignore_errors=True)

def run_all_tests(script_dir, tests, model):
    """Run all tests and print summary"""
    results = {}
    
    for i, test_file in enumerate(tests, 1):
        print(f"\n[{i}/{len(tests)}] Running: {test_file.stem}")
        exit_code = run_single_test(script_dir, test_file, model)
        results[test_file.stem] = "PASS" if exit_code == 0 else "FAIL"
    
    # Print summary
    print("\n" + "=" * 60)
    print("E2E TEST RESULTS SUMMARY")
    print("=" * 60)
    
    passed = sum(1 for r in results.values() if r == "PASS")
    failed = sum(1 for r in results.values() if r == "FAIL")
    
    for test_name, result in results.items():
        status = "✅ PASS" if result == "PASS" else "❌ FAIL"
        print(f"{status}: {test_name}")
    
    print(f"\nTotal: {passed} passed, {failed} failed out of {len(tests)} tests")
    sys.exit(0 if failed == 0 else 1)

if __name__ == "__main__":
    main()
