#!/bin/bash

# Agentic Problem-Solving Test Runner
# Focused on real-world codebase scenarios

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
EVAL_TOOL="$PROJECT_DIR/agentic_eval"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🤖 Agentic Problem-Solving Test Runner${NC}"
echo "======================================"

# Check if evaluation tool exists
if [ ! -f "$EVAL_TOOL" ]; then
    echo -e "${YELLOW}Building agentic evaluation tool...${NC}"
    cd "$PROJECT_DIR"
    go build -o agentic_eval agentic_eval.go
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build agentic evaluation tool${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Agentic evaluation tool built successfully${NC}"
fi

# Parse command line arguments
COMMAND="$1"
shift || true

case "$COMMAND" in
    "quick")
        echo -e "${BLUE}🚀 Running quick agentic test...${NC}"
        $EVAL_TOOL --provider-models "fast_models" --test-suite "quick_agentic" --output "quick_agentic_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "core")
        echo -e "${BLUE}🧠 Running core agentic problem-solving tests...${NC}"
        $EVAL_TOOL --provider-models "all_models" --test-suite "agentic_core" --iterations 1 --output "agentic_core_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "comprehensive")
        echo -e "${BLUE}🔬 Running comprehensive agentic evaluation...${NC}"
        echo -e "${YELLOW}This will run all agentic tests with multiple iterations (30+ minutes)${NC}"
        read -p "Continue? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            $EVAL_TOOL --provider-models "all_models" --test-suite "agentic_comprehensive" --iterations 2 --output "agentic_comprehensive_$(date +%Y%m%d_%H%M%S).json" --verbose
        else
            echo "Cancelled."
        fi
        ;;
        
    "compare-models")
        PROMPT_TYPE="${1:-base/v4_streamlined}"
        echo -e "${BLUE}📊 Comparing models with prompt: $PROMPT_TYPE${NC}"
        $EVAL_TOOL --provider-models "all_models" --prompt-types "$PROMPT_TYPE" --test-suite "agentic_core" --output "model_comparison_agentic_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "fast-models")
        echo -e "${BLUE}⚡ Testing fast models for agentic tasks...${NC}"
        $EVAL_TOOL --provider-models "fast_models" --test-suite "agentic_core" --iterations 2 --output "fast_models_agentic_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "thorough-models")
        echo -e "${BLUE}🎯 Testing thorough models for agentic tasks...${NC}"
        $EVAL_TOOL --provider-models "thorough_models" --test-suite "agentic_core" --iterations 1 --output "thorough_models_agentic_$(date +%Y%m%d_%H%M%S).json" --verbose
        ;;
        
    "custom")
        echo -e "${BLUE}🛠️  Custom agentic test runner${NC}"
        echo "Usage: $0 custom <provider-models> <prompt-types> <test-suite> [iterations] [timeout]"
        
        PROVIDER_MODELS="$1"
        PROMPT_TYPES="$2"
        TEST_SUITE="$3"
        ITERATIONS="${4:-1}"
        TIMEOUT="${5:-300}"
        
        if [ -z "$PROVIDER_MODELS" ] || [ -z "$PROMPT_TYPES" ] || [ -z "$TEST_SUITE" ]; then
            echo -e "${RED}❌ Missing required arguments${NC}"
            echo "Example: $0 custom all_models base/v4_streamlined agentic_core 2 600"
            echo ""
            echo "Available provider-model combinations:"
            echo "  - all_models (gpt-5-mini, qwen3-coder, deepseek-3.1)"
            echo "  - fast_models (qwen3-coder)"
            echo "  - thorough_models (deepseek-3.1)"
            echo "  - balanced_models (gpt-5-mini)"
            echo "  - deepinfra_models (qwen3-coder, deepseek-3.1)"
            exit 1
        fi
        
        echo -e "${YELLOW}Testing: $PROVIDER_MODELS with $PROMPT_TYPES on $TEST_SUITE (${ITERATIONS}x, ${TIMEOUT}s timeout)${NC}"
        $EVAL_TOOL --provider-models "$PROVIDER_MODELS" --prompt-types "$PROMPT_TYPES" --test-suite "$TEST_SUITE" --iterations "$ITERATIONS" --timeout "$TIMEOUT" --verbose --output "custom_agentic_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "benchmark")
        echo -e "${BLUE}📈 Running agentic performance benchmark...${NC}"
        echo "This will test all provider/model combinations with the core agentic test suite"
        
        echo -e "${YELLOW}Phase 1: Fast models...${NC}"
        $EVAL_TOOL --provider-models "fast_models" --test-suite "agentic_core" --output "benchmark_fast_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${YELLOW}Phase 2: Thorough models...${NC}"  
        $EVAL_TOOL --provider-models "thorough_models" --test-suite "agentic_core" --output "benchmark_thorough_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${YELLOW}Phase 3: Balanced models...${NC}"
        $EVAL_TOOL --provider-models "balanced_models" --test-suite "agentic_core" --output "benchmark_balanced_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${GREEN}✅ Benchmark complete! Check results/agentic/ for detailed analysis${NC}"
        ;;
        
    "dry-run")
        echo -e "${BLUE}👀 Dry run - agentic testing preview...${NC}"
        $EVAL_TOOL --provider-models "all_models" --test-suite "agentic_core" --dry-run
        ;;
        
    "list-results")
        echo -e "${BLUE}📂 Recent agentic test results:${NC}"
        ls -la "$PROJECT_DIR/results/agentic/"*.json 2>/dev/null | tail -10 || echo "No agentic results found"
        ;;
        
    "help"|"")
        echo -e "${GREEN}Available commands for agentic testing:${NC}"
        echo ""
        echo -e "${YELLOW}Quick Tests:${NC}"
        echo "  quick              - Fast agentic validation (1 model, 1 test)"
        echo "  dry-run            - Preview what would be tested"
        echo ""
        echo -e "${YELLOW}Core Testing:${NC}"  
        echo "  core               - Essential agentic tests (all models, core suite)"
        echo "  comprehensive      - Full agentic evaluation (long running)"
        echo ""
        echo -e "${YELLOW}Model Comparisons:${NC}"
        echo "  compare-models     - Compare all models on agentic tasks"
        echo "  fast-models        - Test speed-optimized models"
        echo "  thorough-models    - Test detail-oriented models"
        echo ""
        echo -e "${YELLOW}Benchmarking:${NC}"
        echo "  benchmark          - Comprehensive performance benchmark"
        echo ""
        echo -e "${YELLOW}Custom:${NC}"
        echo "  custom <provider-models> <prompt-types> <test-suite> [iterations] [timeout]"
        echo ""
        echo -e "${YELLOW}Utilities:${NC}"
        echo "  list-results       - Show recent test results"
        echo "  help               - Show this help"
        echo ""
        echo -e "${YELLOW}Examples:${NC}"
        echo "  $0 quick                    # Fast test with Qwen3"
        echo "  $0 core                     # Core tests with all models"
        echo "  $0 compare-models           # Compare model performance"
        echo "  $0 custom all_models base/v4_streamlined agentic_core 2 600"
        echo ""
        echo -e "${YELLOW}Available Provider/Model Combinations:${NC}"
        echo "  - all_models       (GPT-5 Mini, Qwen3 Coder, DeepSeek 3.1)"
        echo "  - fast_models      (Qwen3 Coder Turbo)"
        echo "  - thorough_models  (DeepSeek 3.1)"
        echo "  - balanced_models  (GPT-5 Mini)"
        echo "  - deepinfra_models (Qwen3 + DeepSeek)"
        echo ""
        echo -e "${YELLOW}Available Test Suites:${NC}"
        echo "  - quick_agentic    (1 test, ~4 minutes)"
        echo "  - agentic_core     (3 tests, ~15 minutes)"  
        echo "  - agentic_comprehensive (3 tests × 2 iterations, ~30 minutes)"
        ;;
        
    *)
        echo -e "${RED}❌ Unknown command: $COMMAND${NC}"
        echo "Use '$0 help' to see available commands"
        exit 1
        ;;
esac