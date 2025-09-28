#!/bin/bash

echo "=== Updating ledit with alternate screen support ==="
echo

# Show current versions
echo "1. Checking current installations:"
echo "   Local build: $(ls -la ./ledit 2>/dev/null | awk '{print $6, $7, $8, $9}')"
echo "   GOPATH bin:  $(ls -la ~/go/bin/ledit 2>/dev/null | awk '{print $6, $7, $8, $9}')"
echo

# Build fresh
echo "2. Building fresh version..."
go build -o ledit main.go || { echo "Build failed"; exit 1; }
echo "   ✓ Built successfully"
echo

# Install to GOPATH
echo "3. Installing to GOPATH/bin..."
go install || { echo "Install failed"; exit 1; }
echo "   ✓ Installed to $(go env GOPATH)/bin"
echo

# Verify the installation
echo "4. Verifying installation:"
echo "   which ledit: $(which ledit)"
echo "   Local build: $(ls -la ./ledit | awk '{print $6, $7, $8}')"
echo "   Installed:   $(ls -la ~/go/bin/ledit | awk '{print $6, $7, $8}')"
echo

# Test alternate screen
echo "5. Testing alternate screen functionality:"
echo "   When you run ledit agent and then exit, this text should still be visible."
echo ""
echo "   Current directory:"
ls -la | head -3
echo ""
echo "Press Enter to test with LOCAL build (./ledit agent)..."
read

./ledit agent

echo ""
echo "✓ Back to original screen after LOCAL build test"
echo ""
echo "Press Enter to test with INSTALLED version (ledit agent)..."
read

ledit agent

echo ""
echo "✓ Back to original screen after INSTALLED version test"
echo ""
echo "If both tests worked correctly, the alternate screen is functioning!"
echo "If not, you may need to:"
echo "  - Close and reopen your terminal"
echo "  - Run 'hash -r' to clear command cache"
echo "  - Check your terminal emulator supports alternate screen"