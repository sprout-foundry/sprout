#!/bin/bash

# Quick test of the prompt optimization framework

echo "🧪 Quick Test of Prompt Optimization Framework"
echo "==============================================="

# Build the framework
cd framework
echo "Building framework..."
if go build -o prompt_optimizer .; then
    echo "✅ Framework built successfully"
else
    echo "❌ Framework build failed"
    exit 1
fi

# Test one specific prompt
echo ""
echo "Testing text replacement prompt v1..."
./prompt_optimizer \
    --prompt ../prompts/text_replacement_v1.txt \
    --test-cases ../test_cases \
    --results ../results \
    --models "deepinfra:google/gemini-2.5-flash" \
    --verbose

echo ""
echo "🎯 Quick test complete!"
echo "Check results in ../results/"