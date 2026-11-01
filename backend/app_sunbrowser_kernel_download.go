package backend

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"time"
)

// AdsPower 分发 SunBrowser 内核的公开元数据接口。Ant 只读取最新版本号、
// 下载地址和官方 MD5；下载后的内核属于用户本机资产，AdsPower 不参与后续使用。
const (
	sunBrowserKernelAPI     = "https://api.adspower.net/client/browser/get-browser-version"
	sunBrowserKernelType    = "chrome"
	sunBrowserKernelVersion = "152"
)

type SunBrowserKernelInfoResult struct {
	Available   bool   `json:"available"`
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	FileMD5     string `json:"fileMd5"`
	SizeMB      string `json:"sizeMb"`
	System      string `json:"system"`
	CoreName    string `json:"coreName"`
	Message     string `json:"message"`
}

type sunBrowserKernelPayload struct {
	Data struct {
		DownloadURL string `json:"download_url"`
		Version     string `json:"version"`
		FileMD5     string `json:"file_md5"`
		Type        string `json:"type"`
		Kernel      string `json:"kernel"`
		System      string `json:"system"`
		Size        string `json:"size"`
	} `json:"data"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// sunBrowserAPISystem maps the running platform to AdsPower's system codes.
// Verified against their client: darwin arm64 -> "arm64", other mac -> "mac",
// linux -> "linux_x64", windows -> "windows_64"/"windows_32".
func sunBrowserAPISystem(goos, goarch string) string {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "arm64"
		}
		return "mac"
	case "linux":
		return "linux_x64"
	case "windows":
		if goarch == "386" {
			return "windows_32"
		}
		return "windows_64"
	default:
		return ""
	}
}

func sunBrowserDefaultCoreName() string {
	return "SunBrowser " + sunBrowserKernelVersion
}

// SunBrowserKernelInfo queries the latest kernel metadata through the same
// unified proxy chain used for other downloads. No download starts here.
func (a *App) SunBrowserKernelInfo(proxyConfig string) SunBrowserKernelInfoResult {
	system := sunBrowserAPISystem(goruntime.GOOS, goruntime.GOARCH)
	result := SunBrowserKernelInfoResult{System: system, CoreName: sunBrowserDefaultCoreName()}
	if system == "" {
		result.Message = "当前平台不受 SunBrowser 内核支持"
		return result
	}
	client, _, err := a.unifiedProxyCoreHTTPClient(20*time.Second, proxyConfig)
	if err != nil {
		result.Message = "创建查询代理失败: " + err.Error()
		return result
	}
	url := fmt.Sprintf("%s?type=%s&kernel=%s&system=%s&is_self_refresh=1", sunBrowserKernelAPI, sunBrowserKernelType, sunBrowserKernelVersion, system)
	req, err := newSunBrowserKernelRequest(url)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	resp, err := client.Do(req)
	if err != nil {
		result.Message = "查询内核版本失败: " + err.Error()
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		result.Message = fmt.Sprintf("内核版本接口返回 HTTP %d", resp.StatusCode)
		return result
	}
	var payload sunBrowserKernelPayload
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&payload); err != nil {
		result.Message = "内核版本响应解析失败"
		return result
	}
	if payload.Code != 0 || strings.TrimSpace(payload.Data.DownloadURL) == "" {
		result.Message = "该渠道暂未提供此内核版本"
		return result
	}
	result.Available = true
	result.Version = strings.TrimSpace(payload.Data.Version)
	result.DownloadURL = strings.TrimSpace(payload.Data.DownloadURL)
	result.FileMD5 = strings.ToUpper(strings.TrimSpace(payload.Data.FileMD5))
	result.SizeMB = strings.TrimSpace(payload.Data.Size)
	result.Message = "已获取最新内核信息"
	return result
}

func newSunBrowserKernelRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构建内核版本请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AntBrowser/1.8 kernel-fetch")
	return req, nil
}

func openForSunBrowserHash(path string) (*os.File, error) {
	return os.Open(path)
}

// verifySunBrowserKernelMD5 streams the archive and compares the official MD5
// before extraction. Empty expected hash skips verification with a warning.
func verifySunBrowserKernelMD5(path string, expectedMD5 string) (bool, string, error) {
	expectedMD5 = strings.ToUpper(strings.TrimSpace(expectedMD5))
	if expectedMD5 == "" {
		return false, "", nil
	}
	file, err := openForSunBrowserHash(path)
	if err != nil {
		return false, "", err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, "", err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	return actual == strings.ToLower(expectedMD5), actual, nil
}

func (a *App) sunBrowserKernelDownloadGuard(info SunBrowserKernelInfoResult) error {
	if !info.Available {
		return fmt.Errorf("尚未获取到可用内核: %s", info.Message)
	}
	return nil
}

func sunBrowserKernelDownloadContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

// SunBrowserKernelDownload runs the one-click flow: query latest metadata, then
// reuse the standard core download pipeline (multi-thread fetch, extract,
// register). MD5 is verified against the official hash before extraction.
func (a *App) SunBrowserKernelDownload(proxyConfig string) error {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	if a.browserMgr == nil {
		return fmt.Errorf("浏览器管理器未初始化")
	}
	info := a.SunBrowserKernelInfo(proxyConfig)
	if err := a.sunBrowserKernelDownloadGuard(info); err != nil {
		return err
	}
	coreName := info.CoreName
	existing, isRedownload := a.browserMgr.GetCoreByName(coreName)
	return a.startBackgroundTask(func(ctx context.Context) {
		if isRedownload {
			a.browserMgr.RedownloadCoreWithMD5(ctx, existing.CoreId, info.DownloadURL, proxyConfig, info.FileMD5)
		} else {
			a.browserMgr.DownloadAndExtractCoreWithMD5(ctx, coreName, info.DownloadURL, proxyConfig, info.FileMD5)
		}
	})
}
