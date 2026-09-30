package wasmshell

import (
	"path/filepath"
	"testing"
)

func TestSed(t *testing.T) {
	files := map[string]string{"f.txt": "alpha one\nbeta two\ngamma three\ndelta four\n"}
	runShellCases(t, files, []shellCase{
		{"substitute first", "echo aaa | sed s/a/b/", "baa\n", 0},
		{"substitute global", "echo aaa | sed 's/a/b/g'", "bbb\n", 0},
		{"nth occurrence", "echo aaaa | sed 's/a/X/3'", "aaXa\n", 0},
		{"groups and &", `echo 'key=value' | sed -E 's/(\w+)=(\w+)/\2:\1 [&]/'`, "value:key [key=value]\n", 0},
		{"BRE groups", `echo 'key=value' | sed 's/\(.*\)=\(.*\)/\2/'`, "value\n", 0},
		{"BRE alternation", `printf 'cat\ndog\nbird\n' | sed -n '/cat\|dog/p'`, "cat\ndog\n", 0},
		{"print range", "sed -n '2,3p' f.txt", "beta two\ngamma three\n", 0},
		{"delete line", "sed '1d' f.txt", "beta two\ngamma three\ndelta four\n", 0},
		{"last line", "sed -n '$p' f.txt", "delta four\n", 0},
		{"regex range", "sed -n '/beta/,/gamma/p' f.txt", "beta two\ngamma three\n", 0},
		{"negation", "sed '/a/!d' f.txt | wc -l | tr -d ' '", "4\n", 0},
		{"multiple -e", "echo abc | sed -e s/a/1/ -e s/b/2/", "12c\n", 0},
		{"semicolons", "echo abc | sed 's/a/1/;s/c/3/'", "1b3\n", 0},
		{"custom delimiter", "echo /usr/bin | sed 's|/usr|/opt|'", "/opt/bin\n", 0},
		{"case-insensitive flag", "echo HELLO | sed 's/hello/hi/I'", "hi\n", 0},
		{"insert and append", "printf 'a\\nb\\n' | sed '1i\\\nfirst' | sed '$a last'", "first\na\nb\nlast\n", 0},
		{"change", "printf 'a\\nb\\n' | sed '2c\\\nB'", "a\nB\n", 0},
		{"quit", "sed 2q f.txt", "alpha one\nbeta two\n", 0},
		{"line numbers", "printf 'x\\ny\\n' | sed -n '$='", "2\n", 0},
		{"transliterate", "echo hello | sed 'y/abcdefghijklmnopqrstuvwxyz/ABCDEFGHIJKLMNOPQRSTUVWXYZ/'", "HELLO\n", 0},
		{"join lines", `printf 'a\nb\nc\n' | sed ':a;N;$!ba;s/\n/,/g'`, "a,b,c\n", 0},
		{"case conversion", `echo hello world | sed -E 's/(\w+)/\u\1/g'`, "Hello World\n", 0},
		{"in place", "sed -i 's/one/ONE/' f.txt && head -1 f.txt", "alpha ONE\n", 0},
		{"step address", "seq 1 6 | sed -n '0~2p'", "2\n4\n6\n", 0},
		{"plus address", "seq 1 5 | sed -n '/2/,+1p'", "2\n3\n", 0},
		{"no trailing newline kept", "printf 'x' | sed s/x/y/", "y", 0},
		{"write command escalates", "sed 'w out' f.txt", "", 127},
	})
}

