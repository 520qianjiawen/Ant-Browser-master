package backend

import (
	"errors"
	"strings"
	"testing"
)

func TestSunBrowserWindowRuntimeVerifiesReadback(t *testing.T) {
	want := sunBrowserWindowBounds{Left: 0, Top: 30, Width: 1920, Height: 986}
	reads, sets := 0, 0
	call := func(method string, params map[string]any) (map[string]any, error) {
		switch method {
		case "Target.getTargets":
			return map[string]any{"targetInfos": []any{map[string]any{"type": "page", "targetId": "a"}, map[string]any{"type": "page", "targetId": "b"}}}, nil
		case "Browser.getWindowForTarget":
			return map[string]any{"windowId": float64(9)}, nil
		case "Browser.setWindowBounds":
			sets++
			return nil, nil
		case "Browser.getWindowBounds":
			reads++
			width := float64(1672)
			if reads > 1 {
				width = 1920
			}
			return map[string]any{"bounds": map[string]any{"left": float64(0), "top": float64(30), "width": width, "height": float64(986)}}, nil
		}
		return nil, errors.New("unexpected method")
	}
	if err := applySunBrowserAutoWindowWithCall(want, call); err != nil {
		t.Fatal(err)
	}
	if sets != 2 || reads != 2 {
		t.Fatalf("sets=%d reads=%d", sets, reads)
	}
}

func TestSunBrowserWindowRuntimeDoesNotAcceptFailedReadback(t *testing.T) {
	call := func(method string, params map[string]any) (map[string]any, error) {
		switch method {
		case "Target.getTargets":
			return map[string]any{"targetInfos": []any{map[string]any{"type": "page", "targetId": "a"}}}, nil
		case "Browser.getWindowForTarget":
			return map[string]any{"windowId": float64(9)}, nil
		case "Browser.setWindowBounds":
			return nil, nil
		case "Browser.getWindowBounds":
			return nil, errors.New("read failed")
		}
		return nil, errors.New("unexpected method")
	}
	err := applySunBrowserAutoWindowWithCall(sunBrowserWindowBounds{Width: 1920, Height: 986}, call)
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected readback failure, got %v", err)
	}
	if sunBrowserWindowBoundsMatch(map[string]any{"width": float64(1920)}, sunBrowserWindowBounds{Width: 1920}) {
		t.Fatal("missing fields must not match")
	}
}
