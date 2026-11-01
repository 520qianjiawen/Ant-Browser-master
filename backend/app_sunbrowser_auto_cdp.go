package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// sunBrowserAutoLocale uses native CDP overrides, without modifying other device properties.
type sunBrowserAutoLocale struct {
	Language       string
	AcceptLanguage string
	Timezone       string
}

const sunAutoCommandTimeout = 5 * time.Second

var sunAutoControllers = struct {
	sync.Mutex
	entries map[int]*sunAutoController
}{entries: make(map[int]*sunAutoController)}

type sunAutoReply struct {
	ID        int             `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type sunAutoSession struct {
	id, targetID, kind string
	done               chan struct{}
	err                error // published by closing done
}

type sunAutoController struct {
	port      int
	env       sunBrowserAutoLocale
	conn      *websocket.Conn
	writeMu   sync.Mutex
	mu        sync.Mutex
	nextID    int
	pending   map[int]chan sunAutoReply
	sessions  map[string]*sunAutoSession
	targets   map[string]*sunAutoSession
	done      chan struct{}
	ready     chan struct{}
	initErr   error // published by closing ready
	closeOnce sync.Once
	// Published before auto-attach starts, then immutable.
	nativeUA    sunAutoNativeUA
	probeTarget string // guarded by mu; retained for controller lifetime
	probeClosed bool   // guarded by mu
}

// ensureSunBrowserAutoLocale returns only after existing pages have been configured.
// The browser must initially be launched at about:blank, before deferred navigation.
func ensureSunBrowserAutoLocale(debugPort int, env sunBrowserAutoLocale) error {
	if debugPort <= 0 || debugPort > 65535 {
		return fmt.Errorf("invalid CDP port %d", debugPort)
	}
	for {
		sunAutoControllers.Lock()
		old := sunAutoControllers.entries[debugPort]
		if old != nil && old.env == env {
			sunAutoControllers.Unlock()
			<-old.ready
			return old.initErr
		}
		if old != nil {
			delete(sunAutoControllers.entries, debugPort)
			sunAutoControllers.Unlock()
			old.close()
			continue
		}
		c := &sunAutoController{port: debugPort, env: env, pending: make(map[int]chan sunAutoReply), sessions: make(map[string]*sunAutoSession), targets: make(map[string]*sunAutoSession), done: make(chan struct{}), ready: make(chan struct{})}
		sunAutoControllers.entries[debugPort] = c
		sunAutoControllers.Unlock()
		err := c.initialize()
		if err != nil {
			c.close()
		}
		c.initErr = err
		close(c.ready)
		return err
	}
}

func stopSunBrowserAutoLocale(debugPort int) {
	sunAutoControllers.Lock()
	c := sunAutoControllers.entries[debugPort]
	delete(sunAutoControllers.entries, debugPort)
	sunAutoControllers.Unlock()
	if c != nil {
		c.close()
	}
}

func (c *sunAutoController) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		sunAutoControllers.Lock()
		if sunAutoControllers.entries[c.port] == c {
			delete(sunAutoControllers.entries, c.port)
		}
		sunAutoControllers.Unlock()
	})
}

func (c *sunAutoController) initialize() error {
	body, err := cdpGetEndpointBody(c.port, "/json/version")
	if err != nil {
		return err
	}
	var version cdpBrowserVersion
	if err = json.Unmarshal(body, &version); err != nil {
		return err
	}
	if strings.TrimSpace(version.WebSocketDebuggerUrl) == "" {
		return errors.New("missing browser CDP websocket")
	}
	conn, err := cdpDialWebSocket(version.WebSocketDebuggerUrl)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	select {
	case <-c.done:
		_ = conn.Close()
		return errors.New("CDP controller stopped")
	default:
	}
	go c.readLoop()
	// Capture before auto-attach can apply overrides to any execution target.
	if c.env.Language != "" || c.env.AcceptLanguage != "" {
		if c.nativeUA, err = c.captureNativeUA(); err != nil {
			return fmt.Errorf("capture native UA: %w", err)
		}
	}
	if _, err = c.call("", "Target.setAutoAttach", sunAutoAttachParams()); err != nil {
		return err
	}
	result, err := c.call("", "Target.getTargets", nil)
	if err != nil {
		return err
	}
	var targets struct {
		Infos []struct {
			ID   string `json:"targetId"`
			Type string `json:"type"`
		} `json:"targetInfos"`
	}
	if err = json.Unmarshal(result, &targets); err != nil {
		return err
	}
	for _, target := range targets.Infos {
		c.mu.Lock()
		probe := target.ID == c.probeTarget
		c.mu.Unlock()
		if probe || (target.Type != "page" && target.Type != "iframe") {
			continue
		}
		if err := c.configureTarget(target.ID, target.Type); err != nil {
			return err
		}
	}

	select {
	case <-c.done:
		return errors.New("CDP disconnected during locale initialization")
	default:
		return nil
	}
}

// A getTargets snapshot is not a lease: tabs (especially the just-closed
// native probe) can disappear before attach or while native overrides run.
func sunAutoGoneError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, fragment := range []string{"no target with given id", "no session with given id", "session with given id not found", "target closed", "session closed", "inspected target navigated or closed"} {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func (c *sunAutoController) targetExists(target string) (bool, error) {
	raw, err := c.call("", "Target.getTargets", nil)
	if err != nil {
		return false, err
	}
	var result struct {
		Infos []struct {
			ID string `json:"targetId"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, err
	}
	for _, info := range result.Infos {
		if info.ID == target {
			return true, nil
		}
	}
	return false, nil
}

