package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func sunBrowserAutomaticExpected(expected BrowserFingerprintExpectedInfo, state *sunBrowserAutoLocation) BrowserFingerprintExpectedInfo {
	if state == nil || state.State != "applied" {
		return expected
	}
	expected.Language, expected.AcceptLanguage, expected.Timezone = state.Language, state.AcceptLanguage, state.Timezone
	if state.Window != nil {
		expected.WindowSize = fmt.Sprintf("%d,%d", state.Window.Width, state.Window.Height)
	}
	return expected
}

func (a *App) sunBrowserAutoStatePath(profileID string) string {
	return a.resolveAppPath(filepath.ToSlash(filepath.Join("data", "fingerprint-check", safeFingerprintCheckProfilePath(profileID), "auto-environment.json")))
}

func (a *App) writeSunBrowserAutoState(profileID string, state sunBrowserAutoLocation) error {
	path := a.sunBrowserAutoStatePath(profileID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".auto-environment-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (a *App) readSunBrowserAutoState(profileID string) *sunBrowserAutoLocation {
	data, err := os.ReadFile(a.sunBrowserAutoStatePath(profileID))
	if err != nil {
		return nil
	}
	var state sunBrowserAutoLocation
	if json.Unmarshal(data, &state) != nil || state.Timezone == "" || state.Language == "" {
		return nil
	}
	return &state
}

// Upgrade an already-running managed instance without touching its user data.
func (a *App) ensureRunningSunBrowserAutomaticRuntime(profile *BrowserProfile) error {
	if profile == nil || !profile.DebugReady || !a.isSunBrowserCore(profile.CoreId) {
		return nil
	}
	state := a.readSunBrowserAutoState(profile.ProfileId)
	if state == nil || state.DebugPort != profile.DebugPort || state.IPSelection == "" {
		args := a.fingerprintCheckExpectedArgsFromLockedProfile(profile)
		proxy := browserArgValue(args, "--proxy-server")
		if proxy == "" {
			direct := false
			for _, arg := range args {
				if browserFingerprintArgKey(arg) == "--no-proxy-server" {
					direct = true
				}
			}
			if !direct && (profile.ProxyId != "" || profile.ProxyConfig != "") {
				return fmt.Errorf("读取当前实例实际代理失败；请重新启动该实例后应用自动位置")
			}
			proxy = "direct://"
		}
		location, err := resolveSunBrowserAutoLocation(proxy)
		if err != nil {
			return err
		}
		bounds, err := sunBrowserAutoWindowBounds()
		if err != nil {
			return err
		}
		location.Window = &bounds
		location.DebugPort = profile.DebugPort
		if err := a.writeSunBrowserAutoState(profile.ProfileId, location); err != nil {
			return err
		}
	}
	return a.applySunBrowserAutomaticRuntime(profile, profile.DebugPort)
}

func (a *App) applySunBrowserAutomaticRuntime(profile *BrowserProfile, debugPort int) error {
	if profile == nil || !a.isSunBrowserCore(profile.CoreId) {
		return nil
	}
	state := a.readSunBrowserAutoState(profile.ProfileId)
	if state == nil {
		return nil
	}
	if err := ensureSunBrowserAutoLocale(debugPort, sunBrowserAutoLocale{Language: state.Language, AcceptLanguage: state.AcceptLanguage, Timezone: state.Timezone}); err != nil {
		state.State = "failed"
		_ = a.writeSunBrowserAutoState(profile.ProfileId, *state)
		return fmt.Errorf("SunBrowser 自动时区语言设置失败: %w", err)
	}
	if state.Window != nil {
		if err := applySunBrowserAutoWindowRuntime(debugPort, *state.Window); err != nil {
			state.State = "failed"
			_ = a.writeSunBrowserAutoState(profile.ProfileId, *state)
			return fmt.Errorf("SunBrowser 自动窗口设置失败: %w", err)
		}
	}
	state.State = "applied"
	state.DebugPort = debugPort
	if err := a.writeSunBrowserAutoState(profile.ProfileId, *state); err != nil {
		return err
	}
	// Regenerate the embedded context before deferred navigation opens the file.
	if _, err := a.writeFingerprintCheckPageForExpectedArgsAndProfile(profile.ProfileId, profile.LastLaunchArgs, profile); err != nil {
		return err
	}
	return nil
}
