package backend

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSunBrowserAPISystemMapping(t *testing.T) {
	cases := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", "arm64"},
		{"darwin", "amd64", "mac"},
		{"linux", "amd64", "linux_x64"},
		{"windows", "amd64", "windows_64"},
		{"windows", "386", "windows_32"},
		{"freebsd", "amd64", ""},
	}
	for _, tc := range cases {
		got := sunBrowserAPISystem(tc.goos, tc.goarch)
		if got != tc.want {
			t.Errorf("sunBrowserAPISystem(%q, %q) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestVerifyCoreArchiveMD5(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.zip")
	content := []byte("ant-browser-sunbrowser-kernel-test-bytes")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	h := md5.New()
	h.Write(content)
	expectedMD5 := hex.EncodeToString(h.Sum(nil))

	match, actual, err := verifySunBrowserKernelMD5(filePath, expectedMD5)
	if err != nil || !match || actual != expectedMD5 {
		t.Fatalf("expected match, got match=%v actual=%s err=%v", match, actual, err)
	}

	match, actual, err = verifySunBrowserKernelMD5(filePath, "00000000000000000000000000000000")
	if err != nil || match {
		t.Fatalf("expected mismatch, got match=%v actual=%s err=%v", match, actual, err)
	}
}

func TestSunBrowserKernelDownloadGuard(t *testing.T) {
	app := &App{}
	err := app.sunBrowserKernelDownloadGuard(SunBrowserKernelInfoResult{
		Available: false,
		Message:   "network error",
	})
	if err == nil {
		t.Fatal("expected error when kernel not available")
	}

	err = app.sunBrowserKernelDownloadGuard(SunBrowserKernelInfoResult{
		Available: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSunBrowserKernelInfoMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 0,
			"msg": "Success",
			"data": {
				"download_url": "https://version.adspower.net/software/browsers/chrome/20260831/SunBrowser-merge-152-20260831.zip",
				"version": "20260831",
				"file_md5": "AF15895D142EC457E72EA3013B6A4B56",
				"type": "chrome",
				"kernel": "152",
				"system": "arm64",
				"size": "341.53MB"
			}
		}`))
	}))
	defer server.Close()

	req, err := newSunBrowserKernelRequest(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
