package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"
)

type sunBrowserAutoLocation struct {
	IP                string                  `json:"ip"`
	IPFamily          string                  `json:"ipFamily"`
	IPSelection       string                  `json:"ipSelection"`
	ObservedIPv4      string                  `json:"observedIPv4,omitempty"`
	ExitIPSource      string                  `json:"exitIPSource,omitempty"`
	CountryCode       string                  `json:"countryCode"`
	City              string                  `json:"city,omitempty"`
	Latitude          *float64                `json:"latitude,omitempty"`
	Longitude         *float64                `json:"longitude,omitempty"`
	Language          string                  `json:"language"`
	AcceptLanguage    string                  `json:"acceptLanguage"`
	Timezone          string                  `json:"timezone"`
	Source            string                  `json:"source"`
	LanguageSource    string                  `json:"languageSource"`
	State             string                  `json:"state"`
	DebugPort         int                     `json:"debugPort"`
	Window            *sunBrowserWindowBounds `json:"window,omitempty"`
	FingerprintPolicy string                  `json:"fingerprintPolicy"`
}

// A country is not a language preference. These are explicit regional defaults,
// not a claim that IP geolocation can determine a person's preferred language.
var sunBrowserCountryLanguage = map[string]string{
	"US": "en-US", "GB": "en-GB", "AU": "en-AU", "NZ": "en-NZ", "CA": "en-CA", "IE": "en-IE",
	"CN": "zh-CN", "TW": "zh-TW", "HK": "zh-HK", "MO": "zh-MO", "JP": "ja-JP", "KR": "ko-KR",
	"DE": "de-DE", "AT": "de-AT", "CH": "de-CH", "FR": "fr-FR", "BE": "nl-BE", "NL": "nl-NL",
	"ES": "es-ES", "MX": "es-MX", "AR": "es-AR", "CL": "es-CL", "CO": "es-CO", "PE": "es-PE",
	"PT": "pt-PT", "BR": "pt-BR", "IT": "it-IT", "PL": "pl-PL", "CZ": "cs-CZ", "SK": "sk-SK",
	"SE": "sv-SE", "NO": "nb-NO", "DK": "da-DK", "FI": "fi-FI", "IS": "is-IS", "GR": "el-GR",
	"HU": "hu-HU", "RO": "ro-RO", "BG": "bg-BG", "HR": "hr-HR", "SI": "sl-SI", "LT": "lt-LT", "LV": "lv-LV", "EE": "et-EE",
	"RU": "ru-RU", "UA": "uk-UA", "TR": "tr-TR", "IL": "he-IL", "SA": "ar-SA", "AE": "ar-AE",
	"IN": "en-IN", "SG": "en-SG", "MY": "ms-MY", "ID": "id-ID", "TH": "th-TH", "VN": "vi-VN", "PH": "en-PH",
	"ZA": "en-ZA", "NG": "en-NG", "KE": "en-KE", "EG": "ar-EG", "PK": "ur-PK", "BD": "bn-BD",
}

