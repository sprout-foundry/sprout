package computer_use

import (
	"encoding/base64"
	"sync"
)

// minimalPNG is the smallest valid PNG (1x1 red pixel).
var minimalPNG = func() []byte {
	b, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+P+/HgAFhAJ/wl4P9wAAAABJRU5ErkJggg==")
	return b
}()

// MockBackendRecord records a single call for test assertions.
type MockBackendRecord struct {
	Action string
	Args   map[string]any
}

// MockBackend implements ComputerBackend for testing.
type MockBackend struct {
	mu                     sync.Mutex
	Records                []MockBackendRecord
	OverrideScreenshotData []byte
	OverrideScreenshotDims Size
	OverrideError          error
}

func (m *MockBackend) Screenshot(region *Rect) ([]byte, Size, error) {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "Screenshot",
		Args:   map[string]any{"region": region},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return nil, Size{}, m.OverrideError
	}

	if m.OverrideScreenshotData != nil {
		return m.OverrideScreenshotData, m.OverrideScreenshotDims, nil
	}

	return minimalPNG, Size{Width: 1, Height: 1}, nil
}

func (m *MockBackend) MouseClick(x, y int, button MouseButton, double bool) error {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "MouseClick",
		Args: map[string]any{
			"x": x, "y": y,
			"button": button,
			"double": double,
		},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return m.OverrideError
	}
	return nil
}

func (m *MockBackend) MoveTo(x, y int) error {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "MoveTo",
		Args:   map[string]any{"x": x, "y": y},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return m.OverrideError
	}
	return nil
}

func (m *MockBackend) MouseDrag(from, to Point, button MouseButton) error {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "MouseDrag",
		Args: map[string]any{
			"from":   from,
			"to":     to,
			"button": button,
		},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return m.OverrideError
	}
	return nil
}

func (m *MockBackend) KeyboardType(text string) error {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "KeyboardType",
		Args:   map[string]any{"text": text},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return m.OverrideError
	}
	return nil
}

func (m *MockBackend) KeyboardPress(key string) error {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "KeyboardPress",
		Args:   map[string]any{"key": key},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return m.OverrideError
	}
	return nil
}

func (m *MockBackend) Scroll(dir ScrollDir, amount int, at *Point) error {
	m.mu.Lock()
	m.Records = append(m.Records, MockBackendRecord{
		Action: "Scroll",
		Args: map[string]any{
			"dir":    dir,
			"amount": amount,
			"at":     at,
		},
	})
	m.mu.Unlock()

	if m.OverrideError != nil {
		return m.OverrideError
	}
	return nil
}
