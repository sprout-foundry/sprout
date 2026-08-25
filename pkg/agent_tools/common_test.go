package tools

import (
	"strings"
	"testing"
)

func TestNormalizeTodoID(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		// string inputs
		{"already normalized", "todo_1", "todo_1"},
		{"numeric string", "1", "todo_1"},
		{"numeric string large", "99", "todo_99"},
		{"empty string", "", ""},
		{"non-numeric string", "my_title", "my_title"},
		{"alpha-numeric string", "abc123", "abc123"},
		{"with leading zero", "01", "todo_01"},

		// numeric inputs
		{"float64", float64(1.0), "todo_1"},
		{"float64 large", float64(99.9), "todo_99"},
		{"int", int(1), "todo_1"},
		{"int zero", int(0), "todo_0"},

		// nil and unsupported
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTodoID(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeTodoID(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsValidStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		{"pending", "pending", true},
		{"in_progress", "in_progress", true},
		{"completed", "completed", true},
		{"cancelled", "cancelled", true},
		{"empty", "", false},
		{"unknown", "unknown", false},
		{"in progress with space", "in progress", false},
		{"completed upper", "COMPLETED", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidStatus(tt.status)
			if got != tt.expected {
				t.Errorf("IsValidStatus(%q) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestFormatTodoStatusError(t *testing.T) {
	status := "invalid_status"
	got := FormatTodoStatusError(status)

	if !strings.Contains(got, "invalid_status") {
		t.Errorf("FormatTodoStatusError() should contain the invalid status, got: %s", got)
	}

	for _, s := range []string{"pending", "in_progress", "completed", "cancelled"} {
		if !strings.Contains(got, s) {
			t.Errorf("FormatTodoStatusError() should contain %q, got: %s", s, got)
		}
	}
}

func TestValidTodoStatuses(t *testing.T) {
	statuses := ValidTodoStatuses()
	expected := []string{"pending", "in_progress", "completed", "cancelled"}

	if len(statuses) != len(expected) {
		t.Fatalf("Expected %d statuses, got %d", len(expected), len(statuses))
	}

	for i, s := range statuses {
		if s != expected[i] {
			t.Errorf("Expected status[%d] = %q, got %q", i, expected[i], s)
		}
	}
}

func TestIsValidPriority(t *testing.T) {
	tests := []struct {
		name     string
		priority string
		expected bool
	}{
		{"high", "high", true},
		{"medium", "medium", true},
		{"low", "low", true},
		{"empty (optional)", "", true},
		{"unknown", "critical", false},
		{"upper case", "HIGH", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidPriority(tt.priority)
			if got != tt.expected {
				t.Errorf("IsValidPriority(%q) = %v, want %v", tt.priority, got, tt.expected)
			}
		})
	}
}

func TestFormatTodoPriorityError(t *testing.T) {
	priority := "urgent"
	got := FormatTodoPriorityError(priority)

	if !strings.Contains(got, "urgent") {
		t.Errorf("FormatTodoPriorityError() should contain the invalid priority, got: %s", got)
	}

	for _, p := range []string{"high", "medium", "low"} {
		if !strings.Contains(got, p) {
			t.Errorf("FormatTodoPriorityError() should contain %q, got: %s", p, got)
		}
	}
}

func TestValidTodoPriorityList(t *testing.T) {
	priorities := ValidTodoPriorityList()
	expected := []string{"high", "medium", "low"}

	if len(priorities) != len(expected) {
		t.Fatalf("Expected %d priorities, got %d", len(expected), len(priorities))
	}

	for i, p := range priorities {
		if p != expected[i] {
			t.Errorf("Expected priority[%d] = %q, got %q", i, expected[i], p)
		}
	}
}

func TestFormatTodoResponseForID(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		id       string
		expected string
	}{
		{"basic", "Write tests", "todo_1", "Write tests [todo_1]"},
		{"with spaces", "Write tests for the agent", "todo_42", "Write tests for the agent [todo_42]"},
		{"empty title", "", "todo_1", " [todo_1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTodoResponseForID(tt.title, tt.id)
			if got != tt.expected {
				t.Errorf("formatTodoResponseForID(%q, %q) = %q, want %q",
					tt.title, tt.id, got, tt.expected)
			}
		})
	}
}

func TestFormatTodoSuccess(t *testing.T) {
	got := formatTodoSuccess("Write tests", "todo_1")
	expected := "[OK] Added todo: Write tests [todo_1]"
	if got != expected {
		t.Errorf("formatTodoSuccess() = %q, want %q", got, expected)
	}
}

func TestFormatBulkTodoSuccess(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		items       []string
		moreCount   int
		mustContain string
	}{
		{
			name:        "with more items",
			count:       5,
			items:       []string{"A [todo_1]", "B [todo_2]", "C [todo_3]"},
			moreCount:   2,
			mustContain: "[edit] Added 5 todos",
		},
		{
			name:        "without more items",
			count:       3,
			items:       []string{"A [todo_1]", "B [todo_2]", "C [todo_3]"},
			moreCount:   0,
			mustContain: "[edit] Added 3 todos",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBulkTodoSuccess(tt.count, tt.items, tt.moreCount)
			if !strings.Contains(got, tt.mustContain) {
				t.Errorf("formatBulkTodoSuccess() = %q, should contain %q", got, tt.mustContain)
			}
		})
	}
}

