package backend

import (
	"encoding/json"
	"testing"
)

func TestSunBrowserAutomaticExpectedOnlyAfterApply(t *testing.T) {
	original := BrowserFingerprintExpectedInfo{Language: "zh-CN", Timezone: "America/New_York", WindowSize: "2560,1440"}
	state := &sunBrowserAutoLocation{Language: "en-US", AcceptLanguage: "en-US,en", Timezone: "America/Los_Angeles", State: "pending", Window: &sunBrowserWindowBounds{Width: 1920, Height: 986}}
	if got := sunBrowserAutomaticExpected(original, state); got != original {
		t.Fatalf("pending state became effective: %#v", got)
	}
	state.State = "failed"
	if got := sunBrowserAutomaticExpected(original, state); got != original {
		t.Fatalf("failed state became effective: %#v", got)
	}
	state.State = "applied"
	got := sunBrowserAutomaticExpected(original, state)
	if got.Language != "en-US" || got.Timezone != "America/Los_Angeles" || got.WindowSize != "1920,986" || got.Seed != "" || got.CanvasNoise != "" {
		t.Fatalf("wrong auto expected %#v", got)
	}
}

func TestSunBrowserAutoStateContextRoundTrip(t *testing.T) {
	app, profile := sunBrowserExpectedFixture(t)
	state := sunBrowserAutoLocation{IP: "67.230.176.144", CountryCode: "US", Language: "en-US", AcceptLanguage: "en-US,en", Timezone: "America/Los_Angeles", State: "applied", Window: &sunBrowserWindowBounds{Width: 1920, Height: 986}, FingerprintPolicy: "native-unmapped"}
	if err := app.writeSunBrowserAutoState(profile.ProfileId, state); err != nil {
		t.Fatal(err)
	}
	data, err := app.buildFingerprintCheckPageContextForExpectedArgsAndProfile(profile.ProfileId, []string{"--lang=zh-CN", "--timezone=America/New_York", "--fingerprint=12"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	var got fingerprintCheckPageContext
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.AutoEnvironment == nil || got.AutoEnvironment.FingerprintPolicy != "native-unmapped" || got.Expected.Language != "en-US" || got.Expected.Seed != "" {
		t.Fatalf("bad context %s", data)
	}
}