func (c *sunAutoController) configureTarget(target, kind string) error {
	deadline := time.Now().Add(8 * sunAutoCommandTimeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		s := c.targets[target]
		c.mu.Unlock()
		if s == nil {
			raw, err := c.call("", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})
			if err != nil {
				// This error is direct protocol confirmation that this snapshot
				// entry has vanished, not an unsupported locale override.
				if sunAutoGoneError(err) {
					return nil
				}
				return err
			}
			var info struct {
				ID string `json:"sessionId"`
			}
			if err := json.Unmarshal(raw, &info); err != nil {
				return err
			}
			if info.ID == "" {
				return errors.New("empty attached CDP session")
			}
			s = c.session(info.ID, target, kind)
		}
		select {
		case <-s.done:
		case <-c.done:
			return errors.New("CDP disconnected during locale initialization")
		case <-time.After(time.Until(deadline)):
			return errors.New("CDP locale initialization timed out")
		}
		c.mu.Lock()
		current := c.targets[target]
		c.mu.Unlock()
		if current == s && s.err == nil {
			return nil
		}
		if sunAutoGoneError(s.err) || current != s {
			exists, err := c.targetExists(target)
			if err != nil {
				return err
			}
			if !exists {
				return nil
			}
			if current != s {
				continue
			} // configure the replacement generation
		}
		if s.err != nil {
			return s.err
		} // a live page's override failure is fatal
	}
	return errors.New("CDP locale initialization timed out")
}

func sunAutoAttachParams() map[string]any {
	// New Chromium versions reject filters that include both tab containers and
	// page targets. Attach executable targets directly, not browser/tab containers;
	// retain the catch-all so unfamiliar execution targets are still resumed.
	return map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true, "filter": []any{
		map[string]any{"type": "browser", "exclude": true},
		map[string]any{"type": "tab", "exclude": true},
		map[string]any{},
	}}
}

func (c *sunAutoController) call(session, method string, params any) (json.RawMessage, error) {
	reply := make(chan sunAutoReply, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	msg := map[string]any{"id": id, "method": method}
	if session != "" {
		msg["sessionId"] = session
	}
	if params != nil {
		msg["params"] = params
	}
	c.writeMu.Lock()
	select {
	case <-c.done:
		c.writeMu.Unlock()
		return nil, errors.New("CDP disconnected")
	default:
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(sunAutoCommandTimeout))
	err := c.conn.WriteJSON(msg)
	c.writeMu.Unlock()
	if err != nil {
		c.close()
		return nil, err
	}
	timer := time.NewTimer(sunAutoCommandTimeout)
	defer timer.Stop()
	select {
	case response := <-reply:
		if response.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, response.Error.Message)
		}
		return response.Result, nil
	case <-c.done:
		return nil, errors.New("CDP disconnected")
	case <-timer.C:
		return nil, fmt.Errorf("%s timed out", method)
	}
}

