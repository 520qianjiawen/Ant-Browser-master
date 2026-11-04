package backend

import (
	"ant-chrome/backend/internal/browser"
	"ant-chrome/backend/internal/config"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func sunBrowserExpectedFixture(t *testing.T) (*App, *BrowserProfile) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("SunBrowser bundle integration is macOS-only")
	}
	app := NewApp(t.TempDir())
	app.browserMgr = browser.NewManager(&config.Config{}, app.appRoot)
	bundle := filepath.Join(t.TempDir(), "SunBrowser.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", "SunBrowser")
	if err := os.MkdirAll(filepath.Dir(exe), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("inert test fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	app.browserMgr.Config.Browser.Cores = []browser.Core{{CoreId: "sun", CoreName: "Test core", CorePath: bundle, IsDefault: true}}
	profile := &BrowserProfile{ProfileId: "sun-expected", CoreId: "sun", FingerprintArgs: []string{"--fingerprint=123", "--fingerprinting-canvas-image-data-noise", "--lang=en-US", "--timezone=America/Los_Angeles"}, LaunchArgs: []string{"--fingerprint=456", "--fingerprint-platform-version=15.2.0"}}
	return app, profile
}

func TestSunBrowserExpectedArgsFilterEverySource(t *testing.T) {
	app, profile := sunBrowserExpectedFixture(t)
	for _, running := range []bool{false, true} {
		profile.Running = running
		profile.LastLaunchArgs = []string{"--fingerprint=789", "--fingerprinting-client-rects-noise", "--lang=en-US", "--timezone=America/Los_Angeles"}
		original := append([]string(nil), profile.LastLaunchArgs...)
		for _, args := range [][]string{app.fingerprintCheckExpectedArgsFromProfile(profile), app.fingerprintCheckExpectedArgsFromLockedProfile(profile)} {
			expected := buildBrowserFingerprintExpected(args)
			if expected.Seed != "" || expected.CanvasNoise != "" || expected.ClientRectsNoise != "" || expected.PlatformVersion != "" {
				t.Fatalf("unsupported expectation leaked: %#v", expected)
			}
			if expected.Language != "en-US" || expected.Timezone != "America/Los_Angeles" {
				t.Fatalf("genuine requested values discarded: %#v", expected)
			}
		}
		if !reflect.DeepEqual(profile.LastLaunchArgs, original) {
			t.Fatal("runtime launch history was modified")
		}
	}
}

func TestSunBrowserPageContextFiltersStaleExplicitArgs(t *testing.T) {
	app, profile := sunBrowserExpectedFixture(t)
	data, err := app.buildFingerprintCheckPageContextForExpectedArgsAndProfile(profile.ProfileId, []string{"--fingerprint=123", "--fingerprint-hardware-concurrency=10", "--fingerprinting-canvas-image-data-noise", "--lang=en-US", "--timezone=America/Los_Angeles", "--window-size=1920,1080"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	var ctx fingerprintCheckPageContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.Engine != "sunbrowser" || len(ctx.UnsupportedFields) == 0 {
		t.Fatalf("engine capability metadata missing: %s", data)
	}
	if ctx.Expected.Seed != "" || ctx.Expected.HardwareConcurrency != "" || ctx.Expected.CanvasNoise != "" {
		t.Fatalf("stale config leaked: %#v", ctx.Expected)
	}
	if ctx.Expected.Language != "en-US" || ctx.Expected.Timezone != "America/Los_Angeles" || ctx.Expected.WindowSize != "1920,1080" {
		t.Fatalf("real mismatches would be hidden: %#v", ctx.Expected)
	}
}

func TestSunBrowserPageContextShowsNativeCanvasRequest(t *testing.T) {
	app, profile := sunBrowserExpectedFixture(t)
	core, _ := app.browserMgr.GetCore(profile.CoreId)
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleShortVersionString</key><string>152.0.7977.54</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(core.CorePath, "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
		t.Fatal(err)
	}
	profile.Running = true
	args, ok := sunCanvasAddLaunchArg([]string{"--user-data-dir=/tmp/test", "--remote-debugging-port=9222"}, profile.FingerprintArgs, profile.ProfileId, sunCanvasVerifiedVersion)
	if !ok {
		t.Fatal("canvas request not produced")
	}
	data, err := app.buildFingerprintCheckPageContextForExpectedArgsAndProfile(profile.ProfileId, args, profile)
	if err != nil {
		t.Fatal(err)
	}
	var ctx fingerprintCheckPageContext
	if err = json.Unmarshal(data, &ctx); err != nil {
		t.Fatal(err)
	}
	if !ctx.CanvasNative {
		t.Fatalf("missing native canvas request: %s", data)
	}
	found := false
	for _, field := range ctx.UnsupportedFields {
		if field == "canvasNoise" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmapped ImageData coverage should remain explicit: %s", data)
	}
}
