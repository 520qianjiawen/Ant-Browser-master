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

func TestSunCanvasLaunchArgIsStableAndBoundToVerifiedVersion(t *testing.T) {
	raw := []string{"--fingerprinting-canvas-image-data-noise"}
	a, ok := sunCanvasAddLaunchArg([]string{"--lang=en-US"}, raw, "profile-a", sunCanvasVerifiedVersion)
	if !ok || !sunCanvasWasRequested(a, "profile-a", sunCanvasVerifiedVersion) {
		t.Fatalf("missing native canvas request: %v", a)
	}
	data, err := sunCanvasEncoding.DecodeString(strings.TrimPrefix(a[len(a)-1], "--extended-parameters="))
	if err != nil {
		t.Fatal(err)
	}
	var native map[string]any
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if _, exists := native["UserId"]; exists {
		t.Fatal("native UserId must not be emitted in the browser toolbar")
	}
	again, _ := sunCanvasAddLaunchArg([]string{"--lang=en-US"}, raw, "profile-a", sunCanvasVerifiedVersion)
	b, _ := sunCanvasAddLaunchArg([]string{"--lang=en-US"}, raw, "profile-b", sunCanvasVerifiedVersion)
	if !reflect.DeepEqual(a, again) || reflect.DeepEqual(a, b) || sunCanvasWasRequested(a, "profile-b", sunCanvasVerifiedVersion) {
		t.Fatal("profile marks are not stable and distinct")
	}
	if !sunCanvasRequested([]string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=audio,font"}) {
		t.Fatal("unrelated exclusions must not disable canvas")
	}
	for _, tc := range []struct {
		args, raw   []string
		id, version string
	}{
		{nil, raw, "profile-a", "152.0.7977.55"},
		{nil, nil, "profile-a", sunCanvasVerifiedVersion},
		{nil, []string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing"}, "profile-a", sunCanvasVerifiedVersion},
		{nil, []string{"--fingerprinting-canvas-image-data-noise", "--disable-spoofing=canvas"}, "profile-a", sunCanvasVerifiedVersion},
		{[]string{"--extended-parameters=existing"}, raw, "profile-a", sunCanvasVerifiedVersion},
		{nil, raw, "", sunCanvasVerifiedVersion},
	} {
		got, changed := sunCanvasAddLaunchArg(tc.args, tc.raw, tc.id, tc.version)
		if changed || !reflect.DeepEqual(got, tc.args) {
			t.Fatalf("unexpected injection: %#v", tc)
		}
	}
}

func TestSunCanvasRealBrowser(t *testing.T) {
	exe := os.Getenv("ANT_TEST_SUNBROWSER")
	if exe == "" {
		t.Skip("set ANT_TEST_SUNBROWSER to run isolated native integration")
	}
	probe := func(id string) string {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		args := []string{"--protected-disable-safe-open", "--user-data-dir=" + t.TempDir(), fmt.Sprintf("--remote-debugging-port=%d", port), "--no-proxy-server", "--no-first-run", "--no-default-browser-check"}
		if id != "" {
			var ok bool
			args, ok = sunCanvasAddLaunchArg(args, []string{"--fingerprinting-canvas-image-data-noise"}, id, sunCanvasVerifiedVersion)
			if !ok {
				t.Fatal("missing native canvas flag")
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
		expression := `(()=>{const c=document.createElement('canvas');c.width=320;c.height=90;const x=c.getContext('2d');x.fillStyle='#f60';x.fillRect(1,1,200,60);x.font='16px Arial';x.fillStyle='#069';x.fillText('Ant sample 123',3,30);x.beginPath();x.arc(70,35,28,0,7);x.stroke();return c.toDataURL()})()`
		result, err = controller.call(attached.SessionID, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true})
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err = json.Unmarshal(result, &value); err != nil || value.Result.Value == "" {
			t.Fatalf("canvas read failed: %v", err)
		}
		return value.Result.Value
	}
	native, a1, a2, b := probe(""), probe("profile-a"), probe("profile-a"), probe("profile-b")
	if a1 != a2 || a1 == native || a1 == b {
		t.Fatal("canvas marks did not give stable, distinct actual outputs")
	}
}
