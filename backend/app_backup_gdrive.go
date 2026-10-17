package backend

import (
	"ant-chrome/backend/internal/backup/channels/gdrive"
	"ant-chrome/backend/internal/config"
	"ant-chrome/backend/internal/logger"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	DefaultGDriveClientID     = "634676167473-md3pb7efdtg6kfkvdtu4ag50l2p0ud9l.apps.googleusercontent.com"
	DefaultGDriveClientSecret = "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"
)

var (
	oauthServerMu   sync.Mutex
	oauthServer     *http.Server
	gdriveSyncMu    sync.Mutex
	gdriveSyncTimer *time.Timer
)

func (a *App) BackupGDriveGetSettings() (map[string]interface{}, error) {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return nil, err
	}
	scheduler.mu.RLock()
	cfg := scheduler.settings.Channels.GDrive
	scheduler.mu.RUnlock()

	// 若已存在 refresh_token 但尚未缓存用户信息，后台自动获取
	if strings.TrimSpace(cfg.RefreshToken) != "" && strings.TrimSpace(cfg.AccountEmail) == "" {
		go func() {
			client, clientErr := a.backupGDriveClient(nil)
			if clientErr == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				email, name, avatar, fetchErr := client.GetAccountInfo(ctx)
				if fetchErr == nil && email != "" {
					scheduler.mu.Lock()
					next := normalizeBackupSettings(scheduler.settings)
					next.Channels.GDrive.AccountEmail = email
					next.Channels.GDrive.AccountName = name
					next.Channels.GDrive.AccountAvatar = avatar
					_, _ = scheduler.commitSettingsLocked(next)
					scheduler.mu.Unlock()

					a.emitRuntimeEvent("backup:gdrive:oauth_completed", map[string]interface{}{
						"ok":          true,
						"email":       email,
						"displayName": name,
						"photoLink":   avatar,
					})
				}
			}
		}()
	}

	return backupGDriveSettingsResult(cfg), nil
}

func (a *App) BackupGDriveSaveSettings(input map[string]string) (map[string]interface{}, error) {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return nil, err
	}

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

	if scheduler.app == nil || scheduler.app.config == nil {
		return nil, fmt.Errorf("应用配置未初始化")
	}

	next := normalizeBackupSettings(scheduler.settings)
	if clientID, ok := input["clientID"]; ok && strings.TrimSpace(clientID) != "" {
		next.Channels.GDrive.ClientID = strings.TrimSpace(clientID)
	}
	if clientSecret, ok := input["clientSecret"]; ok && strings.TrimSpace(clientSecret) != "" {
		next.Channels.GDrive.ClientSecret = strings.TrimSpace(clientSecret)
	}
	if folderName, ok := input["folderName"]; ok {
		next.Channels.GDrive.FolderName = strings.TrimSpace(folderName)
	}
	if proxyURL, ok := input["proxyURL"]; ok {
		next.Channels.GDrive.ProxyURL = strings.TrimSpace(proxyURL)
	}
	if autoSyncVal, ok := input["autoSync"]; ok {
		next.Channels.GDrive.AutoSync = autoSyncVal == "true" || autoSyncVal == "1"
	}

	if _, err := scheduler.commitSettingsLocked(next); err != nil {
		return nil, err
	}
	return backupGDriveSettingsResult(scheduler.settings.Channels.GDrive), nil
}

