package tools

import "testing"

func TestClassifyShellCommandWindowsForms(t *testing.T) {
	cases := []struct {
		cmd  string
		want SecurityRisk
	}{
		{`cp x C:\Windows\System32\drivers\etc\hosts`, SecurityDangerous},
		{`mv a C:\Users\alan\.ssh\id_rsa`, SecurityDangerous},
		{`cp x c:/windows/system32/x.dll`, SecurityDangerous},
		{`cp x \\server\share\x`, SecurityDangerous},
		{`cp x C:\Users\alan\dev\other\x.go`, SecuritySafe},
		{`del /s /q C:\`, SecurityCaution},
		{`erase notes.txt`, SecurityCaution},
		{`rmdir /s /q C:\Users`, SecurityCaution},
		{`rd /s build`, SecurityCaution},
		{`Remove-Item -Recurse C:\Users`, SecurityCaution},
		{`format C: /q`, SecurityDangerous},
		{`diskpart`, SecurityDangerous},
		{`git log --format=%h`, SecuritySafe},
	}
	for _, tc := range cases {
		res := ClassifyToolCall("shell_command", map[string]interface{}{"command": tc.cmd})
		if res.Risk != tc.want {
			t.Errorf("%q: risk=%v want %v (%s)", tc.cmd, res.Risk, tc.want, res.Reasoning)
		}
	}
}

func TestWindowsDrivePathAsSlash(t *testing.T) {
	cases := map[string]string{`C:\Users\x`: "/Users/x", `d:/data`: "/data"}
	for in, want := range cases {
		if got, ok := windowsDrivePathAsSlash(in); !ok || got != want {
			t.Errorf("windowsDrivePathAsSlash(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"C:", "C:rel", "/usr/bin", "1:/x", "ab:/x"} {
		if _, ok := windowsDrivePathAsSlash(in); ok {
			t.Errorf("windowsDrivePathAsSlash(%q) matched, want no match", in)
		}
	}
}
