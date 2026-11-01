package backend

import (
	"ant-chrome/backend/internal/browser"
	"strings"
)

// isSunBrowserCore resolves the selected/default executable rather than guessing
// from the core display name or the reduced Chromium User-Agent.
func (a *App) isSunBrowserCore(coreID string) bool {
	if a == nil || a.browserMgr == nil || a.browserMgr.Config == nil {
		return false
	}
	core, ok := a.browserMgr.GetCore(coreID)
	if !ok {
		core, ok = a.browserMgr.GetDefaultCore()
	}
	if !ok {
		return false
	}
	exe, _, ok := browser.FindCoreExecutable(a.browserMgr.ResolveRelativePath(core.CorePath))
	return ok && browser.IsSunBrowserExecutable(exe)
}

func (a *App) filterFingerprintExpectedArgs(coreID string, args []string) []string {
	if a.isSunBrowserCore(coreID) {
		return browser.FilterSunBrowserFingerprintArgs(args)
	}
	return args
}

func sunBrowserUnsupportedFields() []string {
	return []string{"seed", "disableSpoofing", "hardwareConcurrency", "deviceMemory", "colorDepth", "touchPoints", "brand", "brandVersion", "platform", "platformVersion", "doNotTrack", "mediaDevices", "canvasNoise", "audioNoise", "clientRectsNoise", "fontList", "webglVendor", "webglRenderer"}
}

func sunBrowserFingerprintLaunchPlan(plan browserFingerprintLaunchPlan) browserFingerprintLaunchPlan {
	plan.launchArgs = browser.FilterSunBrowserFingerprintArgs(plan.launchArgs)
	// Discard fingerprint-chromium version assumptions for this different engine.
	plan.warnings = []string{"SunBrowser 当前为基础启动兼容：独立数据目录、代理及 CDP；尚未映射 AdsPower 专有指纹配置，Ant 指纹种子与噪声参数不传递"}
	for i := range plan.rows {
		row := &plan.rows[i]
		key := browserFingerprintArgKey(row.InputArg)
		if strings.HasPrefix(key, "--fingerprint") || key == "--disable-spoofing" {
			row.Status = "unsupported"
			row.RuntimeArg = ""
			row.Action = "运行时移除"
			row.Note = "该参数属于 fingerprint-chromium；SunBrowser 专有配置尚未适配"
		} else {
			row.Status = "unknown"
			row.Note = "仅保留 Chromium 通用参数；SunBrowser 实际效果需运行自测确认"
		}
	}
	return plan
}
