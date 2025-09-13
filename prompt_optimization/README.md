# Prompt Optimization Suite

A comprehensive evaluation framework for testing and optimizing prompts across different AI models and providers.

## Overview

This suite enables systematic testing of prompt strategies to optimize performance for specific models like GPT-5 Mini, Qwen3 Coder Turbo, and DeepSeek 3.1.

## Directory Structure

```
prompt_optimization/
├── README.md                 # This file
├── configs/                  # Test configuration files
│   ├── models.json          # Model definitions and settings
│   ├── test_suites.json     # Test suite configurations
│   └── evaluation.json      # Evaluation criteria and metrics
├── prompts/                  # Prompt templates and variations
│   ├── base/                # Base prompt templates
│   ├── model_specific/      # Model-optimized prompts
│   └── experiments/         # Experimental prompt variations
├── test_cases/              # Test scenarios and benchmarks
│   ├── coding/              # Programming tasks
│   ├── analysis/            # Analysis and reasoning tasks
│   └── creative/            # Creative and open-ended tasks
├── results/                 # Test results and analysis
│   ├── raw/                 # Raw test output data
│   ├── reports/             # Generated reports
│   └── comparisons/         # Comparative analyses
└── scripts/                 # Automation and utility scripts
    ├── run_tests.sh         # Main test runner
    ├── analyze_results.py   # Results analysis
    └── generate_report.sh   # Report generation
```

## Quick Start

1. **Configure Models**: Edit `configs/models.json` to define your test models
2. **Create Test Cases**: Add test scenarios in `test_cases/`
3. **Design Prompts**: Create prompt variations in `prompts/`
4. **Run Tests**: Use the CLI tool to execute evaluations
5. **Analyze Results**: Review outputs in `results/`

## CLI Usage

```bash
# Run basic evaluation
./prompt_eval --model gpt-5-mini --prompt base/v4_streamlined --test-suite coding

# Compare prompts across models
./prompt_eval --compare --models "gpt-5-mini,qwen3-coder,deepseek-3.1" --prompts "base/v4,optimized/model_specific"

# Custom test with override
./prompt_eval --model qwen3-coder --prompt experiments/concise --test custom_task.json --output results/experiment_1.json
```

## Features

- **Model-Specific Optimization**: Test prompts tailored for different AI models
- **Comparative Analysis**: Side-by-side performance comparisons
- **Metrics Tracking**: Speed, quality, cost, and custom metrics
- **Prompt Override System**: Easy prompt substitution for rapid iteration
- **Automated Reporting**: Generate comprehensive evaluation reports
- **CLI Integration**: Seamless integration with existing agent CLI
- **Result Persistence**: Track improvements over time

## Evaluation Metrics

- **Performance**: Response time, token efficiency
- **Quality**: Task completion, accuracy, relevance
- **Consistency**: Reproducibility across multiple runs
- **Cost**: Token usage and API costs
- **User Experience**: Clarity, helpfulness, format

## Next Steps

1. Set up initial test configurations
2. Create baseline prompt templates
3. Define evaluation benchmarks
4. Implement CLI interface
5. Build analysis and reporting tools