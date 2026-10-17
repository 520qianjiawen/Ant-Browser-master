package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClearSessionRestoreData(t *testing.T) {
	tempDir := t.TempDir()
	defaultDir := filepath.Join(tempDir, "Default")
	sessionsDir := filepath.Join(defaultDir, "Sessions")
	swDir := filepath.Join(defaultDir, "Service Worker", "ScriptCache")

	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions failed: %v", err)
	}
	if err := os.MkdirAll(swDir, 0o755); err != nil {
		t.Fatalf("mkdir service worker failed: %v", err)
	}

	// Create dummy session files and SW cache files
	_ = os.WriteFile(filepath.Join(sessionsDir, "Session_12345"), []byte("dummy session"), 0o644)
	_ = os.WriteFile(filepath.Join(sessionsDir, "Tabs_12345"), []byte("dummy tabs"), 0o644)
	_ = os.WriteFile(filepath.Join(defaultDir, "Current Session"), []byte("dummy legacy"), 0o644)
	_ = os.WriteFile(filepath.Join(swDir, "cached_script_0"), []byte("dummy sw"), 0o644)

	// Create dummy Preferences with crashed status and session restore
	prefData := map[string]any{
		"profile": map[string]any{
			"exit_type":      "Crashed",
			"exited_cleanly": false,
		},
		"sessions": map[string]any{
			"restore_browser": true,
			"event_log": []any{
				map[string]any{"crashed": true},
			},
		},
		"session": map[string]any{
			"restore_on_startup": 5,
		},
	}
	prefBytes, _ := json.Marshal(prefData)
	_ = os.WriteFile(filepath.Join(defaultDir, "Preferences"), prefBytes, 0o644)

	// Run ClearSessionRestoreData
	if err := ClearSessionRestoreData(tempDir); err != nil {
		t.Fatalf("ClearSessionRestoreData failed: %v", err)
	}

	// Verify Sessions dir is recreated and empty
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Fatalf("read sessions dir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected Sessions dir to be empty, got %d files", len(entries))
	}

	// Verify legacy session file is removed
	if _, err := os.Stat(filepath.Join(defaultDir, "Current Session")); !os.IsNotExist(err) {
		t.Errorf("expected Current Session to be deleted")
	}

	// Verify Service Worker dir is preserved
	if _, err := os.Stat(filepath.Join(defaultDir, "Service Worker")); os.IsNotExist(err) {
		t.Errorf("expected Service Worker directory to be preserved, but was deleted")
	}

	// Verify Preferences are reset
	updatedPrefBytes, err := os.ReadFile(filepath.Join(defaultDir, "Preferences"))
	if err != nil {
		t.Fatalf("read updated preferences failed: %v", err)
	}
	var updatedPref map[string]any
	if err := json.Unmarshal(updatedPrefBytes, &updatedPref); err != nil {
		t.Fatalf("unmarshal updated preferences failed: %v", err)
	}

	profileObj, _ := updatedPref["profile"].(map[string]any)
	if profileObj["exit_type"] != "Normal" {
		t.Errorf("expected exit_type 'Normal', got %v", profileObj["exit_type"])
	}
	if profileObj["exited_cleanly"] != true {
		t.Errorf("expected exited_cleanly true, got %v", profileObj["exited_cleanly"])
	}

	if sessionsObj, ok := updatedPref["sessions"].(map[string]any); ok {
		if _, has := sessionsObj["restore_browser"]; has {
			t.Errorf("expected restore_browser to be removed from sessions")
		}
		if _, has := sessionsObj["event_log"]; has {
			t.Errorf("expected event_log to be removed from sessions")
		}
	}

	if sessionObj, ok := updatedPref["session"].(map[string]any); ok {
		if _, has := sessionObj["restore_on_startup"]; has {
			t.Errorf("expected restore_on_startup to be removed from session")
		}
	}

	// Test idempotency
	if err := ClearSessionRestoreData(tempDir); err != nil {
		t.Fatalf("second call to ClearSessionRestoreData failed: %v", err)
	}
}
