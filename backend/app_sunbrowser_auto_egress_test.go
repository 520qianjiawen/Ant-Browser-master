package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSunBrowserEgressUsesIPv4BeforeGeolocation(t *testing.T) {
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		switch r.URL.Path {
		case "/exit":
			fmt.Fprint(w, `{"ip":"67.230.176.144"}`)
		case "/detail/67.230.176.144":
			fmt.Fprint(w, `{"ip":"67.230.176.144","country_code":"US","timezone":{"id":"America/Los_Angeles"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	got, err := resolveSunBrowserLocationWithFamily(context.Background(), server.Client(), server.URL+"/exit", []string{server.URL + "/detail/{ip}"}, []string{server.URL + "/fallback"})
	if err != nil || got.IPFamily != "ipv4" || got.IPSelection != "ipv4-first" || got.ObservedIPv4 != "67.230.176.144" || len(seen) != 2 {
		t.Fatalf("got %#v err=%v seen=%#v", got, err, seen)
	}
}

func TestSunBrowserEgressRejectsWrongIPAndLabelsDualStackFallback(t *testing.T) {
	for _, sourceUnavailable := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/exit":
				if sourceUnavailable {
					http.Error(w, "IPv4 unavailable", 503)
				} else {
					fmt.Fprint(w, `{"ip":"67.230.176.144"}`)
				}
			case strings.HasPrefix(r.URL.Path, "/detail/"):
				fmt.Fprint(w, `{"ip":"1.1.1.1","country_code":"US","timezone":"America/Chicago"}`)
			case r.URL.Path == "/fallback":
				fmt.Fprint(w, `{"ip":"2607:8700:5500:fbb4::2","country_code":"US","timezone":"America/Los_Angeles"}`)
			}
		}))
		got, err := resolveSunBrowserLocationWithFamily(context.Background(), server.Client(), server.URL+"/exit", []string{server.URL + "/detail/{ip}"}, []string{server.URL + "/fallback"})
		server.Close()
		if err != nil || got.IPFamily != "ipv6" || got.IPSelection != "dual-stack-fallback" || got.Timezone != "America/Los_Angeles" {
			t.Fatalf("accepted wrong-IP detail or missing fallback %#v %v", got, err)
		}
		if !sourceUnavailable && got.ObservedIPv4 != "67.230.176.144" {
			t.Fatal("lost same-family observation")
		}
		if sourceUnavailable && got.ObservedIPv4 != "" {
			t.Fatal("invented IPv4 observation")
		}
	}
}

func TestSunBrowserEgressRetainsConfiguredProxyForAllRequests(t *testing.T) {
	hosts := []string{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.URL.Host)
		if r.URL.Host == "exit.test" {
			fmt.Fprint(w, `{"ip":"67.230.176.144"}`)
		} else {
			fmt.Fprint(w, `{"ip":"67.230.176.144","country_code":"US","timezone":"America/Los_Angeles"}`)
		}
	}))
	defer proxy.Close()
	client, _, err := proxyCoreHTTPClient(time.Second, proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	got, err := resolveSunBrowserLocationWithFamily(context.Background(), client, "http://exit.test/ip", []string{"http://geo.test/{ip}"}, nil)
	if err != nil || got.IPSelection != "ipv4-first" || len(hosts) != 2 || hosts[0] != "exit.test" || hosts[1] != "geo.test" {
		t.Fatalf("wrong proxy transport: %#v %v hosts=%v", got, err, hosts)
	}
}