func TestAwk(t *testing.T) {
	files := map[string]string{
		"data.txt": "alice 30 nyc\nbob 25 sf\ncarol 35 nyc\n",
		"ids.txt":  "root:x:0:0\nuser:x:1000:1000\n",
	}
	runShellCases(t, files, []shellCase{
		{"print field", "awk '{print $1}' data.txt", "alice\nbob\ncarol\n", 0},
		{"field separator", "awk -F: '{print $1, $3}' ids.txt", "root 0\nuser 1000\n", 0},
		{"pattern", "awk '$3 == \"nyc\" {print $1}' data.txt", "alice\ncarol\n", 0},
		{"regex pattern", "awk '/^b/' data.txt", "bob 25 sf\n", 0},
		{"sum in END", "awk '{s += $2} END {print s}' data.txt", "90\n", 0},
		{"NR and NF", "awk 'NR==2 {print NR, NF}' data.txt", "2 3\n", 0},
		{"last field", "awk '{print $NF}' data.txt", "nyc\nsf\nnyc\n", 0},
		{"arrays and for-in", "awk '{c[$3]++} END {for (k in c) print k, c[k]}' data.txt", "nyc 2\nsf 1\n", 0},
		{"printf", "awk '{printf \"%-6s|%3d\\n\", $1, $2}' data.txt", "alice | 30\nbob   | 25\ncarol | 35\n", 0},
		{"BEGIN only", "awk 'BEGIN {print 2 ^ 10, 7 / 2, int(7 / 2)}'", "1024 3.5 3\n", 0},
		{"string functions", `awk 'BEGIN {s = "Hello"; print length(s), substr(s, 2, 3), toupper(s), index(s, "l")}'`, "5 ell HELLO 3\n", 0},
		{"gsub", `echo 'a-b-c' | awk '{n = gsub(/-/, "+"); print n, $0}'`, "2 a+b+c\n", 0},
		{"split", `echo 'a,b,c' | awk '{n = split($0, p, ","); print n, p[3]}'`, "3 c\n", 0},
		{"match", `echo 'foo123bar' | awk '{match($0, /[0-9]+/); print RSTART, RLENGTH}'`, "4 3\n", 0},
		{"assign field rebuilds record", `echo 'a b c' | awk '{$2 = "X"; print}'`, "a X c\n", 0},
		{"OFS", `echo 'a b c' | awk 'BEGIN {OFS = "-"} {$1 = $1; print}'`, "a-b-c\n", 0},
		{"-v variable", "awk -v n=2 'NR == n' data.txt", "bob 25 sf\n", 0},
		{"if else and while", `awk 'BEGIN {i = 0; while (i < 3) {if (i % 2) print "odd"; else print "even"; i++}}'`, "even\nodd\neven\n", 0},
		{"for loop and next", "awk 'NR == 1 {next} {for (i = 1; i <= 2; i++) printf \"%s \", $i; print \"\"}' data.txt", "bob 25 \ncarol 35 \n", 0},
		{"user function", `awk 'function sq(x) {return x * x} BEGIN {print sq(7)}'`, "49\n", 0},
		{"range pattern", "seq 1 6 | awk '/2/,/4/'", "2\n3\n4\n", 0},
		{"numeric vs string compare", `printf '10\n9\n' | awk '$1 > 9'`, "10\n", 0},
		{"exit code", "awk 'BEGIN {exit 3}'", "", 3},
		{"ternary and concat", `awk 'BEGIN {x = 5; print (x > 3 ? "big" : "small") "!"}'`, "big!\n", 0},
		{"delete and in", `awk 'BEGIN {a["x"] = 1; delete a["x"]; print ("x" in a)}'`, "0\n", 0},
		{"print to file", `awk '{print $1 > "names.txt"}' data.txt; cat names.txt`, "alice\nbob\ncarol\n", 0},
		{"getline escalates", "awk '{getline line}' data.txt", "", 127},
	})
}

func TestJq(t *testing.T) {
	pkg := `{"name": "app", "version": "1.2.3", "scripts": {"build": "tsc", "test": "vitest"}, "deps": [{"n": "a", "v": 2}, {"n": "b", "v": 1}]}`
	runShellCases(t, map[string]string{"package.json": pkg}, []shellCase{
		{"field raw", "jq -r .version package.json", "1.2.3\n", 0},
		{"nested object keeps order", "jq .scripts package.json", "{\n  \"build\": \"tsc\",\n  \"test\": \"vitest\"\n}\n", 0},
		{"compact", "jq -c .deps package.json", `[{"n":"a","v":2},{"n":"b","v":1}]` + "\n", 0},
		{"iterate and select", `jq -r '.deps[] | select(.v > 1) | .n' package.json`, "a\n", 0},
		{"keys and length", `jq -c '[.scripts | keys, length]' package.json`, `[["build","test"],2]` + "\n", 0},
		{"map and sort_by", `jq -c '.deps | sort_by(.v) | map(.n)' package.json`, `["b","a"]` + "\n", 0},
		{"object construction", `jq -c '{name, v: .version}' package.json`, `{"name":"app","v":"1.2.3"}` + "\n", 0},
		{"string interpolation", `jq -r '"\(.name)@\(.version)"' package.json`, "app@1.2.3\n", 0},
		{"alternative", `jq -r '.missing // "default"' package.json`, "default\n", 0},
		{"has and type", `jq -c '[has("name"), (.deps | type)]' package.json`, `[true,"array"]` + "\n", 0},
		{"to_entries", `jq -r '.scripts | to_entries[] | "\(.key)=\(.value)"' package.json`, "build=tsc\ntest=vitest\n", 0},
		{"arithmetic and add", `jq '[.deps[].v] | add * 10' package.json`, "30\n", 0},
		{"if then else", `echo 5 | jq 'if . > 3 then "big" else "small" end'`, "\"big\"\n", 0},
		{"arg", `jq -n --arg x hi '{greeting: $x}' -c`, `{"greeting":"hi"}` + "\n", 0},
		{"slurp", `printf '1 2 3' | jq -s 'add'`, "6\n", 0},
		{"csv", `echo '[1,"a,b"]' | jq -r '@csv'`, `1,"a,b"` + "\n", 0},
		{"reduce", `jq -n 'reduce (1,2,3) as $x (0; . + $x)'`, "6\n", 0},
		{"test and split", `echo '"a-b"' | jq -c '[test("-"), split("-")]'`, `[true,["a","b"]]` + "\n", 0},
		{"exit status", `echo null | jq -e .`, "null\n", 1},
		{"error exit", `echo 1 | jq '.foo'`, "", 5},
		{"assignment escalates", `jq '.a = 1' package.json`, "", 127},
	})
}

