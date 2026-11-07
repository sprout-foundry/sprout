package ast

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseFileWithinReportsATimeout(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("export function Item")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("() { return <div className=\"a\">{[1, 2, 3].map((n) => <span key={n}>{n}</span>)}</div>; }\n")
	}
	src := []byte(b.String())

	if _, err := ParseFileWithin("big.tsx", src, time.Microsecond); !errors.Is(err, ErrParseTimeout) {
		t.Fatalf("ParseFileWithin with a 1µs budget: err = %v, want ErrParseTimeout", err)
	}
	res, err := ParseFileWithin("small.tsx", []byte("export function A() { return <div/>; }\n"), 5*time.Second)
	if err != nil {
		t.Fatalf("ParseFileWithin on a small file: %v", err)
	}
	res.Release()
}