func TestFormatStatusUpdate(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		title       string
		id          string
		remaining   int
		mustContain string
	}{
		{
			name:        "in_progress",
			status:      "in_progress",
			title:       "Write tests",
			id:          "todo_1",
			remaining:   3,
			mustContain: "[~] Started: Write tests [todo_1]",
		},
		{
			name:        "completed with remaining",
			status:      "completed",
			title:       "Write tests",
			id:          "todo_1",
			remaining:   2,
			mustContain: "[OK] Completed: Write tests [todo_1] (2 remaining)",
		},
		{
			name:        "completed with none remaining",
			status:      "completed",
			title:       "Write tests",
			id:          "todo_1",
			remaining:   0,
			mustContain: "[done] Completed: Write tests - All todos done!",
		},
		{
			name:        "cancelled",
			status:      "cancelled",
			title:       "Write tests",
			id:          "todo_1",
			remaining:   3,
			mustContain: "[FAIL] Cancelled: Write tests [todo_1]",
		},
		{
			name:        "pending (default case)",
			status:      "pending",
			title:       "Write tests",
			id:          "todo_1",
			remaining:   3,
			mustContain: "[edit] Updated: Write tests [todo_1] → pending",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusUpdate(tt.status, tt.title, tt.id, tt.remaining)
			if got != tt.mustContain {
				t.Errorf("formatStatusUpdate() = %q, want %q", got, tt.mustContain)
			}
		})
	}
}

func TestFormatBulkStatusSummary(t *testing.T) {
	tests := []struct {
		name         string
		updatedCount int
		results      []string
		expected     string
	}{
		{
			name:         "zero updates",
			updatedCount: 0,
			results:      []string{},
			expected:     "No updates made",
		},
		{
			name:         "one update",
			updatedCount: 1,
			results:      []string{"A"},
			expected:     "A",
		},
		{
			name:         "three updates (at limit)",
			updatedCount: 3,
			results:      []string{"A", "B", "C"},
			expected:     "A, B, C",
		},
		{
			name:         "five updates (over limit)",
			updatedCount: 5,
			results:      []string{"A", "B", "C", "D", "E"},
			expected:     "Updated 5: A, +4 more",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBulkStatusSummary(tt.updatedCount, tt.results)
			if got != tt.expected {
				t.Errorf("formatBulkStatusSummary() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatStatusWithID(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		title    string
		id       string
		expected string
	}{
		{"pending", "pending", "Write tests", "todo_1", "○ Write tests [todo_1]"},
		{"in_progress", "in_progress", "Write tests", "todo_1", "► Write tests [todo_1]"},
		{"completed", "completed", "Write tests", "todo_1", "[ok] Write tests [todo_1]"},
		{"cancelled", "cancelled", "Write tests", "todo_1", "[fail] Write tests [todo_1]"},
		{"unknown", "unknown", "Write tests", "todo_1", "· Write tests [todo_1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusWithID(tt.status, tt.title, tt.id)
			if got != tt.expected {
				t.Errorf("formatStatusWithID() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetCompactStatusSymbol(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{"pending", "pending", "○"},
		{"in_progress", "in_progress", "►"},
		{"completed", "completed", "[ok]"},
		{"cancelled", "cancelled", "[fail]"},
		{"unknown", "unknown", "·"},
		{"empty", "", "·"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getCompactStatusSymbol(tt.status)
			if got != tt.expected {
				t.Errorf("getCompactStatusSymbol(%q) = %q, want %q",
					tt.status, got, tt.expected)
			}
		})
	}
}