func TestDiff(t *testing.T) {
	files := map[string]string{
		"a.txt": "one\ntwo\nthree\nfour\n",
		"b.txt": "one\n2\nthree\nfour\nfive\n",
		"c.txt": "one\ntwo\nthree\nfour\n",
	}
	runShellCases(t, files, []shellCase{
		{"identical", "diff a.txt c.txt", "", 0},
		{"normal format", "diff a.txt b.txt", "2c2\n< two\n---\n> 2\n4a5\n> five\n", 1},
		{"unified", "diff -u a.txt b.txt", "--- a.txt\n+++ b.txt\n@@ -1,4 +1,5 @@\n one\n-two\n+2\n three\n four\n+five\n", 1},
		{"brief", "diff -q a.txt b.txt", "Files a.txt and b.txt differ\n", 1},
		{"stdin side", "echo one | diff - c.txt | head -1", "1a2,4\n", 0},
		{"cmp", "cmp a.txt c.txt && echo same", "same\n", 0},
	})
}

func TestGrepAndRg(t *testing.T) {
	files := map[string]string{
		"src/a.go":      "package a\nfunc Alpha() {}\n// TODO: alpha\n",
		"src/b.ts":      "export const beta = 1\nconst alpha = 2\n",
		"vendor/x.go":   "func Alpha() {}\n",
		".gitignore":    "vendor/\n",
		".hidden/h.txt": "alpha\n",
	}
	runShellCases(t, files, []shellCase{
		{"BRE alternation", `printf 'cat\ndog\nbird\n' | grep 'cat\|dog'`, "cat\ndog\n", 0},
		{"fixed string", `echo 'a.b axb' | grep -o -F 'a.b'`, "a.b\n", 0},
		{"word match", `printf 'foo\nfoobar\n' | grep -w foo`, "foo\n", 0},
		{"count per file", "grep -c alpha src/a.go src/b.ts", filepath.FromSlash("src/a.go") + ":1\n" + filepath.FromSlash("src/b.ts") + ":1\n", 0},
		{"files with matches", "grep -rl Alpha src", filepath.FromSlash("src/a.go") + "\n", 0},
		{"quiet", "grep -q beta src/b.ts && echo found", "found\n", 0},
		{"multiple patterns", "grep -e beta -e Alpha src/b.ts src/a.go", filepath.FromSlash("src/b.ts") + ":export const beta = 1\n" + filepath.FromSlash("src/a.go") + ":func Alpha() {}\n", 0},
		{"context with separator", "seq 1 10 | grep -A1 -e '^2$' -e '^6$'", "2\n3\n--\n6\n7\n", 0},
		{"exclude-dir", "grep -rl --exclude-dir=vendor --exclude-dir=.hidden -i alpha .", filepath.FromSlash("./src/a.go") + "\n" + filepath.FromSlash("./src/b.ts") + "\n", 0},
		{"max count", "seq 1 20 | grep -m2 1", "1\n10\n", 0},
		{"rg respects gitignore and hidden", "rg -l Alpha", filepath.FromSlash("src/a.go") + "\n", 0},
		{"rg line numbers", "rg -n beta src", filepath.FromSlash("src/b.ts") + ":1:export const beta = 1\n", 0},
		{"rg type filter", "rg -l -t ts alpha", filepath.FromSlash("src/b.ts") + "\n", 0},
		{"rg glob exclude", "rg -l -g '!*.go' -i alpha", filepath.FromSlash("src/b.ts") + "\n", 0},
		{"rg smart case", "rg -S -c alpha src/a.go", "2\n", 0},
		{"rg smart case sensitive with capitals", "rg -S -c Alpha src/a.go", "1\n", 0},
		{"rg files", "rg --files src", filepath.FromSlash("src/a.go") + "\n" + filepath.FromSlash("src/b.ts") + "\n", 0},
		{"rg no match", "rg zebra", "", 1},
	})
}

