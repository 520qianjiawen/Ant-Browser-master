package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ant-chrome/backend/internal/browser"
	"github.com/gorilla/websocket"
)

type sunMockCommand struct {
	ID      int            `json:"id"`
	Session string         `json:"sessionId"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type sunMockBrowser struct {
	t                  *testing.T
	server             *httptest.Server
	port               int
	mu                 sync.Mutex
	writeMu            sync.Mutex
	conn               *websocket.Conn
	commands           []sunMockCommand
	notify             chan sunMockCommand
	connected          chan struct{}
	connectOnce        sync.Once
	fail               string
	probeURL           string
	emptyMetadata      bool
	missingMetadata    bool
	noMetadata         bool
	staleProbe         bool
	disappearingAttach bool
	closeDuringApply   bool
	closedDuringApply  bool
	liveGoneError      bool
	getTargetsCount    int
}

func newSunMockBrowser(t *testing.T, fail string) *sunMockBrowser {
	t.Helper()
	m := &sunMockBrowser{t: t, fail: fail, notify: make(chan sunMockCommand, 200), connected: make(chan struct{})}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_ = json.NewEncoder(w).Encode(map[string]any{"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(m.server.URL, "http") + "/browser"})
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conn = conn
		m.mu.Unlock()
		m.connectOnce.Do(func() { close(m.connected) })
		defer conn.Close()
		for {
			var command sunMockCommand
			if conn.ReadJSON(&command) != nil {
				return
			}
			m.mu.Lock()
			m.commands = append(m.commands, command)
			m.mu.Unlock()
			m.notify <- command
			result := map[string]any{}
			switch command.Method {
			case "Target.createTarget":
				result["targetId"] = "probe"
			case "Target.attachToTarget":
				if command.Params["targetId"] != "gone" {
					m.attach("probe-session", "page", "probe")
					result["sessionId"] = "probe-session"
				}
			case "Page.navigate":
				m.probeURL = command.Params["url"].(string) + "/"
			case "Target.setAutoAttach":
				if command.Session == "" {
					if m.staleProbe {
						m.attach("late-probe", "page", "probe")
					}
					m.attach("initial", "page", "p0")
				}
			case "Target.getTargets":
				infos := []any{}
				if !m.closedDuringApply || m.getTargetsCount == 0 {
					infos = append(infos, map[string]any{"targetId": "p0", "type": "page"})
				}
				if m.staleProbe {
					infos = append(infos, map[string]any{"targetId": "probe", "type": "page"})
				}
				if m.disappearingAttach {
					infos = append(infos, map[string]any{"targetId": "gone", "type": "page"})
				}
				result["targetInfos"] = infos
				m.getTargetsCount++
			case "Runtime.evaluate":
				result["result"] = map[string]any{"value": map[string]any{"userAgent": "Native UA/123", "metadata": map[string]any{"brands": []any{map[string]any{"brand": "Native", "version": "123"}}, "fullVersionList": []any{map[string]any{"brand": "Native", "version": "123.4.5.6"}}, "platform": "NativeOS", "platformVersion": "1.2", "architecture": "arm", "bitness": "64", "model": "", "mobile": false, "wow64": false}}}
				value := result["result"].(map[string]any)["value"].(map[string]any)
				value["url"] = m.probeURL
				if m.noMetadata || (m.missingMetadata && command.Session != "probe-session") {
					delete(value, "metadata")
				}
				if m.emptyMetadata && command.Session != "probe-session" {
					value["metadata"] = map[string]any{"brands": []any{}, "platform": ""}
				}
			}
			reply := map[string]any{"id": command.ID, "result": result, "sessionId": command.Session}
			if command.Method == m.fail || command.Session+":"+command.Method == m.fail {
				delete(reply, "result")
				reply["error"] = map[string]any{"message": "unsupported test command"}
			}

			if command.Method == "Target.attachToTarget" && command.Params["targetId"] == "gone" {
				delete(reply, "result")
				reply["error"] = map[string]any{"message": "No target with given id found"}
			}
			if command.Session == "initial" && command.Method == "Emulation.setTimezoneOverride" && (m.closeDuringApply || m.liveGoneError) {
				if m.closeDuringApply {
					m.closedDuringApply = true
					_ = m.send(map[string]any{"method": "Target.detachedFromTarget", "params": map[string]any{"sessionId": "initial"}})
				}
				delete(reply, "result")
				reply["error"] = map[string]any{"message": "No session with given id"}
			}
			if m.send(reply) != nil {
				return
			}
		}
	}))
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(m.server.URL, "http://"))
	m.port, _ = strconv.Atoi(port)
	t.Cleanup(func() { stopSunBrowserAutoLocale(m.port); m.server.Close() })
	return m
}

func (m *sunMockBrowser) send(value any) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("not connected")
	}
	return conn.WriteJSON(value)
}
func (m *sunMockBrowser) attach(session, kind, target string) {
	_ = m.send(map[string]any{"method": "Target.attachedToTarget", "params": map[string]any{"sessionId": session, "waitingForDebugger": true, "targetInfo": map[string]any{"targetId": target, "type": kind}}})
}
func (m *sunMockBrowser) waitResume(t *testing.T, session string) {
	t.Helper()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		for _, cmd := range m.snapshot() {
			if cmd.Session == session && cmd.Method == "Runtime.runIfWaitingForDebugger" {
				return
			}
		}
		select {
		case <-m.notify:
		case <-timer.C:
			t.Fatalf("session %s never resumed", session)
		}
	}
}
func (m *sunMockBrowser) snapshot() []sunMockCommand {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sunMockCommand(nil), m.commands...)
}

func TestSunBrowserAutoLocalePersistent(t *testing.T) {
	m := newSunMockBrowser(t, "")
	env := sunBrowserAutoLocale{Language: "fr-FR", AcceptLanguage: "fr-FR,fr,en", Timezone: "Europe/Paris"}
	if err := ensureSunBrowserAutoLocale(m.port, env); err != nil {
		t.Fatal(err)
	}
	commands := m.snapshot()
	assertSunSessionOrder(t, commands, "initial")
	count := len(commands)
	if err := ensureSunBrowserAutoLocale(m.port, env); err != nil {
		t.Fatal(err)
	}
	if len(m.snapshot()) != count {
		t.Fatal("same environment was not idempotent")
	}
	m.attach("newpage", "page", "p1")
	m.attach("oopif", "iframe", "f1")
	m.attach("unknown", "future_target_type", "u1")
	m.attach("worker", "worker", "w1")
	m.waitResume(t, "newpage")
	m.waitResume(t, "oopif")
	m.waitResume(t, "unknown")
	m.waitResume(t, "worker")
	commands = m.snapshot()
	for _, cmd := range commands {
		if cmd.Method == "Target.setAutoAttach" {
			filter, ok := cmd.Params["filter"].([]any)
			if !ok || len(filter) != 3 || filter[1].(map[string]any)["type"] != "tab" || filter[1].(map[string]any)["exclude"] != true {
				t.Fatal("auto attach must exclude tab containers while attaching page execution targets")
			}
		}
	}
	assertSunSessionOrder(t, commands, "newpage")
	assertSunSessionOrder(t, commands, "oopif")
	for _, cmd := range commands {
		if cmd.Method != "Emulation.setUserAgentOverride" {
			continue
		}
		if cmd.Params["userAgent"] != "Native UA/123" || cmd.Params["acceptLanguage"] != env.AcceptLanguage {
			t.Fatalf("lost native UA/language: %#v", cmd)
		}
		metadata := cmd.Params["userAgentMetadata"].(map[string]any)
		if metadata["architecture"] != "arm" || metadata["platform"] != "NativeOS" || metadata["wow64"] != false {
			t.Fatalf("metadata changed: %#v", metadata)
		}
		versions := metadata["fullVersionList"].([]any)
		if versions[0].(map[string]any)["version"] != "123.4.5.6" {
			t.Fatal("UA-CH version changed")
		}
	}
}

func assertSunSessionOrder(t *testing.T, commands []sunMockCommand, session string) {
	t.Helper()
	seen := map[string]bool{}
	resumed := false
	for _, cmd := range commands {
		if cmd.Session != session {
			continue
		}
		if cmd.Method == "Runtime.runIfWaitingForDebugger" {
			for _, method := range []string{"Target.setAutoAttach", "Emulation.setTimezoneOverride", "Emulation.setLocaleOverride", "Runtime.evaluate", "Emulation.setUserAgentOverride"} {
				if !seen[method] {
					t.Fatalf("%s resumed before %s", session, method)
				}
			}
			resumed = true
		}
		if cmd.Method == "Emulation.setUserAgentOverride" && !seen["Runtime.evaluate"] {
			t.Fatal("UA overridden before native metadata read")
		}
		seen[cmd.Method] = true
	}
	if !resumed {
		t.Fatalf("session %s was not synchronously resumed", session)
	}
}

func TestSunBrowserAutoLocaleErrorResumesAndCleansCache(t *testing.T) {
	m := newSunMockBrowser(t, "Emulation.setTimezoneOverride")
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "de-DE", Timezone: "Europe/Berlin"}); err == nil {
		t.Fatal("expected native override error")
	}
	m.waitResume(t, "initial")
	sunAutoControllers.Lock()
	cached := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	if cached != nil {
		t.Fatal("failed controller retained in cache")
	}
}

func TestSunBrowserAutoLocaleDisconnectRemovesCache(t *testing.T) {
	m := newSunMockBrowser(t, "")
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{}); err != nil {
		t.Fatal(err)
	}
	sunAutoControllers.Lock()
	c := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	_ = conn.Close()
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("disconnect not detected")
	}
	// close(done) precedes cache eviction; close waits for that cleanup to finish.
	c.close()
	sunAutoControllers.Lock()
	cached := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	if cached != nil {
		t.Fatal("disconnected controller retained in cache")
	}
}

func TestSunBrowserAutoLocaleWorkerUnsupportedStillResumes(t *testing.T) {
	m := newSunMockBrowser(t, "worker2:Emulation.setTimezoneOverride")
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Timezone: "Asia/Tokyo"}); err != nil {
		t.Fatal(err)
	}
	// Unknown types are always resumed without issuing unsupported Emulation calls.
	m.attach("unknown2", "other", "other-target")
	m.attach("worker2", "worker", "worker-target")
	m.waitResume(t, "unknown2")
	m.waitResume(t, "worker2")
	sunAutoControllers.Lock()
	cached := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	if cached == nil {
		t.Fatal("unsupported worker Emulation command closed page controller")
	}
	for _, cmd := range m.snapshot() {
		if cmd.Session == "unknown2" && cmd.Method != "Runtime.runIfWaitingForDebugger" {
			t.Fatalf("unexpected command: %#v", cmd)
		}
	}
}

func TestSunBrowserAutoLocaleReplacement(t *testing.T) {
	m := newSunMockBrowser(t, "")
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "en-US"}); err != nil {
		t.Fatal(err)
	}
	sunAutoControllers.Lock()
	old := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "ja-JP"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.done:
	default:
		t.Fatal("previous controller was not closed")
	}
	sunAutoControllers.Lock()
	replacement := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	if replacement == nil || replacement == old || replacement.env.Language != "ja-JP" {
		t.Fatal("environment replacement failed")
	}
	stopSunBrowserAutoLocale(m.port)
	select {
	case <-replacement.done:
	default:
		t.Fatal("stop did not close controller")
	}
}

func TestSunBrowserAutoLocaleNetworkFallback(t *testing.T) {
	m := newSunMockBrowser(t, "Emulation.setUserAgentOverride")
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "it-IT"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Network.setUserAgentOverride" {
			found = true
			if cmd.Params["userAgent"] != "Native UA/123" || cmd.Params["userAgentMetadata"] == nil {
				t.Fatal("fallback lost native UA metadata")
			}
		}
	}
	if !found {
		t.Fatal("missing Network fallback")
	}
}

func TestSunBrowserAutoLocaleBlankMetadataUsesPristineProbe(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.emptyMetadata = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	m.attach("newblank", "page", "p-new")
	m.waitResume(t, "newblank")
	probeClosed, autoAttach := false, false
	probes := 0
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Target.createTarget" {
			probes++
		}
		if cmd.Method == "Target.closeTarget" {
			probeClosed = true
		}
		if cmd.Method == "Target.setAutoAttach" && cmd.Session == "" {
			autoAttach = true
			if !probeClosed {
				t.Fatal("auto-attach started before pristine probe closed")
			}
		}
		if cmd.Session == "probe-session" && cmd.Method != "Page.navigate" && cmd.Method != "Runtime.evaluate" && cmd.Method != "Runtime.runIfWaitingForDebugger" {
			t.Fatalf("probe corrupted by normal apply: %#v", cmd)
		}
		if cmd.Method == "Emulation.setUserAgentOverride" {
			metadata, ok := cmd.Params["userAgentMetadata"].(map[string]any)
			if !ok || metadata["platform"] != "NativeOS" || len(metadata["brands"].([]any)) == 0 {
				t.Fatalf("empty metadata forwarded: %#v", cmd)
			}
		}
	}
	if probes != 1 || !autoAttach {
		t.Fatalf("probe not cached: probes=%d attach=%v", probes, autoAttach)
	}
}

func TestSunBrowserAutoLocaleMissingMetadataUsesPristineProbe(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.missingMetadata = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Emulation.setUserAgentOverride" {
			metadata, ok := cmd.Params["userAgentMetadata"].(map[string]any)
			if !ok || metadata["platform"] != "NativeOS" {
				t.Fatalf("missing metadata forwarded: %#v", cmd)
			}
		}
	}
}

func TestSunBrowserAutoLocaleGenuineUADataAbsence(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.noMetadata = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Emulation.setUserAgentOverride" && cmd.Params["userAgentMetadata"] != nil {
			t.Fatalf("invented UA-CH: %#v", cmd)
		}
	}
}

func TestSunBrowserAutoLocaleProbeFailureClosesTarget(t *testing.T) {
	m := newSunMockBrowser(t, "probe-session:Runtime.evaluate")
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR"}); err == nil {
		t.Fatal("expected probe failure")
	}
	closed := false
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Target.closeTarget" {
			closed = true
		}
		if cmd.Method == "Emulation.setUserAgentOverride" {
			t.Fatal("override ran after failed probe")
		}
	}
	if !closed {
		t.Fatal("probe leaked")
	}
}

func TestSunBrowserAutoLocaleClosingProbeInSnapshot(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.staleProbe = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	attaches := 0
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Target.attachToTarget" && cmd.Params["targetId"] == "probe" {
			attaches++
		}
		if cmd.Session == "late-probe" && cmd.Method != "Runtime.runIfWaitingForDebugger" {
			t.Fatalf("late probe got page override: %#v", cmd)
		}
	}
	if attaches != 1 {
		t.Fatalf("closing probe reattached from getTargets snapshot: %d", attaches)
	}
}

func TestSunBrowserAutoLocaleSnapshotTargetDisappearsBeforeAttach(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.disappearingAttach = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cmd := range m.snapshot() {
		if cmd.Method == "Target.attachToTarget" && cmd.Params["targetId"] == "gone" {
			found = true
		}
	}
	if !found {
		t.Fatal("disappearing snapshot target was not exercised")
	}
}

func TestSunBrowserAutoLocaleTargetClosesDuringApply(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.closeDuringApply = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR", Timezone: "Europe/Paris"}); err != nil {
		t.Fatal(err)
	}
	m.waitResume(t, "initial")
	sunAutoControllers.Lock()
	c := sunAutoControllers.entries[m.port]
	sunAutoControllers.Unlock()
	if c == nil {
		t.Fatal("closed page terminated healthy controller")
	}
}

func TestSunBrowserAutoLocaleGoneErrorOnLiveTargetStillFails(t *testing.T) {
	m := newSunMockBrowser(t, "")
	m.liveGoneError = true
	if err := ensureSunBrowserAutoLocale(m.port, sunBrowserAutoLocale{Language: "fr-FR", Timezone: "Europe/Paris"}); err == nil {
		t.Fatal("live target's failed apply was swallowed")
	}
}

// Opt-in integration: ANT_TEST_SUNBROWSER is the executable path; no existing profile is touched.
func TestSunBrowserAutoLocaleReal(t *testing.T) {
	exe := os.Getenv("ANT_TEST_SUNBROWSER")
	if exe == "" {
		t.Skip("set ANT_TEST_SUNBROWSER to opt in")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	env := sunBrowserAutoLocale{Language: "fr-FR", AcceptLanguage: "fr-FR,fr", Timezone: "Europe/Paris"}
	launchArgs := []string{"--user-data-dir=" + t.TempDir(), fmt.Sprintf("--remote-debugging-port=%d", port), "--no-proxy-server", "--no-first-run", "--no-default-browser-check", "about:blank"}
	var measured *sunBrowserWindowBounds
	if proxy := os.Getenv("ANT_TEST_AUTO_PROXY"); proxy != "" {
		location, lookupErr := resolveSunBrowserAutoLocation(proxy)
		if lookupErr != nil {
			t.Fatal(lookupErr)
		}
		env = sunBrowserAutoLocale{Language: location.Language, AcceptLanguage: location.AcceptLanguage, Timezone: location.Timezone}
		t.Logf("auto geo IP=%s country=%s city=%s language-source=%s", location.IP, location.CountryCode, location.City, location.LanguageSource)
		if proxy != "direct://" {
			for i, arg := range launchArgs {
				if arg == "--no-proxy-server" {
					launchArgs[i] = "--proxy-server=" + proxy
				}
			}
		}
		bounds, boundsErr := sunBrowserAutoWindowBounds()
		if boundsErr != nil {
			t.Fatal(boundsErr)
		}
		measured = &bounds
		launchArgs, err = sunBrowserAutoWindowArgs(launchArgs)
		if err != nil {
			t.Fatal(err)
		}
	}
	args := browser.AdaptCoreLaunchArgs(exe, launchArgs)
	cmd := exec.CommandContext(ctx, exe, args...)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stopSunBrowserAutoLocale(port); cancel(); _ = cmd.Wait() }()
	for {
		_, err = cdpGetEndpointBody(port, "/json/version")
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	versionBody, err := cdpGetEndpointBody(port, "/json/version")
	if err != nil {
		t.Fatal(err)
	}
	var version cdpBrowserVersion
	if err = json.Unmarshal(versionBody, &version); err != nil {
		t.Fatal(err)
	}
	conn, err := cdpDialWebSocket(version.WebSocketDebuggerUrl)
	if err != nil {
		t.Fatal(err)
	}
	baselineController := &sunAutoController{conn: conn, pending: make(map[int]chan sunAutoReply), sessions: make(map[string]*sunAutoSession), targets: make(map[string]*sunAutoSession), done: make(chan struct{})}
	go baselineController.readLoop()
	baseline, err := baselineController.captureNativeUA()
	baselineController.close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pristine native baseline UA=%s metadata=%s", baseline.UserAgent, baseline.Metadata)
	if err = ensureSunBrowserAutoLocale(port, env); err != nil {
		t.Fatal(err)
	}
	if measured != nil {
		if err = applySunBrowserAutoWindowRuntime(port, *measured); err != nil {
			t.Fatal(err)
		}
	}
	sunAutoControllers.Lock()
	c := sunAutoControllers.entries[port]
	sunAutoControllers.Unlock()
	assertNative := func(sessionID string) {
		t.Helper()
		raw, evalErr := c.call(sessionID, "Runtime.evaluate", map[string]any{"expression": sunAutoNativeUAExpression, "awaitPromise": true, "returnByValue": true})
		if evalErr != nil {
			t.Fatal(evalErr)
		}
		actual, evalErr := sunAutoDecodeNativeUA(raw)
		if evalErr != nil {
			t.Fatal(evalErr)
		}
		var want, got any
		_ = json.Unmarshal(baseline.Metadata, &want)
		_ = json.Unmarshal(actual.Metadata, &got)
		if actual.UserAgent != baseline.UserAgent || !reflect.DeepEqual(want, got) {
			t.Fatalf("native UA-CH changed: baseline=%s actual=%s UA=%s", baseline.Metadata, actual.Metadata, actual.UserAgent)
		}
	}
	c.mu.Lock()
	var original *sunAutoSession
	for _, s := range c.targets {
		if s.kind == "page" {
			original = s
			break
		}
	}
	c.mu.Unlock()
	if original == nil {
		t.Fatal("missing initial blank target")
	}
	// The initial blank document can lack UAData; navigation must restore the
	// exact pristine values, including brands/platform and all entropy fields.
	result, err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	var target struct {
		ID string `json:"targetId"`
	}
	if err = json.Unmarshal(result, &target); err != nil {
		t.Fatal(err)
	}
	var session *sunAutoSession
	for session == nil {
		c.mu.Lock()
		session = c.targets[target.ID]
		c.mu.Unlock()
		if session == nil {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	select {
	case <-session.done:
		if session.err != nil {
			t.Fatal(session.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result, err = c.call(session.id, "Runtime.evaluate", map[string]any{"expression": "({language:navigator.language,languages:navigator.languages,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone})", "returnByValue": true})
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Result struct {
			Value struct {
				Language  string   `json:"language"`
				Languages []string `json:"languages"`
				Timezone  string   `json:"timezone"`
			} `json:"value"`
		} `json:"result"`
	}
	if err = json.Unmarshal(result, &value); err != nil {
		t.Fatal(err)
	}
	if value.Result.Value.Language != env.Language || value.Result.Value.Timezone != env.Timezone || strings.Join(value.Result.Value.Languages, ",") != env.AcceptLanguage {
		t.Fatalf("native locale mismatch: %s", result)
	}
	t.Logf("new tab native JS: %s", result)
	if measured != nil {
		deadline := time.Now().Add(5 * time.Second)
		for {
			windowResult, windowErr := c.call(session.id, "Runtime.evaluate", map[string]any{"expression": "({width:outerWidth,height:outerHeight})", "returnByValue": true})
			if windowErr != nil {
				t.Fatal(windowErr)
			}
			var window struct {
				Result struct {
					Value struct {
						Width  int `json:"width"`
						Height int `json:"height"`
					} `json:"value"`
				} `json:"result"`
			}
			if err = json.Unmarshal(windowResult, &window); err != nil {
				t.Fatal(err)
			}
			dw, dh := window.Result.Value.Width-measured.Width, window.Result.Value.Height-measured.Height
			if dw >= -2 && dw <= 2 && dh >= -2 && dh <= 2 {
				t.Logf("native window outer=%dx%d measured=%dx%d", window.Result.Value.Width, window.Result.Value.Height, measured.Width, measured.Height)
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("native window mismatch: measured=%+v JS=%s", *measured, windowResult)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	headers := make(chan string, 10)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case headers <- r.Header.Get("Accept-Language"):
		default:
		}
		_, _ = w.Write([]byte("<!doctype html><title>locale test</title>"))
	}))
	defer origin.Close()
	if _, err = c.call(session.id, "Page.navigate", map[string]any{"url": origin.URL}); err != nil {
		t.Fatal(err)
	}
	select {
	case header := <-headers:
		parts := strings.Split(header, ",")
		for i, part := range parts {
			parts[i], _, _ = strings.Cut(strings.TrimSpace(part), ";")
		}
		if strings.Join(parts, ",") != env.AcceptLanguage {
			t.Fatalf("HTTP Accept-Language mismatch: got %q expected %q", header, env.AcceptLanguage)
		}
		t.Logf("new tab HTTP Accept-Language: %s", header)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Wait for navigation commit rather than evaluating the old blank context.
	waitURL := func(s *sunAutoSession, url string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			raw, e := c.call(s.id, "Runtime.evaluate", map[string]any{"expression": "location.href", "returnByValue": true})
			if e != nil {
				t.Fatal(e)
			}
			var response struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			_ = json.Unmarshal(raw, &response)
			if response.Result.Value == url {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("navigation did not commit: %s", raw)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitURL(session, origin.URL+"/")
	assertNative(session.id)
	path := filepath.Join(t.TempDir(), "ua.html")
	if err = os.WriteFile(path, []byte("<!doctype html><title>native file test</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	fileURL := "file://" + path
	if _, err = c.call(session.id, "Page.navigate", map[string]any{"url": fileURL}); err != nil {
		t.Fatal(err)
	}
	waitURL(session, fileURL)
	assertNative(session.id)

	// Also cover initial about:blank -> file -> HTTP independently of the new tab.
	if _, err = c.call(original.id, "Page.navigate", map[string]any{"url": fileURL}); err != nil {
		t.Fatal(err)
	}
	waitURL(original, fileURL)
	assertNative(original.id)
	if _, err = c.call(original.id, "Page.navigate", map[string]any{"url": origin.URL}); err != nil {
		t.Fatal(err)
	}
	waitURL(original, origin.URL+"/")
	assertNative(original.id)
	// A previous metadata-free override must not poison subsequent native reads.
	if sunAutoHasMetadata(baseline.Metadata) {
		if _, err = c.call(original.id, "Emulation.setUserAgentOverride", map[string]any{"userAgent": baseline.UserAgent, "acceptLanguage": env.AcceptLanguage}); err != nil {
			t.Fatal(err)
		}
		if err = c.apply(original); err != nil {
			t.Fatal(err)
		}
		assertNative(original.id)
	}

}
