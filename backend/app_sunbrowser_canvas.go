package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// The alphabet, CanvasMark, and AudioFp transport were recovered from the
// installed AdsPower bundle. Only this exact SunBrowser build has been tested here.
const sunCanvasVerifiedVersion = "152.0.7977.54"

var sunCanvasEncoding = base64.NewEncoding("hTy1bfRJz4nLPcBCO7WtmNIaGvVeul5Zo8kq32UxrYw_-0gsjp96SDFXQiEMKdHA")

type sunExtendedPayload struct {
	CanvasMark string `json:"CanvasMark,omitempty"`
	AudioFp    string `json:"AudioFp,omitempty"`
}

type sunNativeRequestedState struct {
	Canvas bool
	Audio  bool
}

func sunCanvasMarkForProfile(profileID string) string {
	sum := sha256.Sum256([]byte("ant-sunbrowser-canvas-v1:" + profileID))
	return hex.EncodeToString(sum[:16])
}

func sunAudioFpForProfile(profileID string) string {
	sum := sha256.Sum256([]byte("ant-sunbrowser-audio-v1:" + profileID))
	return hex.EncodeToString(sum[:16])
}

func sunSpoofingDisabledFor(args []string, target string) bool {
	for _, arg := range args {
		key, value, hasValue := strings.Cut(strings.TrimSpace(arg), "=")
		if !strings.EqualFold(key, "--disable-spoofing") {
			continue
		}
		if !hasValue || strings.TrimSpace(value) == "" {
			return true
		}
		for _, item := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(item)
			if strings.EqualFold(trimmed, target) || strings.EqualFold(trimmed, "all") {
				return true
			}
		}
	}
	return false
}

func sunCanvasRequested(args []string) bool {
	if sunSpoofingDisabledFor(args, "canvas") {
		return false
	}
	requested := false
	for _, arg := range args {
		key, value, hasValue := strings.Cut(strings.TrimSpace(arg), "=")
		switch strings.ToLower(key) {
		case "--fingerprinting-canvas-image-data-noise", "--fingerprint-canvas-noise":
			requested = !hasValue || strings.EqualFold(value, "true") || value == "1"
		}
	}
	return requested
}

// sunAudioRequested enables native AudioFp by default unless spoofing is
// disabled for audio/all or --fingerprint-audio-noise is explicitly disabled.
func sunAudioRequested(args []string) bool {
	if sunSpoofingDisabledFor(args, "audio") {
		return false
	}
	requested := true
	for _, arg := range args {
		key, value, hasValue := strings.Cut(strings.TrimSpace(arg), "=")
		if strings.ToLower(key) == "--fingerprint-audio-noise" {
			if hasValue {
				requested = strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes") || strings.EqualFold(value, "on")
			} else {
				requested = true
			}
		}
	}
	return requested
}

func sunExtendedAddLaunchArg(args, rawArgs []string, profileID, version string) ([]string, bool) {
	if version != sunCanvasVerifiedVersion || strings.TrimSpace(profileID) == "" {
		return args, false
	}
	for _, arg := range args {
		if browserFingerprintArgKey(arg) == "--extended-parameters" {
			return args, false // Preserve an independently supplied native configuration.
		}
	}
	var payload sunExtendedPayload
	if sunCanvasRequested(rawArgs) {
		payload.CanvasMark = sunCanvasMarkForProfile(profileID)
	}
	if sunAudioRequested(rawArgs) {
		payload.AudioFp = sunAudioFpForProfile(profileID)
	}
	if payload.CanvasMark == "" && payload.AudioFp == "" {
		return args, false
	}
	encoded, _ := json.Marshal(payload)
	return append(append([]string{}, args...), "--extended-parameters="+sunCanvasEncoding.EncodeToString(encoded)), true
}

func sunExtendedWasRequested(args []string, profileID, version string) sunNativeRequestedState {
	if version != sunCanvasVerifiedVersion || strings.TrimSpace(profileID) == "" {
		return sunNativeRequestedState{}
	}
	expectedCanvas := sunCanvasMarkForProfile(profileID)
	expectedAudio := sunAudioFpForProfile(profileID)
	var state sunNativeRequestedState
	for _, arg := range args {
		key, value, ok := strings.Cut(strings.TrimSpace(arg), "=")
		if !ok || key != "--extended-parameters" {
			continue
		}
		data, err := sunCanvasEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		var payload sunExtendedPayload
		if json.Unmarshal(data, &payload) != nil {
			continue
		}
		if payload.CanvasMark == expectedCanvas {
			state.Canvas = true
		}
		if payload.AudioFp == expectedAudio {
			state.Audio = true
		}
	}
	return state
}

func sunCanvasWasRequested(args []string, profileID, version string) bool {
	return sunExtendedWasRequested(args, profileID, version).Canvas
}

func sunAudioWasRequested(args []string, profileID, version string) bool {
	return sunExtendedWasRequested(args, profileID, version).Audio
}
