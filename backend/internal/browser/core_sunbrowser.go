package browser

import (
	"path/filepath"
	"strings"
)

// IsSunBrowserExecutable matches the main executable, never its Helper processes.
func IsSunBrowserExecutable(path string) bool {
	name := filepath.Base(strings.TrimSpace(path))
	return strings.EqualFold(name, "SunBrowser") || strings.EqualFold(name, "sunbrowser.exe")
}

// FilterSunBrowserFingerprintArgs omits fingerprint-chromium-specific switches.
// SunBrowser uses a different configuration protocol; forwarding these switches
// would misleadingly imply that Ant's fingerprint settings are supported.
func FilterSunBrowserFingerprintArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		key, _, hasValue := strings.Cut(arg, "=")
		key = strings.ToLower(key)
		if strings.HasPrefix(key, "--fingerprint") || key == "--disable-spoofing" {
			// Noise switches are booleans; other custom fingerprint switches may
			// receive a separate value. Never consume an option or a start URL.
			boolean := strings.HasPrefix(key, "--fingerprinting-")
			if !hasValue && !boolean && i+1 < len(args) {
				next := strings.TrimSpace(args[i+1])
				if next != "" && !strings.HasPrefix(next, "--") && !strings.Contains(next, ":") && !strings.ContainsAny(next, "/\\") {
					i++
				}
			}
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// AdaptCoreLaunchArgs enables the startup convention used by AdsPower's own
// launcher. It does not alter the executable or map proprietary fingerprint data.
func AdaptCoreLaunchArgs(executable string, args []string) []string {
	if !IsSunBrowserExecutable(executable) {
		return args
	}
	filtered := FilterSunBrowserFingerprintArgs(args)
	out := make([]string, 0, len(filtered)+1)
	out = append(out, "--protected-disable-safe-open")
	for _, arg := range filtered {
		key, _, _ := strings.Cut(strings.TrimSpace(arg), "=")
		if strings.EqualFold(key, "--protected-disable-safe-open") {
			continue
		}
		out = append(out, arg)
	}
	return out
}
