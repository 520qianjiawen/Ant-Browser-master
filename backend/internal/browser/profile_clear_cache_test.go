package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeClearCacheOnCloseMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ClearCacheOnCloseFollow},
		{"follow", ClearCacheOnCloseFollow},
		{"default", ClearCacheOnCloseFollow},
		{"inherit", ClearCacheOnCloseFollow},
		{"enabled", ClearCacheOnCloseEnabled},
		{"enable", ClearCacheOnCloseEnabled},
		{"true", ClearCacheOnCloseEnabled},
		{"on", ClearCacheOnCloseEnabled},
		{"disabled", ClearCacheOnCloseDisabled},
		{"disable", ClearCacheOnCloseDisabled},
		{"false", ClearCacheOnCloseDisabled},
		{"off", ClearCacheOnCloseDisabled},
		{"unknown", ClearCacheOnCloseFollow},
	}

	for _, tt := range tests {
		got := NormalizeClearCacheOnCloseMode(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeClearCacheOnCloseMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClearBrowserDiskCache(t *testing.T) {
	tempDir := t.TempDir()

	// 创建测试目录结构：包含缓存目录与核心用户数据目录
	cacheDir := filepath.Join(tempDir, "Default", "Cache")
	codeCacheDir := filepath.Join(tempDir, "Default", "Code Cache")
	gpuCacheDir := filepath.Join(tempDir, "Default", "GPUCache")
	shaderCacheDir := filepath.Join(tempDir, "ShaderCache")

	// 用户核心数据（登录态/设置/扩展等），必须严格保留
	cookiesDir := filepath.Join(tempDir, "Default", "Network")
	localStorageDir := filepath.Join(tempDir, "Default", "Local Storage")

	for _, d := range []string{cacheDir, codeCacheDir, gpuCacheDir, shaderCacheDir, cookiesDir, localStorageDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", d, err)
		}
	}

	// 写入测试文件
	if err := os.WriteFile(filepath.Join(cacheDir, "data_0"), []byte("cached image data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cookiesDir, "Cookies"), []byte("auth cookie content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localStorageDir, "000001.ldb"), []byte("site local storage"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 执行清理
	if err := ClearBrowserDiskCache(tempDir); err != nil {
		t.Fatalf("ClearBrowserDiskCache failed: %v", err)
	}

	// 验证缓存目录已被清理
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("expected Cache dir to be removed, but it exists")
	}
	if _, err := os.Stat(codeCacheDir); !os.IsNotExist(err) {
		t.Errorf("expected Code Cache dir to be removed, but it exists")
	}
	if _, err := os.Stat(gpuCacheDir); !os.IsNotExist(err) {
		t.Errorf("expected GPUCache dir to be removed, but it exists")
	}
	if _, err := os.Stat(shaderCacheDir); !os.IsNotExist(err) {
		t.Errorf("expected ShaderCache dir to be removed, but it exists")
	}

	// 验证核心用户数据与登录 Cookie 保持完好
	if _, err := os.Stat(filepath.Join(cookiesDir, "Cookies")); err != nil {
		t.Errorf("expected Cookies file to be preserved, got error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localStorageDir, "000001.ldb")); err != nil {
		t.Errorf("expected Local Storage to be preserved, got error: %v", err)
	}
}
