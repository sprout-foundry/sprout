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
		"scroll_direction": "down", "amount": float64(3),
	}); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if m.Records[0].Action != "Scroll" || m.Records[0].Args["dir"] != ScrollDown {
		t.Errorf("scroll not recorded: %+v", m.Records[0])
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