func parseSunBrowserAutoLocation(reader io.Reader, source string) (sunBrowserAutoLocation, error) {
	var raw struct {
		IP          string          `json:"ip"`
		CountryCode string          `json:"country_code"`
		City        string          `json:"city"`
		Latitude    *float64        `json:"latitude"`
		Longitude   *float64        `json:"longitude"`
		Success     *bool           `json:"success"`
		Error       bool            `json:"error"`
		Timezone    json.RawMessage `json:"timezone"`
	}
	if err := json.NewDecoder(io.LimitReader(reader, 256*1024)).Decode(&raw); err != nil {
		return sunBrowserAutoLocation{}, fmt.Errorf("位置服务响应格式错误")
	}
	if raw.Error || (raw.Success != nil && !*raw.Success) || net.ParseIP(raw.IP) == nil {
		return sunBrowserAutoLocation{}, fmt.Errorf("位置服务未返回有效出口 IP")
	}
	var tz string
	if err := json.Unmarshal(raw.Timezone, &tz); err != nil {
		var detail struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw.Timezone, &detail)
		tz = detail.ID
	}
	if tz == "" {
		return sunBrowserAutoLocation{}, fmt.Errorf("位置服务未返回 IANA 时区")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return sunBrowserAutoLocation{}, fmt.Errorf("位置服务返回未知时区")
	}
	cc := strings.ToUpper(strings.TrimSpace(raw.CountryCode))
	lang, ok := sunBrowserCountryLanguage[cc]
	if !ok {
		return sunBrowserAutoLocation{}, fmt.Errorf("尚未配置国家 %q 的默认语言", cc)
	}
	primary, _, _ := strings.Cut(lang, "-")
	location := sunBrowserAutoLocation{IP: raw.IP, IPFamily: sunBrowserIPFamily(raw.IP), CountryCode: cc, City: raw.City, Timezone: tz, Language: lang, AcceptLanguage: lang + "," + primary, Source: source, LanguageSource: "country-default", State: "pending", FingerprintPolicy: "native-unmapped"}
	if raw.Latitude != nil && raw.Longitude != nil && !math.IsNaN(*raw.Latitude) && !math.IsNaN(*raw.Longitude) && math.Abs(*raw.Latitude) <= 90 && math.Abs(*raw.Longitude) <= 180 {
		location.Latitude, location.Longitude = raw.Latitude, raw.Longitude
	}
	return location, nil
}

func sunBrowserIPFamily(raw string) string {
	ip := net.ParseIP(raw)
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

func lookupSunBrowserAutoLocation(ctx context.Context, client *http.Client, endpoints []string) (sunBrowserAutoLocation, error) {
	return lookupSunBrowserAutoLocationForIP(ctx, client, endpoints, "")
}

func lookupSunBrowserAutoLocationForIP(ctx context.Context, client *http.Client, endpoints []string, expectedIP string) (sunBrowserAutoLocation, error) {
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var location sunBrowserAutoLocation
		if resp.StatusCode == http.StatusOK {
			location, err = parseSunBrowserAutoLocation(resp.Body, endpoint)
		} else {
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		resp.Body.Close()
		if err == nil && (expectedIP == "" || net.ParseIP(location.IP).Equal(net.ParseIP(expectedIP))) {
			return location, nil
		}
	}
	return sunBrowserAutoLocation{}, fmt.Errorf("出口位置探测失败；保留代理路径，不回退直连，请检查网络后重试")
}

func resolveSunBrowserAutoLocation(effectiveProxy string) (sunBrowserAutoLocation, error) {
	client, _, err := proxyCoreHTTPClient(5*time.Second, effectiveProxy)
	if err != nil {
		return sunBrowserAutoLocation{}, fmt.Errorf("自动位置代理配置无效")
	}
	defer client.CloseIdleConnections()
	// Never follow a redirect to an unencrypted/different geolocation endpoint.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return resolveSunBrowserLocationWithFamily(ctx, client, "https://api.ipify.org?format=json", []string{"https://ipwho.is/{ip}", "https://ipapi.co/{ip}/json/"}, []string{"https://ipwho.is/", "https://ipapi.co/json/"})
}

func replaceSunBrowserAutoArgs(args []string, location sunBrowserAutoLocation) []string {
	out := make([]string, 0, len(args)+3)
	out = append(out, "--lang="+location.Language, "--accept-lang="+location.AcceptLanguage, "--timezone="+location.Timezone)
	for i := 0; i < len(args); i++ {
		key, _, inline := strings.Cut(args[i], "=")
		switch key {
		case "--restore-last-session", "--start-maximized", "--start-fullscreen", "--kiosk":
			// Auto mode uses the measured native frame and initializes locale on blank.
			continue
		case "--lang", "--accept-lang", "--timezone":
			if !inline && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
			}
		default:
			out = append(out, args[i])
		}
	}
	return out
}
