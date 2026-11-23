package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func decodeSunExtendedPayloadForTest(t *testing.T, args []string) map[string]any {
	t.Helper()
	var encoded string
	count := 0
	for _, arg := range args {
		if strings.HasPrefix(arg, "--extended-parameters=") {
			encoded = strings.TrimPrefix(arg, "--extended-parameters=")
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 --extended-parameters flag, got %d in %v", count, args)
	}
	data, err := sunCanvasEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var native map[string]any
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	return native
}

func TestSunExtendedLaunchArgIsStableAndBoundToVerifiedVersion(t *testing.T) {
	raw := []string{"--fingerprinting-canvas-image-data-noise"}
	a, ok := sunExtendedAddLaunchArg([]string{"--lang=en-US"}, raw, "profile-a", sunCanvasVerifiedVersion)
	if !ok {
		t.Fatalf("expected extended parameters to be injected: %v", a)
	}
	state := sunExtendedWasRequested(a, "profile-a", sunCanvasVerifiedVersion)
	if !state.Canvas || !state.Audio || !sunCanvasWasRequested(a, "profile-a", sunCanvasVerifiedVersion) || !sunAudioWasRequested(a, "profile-a", sunCanvasVerifiedVersion) {
		t.Fatalf("expected both canvas and audio native requests, got %+v (%v)", state, a)
	}
	native := decodeSunExtendedPayloadForTest(t, a)
	if _, exists := native["UserId"]; exists {
		t.Fatal("native UserId must not be emitted in the browser toolbar")
	}
	audioVal, okAudio := native["AudioFp"].(float64)
	if native["CanvasMark"] == "" || !okAudio || audioVal <= 0 {
		t.Fatalf("expected non-empty CanvasMark and positive numeric AudioFp in payload, got %#v", native)
	}

	again, _ := sunExtendedAddLaunchArg([]string{"--lang=en-US"}, raw, "profile-a", sunCanvasVerifiedVersion)
	b, _ := sunExtendedAddLaunchArg([]string{"--lang=en-US"}, raw, "profile-b", sunCanvasVerifiedVersion)
	if !reflect.DeepEqual(a, again) || reflect.DeepEqual(a, b) {
		t.Fatal("profile marks are not stable and distinct")
	}
	if sunCanvasWasRequested(a, "profile-b", sunCanvasVerifiedVersion) || sunAudioWasRequested(a, "profile-b", sunCanvasVerifiedVersion) {
		t.Fatal("profile-a marks must not match profile-b")
	}
	if !sunCanvasRequested([]string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=audio,font"}) {
		t.Fatal("unrelated exclusions must not disable canvas")
	}
	if !sunAudioRequested([]string{"--disable-spoofing=canvas,font"}) {
		t.Fatal("unrelated exclusions must not disable audio")
	}

	for _, tc := range []struct {
		args, raw   []string
		id, version string
	}{
		{nil, raw, "profile-a", "152.0.7977.55"},
		{nil, []string{"--disable-spoofing=audio"}, "profile-a", sunCanvasVerifiedVersion},
		{nil, []string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing"}, "profile-a", sunCanvasVerifiedVersion},
		{nil, []string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=all"}, "profile-a", sunCanvasVerifiedVersion},
		{nil, []string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=canvas,audio"}, "profile-a", sunCanvasVerifiedVersion},
		{nil, []string{"--fingerprint-audio-noise=false"}, "profile-a", sunCanvasVerifiedVersion},
		{[]string{"--extended-parameters=existing"}, raw, "profile-a", sunCanvasVerifiedVersion},
		{nil, raw, "", sunCanvasVerifiedVersion},
	} {
		got, changed := sunExtendedAddLaunchArg(tc.args, tc.raw, tc.id, tc.version)
		if changed || !reflect.DeepEqual(got, tc.args) {
			t.Fatalf("unexpected injection: %#v -> %v", tc, got)
		}
	}
}

func TestSunAudioDefaultEnabledAndIndependentExclusion(t *testing.T) {
	// Default enabled when no --disable-spoofing=audio is configured, even without canvas noise.
	audioOnly, ok := sunExtendedAddLaunchArg([]string{"--lang=en-US"}, nil, "profile-a", sunCanvasVerifiedVersion)
	if !ok {
		t.Fatal("expected AudioFp to be enabled by default")
	}
	audioState := sunExtendedWasRequested(audioOnly, "profile-a", sunCanvasVerifiedVersion)
	if audioState.Canvas || !audioState.Audio {
		t.Fatalf("expected audio-only state, got %+v", audioState)
	}
	payload := decodeSunExtendedPayloadForTest(t, audioOnly)
	if _, hasCanvas := payload["CanvasMark"]; hasCanvas {
		t.Fatalf("unexpected CanvasMark in audio-only payload: %#v", payload)
	}
	if payload["AudioFp"] != float64(sunAudioFpForProfile("profile-a")) {
		t.Fatalf("unexpected AudioFp: %#v", payload)
	}

	// --disable-spoofing=audio disables AudioFp while keeping CanvasMark.
	canvasOnly, ok := sunExtendedAddLaunchArg(
		[]string{"--lang=en-US"},
		[]string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=audio"},
		"profile-a",
		sunCanvasVerifiedVersion,
	)
	if !ok {
		t.Fatal("expected CanvasMark to remain enabled when only audio is disabled")
	}
	canvasState := sunExtendedWasRequested(canvasOnly, "profile-a", sunCanvasVerifiedVersion)
	if !canvasState.Canvas || canvasState.Audio {
		t.Fatalf("expected canvas-only state, got %+v", canvasState)
	}
	payload = decodeSunExtendedPayloadForTest(t, canvasOnly)
	if _, hasAudio := payload["AudioFp"]; hasAudio {
		t.Fatalf("unexpected AudioFp when audio spoofing is disabled: %#v", payload)
	}

	// --disable-spoofing=canvas disables CanvasMark while keeping AudioFp.
	canvasExcluded, ok := sunExtendedAddLaunchArg(
		[]string{"--lang=en-US"},
		[]string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=canvas"},
		"profile-a",
		sunCanvasVerifiedVersion,
	)
	if !ok {
		t.Fatal("expected AudioFp to remain enabled when only canvas is disabled")
	}
	excludedState := sunExtendedWasRequested(canvasExcluded, "profile-a", sunCanvasVerifiedVersion)
	if excludedState.Canvas || !excludedState.Audio {
		t.Fatalf("expected audio-only state when canvas is excluded, got %+v", excludedState)
	}
}

func runSunRealBrowserEval(t *testing.T, exe string, rawArgs []string, profileID string, expression string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	args := []string{"--protected-disable-safe-open", "--user-data-dir=" + t.TempDir(), fmt.Sprintf("--remote-debugging-port=%d", port), "--no-proxy-server", "--no-first-run", "--no-default-browser-check"}
	if profileID != "" {
		var ok bool
		args, ok = sunExtendedAddLaunchArg(args, rawArgs, profileID, sunCanvasVerifiedVersion)
		if !ok {
			t.Fatal("missing native extended-parameters flag")
		}
	}
	args = append(args, "about:blank")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	cmd := exec.CommandContext(ctx, exe, args...)
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	var body []byte
	for {
		body, err = cdpGetEndpointBody(port, "/json/version")
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	var v cdpBrowserVersion
	if err = json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	conn, err := cdpDialWebSocket(v.WebSocketDebuggerUrl)
	if err != nil {
		t.Fatal(err)
	}
	controller := &sunAutoController{conn: conn, pending: make(map[int]chan sunAutoReply), sessions: make(map[string]*sunAutoSession), targets: make(map[string]*sunAutoSession), done: make(chan struct{})}
	go controller.readLoop()
	defer controller.close()
	result, err := controller.call("", "Target.getTargets", nil)
	if err != nil {
		t.Fatal(err)
	}
	var targets struct {
		Infos []struct {
			ID   string `json:"targetId"`
			Type string `json:"type"`
		} `json:"targetInfos"`
	}
	if err = json.Unmarshal(result, &targets); err != nil {
		t.Fatal(err)
	}
	var targetID string
	for _, target := range targets.Infos {
		if target.Type == "page" {
			targetID = target.ID
			break
		}
	}
	if targetID == "" {
		t.Fatal("missing page")
	}
	result, err = controller.call("", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		t.Fatal(err)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err = json.Unmarshal(result, &attached); err != nil || attached.SessionID == "" {
		t.Fatalf("attach: %s %v", result, err)
	}
	result, err = controller.call(attached.SessionID, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true})
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err = json.Unmarshal(result, &value); err != nil || value.Result.Value == "" {
		t.Fatalf("eval read failed: %v", err)
	}
	return value.Result.Value
}

func TestSunCanvasRealBrowser(t *testing.T) {
	exe := os.Getenv("ANT_TEST_SUNBROWSER")
	if exe == "" {
		t.Skip("set ANT_TEST_SUNBROWSER to run isolated native integration")
	}
	expression := `(()=>{const c=document.createElement('canvas');c.width=320;c.height=90;const x=c.getContext('2d');x.fillStyle='#f60';x.fillRect(1,1,200,60);x.font='16px Arial';x.fillStyle='#069';x.fillText('Ant sample 123',3,30);x.beginPath();x.arc(70,35,28,0,7);x.stroke();return c.toDataURL()})()`
	raw := []string{"--fingerprinting-canvas-image-data-noise"}
	native := runSunRealBrowserEval(t, exe, raw, "", expression)
	a1 := runSunRealBrowserEval(t, exe, raw, "profile-a", expression)
	a2 := runSunRealBrowserEval(t, exe, raw, "profile-a", expression)
	b := runSunRealBrowserEval(t, exe, raw, "profile-b", expression)
	if a1 != a2 || a1 == native || a1 == b {
		t.Fatal("canvas marks did not give stable, distinct actual outputs")
	}
}

func TestSunAudioRealBrowser(t *testing.T) {
	exe := os.Getenv("ANT_TEST_SUNBROWSER")
	if exe == "" {
		t.Skip("set ANT_TEST_SUNBROWSER to run isolated native integration")
	}
	expression := `(async()=>{const Ctor=window.OfflineAudioContext||window.webkitOfflineAudioContext;const ctx=new Ctor(1,44100,44100);const osc=ctx.createOscillator();const comp=ctx.createDynamicsCompressor();osc.type='triangle';osc.frequency.value=10000;comp.threshold.value=-50;comp.knee.value=40;comp.ratio.value=12;comp.attack.value=0;comp.release.value=0.25;osc.connect(comp);comp.connect(ctx.destination);osc.start(0);const buf=await ctx.startRendering();const data=buf.getChannelData(0).slice(4500,5000);let h=2166136261;const s=Array.from(data,v=>v.toFixed(6)).join(',');for(let i=0;i<s.length;i++){h^=s.charCodeAt(i);h+=(h<<1)+(h<<4)+(h<<7)+(h<<8)+(h<<24);}return ('00000000'+(h>>>0).toString(16)).slice(-8)})()`
	native := runSunRealBrowserEval(t, exe, nil, "", expression)
	a1 := runSunRealBrowserEval(t, exe, nil, "profile-a", expression)
	a2 := runSunRealBrowserEval(t, exe, nil, "profile-a", expression)
	b := runSunRealBrowserEval(t, exe, nil, "profile-b", expression)
	t.Logf("seeds: a=%d b=%d | hashes: native=%s a1=%s a2=%s b=%s", sunAudioFpForProfile("profile-a"), sunAudioFpForProfile("profile-b"), native, a1, a2, b)
	if a1 != a2 || a1 == native || a1 == b {
		t.Fatalf("audio marks did not give stable, distinct actual outputs (native=%s a1=%s a2=%s b=%s)", native, a1, a2, b)
	}
}