func (a *App) BackupGDriveStartOAuth(input map[string]string) (map[string]interface{}, error) {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return nil, err
	}

	scheduler.mu.RLock()
	clientID := strings.TrimSpace(scheduler.settings.Channels.GDrive.ClientID)
	clientSecret := strings.TrimSpace(scheduler.settings.Channels.GDrive.ClientSecret)
	proxyURL := strings.TrimSpace(scheduler.settings.Channels.GDrive.ProxyURL)
	scheduler.mu.RUnlock()

	if customID := strings.TrimSpace(input["clientID"]); customID != "" {
		clientID = customID
	}
	if customSecret := strings.TrimSpace(input["clientSecret"]); customSecret != "" {
		clientSecret = customSecret
	}
	if customProxy := strings.TrimSpace(input["proxyURL"]); customProxy != "" {
		proxyURL = customProxy
	}

	if clientID == "" {
		clientID = DefaultGDriveClientID
	}
	if clientSecret == "" {
		clientSecret = DefaultGDriveClientSecret
	}

	oauthServerMu.Lock()
	if oauthServer != nil {
		_ = oauthServer.Close()
		oauthServer = nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		oauthServerMu.Unlock()
		return nil, fmt.Errorf("启动本地授权服务失败: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", port)
	authURL := gdrive.BuildAuthURL(clientID, redirectURI)

	mux := http.NewServeMux()
	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	oauthServer = server
	oauthServerMu.Unlock()

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		authErr := r.URL.Query().Get("error")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if authErr != "" || code == "" {
			msg := authErr
			if msg == "" {
				msg = "未收到有效授权码"
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>授权失败</title><style>body{font-family:-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#fef2f2}.card{background:#fff;padding:32px;border-radius:16px;box-shadow:0 4px 20px rgba(0,0,0,0.08);text-align:center;max-width:400px}h2{color:#ef4444;margin-top:0}p{color:#666}</style></head><body><div class="card"><h2>✕ 授权未完成</h2><p>%s</p><p style="font-size:13px;color:#999">请关闭此页并在 Ant Browser 中重新尝试。</p></div></body></html>`, msg)
			errChan <- fmt.Errorf("授权失败: %s", msg)
			return
		}

		fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Ant Browser 授权成功</title><style>body{font-family:-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f0fdf4}.card{background:#fff;padding:32px;border-radius:16px;box-shadow:0 4px 20px rgba(0,0,0,0.08);text-align:center;max-width:400px}h2{color:#10b981;margin-top:0}p{color:#666}</style></head><body><div class="card"><h2>✓ 授权成功</h2><p>Ant Browser 已成功连接您的 Google 云端硬盘。</p><p style="font-size:13px;color:#999">您可以关闭此页面并返回客户端。</p></div></body></html>`)
		codeChan <- code
	})

	go func() {
		_ = server.Serve(listener)
	}()

	go func() {
		defer func() {
			oauthServerMu.Lock()
			if oauthServer == server {
				_ = server.Close()
				oauthServer = nil
			}
			oauthServerMu.Unlock()
		}()

		select {
		case code := <-codeChan:
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			accessToken, refreshToken, expiry, exchangeErr := gdrive.ExchangeCodeForToken(ctx, clientID, clientSecret, redirectURI, code, proxyURL)
			if exchangeErr != nil {
				a.emitRuntimeEvent("backup:gdrive:oauth_completed", map[string]interface{}{
					"ok":    false,
					"error": exchangeErr.Error(),
				})
				return
			}

			clientCfg := gdrive.Config{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
				TokenExpiry:  expiry.Format(time.RFC3339),
				ProxyURL:     proxyURL,
			}
			gdClient, _ := gdrive.NewClient(clientCfg)
			email := ""
			displayName := ""
			photoLink := ""
			if gdClient != nil {
				email, displayName, photoLink, _ = gdClient.GetAccountInfo(ctx)
			}

			scheduler.mu.Lock()
			next := normalizeBackupSettings(scheduler.settings)
			next.Channels.GDrive.ClientID = clientID
			next.Channels.GDrive.ClientSecret = clientSecret
			next.Channels.GDrive.AccessToken = accessToken
			next.Channels.GDrive.RefreshToken = refreshToken
			next.Channels.GDrive.TokenExpiry = expiry.Format(time.RFC3339)
			next.Channels.GDrive.AccountEmail = email
			next.Channels.GDrive.AccountName = displayName
			next.Channels.GDrive.AccountAvatar = photoLink
			next.Channels.GDrive.AutoSync = true
			if proxyURL != "" {
				next.Channels.GDrive.ProxyURL = proxyURL
			}
			_, _ = scheduler.commitSettingsLocked(next)
			scheduler.mu.Unlock()

			a.emitRuntimeEvent("backup:gdrive:oauth_completed", map[string]interface{}{
				"ok":          true,
				"email":       email,
				"displayName": displayName,
				"photoLink":   photoLink,
			})

		case authErr := <-errChan:
			a.emitRuntimeEvent("backup:gdrive:oauth_completed", map[string]interface{}{
				"ok":    false,
				"error": authErr.Error(),
			})
		case <-time.After(5 * time.Minute):
			a.emitRuntimeEvent("backup:gdrive:oauth_completed", map[string]interface{}{
				"ok":    false,
				"error": "授权操作超时，请重新点击授权",
			})
		}
	}()

	if a.ctx != nil {
		wailsRuntime.BrowserOpenURL(a.ctx, authURL)
	}

	return map[string]interface{}{
		"authURL":     authURL,
		"redirectURI": redirectURI,
		"port":        port,
	}, nil
}

func (a *App) BackupGDriveRevokeToken() (map[string]interface{}, error) {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return nil, err
	}

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

	refreshToken := scheduler.settings.Channels.GDrive.RefreshToken
	proxyURL := scheduler.settings.Channels.GDrive.ProxyURL

	if refreshToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = gdrive.RevokeToken(ctx, refreshToken, proxyURL)
	}

	next := normalizeBackupSettings(scheduler.settings)
	next.Channels.GDrive.RefreshToken = ""
	next.Channels.GDrive.AccessToken = ""
	next.Channels.GDrive.TokenExpiry = ""
	next.Channels.GDrive.AccountEmail = ""
	next.Channels.GDrive.AccountName = ""
	next.Channels.GDrive.AccountAvatar = ""
	next.Channels.GDrive.FolderID = ""
	next.Channels.GDrive.AutoSync = false

	if _, err := scheduler.commitSettingsLocked(next); err != nil {
		return nil, err
	}
	a.emitRuntimeEvent("backup:gdrive:revoked", map[string]interface{}{})
	return backupGDriveSettingsResult(scheduler.settings.Channels.GDrive), nil
}

