package backend

import "testing"

func TestFingerprintLaunchProxyMode(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "unknown"},
		{[]string{"--no-proxy-server"}, "direct"},
		{[]string{"--proxy-server=socks5://127.0.0.1:1234"}, "proxy"},
		{[]string{"--proxy-pac-url=https://example.test/proxy.pac"}, "proxy"},
		{[]string{"--no-proxy-server", "--proxy-server=http://localhost:1"}, "unknown"},
	} {
		if got := fingerprintLaunchProxyMode(tc.args); got != tc.want {
			t.Fatalf("%v: got %q want %q", tc.args, got, tc.want)
		}
	}
}
