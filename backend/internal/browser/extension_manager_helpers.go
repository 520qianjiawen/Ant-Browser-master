package browser

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func extractExtensionIDFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.ToLower(strings.TrimSpace(parts[i]))
		if extensionIDPattern.MatchString(candidate) {
			return candidate
		}
	}
	return ""
}

func downloadChromeExtensionCRX(ctx context.Context, extensionID string, client *http.Client) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: extensionDownloadTimeout}
	}
	downloadURL := BuildChromeExtensionDownloadURL(extensionID)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		data, err := downloadChromeExtensionCRXOnce(ctx, client, downloadURL)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !isRetryableExtensionDownloadError(err) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
		}
	}
	return nil, formatExtensionDownloadError(lastErr)
}

func downloadChromeExtensionCRXOnce(ctx context.Context, client *http.Client, downloadURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 AppleWebKit/537.36 Chrome/132.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "*/*")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("下载插件失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.StatusCode == http.StatusNoContent {
		return nil, fmt.Errorf("下载插件失败: HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, extensionMaxPackageBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("读取插件包失败: %w", err)
	}
	if len(data) > extensionMaxPackageBytes {
		return nil, fmt.Errorf("插件包超过限制")
	}
	return data, nil
}

func isRetryableExtensionDownloadError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "eof") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "temporarily unavailable")
}

func formatExtensionDownloadError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "eof") {
		return fmt.Errorf("下载插件失败: 连接在下载过程中提前关闭（EOF），已重试 3 次仍失败；请换一个下载代理节点或稍后重试")
	}
	if strings.Contains(message, "connectex") || strings.Contains(message, "dial tcp") || strings.Contains(message, "i/o timeout") {
		return fmt.Errorf("下载插件失败: 无法连接 Chrome 插件下载服务，请确认网络或下载代理可访问 clients2.google.com: %w", err)
	}
	return err
}

func normalizeExtensionArchiveData(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return data, nil
	}
	zipOffset := bytes.Index(data, []byte("PK\x03\x04"))
	if zipOffset < 0 {
		return nil, fmt.Errorf("插件包不是有效的 CRX/ZIP 文件")
	}
	return data[zipOffset:], nil
}

func readExtensionManifestFromZip(data []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("打开插件包失败: %w", err)
	}
	for _, file := range reader.File {
		if normalizeZipEntryPath(file.Name) == "manifest.json" {
			return readZipFile(file, 2<<20)
		}
	}
	return nil, fmt.Errorf("插件包缺少 manifest.json")
}

func parseExtensionManifest(data []byte) (extensionManifest, error) {
	var manifest extensionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("解析 manifest.json 失败: %w", err)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return manifest, fmt.Errorf("manifest.json 缺少 version")
	}
	return manifest, nil
}

func readExtensionLocaleMessagesFromZip(data []byte, manifest extensionManifest) map[string]string {
	locale := resolveExtensionLocale(manifest)
	if locale == "" {
		return nil
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.EqualFold(normalizeZipEntryPath(file.Name), "_locales/"+locale+"/messages.json") {
			content, err := readZipFile(file, 1<<20)
			if err != nil {
				return nil
			}
			return parseExtensionLocaleMessages(content)
		}
	}
	return nil
}

func readExtensionLocaleMessagesFromDir(sourceDir string, manifest extensionManifest) map[string]string {
	locale := resolveExtensionLocale(manifest)
	if locale == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(sourceDir, "_locales", locale, "messages.json"))
	if err != nil {
		return nil
	}
	return parseExtensionLocaleMessages(data)
}

func parseExtensionLocaleMessages(data []byte) map[string]string {
	var raw map[string]struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	messages := make(map[string]string, len(raw))
	for key, value := range raw {
		if key = strings.TrimSpace(key); key != "" {
			messages[key] = strings.TrimSpace(value.Message)
		}
	}
	return messages
}

func resolveExtensionLocale(manifest extensionManifest) string {
	locale := strings.TrimSpace(manifest.DefaultLocale)
	locale = strings.Trim(locale, "/\\. ")
	if locale == "" || strings.Contains(locale, "/") || strings.Contains(locale, "\\") {
		return ""
	}
	return locale
}

func resolveExtensionDescription(manifest extensionManifest, messages map[string]string) string {
	return resolveExtensionMessage(strings.TrimSpace(manifest.Description), messages)
}

