// Assertion evaluation for the conformance test runner.
package main

import (
	"fmt"
	"strings"
)

// EvaluateAssertions runs all spec assertions against collected results.
func EvaluateAssertions(spec Spec, responses map[int]*CollectedResponse, events []CollectedEvent) []AssertionResult {
	results := make([]AssertionResult, 0, len(spec.Assertions))
	for _, asrt := range spec.Assertions {
		results = append(results, evaluateSingle(asrt, responses, events))
	}
	return results
}

func evaluateSingle(asrt SpecAssertion, responses map[int]*CollectedResponse, events []CollectedEvent) AssertionResult {
	switch asrt.Type {
	case "noError":
		return assertNoError(asrt, responses)
	case "response":
		return assertResponse(asrt, responses)
	case "error":
		return assertError(asrt, responses)
	case "event":
		return assertEvent(asrt, events)
	case "events":
		return assertEventsCount(asrt, events)
	case "state":
		return assertState(asrt, responses)
	case "mock":
		return assertMock(asrt, responses)
	default:
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("unknown assertion type: %s", asrt.Type)}
	}
}

// assertNoError checks that the response for a given ID has no error.
func assertNoError(asrt SpecAssertion, responses map[int]*CollectedResponse) AssertionResult {
	resp, ok := responses[asrt.ID]
	if !ok {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("no response found for id %d", asrt.ID)}
	}
	if resp.Error != nil {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected no error for id %d, got: %s", asrt.ID, errorMessage(resp.Error))}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}

// assertResponse checks the result map of a response for the given ID.
func assertResponse(asrt SpecAssertion, responses map[int]*CollectedResponse) AssertionResult {
	resp, ok := responses[asrt.ID]
	if !ok {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("no response found for id %d", asrt.ID)}
	}

	if resp.Error != nil && asrt.Error != nil {
		if asrt.Error.Code != 0 {
			codeVal, _ := (*resp.Error)["code"]
			codeFloat, _ := codeVal.(float64)
			if int(codeFloat) != asrt.Error.Code {
				return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected error code %d, got %v", asrt.Error.Code, codeVal)}
			}
		}
		if asrt.Error.Message != "" {
			msgStr := errorMessage(resp.Error)
			if !strings.Contains(msgStr, asrt.Error.Message) {
				return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected error message containing %q, got %q", asrt.Error.Message, msgStr)}
			}
		}
		return AssertionResult{Assertion: asrt, Passed: true}
	}

	if asrt.Result == nil || len(asrt.Result) == 0 {
		return AssertionResult{Assertion: asrt, Passed: true}
	}

	if resp.Result == nil {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected result for id %d, got nil result", asrt.ID)}
	}

	for key, expected := range asrt.Result {
		actual, ok := resp.Result[key]
		if !ok {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected result key %q not found in response %d", key, asrt.ID)}
		}
		if !valuesMatch(expected, actual) {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected result[%s] = %v, got %v", key, expected, actual)}
		}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}

// assertError checks that a response has an error with the expected type and/or message.
func assertError(asrt SpecAssertion, responses map[int]*CollectedResponse) AssertionResult {
	resp, ok := responses[asrt.ID]
	if !ok {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("no response found for id %d", asrt.ID)}
	}
	if resp.Error == nil {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected error for id %d, got no error", asrt.ID)}
	}

	actualMsg := errorMessage(resp.Error)

	if asrt.ErrorType != "" {
		substrings, ok := errorTypeMap[asrt.ErrorType]
		if !ok {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("unknown errorType: %s", asrt.ErrorType)}
		}
		matched := false
		for _, substr := range substrings {
			if strings.Contains(actualMsg, substr) {
				matched = true
				break
			}
		}
		if !matched {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected error type %q (matching %v), got message: %s", asrt.ErrorType, substrings, actualMsg)}
		}
	}

	if asrt.Contains != "" && !strings.Contains(actualMsg, asrt.Contains) {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected error message containing %q, got: %s", asrt.Contains, actualMsg)}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}

// assertEvent checks that at least one event of the given type was emitted.
func assertEvent(asrt SpecAssertion, events []CollectedEvent) AssertionResult {
	var matching []CollectedEvent
	for _, ev := range events {
		if ev.Event == asrt.Event {
			if asrt.Contains == "" {
				matching = append(matching, ev)
			} else if containsInMap(ev.Data, asrt.Contains) {
				matching = append(matching, ev)
			}
		}
	}
	if len(matching) == 0 {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected event %q not found (contains: %q)", asrt.Event, asrt.Contains)}
	}
	if asrt.Count > 0 && len(matching) != asrt.Count {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected %d events of type %q, got %d", asrt.Count, asrt.Event, len(matching))}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}

// assertEventsCount checks that exactly N events of the given type exist.
func assertEventsCount(asrt SpecAssertion, events []CollectedEvent) AssertionResult {
	count := 0
	for _, ev := range events {
		if ev.Event == asrt.Event {
			count++
		}
	}
	if asrt.Count > 0 && count != asrt.Count {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected %d events of type %q, got %d", asrt.Count, asrt.Event, count)}
	}
	if count == 0 {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected events of type %q, found none", asrt.Event)}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}

// assertState checks state from a response using the given path.
func assertState(asrt SpecAssertion, responses map[int]*CollectedResponse) AssertionResult {
	resp := getResponseForAssertion(asrt.ID, responses)
	if resp == nil {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("no response found for id %d (state assertion)", asrt.ID)}
	}
	if resp.Result == nil {
		if asrt.NotEmpty {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected non-empty state result for id %d, got nil", asrt.ID)}
		}
		return AssertionResult{Assertion: asrt, Passed: true}
	}

	val := navigatePath(resp.Result, asrt.Path)

	if val == nil && asrt.NotEmpty {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected non-empty value at path %q, got nil", asrt.Path)}
	}
	if val != nil && asrt.NotEmpty {
		return AssertionResult{Assertion: asrt, Passed: true}
	}
	if asrt.Equals != nil {
		if !valuesMatch(asrt.Equals, val) {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected state[%s] = %v, got %v", asrt.Path, asrt.Equals, val)}
		}
	}
	if asrt.Contains != "" {
		valStr := fmt.Sprint(val)
		if !strings.Contains(valStr, asrt.Contains) {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected state[%s] to contain %q, got %v", asrt.Path, asrt.Contains, val)}
		}
	}
	if asrt.GreaterThan != nil {
		gtVal, ok := toFloat(asrt.GreaterThan)
		if !ok {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("greaterThan value is not a number: %v", asrt.GreaterThan)}
		}
		actualFloat, ok := toFloat(val)
		if !ok {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("cannot compare state[%s] (%v) with greaterThan %v", asrt.Path, val, asrt.GreaterThan)}
		}
		if actualFloat <= gtVal {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected state[%s] > %v, got %v", asrt.Path, asrt.GreaterThan, val)}
		}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}

// assertMock checks mock state from a response.
func assertMock(asrt SpecAssertion, responses map[int]*CollectedResponse) AssertionResult {
	resp := getResponseForAssertion(asrt.ID, responses)
	if resp == nil {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("no response found for id %d (mock assertion)", asrt.ID)}
	}
	if resp.Result == nil {
		if asrt.NotEmpty {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected non-empty mock result for id %d, got nil", asrt.ID)}
		}
		return AssertionResult{Assertion: asrt, Passed: true}
	}

	val := navigatePath(resp.Result, asrt.Path)

	if val == nil && asrt.NotEmpty {
		return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected non-empty mock value at path %q, got nil", asrt.Path)}
	}
	if val != nil && asrt.NotEmpty {
		return AssertionResult{Assertion: asrt, Passed: true}
	}
	if asrt.Equals != nil {
		if !valuesMatch(asrt.Equals, val) {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected mock[%s] = %v, got %v", asrt.Path, asrt.Equals, val)}
		}
	}
	if asrt.Contains != "" {
		valStr := fmt.Sprint(val)
		if !strings.Contains(valStr, asrt.Contains) {
			return AssertionResult{Assertion: asrt, Passed: false, Message: fmt.Sprintf("expected mock[%s] to contain %q, got %v", asrt.Path, asrt.Contains, val)}
		}
	}
	return AssertionResult{Assertion: asrt, Passed: true}
}
