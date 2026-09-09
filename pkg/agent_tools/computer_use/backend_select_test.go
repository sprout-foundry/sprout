package computer_use

import (
	"strings"
	"testing"
)

func TestCheckPermissions_AllGranted(t *testing.T) {
	prevRun := commandRunner
	commandRunner = func(name string, args ...string) ([]byte, error) {
		if name == "cliclick" {
			return nil, nil // success, no warning
		}
		return nil, nil
	}
	t.Cleanup(func() { commandRunner = prevRun })

	// Screenshot probe uses the real platform backend; on CI (no permission)
	// it may report denied — that's environment-dependent, so only assert on
	// the Accessibility check whose runner we control.
	perms := CheckPermissions()
	var access *PermissionCheck
	for i := range perms {
		if perms[i].Name == "Accessibility" {
			access = &perms[i]
		}
	}
	if access == nil {
		t.Fatal("Accessibility check missing from results")
	}
	if !access.OK {
		t.Errorf("Accessibility should be OK with clean cliclick run, got: %+v", access)
	}
}

func TestCheckPermissions_AccessibilityDenied(t *testing.T) {
	prevRun := commandRunner
	commandRunner = func(name string, args ...string) ([]byte, error) {
		if name == "cliclick" {
			return []byte("WARNING: Accessibility privileges not enabled."), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { commandRunner = prevRun })

	perms := CheckPermissions()
	var access *PermissionCheck
	for i := range perms {
		if perms[i].Name == "Accessibility" {
			access = &perms[i]
		}
	}
	if access == nil {
		t.Fatal("Accessibility check missing from results")
	}
	if access.OK {
		t.Errorf("Accessibility should be denied when cliclick warns, got: %+v", access)
	}
	if !strings.Contains(access.Detail, "Accessibility privileges not enabled") {
		t.Errorf("detail should carry cliclick's warning, got: %q", access.Detail)
	}
	if access.FixHint == "" {
		t.Error("denied check should carry a fix hint")
	}
}

func TestCheckPermissions_CliclickError(t *testing.T) {
	prevRun := commandRunner
	commandRunner = func(name string, args ...string) ([]byte, error) {
		if name == "cliclick" {
			return []byte("some failure"), errStubBoom
		}
		return nil, nil
	}
	t.Cleanup(func() { commandRunner = prevRun })

	perms := CheckPermissions()
	var access *PermissionCheck
	for i := range perms {
		if perms[i].Name == "Accessibility" {
			access = &perms[i]
		}
	}
	if access == nil {
		t.Fatal("Accessibility check missing from results")
	}
	if access.OK {
		t.Errorf("Accessibility should not be OK when cliclick errors, got: %+v", access)
	}
}

var errStubBoom = &stubError{}

type stubError struct{}

func (*stubError) Error() string { return "stub boom" }