func resolveExtensionMessage(value string, messages map[string]string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "__MSG_") || !strings.HasSuffix(trimmed, "__") {
		return trimmed
	}
	key := strings.TrimSuffix(strings.TrimPrefix(trimmed, "__MSG_"), "__")
	if message := strings.TrimSpace(messages[key]); message != "" {
		return message
	}
	return trimmed
}

func readExtensionIconDataURLFromZip(data []byte, manifest extensionManifest) string {
	iconPath := resolveExtensionIconPath(manifest)
	if iconPath == "" {
		return ""
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.EqualFold(normalizeZipEntryPath(file.Name), iconPath) {
			content, err := readZipFile(file, 1<<20)
			if err != nil {
				return ""
			}
			return extensionIconDataURL(iconPath, content)
		}
	}
	return ""
}

func readExtensionIconDataURLFromDir(sourceDir string, manifest extensionManifest) string {
	iconPath := resolveExtensionIconPath(manifest)
	if iconPath == "" {
		return ""
	}
	fullPath := filepath.Join(sourceDir, filepath.FromSlash(iconPath))
	content, err := os.ReadFile(fullPath)
	if err != nil || len(content) > 1<<20 {
		return ""
	}
	return extensionIconDataURL(iconPath, content)
}

func resolveExtensionIconPath(manifest extensionManifest) string {
	for _, candidate := range []map[string]any{manifest.Action, manifest.BrowserAction} {
		if path := mapStringValue(candidate, "default_icon"); path != "" {
			return normalizeExtensionAssetPath(path)
		}
	}
	bestSize := -1
	bestPath := ""
	for size, path := range manifest.Icons {
		if normalizedPath := normalizeExtensionAssetPath(path); normalizedPath != "" {
			parsedSize := parseExtensionIconSize(size)
			if parsedSize > bestSize {
				bestSize = parsedSize
				bestPath = normalizedPath
			}
		}
	}
	return bestPath
}

func mapStringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return value
	}
	if nested, ok := values[key].(map[string]any); ok {
		bestSize := -1
		bestPath := ""
		for size, rawPath := range nested {
			path, ok := rawPath.(string)
			if !ok {
				continue
			}
			parsedSize := parseExtensionIconSize(size)
			if parsedSize > bestSize {
				bestSize = parsedSize
				bestPath = path
			}
		}
		return bestPath
	}
	return ""
}

func normalizeExtensionAssetPath(value string) string {
	path := strings.TrimSpace(filepath.ToSlash(value))
	path = strings.TrimLeft(path, "/")
	if path == "" || strings.Contains(path, "..") || filepath.IsAbs(path) {
		return ""
	}
	return path
}

func parseExtensionIconSize(value string) int {
	var size int
	_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &size)
	return size
}

