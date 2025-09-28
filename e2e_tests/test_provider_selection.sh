#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Provider Selection Test"
}

# Function to run the test logic
run_test_logic() {
    echo "--- TEST: Provider Selection ---"
    
    # Guard for provider tests requiring network/key
    if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
        echo "SKIP: OPENROUTER_API_KEY not set; skipping provider selection tests"
        return 0
    fi
    
    # Create a simple test file
    cat > test_provider.txt << 'EOF'
This is a test file for provider selection.
EOF

    # Use provided keys from environment; DeepInfra optional in this test
    
    # Test 1: Start with deepinfra provider
    echo "1. Testing initial provider setup..."
    OUTPUT=$(../../ledit agent --provider deepinfra --dry-run "test" <<< "/providers status" 2>&1)
    
    if echo "$OUTPUT" | grep -q "Active Provider.*DeepInfra"; then
        echo "✓ Initial provider set correctly to DeepInfra"
    else
        echo "✗ Failed to set initial provider"
        echo "$OUTPUT"
        exit 1
    fi
    
    # Test 2: Switch to openrouter using /providers command
    echo -e "\n2. Testing provider switch command..."
    OUTPUT=$(../../ledit agent --provider deepinfra --dry-run "test" << 'EOF' 2>&1
/providers openrouter
/providers status
EOF
)
    
    if echo "$OUTPUT" | grep -q "Provider switched to: OpenRouter"; then
        echo "✓ Provider switch command executed"
        
        # Check if the switch actually took effect
        if echo "$OUTPUT" | grep -A5 "Provider Status:" | grep -q "Active Provider.*OpenRouter"; then
            echo "✓ Provider successfully switched to OpenRouter"
        else
            echo "✗ Provider switch did not take effect"
            echo "$OUTPUT"
            exit 1
        fi
    else
        echo "✗ Provider switch command failed"
        echo "$OUTPUT"
        exit 1
    fi
    
    # Test 3: Verify model list shows correct provider's models
    echo -e "\n3. Testing model list after provider switch..."
    OUTPUT=$(../../ledit agent --provider deepinfra --dry-run "test" << 'EOF' 2>&1
/providers openrouter
/models
EOF
)
    
    if echo "$OUTPUT" | grep -q "Available Models (OpenRouter)"; then
        echo "✓ Model list correctly shows OpenRouter models"
    else
        echo "✗ Model list still showing wrong provider"
        echo "$OUTPUT"
        exit 1
    fi
    
    # Clean up
    rm -f test_provider.txt
    
    echo -e "\n✅ All provider selection tests passed!"
}

# If called directly, run the test
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_test_logic
fi