func (a *App) BackupGDriveTest(input map[string]string) (map[string]interface{}, error) {
	client, err := a.backupGDriveClient(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gdrive.ControlTimeout)
	defer cancel()

	if err := client.Test(ctx); err != nil {
		return nil, fmt.Errorf("Google Drive 连通性测试失败：%w", err)
	}
	email, displayName, photoLink, _ := client.GetAccountInfo(ctx)
	return map[string]interface{}{
		"ok":          true,
		"message":     "Google Drive 连接正常",
		"email":       email,
		"displayName": displayName,
		"photoLink":   photoLink,
	}, nil
}

func (a *App) BackupGDriveList(input map[string]string) ([]map[string]interface{}, error) {
	client, err := a.backupGDriveClient(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gdrive.ControlTimeout)
	defer cancel()

	items, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取 Google Drive 备份列表失败：%w", err)
	}

	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"name":       item.Name,
			"size":       item.Size,
			"modifiedAt": item.ModifiedAt,
		})
	}
	return result, nil
}

func (a *App) BackupGDriveDownload(fileNameOrID string) (map[string]interface{}, error) {
	client, err := a.backupGDriveClient(nil)
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "ant-gdrive-download-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时下载目录失败: %w", err)
	}

	targetName := fileNameOrID
	if !strings.HasSuffix(strings.ToLower(targetName), ".zip") && !strings.HasSuffix(strings.ToLower(targetName), ".json") {
		targetName += ".zip"
	}
	destPath := filepath.Join(tempDir, targetName)

	ctx, cancel := context.WithTimeout(context.Background(), gdrive.TransferTimeout)
	defer cancel()

	if err := client.Download(ctx, fileNameOrID, destPath); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("从 Google Drive 下载备份失败：%w", err)
	}

	stat, _ := os.Stat(destPath)
	return map[string]interface{}{
		"localPath": destPath,
		"fileName":  targetName,
		"size":      stat.Size(),
	}, nil
}

