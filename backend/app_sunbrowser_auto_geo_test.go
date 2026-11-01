package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSunBrowserAutoLocationParsing(t *testing.T) {
	for _, input := range []string{`{"ip":"67.230.176.144","country_code":"US","timezone":{"id":"America/Los_Angeles"}}`, `{"ip":"67.230.176.144","country_code":"US","timezone":"America/Los_Angeles"}`} {
		got, err := parseSunBrowserAutoLocation(strings.NewReader(input), "fixture")
		if err != nil || got.Language != "en-US" || got.AcceptLanguage != "en-US,en" || got.Timezone != "America/Los_Angeles" || got.State != "pending" {
			t.Fatalf("got %#v, %v", got, err)
		}
	}
	for _, input := range []string{`{"success":false}`, `{"ip":"bad","country_code":"US","timezone":"UTC"}`, `{"ip":"1.1.1.1","country_code":"US","timezone":"Invalid/Zone"}`, `{"ip":"1.1.1.1","country_code":"XX","timezone":"UTC"}`} {
		if _, err := parseSunBrowserAutoLocation(strings.NewReader(input), "fixture"); err == nil {
			t.Fatalf("accepted invalid response %s", input)
		}
	}
}

func TestSunBrowserLocationFallbackUsesSameClient(t *testing.T) {
	calls := 0
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/first" {
			http.Error(w, "failed", 503)
			return
		}
		_, _ = w.Write([]byte(`{"ip":"67.230.176.144","country_code":"US","timezone":"America/Los_Angeles"}`))
	}))
	defer service.Close()
	got, err := lookupSunBrowserAutoLocation(context.Background(), service.Client(), []string{service.URL + "/first", service.URL + "/second"})
	if err != nil || got.Language != "en-US" || calls != 2 {
		t.Fatalf("got %#v %v calls=%d", got, err, calls)
	}
}

func TestSunBrowserAutoArgsOverrideExplicitLocaleWithoutMutatingInput(t *testing.T) {
	args := []string{"--lang=zh-CN", "--timezone", "Asia/Tokyo", "--accept-lang=zh-CN,zh", "--restore-last-session", "--start-maximized", "--proxy-server=socks5://127.0.0.1:8888", "https://example.test"}
	saved := append([]string(nil), args...)
	location := sunBrowserAutoLocation{Language: "en-US", AcceptLanguage: "en-US,en", Timezone: "America/Los_Angeles"}
	got := replaceSunBrowserAutoArgs(args, location)
	want := []string{"--lang=en-US", "--accept-lang=en-US,en", "--timezone=America/Los_Angeles", "--proxy-server=socks5://127.0.0.1:8888", "https://example.test"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(args, saved) {
		t.Fatalf("args=%#v original=%#v", got, args)
	}
}
