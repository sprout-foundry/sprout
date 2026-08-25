package computer_use

import "testing"

func TestTranslateAnthropic_Screenshot(t *testing.T) {
	m := withMock(t)
	m.OverrideScreenshotData = minimalPNG
	m.OverrideScreenshotDims = Size{Width: 100, Height: 50}
	out, err := TranslateAnthropicAction("screenshot", nil)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}
	if res["width"] != 100 || res["height"] != 50 {
		t.Errorf("unexpected dims: %+v", res)
	}
	if _, ok := res["data_uri"].(string); !ok {
		t.Error("missing data_uri")
	}
}

func TestTranslateAnthropic_LeftClick(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("left_click", map[string]any{
		"coordinate": []any{float64(12), float64(34)},
	}); err != nil {
		t.Fatalf("left_click: %v", err)
	}
	if len(m.Records) != 1 || m.Records[0].Action != "MouseClick" {
		t.Fatalf("expected MouseClick, got %+v", m.Records)
	}
	if m.Records[0].Args["x"] != 12 || m.Records[0].Args["y"] != 34 {
		t.Errorf("bad coords: %+v", m.Records[0].Args)
	}
}

func TestTranslateAnthropic_TypeAndKey(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("type", map[string]any{"text": "hello"}); err != nil {
		t.Fatalf("type: %v", err)
	}
	if _, err := TranslateAnthropicAction("key", map[string]any{"key": "Return"}); err != nil {
		t.Fatalf("key: %v", err)
	}
	if m.Records[0].Action != "KeyboardType" || m.Records[0].Args["text"] != "hello" {
		t.Errorf("type not recorded: %+v", m.Records[0])
	}
	if m.Records[1].Action != "KeyboardPress" || m.Records[1].Args["key"] != "Return" {
		t.Errorf("key not recorded: %+v", m.Records[1])
	}
}

func TestTranslateAnthropic_Scroll(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("scroll", map[string]any{
		"scroll_direction": "down", "scroll_amount": float64(3),
	}); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if m.Records[0].Action != "Scroll" || m.Records[0].Args["dir"] != ScrollDown {
		t.Errorf("scroll not recorded: %+v", m.Records[0])
	}
	if m.Records[0].Args["amount"] != 3 {
		t.Errorf("amount = %v, want 3", m.Records[0].Args["amount"])
	}
}

func TestTranslateAnthropic_UnknownAction(t *testing.T) {
	withMock(t)
	if _, err := TranslateAnthropicAction("teleport", nil); err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestTranslateAnthropic_MissingCoordinate(t *testing.T) {
	withMock(t)
	if _, err := TranslateAnthropicAction("left_click", map[string]any{}); err == nil {
		t.Error("expected error for missing coordinate")
	}
}

func TestTranslateAnthropic_MouseMove(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("mouse_move", map[string]any{
		"coordinate": []any{float64(42), float64(99)},
	}); err != nil {
		t.Fatalf("mouse_move: %v", err)
	}
	if len(m.Records) != 1 || m.Records[0].Action != "MoveTo" {
		t.Fatalf("expected MoveTo, got %+v", m.Records)
	}
	if m.Records[0].Args["x"] != 42 || m.Records[0].Args["y"] != 99 {
		t.Errorf("bad coords: %+v", m.Records[0].Args)
	}
}

func TestTranslateAnthropic_DragParams(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("left_click_drag", map[string]any{
		"start_coordinate": []any{float64(10), float64(20)},
		"coordinate":       []any{float64(30), float64(40)},
	}); err != nil {
		t.Fatalf("left_click_drag: %v", err)
	}
	if len(m.Records) != 1 || m.Records[0].Action != "MouseDrag" {
		t.Fatalf("expected MouseDrag, got %+v", m.Records)
	}
	from := m.Records[0].Args["from"].(Point)
	to := m.Records[0].Args["to"].(Point)
	if from.X != 10 || from.Y != 20 {
		t.Errorf("bad from: %+v", from)
	}
	if to.X != 30 || to.Y != 40 {
		t.Errorf("bad to: %+v", to)
	}
}

func TestTranslateAnthropic_DragMissingStartCoordinate(t *testing.T) {
	withMock(t)
	if _, err := TranslateAnthropicAction("left_click_drag", map[string]any{
		"coordinate": []any{float64(30), float64(40)},
	}); err == nil {
		t.Error("expected error for missing start_coordinate")
	}
}

func TestTranslateAnthropic_ScrollAmount(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("scroll", map[string]any{
		"scroll_direction": "down", "scroll_amount": float64(3),
	}); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if len(m.Records) != 1 || m.Records[0].Action != "Scroll" {
		t.Fatalf("expected Scroll, got %+v", m.Records)
	}
	if m.Records[0].Args["dir"] != ScrollDown {
		t.Errorf("bad dir: %+v", m.Records[0].Args)
	}
	if m.Records[0].Args["amount"] != 3 {
		t.Errorf("bad amount: %+v", m.Records[0].Args)
	}
}

func TestTranslateAnthropic_WaitDuration(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("wait", map[string]any{
		"duration": float64(0.1),
	}); err != nil {
		t.Fatalf("wait: %v", err)
	}
	// No backend record for wait — it just sleeps.
	if len(m.Records) != 0 {
		t.Errorf("expected no backend calls for wait, got %+v", m.Records)
	}
}

func TestTranslateAnthropic_WaitMissingDuration(t *testing.T) {
	withMock(t)
	if _, err := TranslateAnthropicAction("wait", map[string]any{}); err == nil {
		t.Error("expected error for missing duration")
	}
}

func TestTranslateAnthropic_TripleClick(t *testing.T) {
	m := withMock(t)
	if _, err := TranslateAnthropicAction("triple_click", map[string]any{
		"coordinate": []any{float64(5), float64(6)},
	}); err != nil {
		t.Fatalf("triple_click: %v", err)
	}
	// Three rapid single clicks at the same spot.
	if len(m.Records) != 3 {
		t.Fatalf("expected 3 MouseClick records, got %d: %+v", len(m.Records), m.Records)
	}
	for i, rec := range m.Records {
		if rec.Action != "MouseClick" {
			t.Errorf("record %d: expected MouseClick, got %s", i, rec.Action)
		}
		if rec.Args["x"] != 5 || rec.Args["y"] != 6 {
			t.Errorf("record %d: bad coords: %+v", i, rec.Args)
		}
		if rec.Args["double"] != false {
			t.Errorf("record %d: expected double=false", i)
		}
	}
}

func TestTranslateAnthropic_HoldKeyUnsupported(t *testing.T) {
	withMock(t)
	if _, err := TranslateAnthropicAction("hold_key", map[string]any{"key": "shift"}); err == nil {
		t.Error("expected error for unsupported hold_key action")
	}
}
