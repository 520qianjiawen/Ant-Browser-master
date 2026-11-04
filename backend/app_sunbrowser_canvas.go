package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// The alphabet and CanvasMark transport were recovered from the installed
// AdsPower bundle. Only this exact SunBrowser build has been tested here.
const sunCanvasVerifiedVersion = "152.0.7977.54"

var sunCanvasEncoding = base64.NewEncoding("hTy1bfRJz4nLPcBCO7WtmNIaGvVeul5Zo8kq32UxrYw_-0gsjp96SDFXQiEMKdHA")

func sunCanvasRequested(args []string) bool {
	requested := false
	for _, arg := range args {
		key, value, hasValue := strings.Cut(strings.TrimSpace(arg), "=")
		switch strings.ToLower(key) {
		case "--disable-spoofing":
			if !hasValue || value == "" {
				return false
			}
			for _, item := range strings.Split(value, ",") {
				if strings.EqualFold(strings.TrimSpace(item), "canvas") || strings.EqualFold(strings.TrimSpace(item), "all") {
					return false
				}
			}
		case "--fingerprinting-canvas-image-data-noise", "--fingerprint-canvas-noise":
			requested = !hasValue || strings.EqualFold(value, "true") || value == "1"
		}
	}
	return requested
}

func sunCanvasAddLaunchArg(args, rawArgs []string, profileID, version string) ([]string, bool) {
	if version != sunCanvasVerifiedVersion || strings.TrimSpace(profileID) == "" || !sunCanvasRequested(rawArgs) {
		return args, false
	}
	for _, arg := range args {
		if browserFingerprintArgKey(arg) == "--extended-parameters" {
			return args, false // Preserve an independently supplied native configuration.
		}
	}
	sum := sha256.Sum256([]byte("ant-sunbrowser-canvas-v1:" + profileID))
	mark := hex.EncodeToString(sum[:16])
	payload, _ := json.Marshal(map[string]string{"CanvasMark": mark})
	return append(append([]string{}, args...), "--extended-parameters="+sunCanvasEncoding.EncodeToString(payload)), true
}

func sunCanvasWasRequested(args []string, profileID, version string) bool {
	if version != sunCanvasVerifiedVersion || strings.TrimSpace(profileID) == "" {
		return false
	}
	for _, arg := range args {
		key, value, ok := strings.Cut(strings.TrimSpace(arg), "=")
		if !ok || key != "--extended-parameters" {
			continue
		}
		data, err := sunCanvasEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		var payload struct {
			CanvasMark string `json:"CanvasMark"`
		}
		if json.Unmarshal(data, &payload) != nil {
			continue
		}
		sum := sha256.Sum256([]byte("ant-sunbrowser-canvas-v1:" + profileID))
		mark := hex.EncodeToString(sum[:16])
		if payload.CanvasMark == mark {
			return true
		}
	}
	return false
}
