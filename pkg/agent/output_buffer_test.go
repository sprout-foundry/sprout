package agent

import "testing"

func TestNewOutputBuffer(t *testing.T) {
	ob := NewOutputBuffer()
	if ob == nil {
		t.Fatal("NewOutputBuffer() returned nil")
	}
	out := ob.GetOutput()
	if out != "" {
		t.Errorf("NewOutputBuffer().GetOutput() = %q, want empty", out)
	}
}

func TestOutputBufferPrintf(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Printf("hello %d", 42)
	out := ob.GetOutput()
	if out != "hello 42" {
		t.Errorf("Printf output = %q, want %q", out, "hello 42")
	}
}

func TestOutputBufferPrintfMultiple(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Printf("a")
	ob.Printf("b")
	ob.Printf("c")
	out := ob.GetOutput()
	if out != "abc" {
		t.Errorf("Printf multiple = %q, want %q", out, "abc")
	}
}

func TestOutputBufferPrint(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Print("hello", " ", "world")
	out := ob.GetOutput()
	if out != "hello world" {
		t.Errorf("Print output = %q, want %q", out, "hello world")
	}
}

func TestOutputBufferPrintln(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Println("line1")
	ob.Println("line2")
	out := ob.GetOutput()
	if out != "line1\nline2\n" {
		t.Errorf("Println output = %q, want %q", out, "line1\nline2\n")
	}
}

func TestOutputBufferGetOutputAfterPrints(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Print("a")
	ob.Printf("b%d", 2)
	ob.Println("c")
	out := ob.GetOutput()
	if out != "ab2c\n" {
		t.Errorf("Combined output = %q, want %q", out, "ab2c\n")
	}
}

func TestOutputBufferClear(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Print("data")
	if ob.GetOutput() != "data" {
		t.Errorf("Before clear: %q, want %q", ob.GetOutput(), "data")
	}
	ob.Clear()
	if ob.GetOutput() != "" {
		t.Errorf("After clear: %q, want empty", ob.GetOutput())
	}
}

func TestOutputBufferGetAndClear(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Println("line1")
	ob.Println("line2")

	first := ob.GetAndClear()
	if first != "line1\nline2" {
		t.Errorf("GetAndClear first = %q, want %q", first, "line1\nline2")
	}

	second := ob.GetAndClear()
	if second != "" {
		t.Errorf("GetAndClear second = %q, want empty", second)
	}
}

func TestOutputBufferGetAndClearTrimsSpace(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Println("  spaced  ")
	out := ob.GetAndClear()
	if out != "spaced" {
		t.Errorf("GetAndClear trimmed = %q, want %q", out, "spaced")
	}
}

func TestOutputBufferMixedOperations(t *testing.T) {
	ob := NewOutputBuffer()
	ob.Print("start")
	ob.Printf(" mid%d", 1)
	ob.Println("end")
	ob.Clear()

	ob.Print("reset")
	out := ob.GetOutput()
	if out != "reset" {
		t.Errorf("After clear and print = %q, want %q", out, "reset")
	}
}