func (a *App) BackupGDriveDelete(fileNameOrID string) (map[string]interface{}, error) {
	client, err := a.backupGDriveClient(nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gdrive.ControlTimeout)
	defer cancel()

	if err := client.Delete(ctx, fileNameOrID); err != nil {
		return nil, fmt.Errorf("删除 Google Drive 备份失败：%w", err)
	}
	return map[string]interface{}{
		"ok":      true,
		"message": "已从 Google Drive 删除备份文件",
	}, nil
}

func (a *App) backupGDriveClient(input map[string]string) (*gdrive.Client, error) {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return nil, err
	}

	scheduler.mu.RLock()
	cfg := scheduler.settings.Channels.GDrive
	scheduler.mu.RUnlock()

	if input != nil {
		if cid, ok := input["clientID"]; ok && strings.TrimSpace(cid) != "" {
			cfg.ClientID = strings.TrimSpace(cid)
		}
		if cs, ok := input["clientSecret"]; ok && strings.TrimSpace(cs) != "" {
			cfg.ClientSecret = strings.TrimSpace(cs)
		}
		if fn, ok := input["folderName"]; ok && strings.TrimSpace(fn) != "" {
			cfg.FolderName = strings.TrimSpace(fn)
		}
		if px, ok := input["proxyURL"]; ok {
			cfg.ProxyURL = strings.TrimSpace(px)
		}
	}

	if cfg.ClientID == "" {
		cfg.ClientID = DefaultGDriveClientID
	}
	if cfg.ClientSecret == "" {
		cfg.ClientSecret = DefaultGDriveClientSecret
	}
	if cfg.RefreshToken == "" {
		return nil, fmt.Errorf("Google Drive 尚未完成授权登录")
	}

	if strings.TrimSpace(cfg.ProxyURL) == "" {
		if envProxy := os.Getenv("HTTPS_PROXY"); envProxy != "" {
			cfg.ProxyURL = envProxy
		} else if envProxy := os.Getenv("HTTP_PROXY"); envProxy != "" {
			cfg.ProxyURL = envProxy
		} else if envProxy := os.Getenv("ALL_PROXY"); envProxy != "" {
			cfg.ProxyURL = envProxy
		}
	}

	return gdrive.NewClient(gdrive.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RefreshToken: cfg.RefreshToken,
		AccessToken:  cfg.AccessToken,
		TokenExpiry:  cfg.TokenExpiry,
		AccountEmail: cfg.AccountEmail,
		FolderName:   cfg.FolderName,
		FolderID:     cfg.FolderID,
		ProxyURL:     cfg.ProxyURL,
		OnTokenUpdate: func(accessToken, refreshToken, tokenExpiry string) {
			scheduler.mu.Lock()
			next := normalizeBackupSettings(scheduler.settings)
			next.Channels.GDrive.AccessToken = accessToken
			if refreshToken != "" {
				next.Channels.GDrive.RefreshToken = refreshToken
			}
			next.Channels.GDrive.TokenExpiry = tokenExpiry
			_, _ = scheduler.commitSettingsLocked(next)
			scheduler.mu.Unlock()
		},
	})
}

func backupGDriveSettingsResult(settings config.GDriveChannelConfig) map[string]interface{} {
	configured := strings.TrimSpace(settings.RefreshToken) != ""
	return map[string]interface{}{
		"configured":             configured,
		"clientID":               settings.ClientID,
		"clientSecretConfigured": strings.TrimSpace(settings.ClientSecret) != "",
		"accountEmail":           settings.AccountEmail,
		"accountName":            settings.AccountName,
		"accountAvatar":          settings.AccountAvatar,
		"folderName":             settings.FolderName,
		"folderID":               settings.FolderID,
		"proxyURL":               settings.ProxyURL,
		"tokenExpiry":            settings.TokenExpiry,
		"autoSync":               settings.AutoSync,
		"lastSyncTime":           settings.LastSyncTime,
	}
}

