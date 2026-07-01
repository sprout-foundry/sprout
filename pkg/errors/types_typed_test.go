package errors

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Constructor tests: each produces correct Code, Severity, Status, Retryable, Time != zero
// ---------------------------------------------------------------------------

func TestNewValidation(t *testing.T) {
	details := map[string]any{"field": "x"}
	err := NewValidation("bad input", details)

	if err.Code != CodeValidation {
		t.Errorf("Code = %s; want %s", err.Code, CodeValidation)
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityError)
	}
	if err.Status != http.StatusBadRequest {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusBadRequest)
	}
	if err.Retryable {
		t.Error("Retryable = true; want false")
	}
	if err.Message != "bad input" {
		t.Errorf("Message = %q; want %q", err.Message, "bad input")
	}
	if err.Time.IsZero() {
		t.Error("Time is zero; want non-zero")
	}
	if err.Details["field"] != "x" {
		t.Error("Details missing expected key")
	}
}

func TestNewNotFound(t *testing.T) {
	err := NewNotFound("session")

	if err.Code != CodeNotFound {
		t.Errorf("Code = %s; want %s", err.Code, CodeNotFound)
	}
	if err.Message != "session not found" {
		t.Errorf("Message = %q; want %q", err.Message, "session not found")
	}
	if err.Status != http.StatusNotFound {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusNotFound)
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityError)
	}
	if err.Retryable {
		t.Error("Retryable = true; want false")
	}
	if err.Time.IsZero() {
		t.Error("Time is zero; want non-zero")
	}
}

func TestNewTimeout(t *testing.T) {
	err := NewTimeout("op", 5*time.Second)

	if err.Code != CodeTimeout {
		t.Errorf("Code = %s; want %s", err.Code, CodeTimeout)
	}
	if err.Message != "op timed out after 5s" {
		t.Errorf("Message = %q; want %q", err.Message, "op timed out after 5s")
	}
	if err.Status != http.StatusRequestTimeout {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusRequestTimeout)
	}
	if err.Retryable != true {
		t.Error("Retryable = false; want true")
	}
	if err.Severity != SeverityWarning {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityWarning)
	}
	if err.Time.IsZero() {
		t.Error("Time is zero; want non-zero")
	}
}

func TestNewPermission(t *testing.T) {
	details := map[string]any{"user": "alice"}
	err := NewPermission("forbidden", details)

	if err.Code != CodePermission {
		t.Errorf("Code = %s; want %s", err.Code, CodePermission)
	}
	if err.Status != http.StatusForbidden {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusForbidden)
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityError)
	}
	if err.Retryable {
		t.Error("Retryable = true; want false")
	}
	if err.Details["user"] != "alice" {
		t.Error("Details missing expected key")
	}
}

func TestNewNetwork(t *testing.T) {
	cause := io.EOF
	err := NewNetwork("connection refused", cause)

	if err.Code != CodeNetwork {
		t.Errorf("Code = %s; want %s", err.Code, CodeNetwork)
	}
	if err.Status != http.StatusBadGateway {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusBadGateway)
	}
	if err.Retryable != true {
		t.Error("Retryable = false; want true")
	}
	if !errors.Is(err, io.EOF) {
		t.Error("errors.Is(err, io.EOF) = false; want true")
	}
}

func TestNewConfig(t *testing.T) {
	cause := errors.New("bad yaml")
	err := NewConfig("parse failed", cause)

	if err.Code != CodeConfig {
		t.Errorf("Code = %s; want %s", err.Code, CodeConfig)
	}
	if err.Severity != SeverityCritical {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityCritical)
	}
	if err.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusInternalServerError)
	}
	if err.Retryable {
		t.Error("Retryable = true; want false")
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false; want true")
	}
}

