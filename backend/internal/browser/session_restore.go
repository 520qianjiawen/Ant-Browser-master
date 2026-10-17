package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var sessionRestoreLegacyFiles = []string{
	"Current Session",
	"Current Tabs",
	"Last Session",
	"Last Tabs",
}

// ClearSessionRestoreData 删除 Chromium 用于恢复上次标签页的会话文件，
// 并重置 Preferences 中的崩溃与恢复标记，保留 cookies、Local Storage 与 Service Worker 等用户数据不变。
func ClearSessionRestoreData(userDataDir string) error {
	rootDir := strings.TrimSpace(userDataDir)
	if rootDir == "" {
		return fmt.Errorf("user data dir is empty")
	}

	profileDir := filepath.Join(rootDir, "Default")
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat profile dir: %w", err)
	}

	sessionsDir := filepath.Join(profileDir, "Sessions")
	var errs []error

	if _, statErr := os.Stat(sessionsDir); statErr == nil {
		if err := os.RemoveAll(sessionsDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove sessions dir: %w", err))
		} else if err == nil {
			if mkErr := os.MkdirAll(sessionsDir, 0o755); mkErr != nil {
				errs = append(errs, fmt.Errorf("recreate sessions dir: %w", mkErr))
			}
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		errs = append(errs, fmt.Errorf("stat sessions dir: %w", statErr))
	}

	for _, name := range sessionRestoreLegacyFiles {
		if err := os.Remove(filepath.Join(profileDir, name)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", name, err))
		}
	}

	if err := ResetProfileSessionPreferences(profileDir); err != nil {
		errs = append(errs, fmt.Errorf("reset session preferences: %w", err))
	}

	return errors.Join(errs...)
}

// ResetProfileSessionPreferences 重置 profile 偏好设置中的崩溃退出与会话自愈配置，防止 Chromium 重启时因非正常退出强行恢复旧标签页
func ResetProfileSessionPreferences(profileDir string) error {
	prefPath := filepath.Join(profileDir, "Preferences")
	data, err := os.ReadFile(prefPath)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read preferences: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("unmarshal preferences: %w", err)
	}

	modified := false

	profileObj, _ := root["profile"].(map[string]any)
	if profileObj == nil {
		profileObj = make(map[string]any)
		root["profile"] = profileObj
		modified = true
	}
	if profileObj["exit_type"] != "Normal" {
		profileObj["exit_type"] = "Normal"
		modified = true
	}
	if profileObj["exited_cleanly"] != true {
		profileObj["exited_cleanly"] = true
		modified = true
	}

	if sessionsObj, ok := root["sessions"].(map[string]any); ok && sessionsObj != nil {
		if _, has := sessionsObj["event_log"]; has {
			delete(sessionsObj, "event_log")
			modified = true
		}
		if _, has := sessionsObj["restore_browser"]; has {
			delete(sessionsObj, "restore_browser")
			modified = true
		}
	}

	if sessionObj, ok := root["session"].(map[string]any); ok && sessionObj != nil {
		if _, has := sessionObj["restore_on_startup"]; has {
			delete(sessionObj, "restore_on_startup")
			modified = true
		}
	}

	extensionsObj, _ := root["extensions"].(map[string]any)
	if extensionsObj == nil {
		extensionsObj = make(map[string]any)
		root["extensions"] = extensionsObj
		modified = true
	}
	uiObj, _ := extensionsObj["ui"].(map[string]any)
	if uiObj == nil {
		uiObj = make(map[string]any)
		extensionsObj["ui"] = uiObj
		modified = true
	}
	if uiObj["developer_mode"] != true {
		uiObj["developer_mode"] = true
		modified = true
	}

	if !modified {
		return nil
	}

	updatedData, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}

	if err := os.WriteFile(prefPath, updatedData, 0o644); err != nil {
		return fmt.Errorf("write preferences: %w", err)
	}

	return nil
}
