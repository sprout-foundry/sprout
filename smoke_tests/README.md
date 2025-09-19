# Smoke Tests for Registry Removal

This directory contains smoke tests to verify that the registry removal changes are working correctly.

## Tests

### API Functionality Test (`test_api_functionality.go`)

This test verifies the core API functionality after removing the provider and model registries:

1. **Provider Factory** - Tests that `CreateProviderClient()` works correctly
2. **Model Listing** - Verifies providers can list their available models from APIs
3. **No Hardcoded Defaults** - Ensures no static default models are returned
4. **Provider Names** - Tests provider name functions work correctly
5. **OpenRouter Support** - Verifies OpenRouter provider creation (streaming support)

## Running the Tests

### Quick Run
```bash
chmod +x run_api_test.sh
./run_api_test.sh
```

### Direct Run
```bash
cd smoke_tests
go run test_api_functionality.go
```

## Requirements

The tests will use real API calls if you have the following environment variables set:
- `OPENAI_API_KEY` - For OpenAI provider tests
- `OPENROUTER_API_KEY` - For OpenRouter provider tests  
- `DEEPINFRA_API_KEY` - For DeepInfra provider tests

If no API keys are set, the tests will skip the provider-specific tests.

## Expected Results

When run with API keys available, you should see output like:

```
=== Testing Core API Functionality ===
This test verifies the registry removal changes

1. Testing CreateProviderClient... PASSED (tested 3 providers)
2. Testing GetModelsForProvider... PASSED - openai: 25, openrouter: 327, deepinfra: 168 models
3. Testing removal of hardcoded defaults... PASSED - No hardcoded defaults
4. Testing provider name functions... PASSED
5. Testing OpenRouter provider creation... PASSED - Provider with streaming support created

=== Test Summary ===
Tests passed: 5
Tests failed: 0
Total tests run: 5

✅ All API functionality tests passed!
The registry removal is working correctly.
```

## What These Tests Verify

These smoke tests confirm that:

1. The provider and model registries have been successfully removed
2. Providers can be created using the new factory pattern
3. Model information comes directly from provider APIs, not static data
4. No hardcoded default models exist in the system
5. The system is more maintainable and flexible

## When to Run

Run these tests:
- After making changes to the provider system
- Before releases to ensure API functionality works
- When debugging provider-related issues
- To verify API keys are working correctly