package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	ClearCacheOnCloseFollow   = ""
	ClearCacheOnCloseEnabled  = "enabled"
	ClearCacheOnCloseDisabled = "disabled"
)

// NormalizeClearCacheOnCloseMode 规范化退出清理缓存模式
func NormalizeClearCacheOnCloseMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "follow", "default", "inherit":
		return ClearCacheOnCloseFollow
	case "on", "true", "enabled", "enable":
		return ClearCacheOnCloseEnabled
	case "off", "false", "disabled", "disable":
		return ClearCacheOnCloseDisabled
	default:
		return ClearCacheOnCloseFollow
	}
}

// ClearBrowserDiskCache 清理实例在运行过程中产生的所有缓存图片、多媒体、脚本字节码及着色器缓存文件，
// 严格保留 cookies、Local Storage、IndexedDB、扩展、历史记录与偏好设置等用户核心数据（保持登录态不变）。
func ClearBrowserDiskCache(userDataDir string) error {
	rootDir := strings.TrimSpace(userDataDir)
	if rootDir == "" {
		return fmt.Errorf("user data dir is empty")
	}

	// 待清理的目录相对路径列表（位于 userDataDir 内部）
	targetRelativeDirs := []string{
		filepath.Join("Default", "Cache"),
		filepath.Join("Default", "Code Cache"),
		filepath.Join("Default", "DawnGraphiteCache"),
		filepath.Join("Default", "DawnWebGPUCache"),
		filepath.Join("Default", "GPUCache"),
		filepath.Join("Default", "AutofillAiModelCache"),
		"GrShaderCache",
		"ShaderCache",
		"GraphiteDawnCache",
		filepath.Join("GPUPersistentCache", "GPUCache"),
		"component_crx_cache",
	}

	var errs []error

	for _, rel := range targetRelativeDirs {
		dirPath := filepath.Join(rootDir, rel)
		if stat, err := os.Stat(dirPath); err == nil && stat.IsDir() {
			if rmErr := os.RemoveAll(dirPath); rmErr != nil && !os.IsNotExist(rmErr) {
				errs = append(errs, fmt.Errorf("remove cache dir %s: %w", rel, rmErr))
			}
		}
	}

	// 在 macOS 下，Chromium 会将主 HTTP 缓存存放在 ~/Library/Caches/... 对应路径中
	if goruntime.GOOS == "darwin" {
		const appSupport = "/Library/Application Support/"
		if idx := strings.Index(rootDir, appSupport); idx != -1 {
			macCacheBase := rootDir[:idx] + "/Library/Caches/" + rootDir[idx+len(appSupport):]
			macTargetDirs := []string{
				filepath.Join(macCacheBase, "Default", "Cache"),
				filepath.Join(macCacheBase, "Default", "Code Cache"),
				filepath.Join(macCacheBase, "Cache"),
				filepath.Join(macCacheBase, "Code Cache"),
			}
			for _, mDir := range macTargetDirs {
				if stat, err := os.Stat(mDir); err == nil && stat.IsDir() {
					if rmErr := os.RemoveAll(mDir); rmErr != nil && !os.IsNotExist(rmErr) {
						errs = append(errs, fmt.Errorf("remove mac cache dir %s: %w", mDir, rmErr))
					}
				}
			}
		}
	}

	return errors.Join(errs...)
}
