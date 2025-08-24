package math

import "testing"

func TestMultiply(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 2},
		{0, 5, 0},
		{-1, 1, -1},
		{-2, -3, 6},
	}

	for _, test := range tests {
		result := Multiply(test.a, test.b)
		if result != test.expected {
			t.Errorf("Multiply(%d, %d) = %d; expected %d", test.a, test.b, result, test.expected)
		}
	}
}
