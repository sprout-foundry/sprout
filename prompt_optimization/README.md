# Prompt Optimization Framework

A systematic approach to testing, validating, and optimizing LLM prompts used throughout the ledit project.

## Overview

This framework provides:
- **Automated prompt testing** against defined goals and validation criteria
- **Iterative optimization** with performance tracking
- **A/B testing** capabilities for prompt variants
- **Comprehensive validation** including accuracy, format, and quality metrics
- **Cost tracking** for different prompt strategies

## Directory Structure

```
prompt_optimization/
├── framework/          # Core testing and optimization logic
├── test_cases/         # Test scenarios for different prompt types
├── prompts/            # Current and candidate prompt versions
├── results/            # Test results, metrics, and iteration history
├── configs/            # Configuration files for different test scenarios
└── README.md           # This file
```

## Key Components

### 1. Prompt Testing Framework (`framework/`)
- `prompt_tester.go` - Core testing engine
- `validators.go` - Validation logic for different prompt types
- `metrics.go` - Performance and quality metrics
- `optimizer.go` - Automated prompt improvement logic

### 2. Test Cases (`test_cases/`)
- Organized by prompt type (code_generation, text_replacement, analysis, etc.)
- Each test case defines inputs, expected outcomes, and validation criteria
- Covers edge cases and common failure scenarios

### 3. Prompt Library (`prompts/`)
- Current prompts extracted from the codebase
- Candidate prompt variations for testing
- Version history and performance tracking

### 4. Results Tracking (`results/`)
- Detailed test results with metrics
- Performance comparisons between prompt versions
- Optimization history and insights

## Usage

1. **Catalog existing prompts**: Extract and document current prompts
2. **Define test scenarios**: Create comprehensive test cases
3. **Set optimization goals**: Define success criteria for each prompt type
4. **Run optimization**: Execute iterative improvement process
5. **Deploy improvements**: Integrate optimized prompts back into the codebase

## Quick Start

```bash
# Run prompt optimization for a specific type
go run framework/prompt_tester.go --type code_generation --optimize

# Test a specific prompt against its test cases  
go run framework/prompt_tester.go --prompt prompts/code_generation_v2.txt --validate

# Compare multiple prompt versions
go run framework/prompt_tester.go --compare prompts/code_generation_v*.txt
```

## Goals

The framework aims to achieve:
- **95%+ accuracy** for code generation tasks
- **100% format compliance** for structured outputs
- **Cost efficiency** through optimized prompt design
- **Consistent behavior** across different model types
- **Measurable improvements** through systematic iteration