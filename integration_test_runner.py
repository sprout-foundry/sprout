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

# Force output to be immediate
def print_flush(msg):
    print(msg, flush=True)

# Set default model for integration tests
DEFAULT_MODEL = "test:test"
TEST_DIR = "integration_tests"

def main():
    print_flush("🚀 Starting integration test runner...")

    parser = argparse.ArgumentParser(description="Run ledit integration tests")
    parser.add_argument("-t", "--test", type=int, help="Run specific test by number")
    parser.add_argument("-l", "--list", action="store_true", help="List available tests")
    parser.add_argument("-m", "--model", default=DEFAULT_MODEL, help=f"Model to use (default: {DEFAULT_MODEL})")
    args = parser.parse_args()

    # Find test directory
    script_dir = Path(__file__).parent
    print_flush(f"📁 Script directory: {script_dir}")
    print_flush(f"🔍 Looking for test directory: {TEST_DIR}")

    test_path = script_dir / TEST_DIR

    if not test_path.exists():
        print_flush(f"❌ Error: {TEST_DIR} directory not found at {test_path}")
        sys.exit(1)

    print_flush(f"✅ Test directory found: {test_path}")

    # Discover tests
    tests = sorted([f for f in test_path.glob("*.sh") if f.is_file()])
    print_flush(f"🔍 Found {len(tests)} test files")
    
    if args.list:
        print("Available integration tests:")
        for i, test in enumerate(tests, 1):
            print(f"{i}: {test.stem}")
        sys.exit(0)
    
    # If no specific test is requested, run all tests
    if not args.test:
        print_flush("Running ALL integration tests:")
        for i, test in enumerate(tests, 1):
            print_flush(f"{i}: {test.stem}")
        print_flush(f"\nRunning {len(tests)} tests with model: {args.model}")
        print_flush("=" * 50)
        
        # Check if ledit binary already exists and build if needed
        ledit_binary = script_dir / "ledit"
        if ledit_binary.exists():
            print("✅ Using existing ledit binary")
        else:
            print("Building ledit binary...")
            build_result = subprocess.run(["go", "build", "-o", "ledit"], capture_output=True, text=True, cwd=script_dir)
            if build_result.returncode != 0:
                print("❌ Build failed:")
                print("STDOUT:", build_result.stdout)
                print("STDERR:", build_result.stderr)
                sys.exit(1)
            print("✅ Build completed successfully")

        passed = 0
        failed = 0
        
        for i, test_file in enumerate(tests, 1):
            print_flush(f"\n[{i}/{len(tests)}] Running: {test_file.stem}")
            print_flush("-" * 50)
            
            try:
                env = os.environ.copy()
                # Prepend script_dir (which contains built ledit) to PATH so tests can invoke `ledit`
                env["PATH"] = f"{script_dir}:{env.get('PATH', '')}"
                result = subprocess.run(
                    ["bash", str(test_file), args.model],
                    cwd=script_dir,
                    capture_output=True,
                    text=True,
                    timeout=60,  # 1 minute timeout per test
                    env=env,
                )
                
                if result.returncode == 0:
                    print_flush("✅ PASSED")
                    passed += 1
                else:
                    print_flush("❌ FAILED")
                    print_flush(f"Return code: {result.returncode}")
                    if result.stdout:
                        print_flush("STDOUT:")
                        print_flush(result.stdout)
                    if result.stderr:
                        print_flush("STDERR:")
                        print_flush(result.stderr)
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
    try:
        main()
    except Exception as e:
        print(f"❌ UNHANDLED ERROR: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)
