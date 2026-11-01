package backend

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Discover IPv4 through the same final proxy transport used for geolocation.
// Do not force tcp4 at the proxy socket: that would not select the tunneled
// destination's address family and would break IPv6-only proxy listeners.
func sunBrowserObservedIPv4(ctx context.Context, client *http.Client, endpoint string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	response, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var raw struct {
		IP string `json:"ip"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&raw) != nil {
		return ""
	}
	ip := net.ParseIP(strings.TrimSpace(raw.IP))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.To4().String()
}

func resolveSunBrowserLocationWithFamily(ctx context.Context, client *http.Client, ipEndpoint string, detailTemplates, fallbackEndpoints []string) (sunBrowserAutoLocation, error) {
	ipv4 := sunBrowserObservedIPv4(ctx, client, ipEndpoint)
	if ipv4 != "" {
		details := make([]string, 0, len(detailTemplates))
		for _, template := range detailTemplates {
			details = append(details, strings.ReplaceAll(template, "{ip}", ipv4))
		}
		if location, err := lookupSunBrowserAutoLocationForIP(ctx, client, details, ipv4); err == nil {
			location.IPSelection = "ipv4-first"
			location.ObservedIPv4 = ipv4
			location.ExitIPSource = ipEndpoint
			return location, nil
		}
	}
	// IPv4 discovery/geolocation may be unavailable on IPv6-only networks. Keep
	// the proxy unchanged, label the fallback, and never invent matching IPs.
	location, err := lookupSunBrowserAutoLocation(ctx, client, fallbackEndpoints)
	if err != nil {
		return sunBrowserAutoLocation{}, err
	}
	location.IPSelection = "dual-stack-fallback"
	location.ObservedIPv4 = ipv4
	if ipv4 != "" {
		location.ExitIPSource = ipEndpoint
	}
	return location, nil
}