func TestFindExtended(t *testing.T) {
	files := map[string]string{"a.go": "x", "b.txt": "", "sub/c.go": "yy", "sub/deep/d.go": "", "vendor/v.go": ""}
	runShellCases(t, files, []shellCase{
		{"relative paths", "find . -name '*.go' | sort", filepath.FromSlash("./a.go\n./sub/c.go\n./sub/deep/d.go\n./vendor/v.go\n"), 0},
		{"path and prune", "find . -path ./vendor -prune -o -name '*.go' -print | sort", filepath.FromSlash("./a.go\n./sub/c.go\n./sub/deep/d.go\n"), 0},
		{"not path", "find . -name '*.go' -not -path '*/vendor/*' | sort", filepath.FromSlash("./a.go\n./sub/c.go\n./sub/deep/d.go\n"), 0},
		{"empty files", "find . -type f -empty | sort", filepath.FromSlash("./b.txt\n./sub/deep/d.go\n./vendor/v.go\n"), 0},
		{"size", "find . -type f -size +1c", filepath.FromSlash("./sub/c.go\n"), 0},
		{"mindepth maxdepth", "find . -mindepth 2 -maxdepth 2 -type f | sort", filepath.FromSlash("./sub/c.go\n./vendor/v.go\n"), 0},
		{"exec per file", "find sub -name '*.go' -exec wc -c {} \\; | sort", filepath.FromSlash("0 sub/deep/d.go\n2 sub/c.go\n"), 0},
		{"exec batch", "find . -maxdepth 1 -name '*.go' -exec echo {} +", filepath.FromSlash("./a.go\n"), 0},
		{"delete", "find . -name '*.txt' -delete; ls b.txt", filepath.FromSlash(""), 2},
		{"printf", "find sub -maxdepth 1 -type f -printf '%f %s\\n'", filepath.FromSlash("c.go 2\n"), 0},
		{"iname and grouping", "find . \\( -iname 'A.GO' -o -name 'b.*' \\) | sort", filepath.FromSlash("./a.go\n./b.txt\n"), 0},
	})
}

