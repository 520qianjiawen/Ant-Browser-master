package backend

import (
	"reflect"
	"testing"
)

func TestSunBrowserFingerprintLaunchPlan(t *testing.T) {
	original := buildBrowserFingerprintLaunchPlan("sun-test", []string{
		"--fingerprint=123", "--fingerprinting-canvas-image-data-noise", "--lang=en-US", "--window-size=1200,900",
	}, "152.0.7977.54")
	plan := sunBrowserFingerprintLaunchPlan(original)
	if len(plan.warnings) == 0 {
		t.Fatal("missing compatibility warning")
	}
	for _, arg := range plan.launchArgs {
		if browserFingerprintArgKey(arg) == "--fingerprint" || browserFingerprintArgKey(arg) == "--fingerprinting-canvas-image-data-noise" {
			t.Fatalf("custom switch leaked: %q", arg)
		}
	}
	for _, row := range plan.rows {
		if row.InputArg == "--fingerprinting-canvas-image-data-noise" && (row.Status != "unsupported" || row.RuntimeArg != "") {
			t.Fatalf("misleading fingerprint row: %#v", row)
		}
	}
	if !reflect.DeepEqual(plan.launchArgs, []string{"--lang=en-US", "--window-size=1200,900", "--accept-lang=en-US,en"}) {
		t.Fatalf("unexpected compatible args: %#v", plan.launchArgs)
	}
}
