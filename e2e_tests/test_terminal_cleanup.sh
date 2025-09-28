#!/bin/bash

# Test script for terminal cleanup
echo "Testing terminal cleanup..."

echo ""
echo "This test will run the ledit agent in different scenarios:"
echo "1. Exit with /quit command"
echo "2. Exit with Ctrl+C (twice)"
echo "3. Exit with kill signal"
echo ""
echo "After each test, run 'ls' or any command to verify terminal works properly."
echo "If the terminal is broken, you'll see all output on one line without scrolling."
echo ""
# Skip if not a TTY
if [ ! -t 1 ]; then
  echo "SKIP: Not a TTY; terminal cleanup test requires interactive terminal"
  exit 0
fi

echo "Press Enter to continue..."
read

# Test 1: Normal exit with /quit
echo "Test 1: Testing /quit command"
echo "Type /quit to exit, then check if terminal works properly"
./ledit agent || true

echo "Terminal after /quit - testing with ls:"
ls
echo "Does the terminal work properly? (y/n)"
read response

# Test 2: Ctrl+C exit
echo ""
echo "Test 2: Testing Ctrl+C exit"
echo "Press Ctrl+C twice to exit, then check if terminal works properly"
./ledit agent || true

echo "Terminal after Ctrl+C - testing with ls:"
ls
echo "Does the terminal work properly? (y/n)"
read response

echo ""
echo "Test complete!"