func TestNewAgent(t *testing.T) {
	cause := errors.New("runner panic")
	err := NewAgent("agent.Runner", "crashed", cause)

	if err.Code != CodeAgent {
		t.Errorf("Code = %s; want %s", err.Code, CodeAgent)
	}
	if err.Component != "agent.Runner" {
		t.Errorf("Component = %q; want %q", err.Component, "agent.Runner")
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityError)
	}
	if err.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusInternalServerError)
	}
	if err.Retryable {
		t.Error("Retryable = true; want false")
	}
}

func TestNewTool(t *testing.T) {
	cause := io.EOF
	err := NewTool("shell", "boom", cause)

	if err.Code != CodeTool {
		t.Errorf("Code = %s; want %s", err.Code, CodeTool)
	}
	if err.Component != "tool.shell" {
		t.Errorf("Component = %q; want %q", err.Component, "tool.shell")
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityError)
	}
	if err.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusInternalServerError)
	}
	if !errors.Is(err, io.EOF) {
		t.Error("errors.Is(err, io.EOF) = false; want true")
	}
}

func TestNewApproval(t *testing.T) {
	details := map[string]any{"action": "deploy"}
	err := NewApproval("denied", details)

	if err.Code != CodeApproval {
		t.Errorf("Code = %s; want %s", err.Code, CodeApproval)
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %s; want %s", err.Severity, SeverityError)
	}
	if err.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d; want %d", err.Status, http.StatusInternalServerError)
	}
	if err.Details["action"] != "deploy" {
		t.Error("Details missing expected key")
	}
}

// ---------------------------------------------------------------------------
// Error() formatting
// ---------------------------------------------------------------------------

func TestErrorFormatWithoutCause(t *testing.T) {
	err := NewValidation("bad input", nil)
	got := err.Error()
	want := "[validation] bad input"
	if got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}
}

