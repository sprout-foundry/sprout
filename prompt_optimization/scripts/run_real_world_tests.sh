#!/bin/bash

# Real-World Agentic Problem-Solving Test Runner
# Tests agent capabilities on actual git branches with realistic codebase issues

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
REAL_WORLD_EVAL="$PROJECT_DIR/real_world_eval"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

echo -e "${PURPLE}🌍 Real-World Agentic Problem-Solving Test Runner${NC}"
echo "=================================================="

# Check if evaluation tool exists
if [ ! -f "$REAL_WORLD_EVAL" ]; then
    echo -e "${YELLOW}Building real-world evaluation tool...${NC}"
    cd "$PROJECT_DIR"
    go build -o real_world_eval real_world_eval.go
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build real-world evaluation tool${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Real-world evaluation tool built successfully${NC}"
fi

# Parse command line arguments
COMMAND="$1"
shift || true

case "$COMMAND" in
    "architecture-refactor")
        echo -e "${BLUE}🏗️  Testing modular architecture refactoring...${NC}"
        PROVIDER="${1:-deepinfra}"
        MODEL="${2:-Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo}"
        echo -e "${YELLOW}Using $PROVIDER/$MODEL${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/refactor_modular_architecture.json" --provider "$PROVIDER" --model "$MODEL" --output "real_world_architecture_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "concurrent-bugs")
        echo -e "${BLUE}🐛 Testing concurrent access bug fixing...${NC}"
        PROVIDER="${1:-deepinfra}"
        MODEL="${2:-Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo}"
        echo -e "${YELLOW}Using $PROVIDER/$MODEL${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/fix_concurrent_access_bugs.json" --provider "$PROVIDER" --model "$MODEL" --output "real_world_concurrency_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "workspace-caching")
        echo -e "${BLUE}⚡ Testing workspace caching implementation...${NC}"
        PROVIDER="${1:-deepinfra}"
        MODEL="${2:-Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo}"
        echo -e "${YELLOW}Using $PROVIDER/$MODEL${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/implement_workspace_caching.json" --provider "$PROVIDER" --model "$MODEL" --output "real_world_caching_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "all-tests")
        echo -e "${BLUE}🔬 Running all real-world tests...${NC}"
        PROVIDER="${1:-deepinfra}"
        MODEL="${2:-Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo}"
        echo -e "${YELLOW}Using $PROVIDER/$MODEL${NC}"
        
        echo -e "${YELLOW}Phase 1: Architecture Refactoring...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/refactor_modular_architecture.json" --provider "$PROVIDER" --model "$MODEL" --output "all_tests_architecture_$(date +%Y%m%d_%H%M%S).json" --verbose
        
        echo -e "${YELLOW}Phase 2: Concurrent Bug Fixing...${NC}"  
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/fix_concurrent_access_bugs.json" --provider "$PROVIDER" --model "$MODEL" --output "all_tests_concurrency_$(date +%Y%m%d_%H%M%S).json" --verbose
        
        echo -e "${YELLOW}Phase 3: Workspace Caching Implementation...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/implement_workspace_caching.json" --provider "$PROVIDER" --model "$MODEL" --output "all_tests_caching_$(date +%Y%m%d_%H%M%S).json" --verbose
        
        echo -e "${GREEN}✅ All real-world tests complete!${NC}"
        ;;
        
    "compare-models")
        TEST_TYPE="${1:-concurrent-bugs}"
        echo -e "${BLUE}📊 Comparing models on $TEST_TYPE...${NC}"
        
        echo -e "${YELLOW}Testing Qwen3 Coder...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/fix_concurrent_access_bugs.json" --provider "deepinfra" --model "Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo" --output "comparison_qwen3_$(date +%Y%m%d_%H%M%S).json" --verbose
        
        echo -e "${YELLOW}Testing DeepSeek 3.1...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/fix_concurrent_access_bugs.json" --provider "deepinfra" --model "deepseek-ai/DeepSeek-V3.1" --output "comparison_deepseek_$(date +%Y%m%d_%H%M%S).json" --verbose
        
        echo -e "${YELLOW}Testing GPT-5 Mini...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/fix_concurrent_access_bugs.json" --provider "openai" --model "gpt-5-mini-2025-08-07" --output "comparison_gpt5_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "custom")
        echo -e "${BLUE}🛠️  Custom real-world test runner${NC}"
        echo "Usage: $0 custom <test-case> <provider> <model> [timeout] [iterations]"
        
        TEST_CASE="$1"
        PROVIDER="$2" 
        MODEL="$3"
        TIMEOUT="${4:-900}"
        ITERATIONS="${5:-1}"
        
        if [ -z "$TEST_CASE" ] || [ -z "$PROVIDER" ] || [ -z "$MODEL" ]; then
            echo -e "${RED}❌ Missing required arguments${NC}"
            echo "Available test cases:"
            echo "  - test_cases/real_world/refactor_modular_architecture.json"
            echo "  - test_cases/real_world/fix_concurrent_access_bugs.json"
            echo "  - test_cases/real_world/implement_workspace_caching.json"
            echo ""
            echo "Example: $0 custom test_cases/real_world/fix_concurrent_access_bugs.json deepinfra Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo"
            exit 1
        fi
        
        echo -e "${YELLOW}Testing: $TEST_CASE with $PROVIDER/$MODEL (${TIMEOUT}s timeout, ${ITERATIONS}x)${NC}"
        $REAL_WORLD_EVAL --test-case "$TEST_CASE" --provider "$PROVIDER" --model "$MODEL" --timeout "$TIMEOUT" --iterations "$ITERATIONS" --verbose --output "custom_real_world_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "benchmark")
        echo -e "${BLUE}📈 Running real-world performance benchmark...${NC}"
        echo "This will test all real-world scenarios with different model configurations"
        
        echo -e "${YELLOW}Phase 1: Fast model (Qwen3)...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/fix_concurrent_access_bugs.json" --provider "deepinfra" --model "Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo" --output "benchmark_fast_real_world_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${YELLOW}Phase 2: Thorough model (DeepSeek)...${NC}"  
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/refactor_modular_architecture.json" --provider "deepinfra" --model "deepseek-ai/DeepSeek-V3.1" --output "benchmark_thorough_real_world_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${YELLOW}Phase 3: Balanced model (GPT-5)...${NC}"
        $REAL_WORLD_EVAL --test-case "test_cases/real_world/implement_workspace_caching.json" --provider "openai" --model "gpt-5-mini-2025-08-07" --output "benchmark_balanced_real_world_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${GREEN}✅ Real-world benchmark complete!${NC}"
        ;;
        
    "dry-run")
        echo -e "${BLUE}👀 Dry run - real-world testing preview...${NC}"
        TEST_CASE="${1:-test_cases/real_world/fix_concurrent_access_bugs.json}"
        PROVIDER="${2:-deepinfra}"
        MODEL="${3:-Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo}"
        $REAL_WORLD_EVAL --test-case "$TEST_CASE" --provider "$PROVIDER" --model "$MODEL" --dry-run
        ;;
        
    "list-results")
        echo -e "${BLUE}📂 Recent real-world test results:${NC}"
        ls -la "$PROJECT_DIR/"*real_world*.json 2>/dev/null | tail -10 || echo "No real-world results found"
        ;;
        
    "help"|"")
        echo -e "${GREEN}Available commands for real-world agentic testing:${NC}"
        echo ""
        echo -e "${YELLOW}Individual Tests:${NC}"
        echo "  architecture-refactor [provider] [model]  - Test modular architecture refactoring"
        echo "  concurrent-bugs [provider] [model]       - Test race condition bug fixing"
        echo "  workspace-caching [provider] [model]     - Test workspace caching implementation"
        echo ""
        echo -e "${YELLOW}Test Suites:${NC}"  
        echo "  all-tests [provider] [model]             - Run all real-world tests sequentially"
        echo "  compare-models [test-type]               - Compare all models on specific test"
        echo "  benchmark                                - Comprehensive performance benchmark"
        echo ""
        echo -e "${YELLOW}Utilities:${NC}"
        echo "  dry-run [test-case] [provider] [model]   - Preview what would be tested"
        echo "  list-results                             - Show recent test results"
        echo "  help                                     - Show this help"
        echo ""
        echo -e "${YELLOW}Custom:${NC}"
        echo "  custom <test-case> <provider> <model> [timeout] [iterations]"
        echo ""
        echo -e "${YELLOW}Examples:${NC}"
        echo "  $0 concurrent-bugs                              # Test bug fixing with default model"
        echo "  $0 architecture-refactor openai gpt-5-mini     # Test refactoring with GPT-5"
        echo "  $0 compare-models concurrent-bugs               # Compare all models on concurrency"
        echo "  $0 all-tests deepinfra deepseek-ai/DeepSeek-V3.1  # Run all tests with DeepSeek"
        echo ""
        echo -e "${YELLOW}Available Test Cases:${NC}"
        echo "  - refactor_modular_architecture.json    (~15 min) - Architecture refactoring"
        echo "  - fix_concurrent_access_bugs.json       (~12 min) - Race condition debugging"
        echo "  - implement_workspace_caching.json      (~18 min) - Performance optimization"
        echo ""
        echo -e "${YELLOW}Supported Providers/Models:${NC}"
        echo "  - openai/gpt-5-mini-2025-08-07         (Balanced, fast)"
        echo "  - deepinfra/Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo  (Fast, coding-focused)"  
        echo "  - deepinfra/deepseek-ai/DeepSeek-V3.1  (Thorough, analytical)"
        echo ""
        echo -e "${PURPLE}🌍 These tests use actual git branches with realistic codebase issues!${NC}"
        ;;
        
    *)
        echo -e "${RED}❌ Unknown command: $COMMAND${NC}"
        echo "Use '$0 help' to see available commands"
        exit 1
        ;;
esac