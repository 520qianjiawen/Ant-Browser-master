package backend

import (
	"fmt"
	"math"
	"time"
)

// Apply native visible-frame bounds even when Chromium restores a previously
// maximized/fullscreen placement from its profile. Does not spoof screen APIs.
func applySunBrowserAutoWindowRuntime(port int, bounds sunBrowserWindowBounds) error {
	return applySunBrowserAutoWindowWithCall(bounds, func(method string, params map[string]any) (map[string]any, error) {
		return cdpBrowserCallResult(port, method, params)
	})
}

func applySunBrowserAutoWindowWithCall(bounds sunBrowserWindowBounds, call func(string, map[string]any) (map[string]any, error)) error {
	if err := bounds.validate(); err != nil {
		return err
	}
	targets, err := call("Target.getTargets", nil)
	if err != nil {
		return err
	}
	infos, _ := targets["targetInfos"].([]any)
	seen := map[float64]bool{}
	for _, raw := range infos {
		target, ok := raw.(map[string]any)
		if !ok || target["type"] != "page" {
			continue
		}
		id, _ := target["targetId"].(string)
		window, err := call("Browser.getWindowForTarget", map[string]any{"targetId": id})
		if err != nil {
			continue
		}
		windowID, ok := window["windowId"].(float64)
		if !ok || seen[windowID] {
			continue
		}
		seen[windowID] = true
		if _, err := call("Browser.setWindowBounds", map[string]any{"windowId": windowID, "bounds": map[string]any{"windowState": "normal"}}); err != nil {
			return err
		}
		if _, err := call("Browser.setWindowBounds", map[string]any{"windowId": windowID, "bounds": bounds}); err != nil {
			return err
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			result, err := call("Browser.getWindowBounds", map[string]any{"windowId": windowID})
			if err != nil {
				return fmt.Errorf("无法回读浏览器窗口尺寸: %w", err)
			}
			actual, _ := result["bounds"].(map[string]any)
			if sunBrowserWindowBoundsMatch(actual, bounds) {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("浏览器窗口未达到目标尺寸 %dx%d，实际边界: %v", bounds.Width, bounds.Height, actual)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("尚未找到可设置的浏览器窗口")
	}
	return nil
}

func sunBrowserWindowBoundsMatch(actual map[string]any, expected sunBrowserWindowBounds) bool {
	for key, want := range map[string]int{"left": expected.Left, "top": expected.Top, "width": expected.Width, "height": expected.Height} {
		value, ok := actual[key].(float64)
		if !ok || math.Abs(value-float64(want)) > 2 {
			return false
		}
	}
	return true
}
