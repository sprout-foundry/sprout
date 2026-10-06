//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSeatbeltProfileCanonicalizesPaths(t *testing.T) {
	raw := t.TempDir()
	if !strings.HasPrefix(raw, "/var/") {
		t.Skipf("temp dir %s is not under /var; nothing to canonicalize", raw)
	}
	link := filepath.Join(realDir(t), "link")
	if err := os.Symlink(raw, link); err != nil {
		t.Fatal(err)
	}
	prof, err := seatbeltProfile(Policy{WorkDir: link, TempDir: "/tmp"}, darwinUserDirs{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prof, `(subpath "/private`+raw+`")`) {
		t.Errorf("WorkDir symlink not resolved to /private%s:\n%s", raw, prof)
	}
	if strings.Contains(prof, `(subpath "`+link+`")`) {
		t.Errorf("profile names the symlink rather than its target:\n%s", prof)
	}
	if !strings.Contains(prof, `(subpath "/private/tmp")`) {
		t.Errorf("/tmp not canonicalized to /private/tmp:\n%s", prof)
	}
}

func TestSeatbeltProfileEscapesPathLiterals(t *testing.T) {
	work := filepath.Join(realDir(t), `we"ird\dir`)
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	prof, err := seatbeltProfile(Policy{WorkDir: work}, darwinUserDirs{})
	if err != nil {
		t.Fatal(err)
	}
	want := `(subpath "` + strings.TrimSuffix(work, `we"ird\dir`) + `we\"ird\\dir")`
	if !strings.Contains(prof, want) {
		t.Errorf("want %s in profile:\n%s", want, prof)
	}
}

func TestSeatbeltProfileRejectsControlCharacters(t *testing.T) {
	_, err := seatbeltProfile(Policy{WorkDir: "/tmp/a\nb"}, darwinUserDirs{})
	if err == nil {
		t.Fatal("expected an error for a path with a newline")
	}
}

func TestSeatbeltProfileRequiresWorkDir(t *testing.T) {
	if _, err := seatbeltProfile(Policy{}, darwinUserDirs{}); err == nil {
		t.Fatal("expected an error for an empty WorkDir")
	}
}

func TestSeatbeltProfileDenyReadComesLast(t *testing.T) {
	home := realDir(t)
	missing := filepath.Join(home, ".ssh")
	prof, err := seatbeltProfile(Policy{
		WorkDir:  home,
		Writable: []string{missing},
		DenyRead: []string{missing},
	}, darwinUserDirs{})
	if err != nil {
		t.Fatal(err)
	}
	denyAt := strings.Index(prof, "(deny file-read* file-write*")
	if denyAt < 0 {
		t.Fatalf("no deny-read block:\n%s", prof)
	}
	if !strings.Contains(prof[denyAt:], `(subpath "`+missing+`")`) {
		t.Errorf("missing deny path %s not in deny block:\n%s", missing, prof)
	}
	if allowAt := strings.LastIndex(prof, "(allow "); allowAt > denyAt {
		t.Errorf("an allow rule follows the deny-read block and would override it:\n%s", prof)
	}
}

func TestSeatbeltProfileNetworkToggle(t *testing.T) {
	work := realDir(t)
	closed, err := seatbeltProfile(Policy{WorkDir: work}, darwinUserDirs{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(closed, "(deny network-outbound)") || !strings.Contains(closed, `(remote ip "localhost:*")`) {
		t.Errorf("closed network profile must deny outbound but allow localhost:\n%s", closed)
	}
	open, err := seatbeltProfile(Policy{WorkDir: work, AllowNetwork: true}, darwinUserDirs{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(open, "network-outbound") {
		t.Errorf("AllowNetwork profile must not restrict outbound:\n%s", open)
	}
}

func TestSeatbeltProfileUserDirs(t *testing.T) {
	base := realDir(t)
	sys := darwinUserDirs{Temp: filepath.Join(base, "T") + "/", Cache: filepath.Join(base, "C")}
	prof, err := seatbeltProfile(Policy{WorkDir: base}, sys)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`(subpath "` + sys.Cache + `")`,
		`(subpath "` + filepath.Join(base, "T", "TemporaryItems") + `")`,
		`(regex #"^` + filepath.Join(base, "T", "xcrun_db") + `")`,
		`(regex #"^` + filepath.Join(base, "T", `TemporaryDirectory\.`) + `")`,
	} {
		if !strings.Contains(prof, want) {
			t.Errorf("want %s in profile:\n%s", want, prof)
		}
	}
	if strings.Contains(prof, `(subpath "`+filepath.Join(base, "T")+`")`) {
		t.Errorf("the whole per-user temp dir must not be writable:\n%s", prof)
	}
}
