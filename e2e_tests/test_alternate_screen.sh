#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Testing alternate screen buffer"
}

# Test script for alternate screen functionality
echo "Testing alternate screen buffer..."

# Skip if not in a TTY (e2e runner captures output and is non-interactive)
if [ ! -t 1 ]; then
  echo "SKIP: Not a TTY; alternate screen test requires interactive terminal"
  exit 0
fi

echo ""
echo "This test verifies that the alternate screen buffer works correctly."
echo "When you exit ledit, this message should still be visible."
echo ""
echo "Current directory contents:"
ls -la | head -5
echo ""
echo "Remember this output above - it should reappear after exiting ledit."
echo ""
echo "Press Enter to start ledit agent..."
read

# Run ledit (interactive)
./ledit agent || true

echo ""
echo "Back to original screen!"
echo "You should see the directory listing above this message."
echo ""
echo "Did the alternate screen work correctly? (The ledit session should have disappeared) (y/n)"
read response

if [ "$response" = "y" ]; then
    echo "✅ Great! Alternate screen is working correctly."
else
    echo "❌ There might be an issue with alternate screen."
fi
