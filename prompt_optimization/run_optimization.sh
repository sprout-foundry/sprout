#!/bin/bash

# Ledit Prompt Optimization Pipeline
# Systematically optimize prompts to improve model performance

set -e

echo "🚀 Ledit Prompt Optimization Pipeline"
echo "====================================="

# Configuration
FRAMEWORK_DIR="framework"
RESULTS_DIR="results" 
TEST_CASES_DIR="test_cases"
PROMPTS_DIR="prompts"
CONFIGS_DIR="configs"

# Ensure directories exist
mkdir -p "$RESULTS_DIR" "$TEST_CASES_DIR" "$PROMPTS_DIR" "$CONFIGS_DIR"

# Build the optimization tool
echo "🔨 Building optimization framework..."
cd "$FRAMEWORK_DIR"
if ! go build -o prompt_optimizer .; then
    echo "❌ Failed to build optimization framework"
    exit 1
fi
cd ..

# Test framework build
echo "✅ Framework built successfully"

# Priority 1: Fix critical text replacement prompt
echo ""
echo "🔥 CRITICAL: Optimizing Text Replacement Prompt"
echo "================================================"
echo "Current status: 0% success rate (generates programs instead of editing)"
echo "Target: 100% success rate for simple text replacements"
echo ""

if [ -f "$CONFIGS_DIR/text_replacement_optimization.json" ]; then
    echo "Starting text replacement optimization..."
    
    # Test each version of the text replacement prompt
    for prompt_file in "$PROMPTS_DIR"/text_replacement_v*.txt; do
        if [ -f "$prompt_file" ]; then
            echo "📝 Testing $(basename "$prompt_file")..."
            ./"$FRAMEWORK_DIR"/prompt_optimizer \
                --prompt "$prompt_file" \
                --test-cases "$TEST_CASES_DIR" \
                --results "$RESULTS_DIR" \
                --models "deepinfra:google/gemini-2.5-flash" \
                --verbose || echo "⚠️  Test failed for $(basename "$prompt_file")"
        fi
    done
    
    # Run optimization on the best performing version
    echo "🔄 Running iterative optimization..."
    ./"$FRAMEWORK_DIR"/prompt_optimizer \
        --type text_replacement \
        --test-cases "$TEST_CASES_DIR" \
        --results "$RESULTS_DIR" \
        --optimize \
        --iterations 10 \
        --target 1.0 \
        --models "deepinfra:google/gemini-2.5-flash,deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo" \
        --verbose
        
    echo "✅ Text replacement optimization complete"
else
    echo "⚠️  Text replacement config not found, skipping optimization"
fi

# Priority 2: Code generation prompts
echo ""
echo "🚨 HIGH: Code Generation Optimization"
echo "======================================"

# Test code generation prompts if they exist
if ls "$PROMPTS_DIR"/code_generation_v*.txt >/dev/null 2>&1; then
    echo "Testing code generation prompts..."
    for prompt_file in "$PROMPTS_DIR"/code_generation_v*.txt; do
        if [ -f "$prompt_file" ]; then
            echo "📝 Testing $(basename "$prompt_file")..."
            ./"$FRAMEWORK_DIR"/prompt_optimizer \
                --prompt "$prompt_file" \
                --test-cases "$TEST_CASES_DIR" \
                --results "$RESULTS_DIR" \
                --models "deepinfra:google/gemini-2.5-flash" || echo "⚠️  Test failed"
        fi
    done
else
    echo "📝 No code generation prompts found - will optimize existing prompts in codebase"
fi

# Generate summary report
echo ""
echo "📊 Generating Optimization Summary"
echo "=================================="

# Create summary of all results
SUMMARY_FILE="$RESULTS_DIR/optimization_summary_$(date +%Y%m%d_%H%M%S).md"

cat > "$SUMMARY_FILE" << EOF
# Prompt Optimization Summary

Generated: $(date)

## Optimization Results

### Text Replacement Prompts
- **Status**: $([ -f "$RESULTS_DIR"/optimization_text_replacement_*.json ] && echo "Optimized" || echo "Pending")
- **Target**: 100% success rate for simple text replacements  
- **Priority**: CRITICAL (was failing at 0%)

### Code Generation Prompts  
- **Status**: $([ -f "$RESULTS_DIR"/*code_generation*.json ] && echo "Tested" || echo "Pending")
- **Target**: 95% accuracy for code modifications
- **Priority**: HIGH

## Next Steps

1. **Deploy optimized prompts** - Integrate successful prompt optimizations back into the codebase
2. **Monitor performance** - Track success rates in production
3. **Iterate on remaining prompts** - Continue optimization for medium priority prompts

## Files Generated

$(find "$RESULTS_DIR" -name "*.json" -newer "$FRAMEWORK_DIR/prompt_optimizer" | while read file; do
    echo "- $(basename "$file"): $(stat -f%Sm -t %H:%M "$file" 2>/dev/null || stat -c%y "$file" | cut -d' ' -f2 | cut -d: -f1,2)"
done)

EOF

echo "📄 Summary report saved to: $SUMMARY_FILE"

echo ""
echo "🎉 Prompt Optimization Pipeline Complete!"
echo "=========================================="
echo "Next steps:"
echo "1. Review results in $RESULTS_DIR"
echo "2. Test optimized prompts in real scenarios"  
echo "3. Deploy successful optimizations to main codebase"
echo "4. Run e2e tests to verify improvements"