func TestErrorFormatWithCause(t *testing.T) {
	cause := errors.New("underlying")
	err := NewValidation("bad input", nil)
	err.Cause = cause
	got := err.Error()
	want := "[validation] bad input: underlying"
	if got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Unwrap()
// ---------------------------------------------------------------------------

func TestTypedErrorUnwrap(t *testing.T) {
	cause := errors.New("root")
	err := NewNetwork("net fail", cause)
	if err.Unwrap() != cause {
		t.Error("Unwrap() did not return the cause")
	}
}

// ---------------------------------------------------------------------------
// Is() matching by Code
// ---------------------------------------------------------------------------

func TestIsMatchesByCode(t *testing.T) {
	err := NewValidation("x", nil)
	sentinel := &TypedError{Code: CodeValidation}
	if !errors.Is(err, sentinel) {
		t.Error("errors.Is(validationErr, validationSentinel) = false; want true")
	}
}

func TestIsDoesNotMatchDifferentCode(t *testing.T) {
	err := NewValidation("x", nil)
	sentinel := &TypedError{Code: CodeNotFound}
	if errors.Is(err, sentinel) {
		t.Error("errors.Is(validationErr, notFoundSentinel) = true; want false")
	}
}

// ---------------------------------------------------------------------------
// errors.As traverses wrapping
// ---------------------------------------------------------------------------

func TestAsTraversesWrapping(t *testing.T) {
	inner := NewNotFound("x")
	wrapped := Wrap(inner, "ctx")
	te := AsTypedError(wrapped)
	if te == nil {
		t.Fatal("AsTypedError returned nil")
	}
	if te.Code != CodeNotFound {
		t.Errorf("Code = %s; want %s", te.Code, CodeNotFound)
	}
}

// ---------------------------------------------------------------------------
// AsTypedError edge cases
// ---------------------------------------------------------------------------

func TestAsTypedErrorNil(t *testing.T) {
	if got := AsTypedError(nil); got != nil {
		t.Errorf("AsTypedError(nil) = %v; want nil", got)
	}
}

func TestAsTypedErrorPlainError(t *testing.T) {
	if got := AsTypedError(errors.New("plain")); got != nil {
		t.Errorf("AsTypedError(plain) = %v; want nil", got)
	}
}

// ---------------------------------------------------------------------------
// WithDetail chainability
// ---------------------------------------------------------------------------

func TestWithDetailChainable(t *testing.T) {
	err := NewValidation("bad", nil)
	same := err.WithDetail("a", 1).WithDetail("b", 2)
	if same != err {
		t.Error("WithDetail did not return the same pointer")
	}
	if err.Details["a"] != 1 || err.Details["b"] != 2 {
		t.Errorf("Details = %v; want a=1, b=2", err.Details)
	}
}

// ---------------------------------------------------------------------------
// WithComponent chainability
// ---------------------------------------------------------------------------

func TestWithComponentChainable(t *testing.T) {
	err := NewAgent("", "msg", nil)
	same := err.WithComponent("agent.Test")
	if same != err {
		t.Error("WithComponent did not return the same pointer")
	}
	if err.Component != "agent.Test" {
		t.Errorf("Component = %q; want %q", err.Component, "agent.Test")
	}
}

// ---------------------------------------------------------------------------
// Wrap helpers
// ---------------------------------------------------------------------------

func TestWrapNilCause(t *testing.T) {
	err := Wrap(nil, "x")
	if err == nil {
		t.Fatal("Wrap(nil, ...) returned nil")
	}
	te := err.(*TypedError)
	if te.Code != CodeAgent {
		t.Errorf("Code = %s; want %s", te.Code, CodeAgent)
	}
}

func TestWrapTypedErrorNoDoubleWrap(t *testing.T) {
	inner := NewNotFound("thing")
	wrapped := Wrap(inner, "extra")
	if wrapped != inner {
		t.Error("Wrap(typedErr, ...) did not return the same pointer")
	}
}

func TestWrapPlainError(t *testing.T) {
	plain := errors.New("oops")
	wrapped := Wrap(plain, "context")
	te := wrapped.(*TypedError)
	if te.Code != CodeAgent {
		t.Errorf("Code = %s; want %s", te.Code, CodeAgent)
	}
	if te.Message != "context" {
		t.Errorf("Message = %q; want %q", te.Message, "context")
	}
	if !errors.Is(wrapped, plain) {
		t.Error("errors.Is(wrapped, plain) = false; want true")
	}
}

func TestWrapfFormats(t *testing.T) {
	wrapped := Wrapf(io.EOF, "read %s", "x")
	te := wrapped.(*TypedError)
	if !strings.Contains(te.Message, "read x") {
		t.Errorf("Message = %q; want to contain %q", te.Message, "read x")
	}
}

// ---------------------------------------------------------------------------
// Canonical mapping tables
// ---------------------------------------------------------------------------

func TestSeverityForAllCodes(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		severity Severity
	}{
		{CodeUnknown, SeverityError},
		{CodeValidation, SeverityError},
		{CodeNotFound, SeverityError},
		{CodePermission, SeverityError},
		{CodeTimeout, SeverityWarning},
		{CodeNetwork, SeverityError},
		{CodeConfig, SeverityCritical},
		{CodeAgent, SeverityError},
		{CodeTool, SeverityError},
		{CodeApproval, SeverityError},
	}
	for _, tt := range tests {
		if got := SeverityFor(tt.code); got != tt.severity {
			t.Errorf("SeverityFor(%s) = %s; want %s", tt.code, got, tt.severity)
		}
	}
}

