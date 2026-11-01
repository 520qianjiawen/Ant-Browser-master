package backend

import "fmt"

// Apply native visible-frame bounds even when Chromium restores a previously
// maximized/fullscreen placement from its profile. Does not spoof screen APIs.
func applySunBrowserAutoWindowRuntime(port int, bounds sunBrowserWindowBounds) error {
	targets, err := cdpBrowserCallResult(port, "Target.getTargets", nil)
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
		window, err := cdpBrowserCallResult(port, "Browser.getWindowForTarget", map[string]any{"targetId": id})
		if err != nil {
			continue
		}
		windowID, ok := window["windowId"].(float64)
		if !ok || seen[windowID] {
			continue
		}
		seen[windowID] = true
		if _, err := cdpBrowserCallResult(port, "Browser.setWindowBounds", map[string]any{"windowId": windowID, "bounds": map[string]any{"windowState": "normal"}}); err != nil {
			return err
		}
		if _, err := cdpBrowserCallResult(port, "Browser.setWindowBounds", map[string]any{"windowId": windowID, "bounds": bounds}); err != nil {
			return err
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("尚未找到可设置的浏览器窗口")
	}
	return nil
}