func (c *sunAutoController) readLoop() {
	defer c.close()
	// No idle read deadline: persistent idle browsers must stay attached. Commands
	// have bounded waits; websocket disconnects terminate this reader and the cache.
	for {
		var message sunAutoReply
		if err := c.conn.ReadJSON(&message); err != nil {
			return
		}
		if message.ID != 0 {
			c.mu.Lock()
			ch := c.pending[message.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- message
			}
			continue
		}
		if message.Method == "Target.detachedFromTarget" {
			var event struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(message.Params, &event) == nil {
				c.mu.Lock()
				if s := c.sessions[event.SessionID]; s != nil {
					delete(c.sessions, event.SessionID)
					if c.targets[s.targetID] == s {
						delete(c.targets, s.targetID)
					}
				}
				c.mu.Unlock()
			}
		}
		if message.Method == "Target.attachedToTarget" {
			var event struct {
				SessionID string `json:"sessionId"`
				Waiting   bool   `json:"waitingForDebugger"`
				Target    struct {
					ID   string `json:"targetId"`
					Type string `json:"type"`
				} `json:"targetInfo"`
			}
			if json.Unmarshal(message.Params, &event) == nil && event.SessionID != "" {
				c.mu.Lock()
				probe := event.Target.ID == c.probeTarget
				closed := c.probeClosed
				c.mu.Unlock()
				if probe && (closed || event.Waiting) {
					// Never block the root reader waiting for its own replies.
					go func(session string, waiting, closed bool) {
						if waiting {
							_, _ = c.call(session, "Runtime.runIfWaitingForDebugger", nil)
						}
						if closed {
							_, _ = c.call("", "Target.detachFromTarget", map[string]any{"sessionId": session})
						}
					}(event.SessionID, event.Waiting, closed)
				}
				if !probe {
					c.session(event.SessionID, event.Target.ID, event.Target.Type)
				}
			}
		}
	}
}

func (c *sunAutoController) session(id, target, kind string) *sunAutoSession {
	c.mu.Lock()
	if existing := c.sessions[id]; existing != nil {
		c.mu.Unlock()
		return existing
	}
	s := &sunAutoSession{id: id, targetID: target, kind: kind, done: make(chan struct{})}
	c.sessions[id] = s
	c.targets[target] = s
	c.mu.Unlock()
	go func() {
		err := c.apply(s)
		// Resume is deliberately independent of apply's result, including unknown types.
		_, resumeErr := c.call(s.id, "Runtime.runIfWaitingForDebugger", nil)
		if err == nil {
			err = resumeErr
		}
		vanished := false
		if sunAutoGoneError(err) {
			c.mu.Lock()
			detached := c.sessions[s.id] != s || c.targets[s.targetID] != s
			c.mu.Unlock()
			vanished = detached
			if !detached {
				exists, checkErr := c.targetExists(s.targetID)
				vanished = checkErr == nil && !exists
			}
		}
		s.err = err
		close(s.done)
		c.mu.Lock()
		active := c.sessions[s.id] == s && c.targets[s.targetID] == s
		c.mu.Unlock()
		if err != nil && active && !vanished && (s.kind == "page" || s.kind == "iframe") {
			c.close()
		}
	}()
	return s
}

// Read both the native UA and its structured metadata before applying any override.
const sunAutoNativeUAExpression = `(async () => {
 const d = navigator.userAgentData;
 let metadata;
 if (d) {
  metadata = {...d.toJSON(), ...await d.getHighEntropyValues(['architecture','bitness','model','platformVersion','uaFullVersion','fullVersionList','wow64','formFactors'])};
  if (metadata.uaFullVersion !== undefined) { metadata.fullVersion = metadata.uaFullVersion; delete metadata.uaFullVersion; }
 }
 return {userAgent: navigator.userAgent, metadata, url: location.href};
})()`

type sunAutoNativeUA struct {
	UserAgent string          `json:"userAgent"`
	Metadata  json.RawMessage `json:"metadata"`
	URL       string          `json:"url"`
}

func sunAutoHasMetadata(raw json.RawMessage) bool {
	var m struct {
		Brands   []json.RawMessage `json:"brands"`
		Platform string            `json:"platform"`
	}
	return json.Unmarshal(raw, &m) == nil && len(m.Brands) > 0 && m.Platform != ""
}

