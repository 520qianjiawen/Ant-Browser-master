package gdrive

import (
	"net/url"
	"testing"
)

func TestBuildAuthURL(t *testing.T) {
	clientID := "test-client-id"
	redirectURI := "http://127.0.0.1:8089/oauth/callback"

	authURL := BuildAuthURL(clientID, redirectURI)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Failed to parse generated auth URL: %v", err)
	}

	q := parsed.Query()
	if q.Get("client_id") != clientID {
		t.Errorf("Expected client_id %s, got %s", clientID, q.Get("client_id"))
	}
	if q.Get("redirect_uri") != redirectURI {
		t.Errorf("Expected redirect_uri %s, got %s", redirectURI, q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("Expected response_type code, got %s", q.Get("response_type"))
	}
	if q.Get("scope") != DriveScope {
		t.Errorf("Expected scope %s, got %s", DriveScope, q.Get("scope"))
	}
}

func TestNewClientDefaults(t *testing.T) {
	cfg := Config{
		ClientID: "cid",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client.config.FolderName != DefaultFolderName {
		t.Errorf("Expected default folder name %s, got %s", DefaultFolderName, client.config.FolderName)
	}
	if client.ID() != "gdrive" {
		t.Errorf("Expected client ID 'gdrive', got %s", client.ID())
	}
}

func TestFormatGDriveAPIError(t *testing.T) {
	htmlErr := []byte("<!DOCTYPE html><html><title>Error 400 (Bad Request)!1</title>Your client has issued a malformed or illegal request.</html>")
	err := formatGDriveAPIError("读取文件列表", 400, htmlErr)
	if err == nil || !urlQueryContains(err.Error(), "malformed request") {
		t.Errorf("Expected sanitized error message, got: %v", err)
	}
}

func urlQueryContains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (s == sub || len(s) >= len(sub) && (s[:len(sub)] == sub || urlQueryContains(s[1:], sub)))
}
