package browser

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestIsSunBrowserExecutable(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "macOS executable", path: filepath.Join("cores", "SunBrowser.app", "Contents", "MacOS", "SunBrowser"), want: true},
		{name: "Windows executable", path: filepath.Join("cores", "sunbrowser.exe"), want: true},
		{name: "case insensitive", path: filepath.Join("cores", "SUNBROWSER.EXE"), want: true},
		{name: "mixed case basename", path: "sUnBrOwSeR", want: true},
		{name: "helper", path: filepath.Join("SunBrowser.app", "Contents", "MacOS", "SunBrowser Helper")},
		{name: "renderer helper", path: "SunBrowser Helper (Renderer)"},
		{name: "Windows helper", path: "sunbrowser_helper.exe"},
		{name: "Chromium", path: filepath.Join("SunBrowser.app", "Contents", "MacOS", "Chromium")},
		{name: "Chrome", path: "chrome.exe"},
		{name: "suffix is not exact", path: "NotSunBrowser"},
		{name: "extension is not exact", path: "SunBrowser.exe.bak"},
		{name: "bundle is not executable", path: "SunBrowser.app"},
		{name: "empty", path: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSunBrowserExecutable(tt.path); got != tt.want {
				t.Errorf("IsSunBrowserExecutable(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestAdaptCoreLaunchArgsSunBrowser(t *testing.T) {
	const safeOpen = "--protected-disable-safe-open"
	tests := []struct {
		name string
		args []string
		want []string // Arguments other than the single safe-open switch, in order.
	}{
		{name: "empty"},
		{
			name: "preserves ordinary launch flags and URLs",
			args: []string{"--proxy-server=socks5://127.0.0.1:1080", "--user-data-dir", "/tmp/profile with spaces", "--remote-debugging-port=9222", "--remote-debugging-address=127.0.0.1", "https://example.test/start", "about:blank"},
			want: []string{"--proxy-server=socks5://127.0.0.1:1080", "--user-data-dir", "/tmp/profile with spaces", "--remote-debugging-port=9222", "--remote-debugging-address=127.0.0.1", "https://example.test/start", "about:blank"},
		},
		{
			name: "filters inline fingerprint switches",
			args: []string{"--fingerprint=123", "--fingerprint-brand=Chrome", "--fingerprint-platform=windows", "--fingerprint-webgl-vendor=Intel", "--fingerprinting-canvas-image-data-noise", "--fingerprinting-client-rects-noise", "--disable-spoofing", "--remote-debugging-pipe", "https://example.test/"},
			want: []string{"--remote-debugging-pipe", "https://example.test/"},
		},
		{
			name: "filters split fingerprint values",
			args: []string{"--fingerprint", "123", "--fingerprint-brand", "Chrome", "--fingerprint-platform", "windows", "--fingerprint-platform-version", "10.0", "--fingerprint-hardware-concurrency", "8", "--disable-spoofing", "true", "--user-data-dir=/tmp/profile", "https://example.test/"},
			want: []string{"--user-data-dir=/tmp/profile", "https://example.test/"},
		},
		{
			name: "boolean noise switches preserve following URL",
			args: []string{"--fingerprinting-canvas-image-data-noise", "https://example.test/", "--fingerprinting-client-rects-noise", "about:blank"},
			want: []string{"https://example.test/", "about:blank"},
		},
		{
			name: "missing split value preserves next option and URL",
			args: []string{"--fingerprint", "--proxy-server=http://127.0.0.1:8080", "--fingerprint-brand", "https://example.test/", "--disable-spoofing", "about:blank", "--fingerprint-platform"},
			want: []string{"--proxy-server=http://127.0.0.1:8080", "https://example.test/", "about:blank"},
		},
		{
			name: "deduplicates existing safe-open switches even after URL",
			args: []string{"--remote-debugging-port=9222", safeOpen, "https://example.test/", safeOpen, "about:blank", safeOpen},
			want: []string{"--remote-debugging-port=9222", "https://example.test/", "about:blank"},
		},
		{
			name: "flag-like text in URL is not filtered",
			args: []string{"https://example.test/?arg=--fingerprint=123&mode=--disable-spoofing"},
			want: []string{"https://example.test/?arg=--fingerprint=123&mode=--disable-spoofing"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.args...)
			got := AdaptCoreLaunchArgs(filepath.Join("cores", "SunBrowser"), tt.args)
			var ordinary []string
			safeOpenIndex := -1
			for i, arg := range got {
				if arg == safeOpen {
					if safeOpenIndex >= 0 {
						t.Fatalf("safe-open switch appears more than once: %#v", got)
					}
					safeOpenIndex = i
					continue
				}
				ordinary = append(ordinary, arg)
			}
			if safeOpenIndex < 0 {
				t.Fatalf("missing safe-open switch: %#v", got)
			}
			if !reflect.DeepEqual(ordinary, tt.want) {
				t.Errorf("remaining args = %#v, want %#v", ordinary, tt.want)
			}
			for i, arg := range got {
				if strings.Contains(arg, ":") && !strings.HasPrefix(arg, "--") {
					if safeOpenIndex >= i {
						t.Errorf("safe-open switch must precede first URL: %#v", got)
					}
					break
				}
			}
			if !reflect.DeepEqual(tt.args, original) {
				t.Errorf("input args were mutated: got %#v, original %#v", tt.args, original)
			}
			if twice := AdaptCoreLaunchArgs("sunbrowser.exe", got); !reflect.DeepEqual(twice, got) {
				t.Errorf("adaptation is not idempotent: first %#v, second %#v", got, twice)
			}
		})
	}
}

func TestAdaptCoreLaunchArgsOtherEnginesUnchanged(t *testing.T) {
	for _, executable := range []string{"chrome", "chrome.exe", "Chromium", "SunBrowser Helper", ""} {
		t.Run(executable, func(t *testing.T) {
			args := []string{"--fingerprint", "123", "--fingerprinting-canvas-image-data-noise", "--disable-spoofing", "--proxy-server=http://127.0.0.1:8080", "https://example.test/"}
			want := append([]string(nil), args...)
			if got := AdaptCoreLaunchArgs(executable, args); !reflect.DeepEqual(got, want) {
				t.Errorf("AdaptCoreLaunchArgs(%q) = %#v, want unchanged %#v", executable, got, want)
			}
			if got := AdaptCoreLaunchArgs(executable, nil); got != nil {
				t.Errorf("nil args changed to %#v", got)
			}
		})
	}
}

func TestFindCoreExecutableSunBrowserApp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("SunBrowser.app discovery uses macOS executable candidates")
	}
	root := t.TempDir()
	bundle := filepath.Join(root, "SunBrowser.app")
	macOS := filepath.Join(bundle, "Contents", "MacOS")
	executable := filepath.Join(macOS, "SunBrowser")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		t.Fatal(err)
	}
	// An inert file is sufficient: discovery must not launch a real browser.
	if err := os.WriteFile(executable, []byte("not a browser executable\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const version = "152.0.7977.54"
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleName</key><string>SunBrowser</string>
<key>CFBundleVersion</key><string>999</string>
<key>CFBundleShortVersionString</key><string>152.0.7977.54</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "core directory", path: root},
		{name: "app bundle", path: bundle},
		{name: "MacOS directory", path: macOS},
		{name: "direct executable", path: executable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, candidate, ok := FindCoreExecutable(tt.path)
			if !ok {
				t.Fatalf("FindCoreExecutable(%q) did not find SunBrowser", tt.path)
			}
			if got != executable {
				t.Errorf("executable = %q, want %q", got, executable)
			}
			if want := "SunBrowser.app/Contents/MacOS/SunBrowser"; candidate != want {
				t.Errorf("candidate = %q, want %q", candidate, want)
			}
			if tt.path != macOS {
				if got := coreAppBundleVersion(tt.path); got != version {
					t.Errorf("coreAppBundleVersion(%q) = %q, want %q", tt.path, got, version)
				}
			}
		})
	}
}