func sunAutoDecodeNativeUA(result json.RawMessage) (sunAutoNativeUA, error) {
	var reply struct {
		Result struct {
			Value sunAutoNativeUA `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &reply); err != nil {
		return sunAutoNativeUA{}, err
	}
	if reply.Result.Value.UserAgent == "" || len(reply.Exception) > 0 {
		return sunAutoNativeUA{}, errors.New("native user agent evaluation failed")
	}
	return reply.Result.Value, nil
}

// Loopback HTTP is a potentially trustworthy (secure) origin. A disposable
// target avoids navigating user pages and is exempt from root event dispatch
// during its manual attach. Every command and the document-ready wait is bounded.
func (c *sunAutoController) captureNativeUA() (sunAutoNativeUA, error) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><title>native UA probe</title>"))
	}))
	defer origin.Close()
	result, err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return sunAutoNativeUA{}, err
	}
	var target struct {
		ID string `json:"targetId"`
	}
	if err = json.Unmarshal(result, &target); err != nil {
		return sunAutoNativeUA{}, err
	}
	if target.ID == "" {
		return sunAutoNativeUA{}, errors.New("empty native probe target")
	}
	c.mu.Lock()
	c.probeTarget = target.ID
	c.mu.Unlock()
	defer func() {
		_, _ = c.call("", "Target.closeTarget", map[string]any{"targetId": target.ID})
		c.mu.Lock()
		c.probeClosed = true
		c.mu.Unlock()
	}()
	result, err = c.call("", "Target.attachToTarget", map[string]any{"targetId": target.ID, "flatten": true})
	if err != nil {
		return sunAutoNativeUA{}, err
	}
	var session struct {
		ID string `json:"sessionId"`
	}
	if err = json.Unmarshal(result, &session); err != nil {
		return sunAutoNativeUA{}, err
	}
	if session.ID == "" {
		return sunAutoNativeUA{}, errors.New("empty native probe session")
	}
	if _, err = c.call(session.ID, "Page.navigate", map[string]any{"url": origin.URL}); err != nil {
		return sunAutoNativeUA{}, err
	}
	deadline := time.Now().Add(sunAutoCommandTimeout)
	for {
		result, err = c.call(session.ID, "Runtime.evaluate", map[string]any{"expression": sunAutoNativeUAExpression, "awaitPromise": true, "returnByValue": true})
		if err != nil {
			return sunAutoNativeUA{}, err
		}
		native, err := sunAutoDecodeNativeUA(result)
		if err != nil {
			return sunAutoNativeUA{}, err
		}
		if native.URL == origin.URL+"/" {
			// Absence here is genuine: never invent brands, versions or OS values.
			// Present-but-empty UAData is not a usable preservation baseline.
			if len(native.Metadata) > 0 && string(native.Metadata) != "null" && !sunAutoHasMetadata(native.Metadata) {
				return sunAutoNativeUA{}, errors.New("native probe returned empty UA-CH metadata")
			}
			return native, nil
		}
		if time.Now().After(deadline) {
			return sunAutoNativeUA{}, errors.New("native probe navigation timed out")
		}
		select {
		case <-c.done:
			return sunAutoNativeUA{}, errors.New("native probe disconnected")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (c *sunAutoController) apply(s *sunAutoSession) error {
	page := s.kind == "page" || s.kind == "iframe"
	worker := s.kind == "worker" || s.kind == "shared_worker" || s.kind == "service_worker"
	if !page && !worker {
		return nil
	}
	var first error
	attempt := func(method string, params any) {
		if _, err := c.call(s.id, method, params); err != nil && first == nil {
			first = err
		}
	}
	if page {
		attempt("Target.setAutoAttach", sunAutoAttachParams())
	}
	if c.env.Timezone != "" {
		attempt("Emulation.setTimezoneOverride", map[string]any{"timezoneId": c.env.Timezone})
	}
	if c.env.Language != "" {
		attempt("Emulation.setLocaleOverride", map[string]any{"locale": c.env.Language})
	}
	accept := c.env.AcceptLanguage
	if accept == "" {
		accept = c.env.Language
	}
	if accept != "" {
		result, err := c.call(s.id, "Runtime.evaluate", map[string]any{"expression": sunAutoNativeUAExpression, "awaitPromise": true, "returnByValue": true})
		native, decodeErr := sunAutoDecodeNativeUA(result)
		if err == nil {
			err = decodeErr
		}
		if err == nil {
			// Missing/empty UA-CH on blank, insecure, or freshly attached contexts
			// is not evidence that this browser lacks native UA-CH.
			if !sunAutoHasMetadata(native.Metadata) && sunAutoHasMetadata(c.nativeUA.Metadata) {
				native = c.nativeUA
			}
			params := map[string]any{"userAgent": native.UserAgent, "acceptLanguage": accept}
			if metadata := native.Metadata; sunAutoHasMetadata(metadata) {
				params["userAgentMetadata"] = metadata
			}
			_, err = c.call(s.id, "Emulation.setUserAgentOverride", params)
			if err != nil {
				_, err = c.call(s.id, "Network.setUserAgentOverride", params)
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	// Worker protocol support varies by Chromium version; best effort, always resume.
	if !page {
		return nil
	}
	return first
}