// BackupGDriveSyncNow 立即将本地配置同步上传至 Google Drive
func (a *App) BackupGDriveSyncNow() (map[string]interface{}, error) {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return nil, err
	}
	scheduler.mu.RLock()
	configured := strings.TrimSpace(scheduler.settings.Channels.GDrive.RefreshToken) != ""
	scheduler.mu.RUnlock()
	if !configured {
		return nil, fmt.Errorf("请先登录并授权 Google 账号")
	}

	res, err := a.BackupCreatePackage(map[string]string{
		"gdrive": "true",
		"local":  "false",
	})
	if err != nil {
		return nil, fmt.Errorf("同步到 Google Drive 失败: %w", err)
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05")
	scheduler.mu.Lock()
	next := normalizeBackupSettings(scheduler.settings)
	next.Channels.GDrive.LastSyncTime = nowStr
	_, _ = scheduler.commitSettingsLocked(next)
	scheduler.mu.Unlock()

	a.emitRuntimeEvent("backup:gdrive:synced", map[string]interface{}{
		"lastSyncTime": nowStr,
		"result":       res,
	})

	return map[string]interface{}{
		"ok":           true,
		"lastSyncTime": nowStr,
		"result":       res,
	}, nil
}

// BackupGDriveRestoreLatest 从 Google Drive 下载并恢复最新的配置备份包
func (a *App) BackupGDriveRestoreLatest() (map[string]interface{}, error) {
	client, err := a.backupGDriveClient(nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gdrive.ControlTimeout)
	defer cancel()

	files, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 Google Drive 备份列表失败: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("Google Drive 中暂无备份文件")
	}

	var latestZipName string
	var latestModified string
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			latestZipName = f.Name
			latestModified = f.ModifiedAt
			break
		}
	}

	if latestZipName == "" {
		return nil, fmt.Errorf("未在 Google Drive 中找到有效的备份文件")
	}

	downloaded, err := a.BackupGDriveDownload(latestZipName)
	if err != nil {
		return nil, err
	}

	localPath, _ := downloaded["localPath"].(string)
	if localPath == "" {
		return nil, fmt.Errorf("下载备份文件路径为空")
	}
	defer os.RemoveAll(filepath.Dir(localPath))

	restoreRes, err := a.BackupRestoreLocalPackage(localPath)
	if err != nil {
		return nil, fmt.Errorf("恢复备份失败: %w", err)
	}

	return map[string]interface{}{
		"ok":         true,
		"fileName":   latestZipName,
		"modifiedAt": latestModified,
		"result":     restoreRes,
	}, nil
}

// TriggerGDriveAutoSync 触发防抖自动同步
func (a *App) TriggerGDriveAutoSync() {
	scheduler, err := a.ensureBackupScheduler()
	if err != nil {
		return
	}
	scheduler.mu.RLock()
	cfg := scheduler.settings.Channels.GDrive
	scheduler.mu.RUnlock()

	if !cfg.AutoSync || strings.TrimSpace(cfg.RefreshToken) == "" {
		return
	}

	gdriveSyncMu.Lock()
	defer gdriveSyncMu.Unlock()

	if gdriveSyncTimer != nil {
		gdriveSyncTimer.Stop()
	}

	gdriveSyncTimer = time.AfterFunc(8*time.Second, func() {
		log := logger.New("GDriveSync")
		// 如果当前有正在运行的浏览器实例，跳过本次自动同步，避免锁库冲突和弹出错误惊扰用户
		if running := a.backupRunningProfileNames(); len(running) > 0 {
			log.Info("后台自动同步静默跳过：当前有浏览器实例正在运行", logger.F("profiles", strings.Join(running, ", ")))
			return
		}

		log.Info("开始执行 Google Drive 后台自动同步...")
		a.emitRuntimeEvent("backup:gdrive:sync_started", map[string]interface{}{})
		_, syncErr := a.BackupGDriveSyncNow()
		if syncErr != nil {
			log.Warn("Google Drive 后台自动同步失败", logger.F("error", syncErr.Error()))
			a.emitRuntimeEvent("backup:gdrive:sync_failed", map[string]interface{}{
				"error":      syncErr.Error(),
				"isAutoSync": true,
			})
		} else {
			log.Info("Google Drive 后台自动同步成功")
		}
	})
}
