# Quick Start Guide - Prompt Optimization Suite

## 🚀 Get Started in 2 Minutes

### 1. Run a Quick Test
```bash
cd prompt_optimization
./scripts/run_tests.sh quick
```

### 2. Compare Models 
```bash
./scripts/run_tests.sh compare-models
```

### 3. Test Model-Specific Optimizations
```bash
# Test optimized prompts for all models
./scripts/run_tests.sh test-optimized

# Or test a specific model
./scripts/run_tests.sh compare-prompts qwen3-coder
```

## 📊 Example Output

When you run a comparison, you'll see:

```
🧪 Prompt Evaluation Tool
=========================
📊 Running evaluation: 3 models × 2 prompts × 2 tests × 2 iterations
...............

📊 EVALUATION SUMMARY
=====================
Run ID: run_1726176234
Tests: 24 | Success Rate: 95.8% | Avg Score: 78.5 | Avg Time: 4250ms

Model Performance:
------------------
gpt-5-mini      | 8 tests | 100.0% success | 82.3 score | 8558ms
qwen3-coder     | 8 tests |  87.5% success | 79.1 score | 2263ms  
deepseek-3.1    | 8 tests | 100.0% success | 74.2 score | 13164ms
```

## 🎯 Key Commands

| Command | Purpose | Example |
|---------|---------|---------|
| `quick` | Fast benchmark | `./scripts/run_tests.sh quick` |
| `compare-models` | Compare all models | `./scripts/run_tests.sh compare-models` |
| `compare-prompts <model>` | Compare prompts for one model | `./scripts/run_tests.sh compare-prompts qwen3-coder` |
| `test-optimized` | Test all optimized prompts | `./scripts/run_tests.sh test-optimized` |
| `custom` | Custom test | `./scripts/run_tests.sh custom qwen3-coder model_specific/qwen3_optimized coding_basic 3` |

## 📁 Key Files

- **Test Cases**: `test_cases/coding/factorial.json` - Define what to test
- **Model Configs**: `configs/models.json` - Model definitions and characteristics  
- **Prompts**: `prompts/model_specific/qwen3_optimized.md` - Optimized prompts
- **Results**: `results/raw/` - JSON output files with detailed results

## 🔧 CLI Usage

Direct CLI usage (without scripts):

```bash
# Basic test
./prompt_eval --model qwen3-coder --prompt base/v4_streamlined --test-suite coding_basic

# Comparison test  
./prompt_eval --models "gpt-5-mini,qwen3-coder,deepseek-3.1" --prompts "base/v4_streamlined,model_specific/qwen3_optimized" --test-suite coding_basic --iterations 2 --verbose

# Save results
./prompt_eval --model qwen3-coder --prompt model_specific/qwen3_optimized --test-suite coding_basic --output my_test.json
```

## 🎯 What's Next?

1. **Run your first comparison**: `./scripts/run_tests.sh compare-models`
2. **Create custom prompts**: Edit files in `prompts/model_specific/`  
3. **Add test cases**: Create new JSON files in `test_cases/`
4. **Analyze results**: Check JSON files in `results/raw/`

The suite is designed for rapid iteration - modify a prompt, run a test, compare results, repeat!