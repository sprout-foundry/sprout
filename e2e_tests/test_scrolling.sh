#!/bin/bash

echo "Testing scrolling functionality in ledit agent"
echo ""
echo "This test will:"
echo "1. Start ledit agent"
echo "2. Generate enough content to fill the screen"
echo "3. Test scrolling with:"
echo "   - Ctrl+U (scroll up half page)"
echo "   - Ctrl+D (scroll down half page)"
echo "   - Page Up (scroll up 10 lines)"
echo "   - Page Down (scroll down 10 lines)"
echo ""
echo "Look for:"
echo "- Scroll indicator showing current position"
echo "- Content moving when using scroll keys"
echo "- Ability to review previous output"
echo ""
echo "Starting ledit agent..."

# Skip if not a TTY
if [ ! -t 1 ]; then
  echo "SKIP: Not a TTY; scrolling test requires interactive terminal"
  exit 0
fi

./ledit agent || true
