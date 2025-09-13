#!/bin/bash

# Prompt Optimization Test Runner
# Provides convenient shortcuts for common testing scenarios

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
EVAL_TOOL="$PROJECT_DIR/prompt_eval"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Prompt Optimization Test Runner${NC}"
echo "=================================="

# Check if evaluation tool exists
if [ ! -f "$EVAL_TOOL" ]; then
    echo -e "${YELLOW}Building evaluation tool...${NC}"
    cd "$PROJECT_DIR"
    go build -o prompt_eval prompt_eval.go
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build evaluation tool${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Evaluation tool built successfully${NC}"
fi

# Parse command line arguments
COMMAND="$1"
shift || true

case "$COMMAND" in
    "quick")
        echo -e "${BLUE}🚀 Running quick benchmark...${NC}"
        $EVAL_TOOL --models "all" --prompt "base/v4_streamlined" --test-suite "quick_benchmark" --output "quick_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "compare-models")
        echo -e "${BLUE}📊 Comparing models with baseline prompt...${NC}"
        $EVAL_TOOL --models "gpt-5-mini,qwen3-coder,deepseek-3.1" --prompt "base/v4_streamlined" --test-suite "coding_basic" --iterations 2 --output "model_comparison_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "compare-prompts")
        MODEL="${1:-qwen3-coder}"
        echo -e "${BLUE}🔬 Comparing prompts for model: $MODEL${NC}"
        $EVAL_TOOL --model "$MODEL" --prompts "base/v4_streamlined,model_specific/${MODEL}_optimized" --test-suite "coding_basic" --iterations 3 --output "prompt_comparison_${MODEL}_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "test-optimized")
        echo -e "${BLUE}🎯 Testing all model-specific optimized prompts...${NC}"
        
        echo -e "${YELLOW}Testing Qwen3 optimized prompt...${NC}"
        $EVAL_TOOL --model "qwen3-coder" --prompt "model_specific/qwen3_optimized" --test-suite "coding_basic" --verbose --output "qwen3_optimized_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${YELLOW}Testing DeepSeek optimized prompt...${NC}"
        $EVAL_TOOL --model "deepseek-3.1" --prompt "model_specific/deepseek_optimized" --test-suite "coding_basic" --verbose --output "deepseek_optimized_$(date +%Y%m%d_%H%M%S).json"
        
        echo -e "${YELLOW}Testing GPT-5 optimized prompt...${NC}"
        $EVAL_TOOL --model "gpt-5-mini" --prompt "model_specific/gpt5_optimized" --test-suite "coding_basic" --verbose --output "gpt5_optimized_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "full-evaluation")
        echo -e "${BLUE}🔬 Running comprehensive evaluation...${NC}"
        echo -e "${YELLOW}This will test all models with all prompts across multiple test suites${NC}"
        read -p "Continue? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            $EVAL_TOOL --models "all" --prompts "base/v4_streamlined,model_specific/qwen3_optimized,model_specific/deepseek_optimized,model_specific/gpt5_optimized" --test-suite "coding_basic" --iterations 2 --output "full_evaluation_$(date +%Y%m%d_%H%M%S).json" --verbose
        else
            echo "Cancelled."
        fi
        ;;
        
    "dry-run")
        echo -e "${BLUE}👀 Dry run - showing what would be tested...${NC}"
        $EVAL_TOOL --models "all" --prompts "base/v4_streamlined,model_specific/qwen3_optimized" --test-suite "coding_basic" --dry-run
        ;;
        
    "custom")
        echo -e "${BLUE}🛠️  Custom test runner${NC}"
        echo "Usage: $0 custom <model> <prompt> <test-suite> [iterations]"
        
        MODEL="$1"
        PROMPT="$2"
        TEST_SUITE="$3"
        ITERATIONS="${4:-1}"
        
        if [ -z "$MODEL" ] || [ -z "$PROMPT" ] || [ -z "$TEST_SUITE" ]; then
            echo -e "${RED}❌ Missing required arguments${NC}"
            echo "Example: $0 custom qwen3-coder model_specific/qwen3_optimized coding_basic 2"
            exit 1
        fi
        
        echo -e "${YELLOW}Testing: $MODEL with $PROMPT on $TEST_SUITE (${ITERATIONS}x)${NC}"
        $EVAL_TOOL --model "$MODEL" --prompt "$PROMPT" --test-suite "$TEST_SUITE" --iterations "$ITERATIONS" --verbose --output "custom_${MODEL}_$(date +%Y%m%d_%H%M%S).json"
        ;;
        
    "list-results")
        echo -e "${BLUE}📂 Recent test results:${NC}"
        ls -la "$PROJECT_DIR/results/raw/"*.json 2>/dev/null | tail -10 || echo "No results found"
        ;;
        
    "help"|"")
        echo -e "${GREEN}Available commands:${NC}"
        echo ""
        echo -e "${YELLOW}Quick Tests:${NC}"
        echo "  quick              - Fast benchmark with all models"
        echo "  dry-run            - Show what would be tested without running"
        echo ""
        echo -e "${YELLOW}Comparisons:${NC}"
        echo "  compare-models     - Compare all models with baseline prompt"
        echo "  compare-prompts    - Compare prompts for specific model"
        echo ""
        echo -e "${YELLOW}Optimization Testing:${NC}"
        echo "  test-optimized     - Test all model-specific optimized prompts"
        echo "  full-evaluation    - Comprehensive test across all combinations"
        echo ""
        echo -e "${YELLOW}Custom:${NC}"
        echo "  custom <model> <prompt> <test-suite> [iterations]"
        echo ""
        echo -e "${YELLOW}Utilities:${NC}"
        echo "  list-results       - Show recent test results"
        echo "  help               - Show this help"
        echo ""
        echo -e "${YELLOW}Examples:${NC}"
        echo "  $0 quick"
        echo "  $0 compare-prompts qwen3-coder"
        echo "  $0 custom deepseek-3.1 model_specific/deepseek_optimized coding_basic 3"
        ;;
        
    *)
        echo -e "${RED}❌ Unknown command: $COMMAND${NC}"
        echo "Use '$0 help' to see available commands"
        exit 1
        ;;
esac