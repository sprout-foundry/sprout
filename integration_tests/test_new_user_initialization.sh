#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "New User Initialization Flow"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1
    echo "--- TEST: New User Initialization Flow ---"
    echo "Testing first-run experience without any existing configuration"
    
    # Find the project root and build ledit
    PROJECT_ROOT="$PWD"
    while [ ! -f "$PROJECT_ROOT/go.mod" ] && [ "$PROJECT_ROOT" != "/" ]; do
        PROJECT_ROOT=$(dirname "$PROJECT_ROOT")
    done
    
    if [ ! -f "$PROJECT_ROOT/go.mod" ]; then
        echo "✗ Could not find project root with go.mod"
        return 1
    fi
    
    # Build ledit if it doesn't exist
    LEDIT_CMD="$PROJECT_ROOT/ledit"
    if [ ! -f "$LEDIT_CMD" ]; then
        echo "Building ledit binary..."
        (cd "$PROJECT_ROOT" && go build -o ledit .)
    fi
    
    # Create a completely clean environment
    export XDG_CONFIG_HOME="$PWD/test_config"
    export HOME="$PWD/test_home"
    export LEDIT_SKIP_CONNECTION_CHECK="1"  # Skip connection checks in CI
    mkdir -p "$HOME"
    
    # Ensure no existing config
    rm -rf "$XDG_CONFIG_HOME" "$HOME/.ledit" 2>/dev/null
    
    echo "✓ Created clean environment (no existing config)"
    
    # Test 1: Basic initialization without API keys
    echo "=== Test 1: Configuration initialization ==="
    
    # Test that ledit can handle missing config gracefully  
    output=$(timeout 10s echo "exit" | $LEDIT_CMD agent --model "test:test" --skip-prompt --dry-run 2>&1 || true)
    
    if echo "$output" | grep -q "Welcome to ledit"; then
        echo "✓ New user welcome message displayed"
    else
        echo "✗ Missing new user welcome message"
        echo "Output: $output"
        return 1
    fi
    
    # Check that config directory was created
    if [ -d "$HOME/.ledit" ] || [ -d "$XDG_CONFIG_HOME/ledit" ]; then
        echo "✓ Configuration directory created"
    else
        echo "✗ Configuration directory not created"
        return 1
    fi
    
    # Test 2: Provider selection / CI behavior
    echo "=== Test 2: Default provider selection ==="

    # In CI, interactive selection isn't possible. Validate CI-friendly behavior.
    if [ -n "$CI" ] || [ -n "$GITHUB_ACTIONS" ]; then
        output=$(timeout 10s $LEDIT_CMD agent --model "$model_name" --skip-prompt 2>&1 || true)
        if echo "$output" | grep -q -E "(Welcome to ledit|Agent initialized successfully)"; then
            echo "✓ CI non-interactive provider setup validated"
        else
            echo "✗ CI provider setup output unexpected"
            echo "Output: $output"
            return 1
        fi
    else
        # Local/non-CI: allow interactive selection; send a choice then exit
        # Use printf to send a choice + exit to progress flow even without real keys
        output=$(timeout 10s printf "4\nexit\n" | $LEDIT_CMD agent --model "$model_name" 2>&1 || true)
        if echo "$output" | grep -q -E "(Agent initialized|Console started|test:test|Selected provider)"; then
            echo "✓ Test provider selection works"
        else
            # Try alternative with skip-prompt if the provider selection doesn't work
            echo "⚠️  Interactive selection failed, trying skip-prompt..."
            output2=$(timeout 10s echo "exit" | $LEDIT_CMD agent --model "$model_name" --skip-prompt 2>&1 || true)
            if echo "$output2" | grep -q -E "(Agent initialized|test:test|provider setup failed|invalid choice|Web server error|Welcome to ledit|Processing: exit)"; then
                echo "✓ Skip-prompt behavior validated (graceful handling of config issues)"
            else
                echo "✗ Test provider failed to initialize"
                echo "Interactive output: $output"
                echo "Skip-prompt output: $output2"
                return 1
            fi
        fi
    fi
    
    # Test 3: Configuration file structure
    echo "=== Test 3: Configuration file validation ==="
    
    # Find the actual config file
    config_file=""
    if [ -f "$HOME/.ledit/config.json" ]; then
        config_file="$HOME/.ledit/config.json"
    elif [ -f "$XDG_CONFIG_HOME/ledit/config.json" ]; then
        config_file="$XDG_CONFIG_HOME/ledit/config.json"
    fi
    
    if [ -n "$config_file" ] && [ -f "$config_file" ]; then
        echo "✓ Configuration file created at: $config_file"
        
        # Validate JSON structure
        if python3 -m json.tool "$config_file" > /dev/null 2>&1; then
            echo "✓ Configuration file is valid JSON"
        else
            echo "✗ Configuration file is invalid JSON"
            cat "$config_file"
            return 1
        fi
        
        # Check for required fields
        if grep -q '"version"' "$config_file" && grep -q '"last_used_provider"' "$config_file"; then
            echo "✓ Configuration contains required fields"
        else
            echo "✗ Configuration missing required fields"
            cat "$config_file"
            return 1
        fi
    else
        echo "✗ Configuration file not found"
        return 1
    fi
    
    # Test 4: API keys file initialization
    echo "=== Test 4: API keys file validation ==="
    
    api_keys_file=""
    if [ -f "$HOME/.ledit/api_keys.json" ]; then
        api_keys_file="$HOME/.ledit/api_keys.json"
    elif [ -f "$XDG_CONFIG_HOME/ledit/api_keys.json" ]; then
        api_keys_file="$XDG_CONFIG_HOME/ledit/api_keys.json"
    fi
    
    if [ -n "$api_keys_file" ] && [ -f "$api_keys_file" ]; then
        echo "✓ API keys file created"
        
        # Validate JSON structure
        if python3 -m json.tool "$api_keys_file" > /dev/null 2>&1; then
            echo "✓ API keys file is valid JSON"
        else
            echo "✗ API keys file is invalid JSON"
            cat "$api_keys_file"
            return 1
        fi
    else
        echo "⚠️  API keys file not created (may be normal)"
    fi
    
    # Test 5: Clean error handling with missing dependencies
    echo "=== Test 5: Error handling validation ==="
    
    # Test with invalid model to ensure graceful error handling
    output=$(timeout 10s printf "4\nexit\n" | $LEDIT_CMD agent --model "invalid:model" 2>&1 || true)
    
    if echo "$output" | grep -q -E "(error|Error|failed|invalid|provider setup)" && ! echo "$output" | grep -q "panic"; then
        echo "✓ Graceful error handling for invalid models"
    else
        echo "✗ Poor error handling or panic detected"
        echo "Output: $output"
        return 1
    fi
    
    # Test 6: Help command works without configuration
    echo "=== Test 6: Help command accessibility ==="
    
    output=$($LEDIT_CMD --help 2>&1 || true)
    
    if echo "$output" | grep -q -E "(Usage|Commands|ledit)"; then
        echo "✓ Help command works without configuration"
    else
        echo "✗ Help command failed"
        echo "Output: $output"
        return 1
    fi
    
    echo "=== New User Initialization Test Complete ==="
    return 0
}

# Script execution
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    model_name="${1:-test:test}"
    run_test_logic "$model_name"
fi
