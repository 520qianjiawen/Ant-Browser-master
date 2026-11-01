package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Explicit opt-in: starts only a temporary, direct-connection browser profile.
func TestSunBrowserCDPIntegration(t *testing.T) {
	exe := os.Getenv("ANT_TEST_SUNBROWSER")
	if exe == "" {
		t.Skip("set ANT_TEST_SUNBROWSER to opt in to a real browser launch")
	}
	if !IsSunBrowserExecutable(exe) {
		t.Fatal("expected main SunBrowser executable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	args := AdaptCoreLaunchArgs(exe, []string{
		"--user-data-dir=" + t.TempDir(), fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-proxy-server", "--no-first-run", "--no-default-browser-check", "about:blank",
	})
	cmd := exec.CommandContext(ctx, exe, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	client := &http.Client{Timeout: time.Second}
	var info struct {
		Browser      string `json:"Browser"`
		WebSocketURL string `json:"webSocketDebuggerUrl"`
	}
	for ctx.Err() == nil {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
		if err == nil {
			err = json.NewDecoder(response.Body).Decode(&info)
			response.Body.Close()
			if err == nil && info.WebSocketURL != "" {
				break
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	if info.WebSocketURL == "" {
		t.Fatal("SunBrowser did not expose CDP before deadline")
	}
	t.Logf("CDP ready: %s", info.Browser)
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, info.WebSocketURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := connection.WriteJSON(map[string]interface{}{"id": 1, "method": "Browser.close"}); err != nil {
		t.Fatal(err)
	}
	// Allow Chromium to flush its isolated profile before deferred process cleanup.
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = connection.ReadMessage()
}
