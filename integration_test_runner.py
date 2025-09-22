#!/usr/bin/env python3
"""
Integration Test Runner for ledit
Tests infrastructure and mechanics without requiring real AI models
"""

import os
import sys
import subprocess
import argparse
import json
from pathlib import Path

# Set default model for integration tests
DEFAULT_MODEL = "test:test"
TEST_DIR = "integration_tests"

def main():
    parser = argparse.ArgumentParser(description="Run ledit integration tests")
    parser.add_argument("-t", "--test", type=int, help="Run specific test by number")
    parser.add_argument("-l", "--list", action="store_true", help="List available tests")
    parser.add_argument("-m", "--model", default=DEFAULT_MODEL, help=f"Model to use (default: {DEFAULT_MODEL})")
    args = parser.parse_args()

    # Find test directory
    script_dir = Path(__file__).parent
    test_path = script_dir / TEST_DIR
    
    if not test_path.exists():
        print(f"Error: {TEST_DIR} directory not found")
        sys.exit(1)

    # Discover tests
    tests = sorted([f for f in test_path.glob("*.sh") if f.is_file()])
    
    if args.list:
        print("Available integration tests:")
        for i, test in enumerate(tests, 1):
            print(f"{i}: {test.stem}")
        sys.exit(0)
    
    # If no specific test is requested, run all tests
    if not args.test:
        print("Running ALL integration tests:")
        for i, test in enumerate(tests, 1):
            print(f"{i}: {test.stem}")
        print(f"\nRunning {len(tests)} tests with model: {args.model}")
        print("=" * 50)
        
        passed = 0
        failed = 0
        
        for i, test_file in enumerate(tests, 1):
            print(f"\n[{i}/{len(tests)}] Running: {test_file.stem}")
            print("-" * 50)
            
            try:
                result = subprocess.run(
                    ["bash", str(test_file), args.model],
                    cwd=script_dir,
                    capture_output=True,
                    text=True,
                    timeout=120  # 2 minute timeout per test
                )
                
                if result.returncode == 0:
                    print("✅ PASSED")
                    passed += 1
                else:
                    print("❌ FAILED")
                    print("STDOUT:", result.stdout)
                    print("STDERR:", result.stderr)
                    failed += 1
                    
            except subprocess.TimeoutExpired:
                print("❌ FAILED (timeout)")
                failed += 1
            except Exception as e:
                print(f"❌ FAILED (error): {e}")
                failed += 1
        
        print("=" * 50)
        print(f"Results: {passed} passed, {failed} failed")
        
        if failed > 0:
            print(f"\n❌ {failed} test(s) failed")
            sys.exit(1)
        else:
            print(f"\n✅ All {passed} tests passed!")
            sys.exit(0)

    # Run specific test
    if args.test:
        if args.test < 1 or args.test > len(tests):
            print(f"Error: Test number must be between 1 and {len(tests)}")
            sys.exit(1)
            
        test_file = tests[args.test - 1]
        print(f"\nRunning integration test: {test_file.stem}")
        print(f"Using model: {args.model}")
        print("-" * 50)
        
        # Build ledit if needed
        build_result = subprocess.run(["go", "build", "-o", "ledit"], 
                                    capture_output=True, text=True)
        if build_result.returncode != 0:
            print(f"Build failed: {build_result.stderr}")
            sys.exit(1)
            
        # Create temp directory for test
        test_dir = script_dir / "testing" / test_file.stem
        test_dir.mkdir(parents=True, exist_ok=True)
        
        # Run the test
        env = os.environ.copy()
        env["PATH"] = f"{script_dir}:{env.get('PATH', '')}"
        
        try:
            result = subprocess.run(
                ["bash", str(test_file), args.model],
                cwd=str(test_dir),
                capture_output=True,
                text=True,
                timeout=300,  # 5 minute timeout
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
            
            sys.exit(result.returncode)
            
        except subprocess.TimeoutExpired:
            print(f"❌ TIMEOUT: {test_file.stem}")
            sys.exit(1)
        finally:
            # Cleanup
            import shutil
            if test_dir.parent.exists():
                shutil.rmtree(test_dir.parent, ignore_errors=True)

if __name__ == "__main__":
    main()