func extensionIconDataURL(path string, data []byte) string {
	if len(data) == 0 || len(data) > 1<<20 {
		return ""
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return ""
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func replaceExtensionDirFromZip(data []byte, installDir string) error {
	tmpDir := installDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("创建插件目录失败: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("打开插件包失败: %w", err)
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		relativePath := normalizeZipEntryPath(file.Name)
		if relativePath == "" {
			continue
		}
		targetPath := filepath.Join(tmpDir, filepath.FromSlash(relativePath))
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(tmpDir)+string(os.PathSeparator)) {
			return fmt.Errorf("插件包包含非法路径: %s", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("创建插件文件目录失败: %w", err)
		}
		content, err := readZipFile(file, extensionMaxPackageBytes)
		if err != nil {
			return err
		}
		if err := os.WriteFile(targetPath, content, 0o644); err != nil {
			return fmt.Errorf("写入插件文件失败: %w", err)
		}
	}
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("清理旧插件失败: %w", err)
	}
	if err := os.Rename(tmpDir, installDir); err != nil {
		return fmt.Errorf("安装插件失败: %w", err)
	}
	success = true
	return nil
}

func copyExtensionDirectory(sourceDir string, installDir string) error {
	tmpDir := installDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("创建插件目录失败: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	sourceClean, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	if err := filepath.WalkDir(sourceClean, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("插件目录包含符号链接: %s", path)
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > extensionMaxPackageBytes {
			return fmt.Errorf("插件文件过大: %s", path)
		}
		relativePath, err := filepath.Rel(sourceClean, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(tmpDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	}); err != nil {
		return fmt.Errorf("复制插件目录失败: %w", err)
	}
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("清理旧插件失败: %w", err)
	}
	if err := os.Rename(tmpDir, installDir); err != nil {
		return fmt.Errorf("安装插件失败: %w", err)
	}
	success = true
	return nil
}

func normalizeZipEntryPath(value string) string {
	path := strings.TrimSpace(filepath.ToSlash(value))
	path = strings.TrimLeft(path, "/")
	if path == "" || strings.Contains(path, "..") || filepath.IsAbs(path) {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) > 1 && parts[0] != "" && parts[1] == "manifest.json" {
		return strings.Join(parts[1:], "/")
	}
	return path
}

func readZipFile(file *zip.File, limit int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("读取插件文件失败: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取插件文件失败: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("插件文件过大: %s", file.Name)
	}
	return data, nil
}

func extensionIDFromManifest(manifestData []byte) string {
	sum := sha256.Sum256(manifestData)
	hexValue := hex.EncodeToString(sum[:16])
	var builder strings.Builder
	for _, char := range hexValue {
		if char >= '0' && char <= '9' {
			builder.WriteByte(byte('a' + char - '0'))
			continue
		}
		builder.WriteByte(byte('k' + char - 'a'))
	}
	return builder.String()
}

func resolveExtensionName(manifest extensionManifest, fallback string) string {
	for _, value := range []string{manifest.Name, manifest.ShortName, fallback} {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return "Chrome 插件"
}

var welcomeTabRegexes = []*regexp.Regexp{
	// 匹配带 install / welcome / onboard / changelog / update-history / first-run / getting-started 等欢迎 URL 的 tabs.create 调用
	regexp.MustCompile(`(?i)(?:await\s+)?(?:chrome|browser|[a-zA-Z0-9_$]+)\.tabs\.create\s*\(\s*\{[^\}]*url\s*:\s*["'][^"']*(?:install|welcome|update-history|changelog|onboard|first-run|getting-started)[^"']*["'][^\}]*\}\s*\)`),
	// 匹配形如 I(chrome.tabs.create({url:t})) 的包装调用
	regexp.MustCompile(`(?i)I\s*\(\s*(?:chrome|browser)\.tabs\.create\s*\(\s*\{\s*url\s*:\s*[a-zA-Z0-9_$]+\s*\}\s*\)\s*\)`),
}

func sanitizeExtensionWelcomeTabs(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	replacements := []struct {
		target      string
		replacement string
	}{
		// Mobile View
		{`I(chrome.tabs.create({url:t}))`, `void 0 /* suppressed Mobile View install tab */`},
		{`v+"/ru/install"`, `"about:blank"`},
		{`v+"/install"`, `"about:blank"`},

		// AIX Downloader
		{`"install"==e.reason?null:"update"==e.reason&&(chrome.tabs.create({url:"https://aixdownloader.com/update-history"}),chrome.downloads.setShelfEnabled(!0))`, `"install"==e.reason?null:"update"==e.reason&&(chrome.downloads.setShelfEnabled(!0))`},
		{`chrome.tabs.create({url:"https://aixdownloader.com/update-history"})`, `void 0 /* suppressed aix update tab */`},
		{`chrome.tabs.create({url:"https://aixdownloader.com/welcome"})`, `void 0 /* suppressed aix welcome tab */`},
		{`"install"==e.reason?chrome.tabs.create({url:"https://aixdownloader.com/welcome"})`, `"install"==e.reason?null`},

		// AITDK
		{`chrome.tabs.create({url:"https://aitdk.com/extension?utm_source=install"})`, `void 0 /* suppressed aitdk install tab */`},

		// Feed Sorter
		{`chrome.tabs.create({url:l+"extension-install/?id="+f})`, `void 0 /* suppressed feed sorter install tab */`},

		// Wappalyzer
		{`open(\n            'https://www.wappalyzer.com/installed/`, `void (\n            'https://www.wappalyzer.com/installed/`},
		{`open('https://www.wappalyzer.com/installed/`, `void ('https://www.wappalyzer.com/installed/`},
		{`https://www.wappalyzer.com/installed/`, `https://www.wappalyzer.com/suppressed/`},
		{`Browser.gui.showPage('demo.html');`, `/* Browser.gui.showPage('demo.html'); */`},

		// AdGuard
		{`await PagesApi.openPostInstallPage();`, `void 0; /* suppressed openPostInstallPage */`},
		{`await PagesApi.openFiltersOnSettingsPage();`, `void 0; /* suppressed openFiltersOnSettingsPage */`},
		{`await PagesApi.openExtensionPopup();`, `void 0; /* suppressed openExtensionPopup */`},
		{`await PagesApi.openChromeExtensionsSettingsPage();`, `void 0; /* suppressed openChromeExtensionsSettingsPage */`},
		{`url: PagesApiCommon.postInstallPageUrl`, `url: "about:blank"`},
		{`url: thankYouUrl`, `url: "about:blank"`},
		{`v(chrome.tabs.create({url:t}))`, `void 0 /* suppressed v(chrome.tabs.create) */`},
		{`"install"===e.reason&&chrome.tabs.create`, `false&&chrome.tabs.create`},
		{`n.reason===chrome.runtime.OnInstalledReason.INSTALL&&chrome.tabs.create`, `false&&chrome.tabs.create`},

		// Tampermonkey
		{`gi.scheduleNotification(void 0,!0)`, `gi.scheduleNotification(void 0,!1)`},
		{`notification_showUpdate:"changelog"`, `notification_showUpdate:"off"`},
		{`"changelog"==wn.values.notification_showUpdate`, `false&&"changelog"==wn.values.notification_showUpdate`},
		{`onInstalled:()=>{gi.scheduleNotification(void 0,!1)}`, `onInstalled:()=>{}`},
		{`chrome.tabs.create({ url: "install.html" });`, `void 0; /* suppressed install.html */`},
		{`chrome.tabs.create({url: "install.html"});`, `void 0; /* suppressed install.html */`},
		{`di=()=>{if(!_.late)`, `di=()=>{return;if(!_.late)`},
		{`Ap=async()=>{var e;if(!S()){`, `Ap=async()=>{return;var e;if(!S()){`},
		{`Ic={SEVERITY_MAX:10,SEVERITY_MANUALLY_DEFINED:11,SEVERITY_FOISTED_SCRIPT:12`, `Ic={SEVERITY_MAX:99999,SEVERITY_MANUALLY_DEFINED:99999,SEVERITY_FOISTED_SCRIPT:0`},
		{`const r=e=>{t={},e&&Object.keys(e).forEach((n=>{t[n]=e[n]})),n.resolve()};xt.local.get(null,r)`, `const r=e=>{t={},e&&Object.keys(e).forEach((n=>{let v=e[n];if(n.includes("@meta#")&&v&&v.value){v.value.evilness=0;v.value.enabled=!0}t[n]=v})),n.resolve()};xt.local.get(null,r)`},
		{`G.SECURE||sa.getUidList().forEach((e=>{const t=sa.getByUid(e);t.script&&t.cond&&t.script.evilness!=Uc.SEVERITY_FOISTED_SCRIPT`, `false&&sa.getUidList().forEach((e=>{const t=sa.getByUid(e);t.script&&t.cond&&t.script.evilness!=Uc.SEVERITY_FOISTED_SCRIPT`},
		{`if(!e)return v.error("sb: no UUID set"),{};let n,r=nn.getValue(I.PREFIX.META+e,null);if(r){`, `if(!e)return v.error("sb: no UUID set"),{};let n,r=nn.getValue(I.PREFIX.META+e,null);if(r){r.evilness=0;r.enabled=true;`},
		{`setByUid:(e,t)=>{if(!e)throw v.error("sb: no UUID set",t),new Error("No UUID set!");if("string"!=typeof t.textContent)throw new Error("No script code set!");`, `setByUid:(e,t)=>{if(!e)throw v.error("sb: no UUID set",t),new Error("No UUID set!");if("string"!=typeof t.textContent)throw new Error("No script code set!");t.evilness=0;`},
		{`sl.clean(),await y.Pledge()`, `sl.clean(),sa.getUidList().forEach((e=>{try{const t=sa.getByUid(e);if(t.script&&t.cond){t.script.evilness=0;t.script.enabled=true;sl.doModify(e,t.script,!1)}}catch(_){}})),await y.Pledge()`},
		{`el=(e,t)=>{const n=t||{},r=Object.values(e).map((t=>{let r,s,i;`, `el=(e,t)=>{const n=t||{},r=Object.values(e).map((t=>{let r,s,i;t.evilness=0;`},
		{`for(const t of i){if(v.verbose("check "+t.name`, `for(const t of i){t.evilness=0;if(v.verbose("check "+t.name`},
		{`if(b&&b>=Math.min(Uc.SEVERITY_MAX,wn.values.script_blacklist_severity))return;`, `b=0;/* bypass evilness in UA */`},
		{`if(o.evilness&&o.evilness>=Math.min(Uc.SEVERITY_MAX,wn.values.script_blacklist_severity))return y.Breach();`, `o.evilness=0;`},
		{`continuet.evilness=0;`, `continue;`},
		{`t.evilness=0;}(Lo(e)||!0!==t.options.noframes`, `}(Lo(e)||!0!==t.options.noframes`},
		{`e=t.blacklisted||t.foisted?"enabler_warning":t.enabled?`, `e=t.blacklisted?"enabler_warning":t.enabled?`},
		{`const n=t.blacklisted||t.foisted||(t.enabled?xt("Enabled"):xt("Disabled"))`, `const n=t.blacklisted||(t.enabled?xt("Enabled"):xt("Disabled"))`},
		{`o=a.blacklisted||a.foisted?"enabler_warning":a.enabled?`, `o=a.blacklisted?"enabler_warning":a.enabled?`},
		{`const d=a.blacklisted||a.foisted||(a.enabled?xt("Enabled"):xt("Disabled"))`, `const d=a.blacklisted||(a.enabled?xt("Enabled"):xt("Disabled"))`},
		{`a.foisted?p(a.uuid,"whitewash",!0):p(a.uuid,"enabled",!a.enabled)`, `p(a.uuid,"enabled",!a.enabled)`},
		{`(t.blacklisted||t.foisted)&&A.push("crossedout")`, `t.blacklisted&&A.push("crossedout")`},
		{`(t.blacklisted||t.foisted)&&(z=t.blacklisted?`, `t.blacklisted&&(z=t.blacklisted?`},
		{`createGobalHint:(e,t)=>{`, `createGobalHint:(e,t)=>{if(e&&(e.qr||e.key==="android_hint"||e.id==="android_hint"))return;`},
	}

	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		lower := strings.ToLower(entry.Name())
		if lower == "style.css" {
			cssData, err := os.ReadFile(path)
			if err == nil {
				cssContent := string(cssData)
				rule := "\n#status, .global_hint, [id*=\"android_hint\"] { display: none !important; }\n"
				if !strings.Contains(cssContent, "android_hint") {
					_ = os.WriteFile(path, []byte(cssContent+rule), 0o644)
				}
			}
			return nil
		}
		if !strings.HasSuffix(lower, ".js") && !strings.HasSuffix(lower, ".mjs") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		modified := false
		for _, r := range replacements {
			if strings.Contains(r.replacement, r.target) {
				if strings.Contains(content, r.target) && !strings.Contains(content, r.replacement) {
					content = strings.ReplaceAll(content, r.target, r.replacement)
					modified = true
				}
			} else if strings.Contains(content, r.target) {
				content = strings.ReplaceAll(content, r.target, r.replacement)
				modified = true
			}
		}
		for _, re := range welcomeTabRegexes {
			if re.MatchString(content) {
				content = re.ReplaceAllString(content, `void 0 /* suppressed welcome tab */`)
				modified = true
			}
		}
		if modified {
			_ = os.WriteFile(path, []byte(content), 0o644)
		}
		return nil
	})
}

// SanitizeAllExtensions 扫描已安装插件目录并净化所有插件的新手安装与更新弹窗代码，同时清理各实例的 Service Worker 缓存和会话历史
func (m *Manager) SanitizeAllExtensions() error {
	if m == nil {
		return nil
	}
	extensionsRoot := filepath.Join(m.ResolveRelativePath(filepath.Join("data", extensionsRootDir)))
	entries, err := os.ReadDir(extensionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(extensionsRoot, entry.Name())
		sanitizeExtensionWelcomeTabs(dir)
	}

	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	for _, profile := range m.Profiles {
		if profile == nil || profile.Running {
			continue
		}
		userDataDir := m.ResolveUserDataDir(profile)
		_ = ClearSessionRestoreData(userDataDir)
	}

	return nil
}