func TestStatusForAllCodes(t *testing.T) {
	tests := []struct {
		code   ErrorCode
		status int
	}{
		{CodeUnknown, http.StatusInternalServerError},
		{CodeValidation, http.StatusBadRequest},
		{CodeNotFound, http.StatusNotFound},
		{CodePermission, http.StatusForbidden},
		{CodeTimeout, http.StatusRequestTimeout},
		{CodeNetwork, http.StatusBadGateway},
		{CodeConfig, http.StatusInternalServerError},
		{CodeAgent, http.StatusInternalServerError},
		{CodeTool, http.StatusInternalServerError},
		{CodeApproval, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		if got := StatusFor(tt.code); got != tt.status {
			t.Errorf("StatusFor(%s) = %d; want %d", tt.code, got, tt.status)
		}
	}
}

func TestRetryableForAllCodes(t *testing.T) {
	retryableCodes := []ErrorCode{CodeTimeout, CodeNetwork}
	nonRetryableCodes := []ErrorCode{
		CodeUnknown, CodeValidation, CodeNotFound,
		CodePermission, CodeConfig, CodeAgent, CodeTool, CodeApproval,
	}
	for _, c := range retryableCodes {
		if !RetryableFor(c) {
			t.Errorf("RetryableFor(%s) = false; want true", c)
		}
	}
	for _, c := range nonRetryableCodes {
		if RetryableFor(c) {
			t.Errorf("RetryableFor(%s) = true; want false", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Time is set to a recent value
// ---------------------------------------------------------------------------

func TestConstructorsSetRecentTime(t *testing.T) {
	before := time.Now()
	errs := []error{
		NewValidation("x", nil),
		NewNotFound("x"),
		NewPermission("x", nil),
		NewTimeout("x", time.Second),
		NewNetwork("x", nil),
		NewConfig("x", nil),
		NewAgent("c", "x", nil),
		NewTool("t", "x", nil),
		NewApproval("x", nil),
	}
	after := time.Now()
	for i, err := range errs {
		te := err.(*TypedError)
		if te.Time.IsZero() {
			t.Errorf("err[%d].Time is zero", i)
		}
		if te.Time.Before(before) || te.Time.After(after) {
			t.Errorf("err[%d].Time = %v; not between before=%v and after=%v", i, te.Time, before, after)
		}
	}
}

// ---------------------------------------------------------------------------
// Nil receiver safety
// ---------------------------------------------------------------------------

func TestTypedErrorIsNilReceiver(t *testing.T) {
	var nilErr *TypedError
	if nilErr.Is(errors.New("x")) {
		t.Error("nil receiver should return false")
	}
	// also verify no panic
	_ = nilErr.Is(&TypedError{Code: CodeValidation})
}

func TestTypedErrorErrorNilReceiver(t *testing.T) {
	var nilErr *TypedError
	_ = nilErr.Error() // must not panic
}

func TestTypedErrorWithDetailNilReceiver(t *testing.T) {
	var nilErr *TypedError
	out := nilErr.WithDetail("k", "v")
	if out == nil {
		t.Fatal("WithDetail on nil receiver should return a non-nil TypedError, not nil")
	}
	if out.Details["k"] != "v" {
		t.Errorf("expected detail set, got %v", out.Details)
	}
}

// ---------------------------------------------------------------------------
// Details map aliasing — constructors must clone caller maps
// ---------------------------------------------------------------------------

func TestNewValidationClonesDetails(t *testing.T) {
	src := map[string]any{"field": "x"}
	err := NewValidation("bad", src)
	// mutate source after construction
	src["field"] = "mutated"
	src["injected"] = true
	if err.Details["field"] != "x" {
		t.Errorf("expected field=x in error, got %v (constructor aliased caller map)", err.Details["field"])
	}
	if _, ok := err.Details["injected"]; ok {
		t.Error("expected 'injected' key absent in error, but it's present (constructor aliased caller map)")
	}
}

func TestNewPermissionClonesDetails(t *testing.T) {
	src := map[string]any{"op": "write"}
	err := NewPermission("denied", src)
	src["op"] = "mutated"
	if err.Details["op"] != "write" {
		t.Error("expected op=write preserved, got aliasing")
	}
}

func TestNewApprovalClonesDetails(t *testing.T) {
	src := map[string]any{"req": "abc"}
	err := NewApproval("needs review", src)
	src["req"] = "mutated"
	if err.Details["req"] != "abc" {
		t.Error("expected req=abc preserved, got aliasing")
	}
}