func TestTextTools(t *testing.T) {
	files := map[string]string{"n.txt": "b 2\na 10\nc 1\na 10\n"}
	runShellCases(t, files, []shellCase{
		{"sort numeric key", "sort -k2 -n n.txt", "c 1\nb 2\na 10\na 10\n", 0},
		{"sort unique reverse", "sort -ru n.txt", "c 1\nb 2\na 10\n", 0},
		{"sort field separator", "printf 'x:3\\ny:1\\n' | sort -t: -k2", "y:1\nx:3\n", 0},
		{"sort human", "printf '1K\\n2M\\n3\\n' | sort -h", "3\n1K\n2M\n", 0},
		{"sort version", "printf 'v1.10\\nv1.9\\nv1.2\\n' | sort -V", "v1.2\nv1.9\nv1.10\n", 0},
		{"uniq count adjacent", "printf 'a\\na\\nb\\na\\n' | uniq -c", "      2 a\n      1 b\n      1 a\n", 0},
		{"uniq dups", "sort n.txt | uniq -d", "a 10\n", 0},
		{"cut fields range", "echo a:b:c:d | cut -d: -f2-3", "b:c\n", 0},
		{"cut chars", "echo abcdef | cut -c2-4", "bcd\n", 0},
		{"cut file", "cut -d' ' -f1 n.txt | tr -d '\\n'", "baca", 0},
		{"tr ranges", "echo Hello | tr a-z A-Z", "HELLO\n", 0},
		{"tr classes", "echo Hello | tr '[:upper:]' '[:lower:]'", "hello\n", 0},
		{"tr delete newline", "printf 'a\\nb\\n' | tr -d '\\n'", "ab", 0},
		{"tr squeeze", "echo 'a    b' | tr -s ' '", "a b\n", 0},
		{"tr complement", "echo 'ab-12' | tr -cd '0-9'", "12", 0},
		{"paste serial", "printf 'a\\nb\\nc\\n' | paste -sd,", "a,b,c\n", 0},
		{"rev and tac", "printf 'ab\\ncd\\n' | tac | rev", "dc\nba\n", 0},
		{"nl", "printf 'x\\n\\ny\\n' | nl", "     1\tx\n       \n     2\ty\n", 0},
		{"column", "printf 'a bb\\nccc d\\n' | column -t", "a    bb\nccc  d\n", 0},
		{"head and tail combos", "seq 1 10 | head -n 7 | tail -n 2", "6\n7\n", 0},
		{"wc bytes", "printf 'héllo' | wc -c | tr -d ' '", "6\n", 0},
		{"xargs", "printf 'a b\\nc\\n' | xargs echo", "a b c\n", 0},
		{"xargs -n", "seq 1 4 | xargs -n 2 echo", "1 2\n3 4\n", 0},
		{"xargs -I", "printf 'x\\ny\\n' | xargs -I{} echo item-{}", "item-x\nitem-y\n", 0},
		{"xargs grep", "echo n.txt | xargs grep -c a", "2\n", 0},
		{"printf formats", `printf '%s=%d %05.2f %x %q\n' a 42 3.14159 255 'b c'`, "a=42 03.14 ff 'b c'\n", 0},
		{"printf reuses format", `printf '%s-' a b c; echo`, "a-b-c-\n", 0},
		{"echo -e and -n", `echo -ne 'a\tb\n'`, "a\tb\n", 0},
		{"seq", "seq -s, 3", "1,2,3\n", 0},
		{"seq step width", "seq -w 8 2 12", "08\n10\n12\n", 0},
		{"base64 round trip", "echo hello | base64 | base64 -d", "hello\n", 0},
		{"sha256sum", "printf abc | sha256sum", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  -\n", 0},
		{"md5sum file", "md5sum n.txt | cut -d' ' -f3", "n.txt\n", 0},
		{"date format", "date -d @0 -u '+%Y-%m-%d %H:%M:%S'", "1970-01-01 00:00:00\n", 0},
		{"basename suffix", "basename /a/b/file.go .go", "file\n", 0},
	})
}

func TestFileTools(t *testing.T) {
	files := map[string]string{"a.txt": "A\n", "b.txt": "B", "dir/x.txt": "X\n"}
	runShellCases(t, files, []shellCase{
		{"cp into directory", "cp a.txt dir/ && cat dir/a.txt", "A\n", 0},
		{"cp several into directory", "mkdir out && cp a.txt b.txt out && ls out", "a.txt\nb.txt\n", 0},
		{"cp -r", "cp -r dir copy && cat copy/x.txt", "X\n", 0},
		{"mv into directory", "mv a.txt dir && ls dir", "a.txt\nx.txt\n", 0},
		{"mv rename dir", "mv dir moved && cat moved/x.txt && [ ! -d dir ] && echo gone", "X\ngone\n", 0},
		{"rm -r", "rm -r dir && [ ! -e dir ] && echo removed", "removed\n", 0},
		{"rm dir without -r", "rm dir", "", 1},
		{"cat keeps bytes", "cat b.txt a.txt", "BA\n", 0},
		{"cat numbering", "cat -n a.txt", "     1  A\n", 0},
		{"cd dash", "cd dir && cd - >/dev/null && ls a.txt", "a.txt\n", 0},
		{"stat format", "stat -c '%n %s' a.txt", "a.txt 2\n", 0},
		{"du summary", "du -s dir | cut -f2", "dir\n", 0},
		{"mktemp", "t=$(mktemp -p .) && [ -f $t ] && echo made", "made\n", 0},
		{"test file ops", "[ -f a.txt ] && [ -d dir ] && [ ! -e none ] && [ -s a.txt ] && echo ok", "ok\n", 0},
		{"file type", "file a.txt", "a.txt: ASCII text\n", 0},
		{"touch missing dir", "touch nodir/x", "", 1},
	})
}
