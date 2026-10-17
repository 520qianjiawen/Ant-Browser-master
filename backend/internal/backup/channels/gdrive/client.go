package gdrive

import (
	"ant-chrome/backend/internal/backup/channels"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	TransferTimeout     = 2 * time.Hour
	ControlTimeout      = time.Minute
	DefaultFolderName   = "AntBrowserBackup"
	OAuthAuthEndpoint   = "https://accounts.google.com/o/oauth2/v2/auth"
	OAuthTokenEndpoint  = "https://oauth2.googleapis.com/token"
	OAuthRevokeEndpoint = "https://oauth2.googleapis.com/revoke"
	DriveAPIBase        = "https://www.googleapis.com/drive/v3"
	DriveUploadBase     = "https://www.googleapis.com/upload/drive/v3"
	DriveScope          = "https://www.googleapis.com/auth/drive.file"
)

type TokenUpdateFunc func(accessToken, refreshToken, tokenExpiry string)

type Config struct {
	ClientID      string
	ClientSecret  string
	RefreshToken  string
	AccessToken   string
	TokenExpiry   string
	AccountEmail  string
	FolderName    string
	FolderID      string
	ProxyURL      string
	OnTokenUpdate TokenUpdateFunc
}

type Client struct {
	config        Config
	mu            sync.RWMutex
	controlClient *http.Client
	dataClient    *http.Client
	folderID      string
}

type File = channels.File

func (c *Client) ID() channels.ID {
	return channels.GDrive
}

func NewClient(cfg Config) (*Client, error) {
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.RefreshToken = strings.TrimSpace(cfg.RefreshToken)
	cfg.AccessToken = strings.TrimSpace(cfg.AccessToken)
	cfg.TokenExpiry = strings.TrimSpace(cfg.TokenExpiry)
	cfg.FolderName = strings.TrimSpace(cfg.FolderName)
	if cfg.FolderName == "" {
		cfg.FolderName = DefaultFolderName
	}
	cfg.FolderID = strings.TrimSpace(cfg.FolderID)

	controlClient, err := newHTTPClient(cfg.ProxyURL, ControlTimeout)
	if err != nil {
		return nil, fmt.Errorf("创建控制客户端失败: %w", err)
	}

	dataClient, err := newHTTPClient(cfg.ProxyURL, TransferTimeout)
	if err != nil {
		return nil, fmt.Errorf("创建传输客户端失败: %w", err)
	}

	return &Client{
		config:        cfg,
		controlClient: controlClient,
		dataClient:    dataClient,
		folderID:      cfg.FolderID,
	}, nil
}

func newHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if strings.TrimSpace(proxyURL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(proxyURL))
		if err != nil {
			return nil, fmt.Errorf("无效代理地址: %w", err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// BuildAuthURL 生成用于浏览器登录授权的 URL
func BuildAuthURL(clientID string, redirectURI string) string {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(clientID))
	values.Set("redirect_uri", strings.TrimSpace(redirectURI))
	values.Set("response_type", "code")
	values.Set("scope", DriveScope)
	values.Set("access_type", "offline")
	values.Set("prompt", "consent")
	return OAuthAuthEndpoint + "?" + values.Encode()
}

// ExchangeCodeForToken 使用授权码换取 access_token 与 refresh_token
func ExchangeCodeForToken(ctx context.Context, clientID, clientSecret, redirectURI, code, proxyURL string) (accessToken, refreshToken string, expiry time.Time, err error) {
	client, err := newHTTPClient(proxyURL, ControlTimeout)
	if err != nil {
		return "", "", time.Time{}, err
	}

	data := url.Values{}
	data.Set("client_id", strings.TrimSpace(clientID))
	data.Set("client_secret", strings.TrimSpace(clientSecret))
	data.Set("redirect_uri", strings.TrimSpace(redirectURI))
	data.Set("code", strings.TrimSpace(code))
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("换取 Token 网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", time.Time{}, fmt.Errorf("换取 Token 失败 (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", "", time.Time{}, fmt.Errorf("解析 Token 响应失败: %w", err)
	}
	if res.Error != "" {
		return "", "", time.Time{}, fmt.Errorf("授权失败: %s - %s", res.Error, res.ErrorDesc)
	}

	expiry = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	return res.AccessToken, res.RefreshToken, expiry, nil
}

// RevokeToken 撤销指定的访问令牌或刷新令牌
func RevokeToken(ctx context.Context, token, proxyURL string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	client, err := newHTTPClient(proxyURL, ControlTimeout)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthRevokeEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("撤销 Token 网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("撤销 Token 失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) ensureAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config.AccessToken != "" && c.config.TokenExpiry != "" {
		expiry, err := time.Parse(time.RFC3339, c.config.TokenExpiry)
		if err == nil && time.Now().Add(2*time.Minute).Before(expiry) {
			return c.config.AccessToken, nil
		}
	}

	if c.config.RefreshToken == "" {
		return "", errors.New("Google Drive 尚未授权或缺少 Refresh Token")
	}

	data := url.Values{}
	data.Set("client_id", c.config.ClientID)
	data.Set("client_secret", c.config.ClientSecret)
	data.Set("refresh_token", c.config.RefreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.controlClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("刷新 Access Token 失败: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("刷新 Access Token 失败 (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", fmt.Errorf("解析 Token 响应失败: %w", err)
	}
	if res.Error != "" {
		return "", fmt.Errorf("刷新授权失败: %s - %s", res.Error, res.ErrorDesc)
	}

	c.config.AccessToken = res.AccessToken
	if res.RefreshToken != "" {
		c.config.RefreshToken = res.RefreshToken
	}
	expiry := time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	c.config.TokenExpiry = expiry.Format(time.RFC3339)

	if c.config.OnTokenUpdate != nil {
		c.config.OnTokenUpdate(c.config.AccessToken, c.config.RefreshToken, c.config.TokenExpiry)
	}

	return c.config.AccessToken, nil
}

// GetAccountInfo 获取当前授权 Google 账号的信息
func (c *Client) GetAccountInfo(ctx context.Context) (email, displayName, photoLink string, err error) {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return "", "", "", err
	}

	reqURL := DriveAPIBase + "/about?fields=user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.controlClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("请求用户信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("获取用户信息失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var res struct {
		User struct {
			DisplayName  string `json:"displayName"`
			EmailAddress string `json:"emailAddress"`
			PhotoLink    string `json:"photoLink"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", "", "", err
	}
	return res.User.EmailAddress, res.User.DisplayName, res.User.PhotoLink, nil
}

func (c *Client) getFolderID(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.folderID != "" {
		folderID := c.folderID
		c.mu.RUnlock()
		return folderID, nil
	}
	folderName := c.config.FolderName
	if folderName == "" {
		folderName = DefaultFolderName
	}
	c.mu.RUnlock()

	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf("mimeType = 'application/vnd.google-apps.folder' and name = '%s' and trashed = false", strings.ReplaceAll(folderName, "'", "\\'"))
	searchParams := url.Values{}
	searchParams.Set("q", query)
	searchParams.Set("fields", "files(id,name)")
	searchParams.Set("spaces", "drive")
	searchURL := fmt.Sprintf("%s/files?%s", DriveAPIBase, searchParams.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.controlClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("查询备份目录失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", formatGDriveAPIError("查询备份目录", resp.StatusCode, body)
	}

	var searchRes struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchRes); err != nil {
		return "", err
	}

	if len(searchRes.Files) > 0 {
		c.mu.Lock()
		c.folderID = searchRes.Files[0].ID
		c.mu.Unlock()
		return searchRes.Files[0].ID, nil
	}

	createReqBody, _ := json.Marshal(map[string]string{
		"name":     folderName,
		"mimeType": "application/vnd.google-apps.folder",
	})
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, DriveAPIBase+"/files", bytes.NewReader(createReqBody))
	if err != nil {
		return "", err
	}
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json; charset=UTF-8")

	createResp, err := c.controlClient.Do(createReq)
	if err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		return "", formatGDriveAPIError("创建备份目录", createResp.StatusCode, body)
	}

	var createRes struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createRes); err != nil {
		return "", err
	}

	c.mu.Lock()
	c.folderID = createRes.ID
	c.mu.Unlock()
	return createRes.ID, nil
}

func (c *Client) Test(ctx context.Context) error {
	email, _, _, err := c.GetAccountInfo(ctx)
	if err != nil {
		return err
	}
	if email == "" {
		return errors.New("无法读取授权账号邮箱")
	}
	_, err = c.getFolderID(ctx)
	return err
}

func (c *Client) List(ctx context.Context) ([]File, error) {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	folderID, err := c.getFolderID(ctx)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("'%s' in parents and trashed = false", strings.ReplaceAll(folderID, "'", "\\'"))
	listParams := url.Values{}
	listParams.Set("q", query)
	listParams.Set("fields", "files(id,name,size,modifiedTime)")
	listParams.Set("pageSize", "100")
	listParams.Set("orderBy", "modifiedTime desc")
	listURL := fmt.Sprintf("%s/files?%s", DriveAPIBase, listParams.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.controlClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取文件列表失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, formatGDriveAPIError("读取文件列表", resp.StatusCode, body)
	}

	var listRes struct {
		Files []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Size         string `json:"size"`
			ModifiedTime string `json:"modifiedTime"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listRes); err != nil {
		return nil, err
	}

	files := make([]File, 0, len(listRes.Files))
	for _, item := range listRes.Files {
		size, _ := strconv.ParseInt(item.Size, 10, 64)
		files = append(files, File{
			Name:       item.Name,
			Size:       size,
			ModifiedAt: item.ModifiedTime,
			Directory:  false,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModifiedAt > files[j].ModifiedAt
	})
	return files, nil
}

func (c *Client) Upload(ctx context.Context, localPath, remoteFileName string) (File, error) {
	return c.UploadWithProgress(ctx, localPath, remoteFileName, nil)
}

func (c *Client) UploadMetadata(ctx context.Context, localPath, remoteFileName string) (File, error) {
	return c.UploadMetadataWithProgress(ctx, localPath, remoteFileName, nil)
}

func (c *Client) UploadMetadataWithProgress(ctx context.Context, localPath, remoteFileName string, progressFunc channels.UploadProgressFunc) (File, error) {
	return c.uploadFile(ctx, localPath, remoteFileName, "application/json", progressFunc)
}

func (c *Client) UploadWithProgress(ctx context.Context, localPath, remoteFileName string, progressFunc channels.UploadProgressFunc) (File, error) {
	return c.uploadFile(ctx, localPath, remoteFileName, "application/zip", progressFunc)
}

func (c *Client) uploadFile(ctx context.Context, localPath, remoteFileName, contentType string, progressFunc channels.UploadProgressFunc) (File, error) {
	stat, err := os.Stat(localPath)
	if err != nil {
		return File{}, fmt.Errorf("读取本地文件信息失败: %w", err)
	}

	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return File{}, err
	}
	folderID, err := c.getFolderID(ctx)
	if err != nil {
		return File{}, err
	}

	metaBody, _ := json.Marshal(map[string]interface{}{
		"name":    remoteFileName,
		"parents": []string{folderID},
	})

	initURL := DriveUploadBase + "/files?uploadType=resumable"
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, initURL, bytes.NewReader(metaBody))
	if err != nil {
		return File{}, err
	}
	initReq.Header.Set("Authorization", "Bearer "+token)
	initReq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	initReq.Header.Set("X-Upload-Content-Type", contentType)
	initReq.Header.Set("X-Upload-Content-Length", strconv.FormatInt(stat.Size(), 10))

	initResp, err := c.controlClient.Do(initReq)
	if err != nil {
		return File{}, fmt.Errorf("初始化断点续传失败: %w", err)
	}
	defer initResp.Body.Close()

	if initResp.StatusCode != http.StatusOK && initResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(initResp.Body)
		return File{}, formatGDriveAPIError("初始化断点续传", initResp.StatusCode, body)
	}

	sessionURL := initResp.Header.Get("Location")
	if sessionURL == "" {
		return File{}, errors.New("Google Drive 未返回有效的上传会话 URL")
	}

	file, err := os.Open(localPath)
	if err != nil {
		return File{}, fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer file.Close()

	var uploadBody io.Reader = file
	if progressFunc != nil {
		uploadBody = channels.NewUploadProgressReader(file, stat.Size(), progressFunc)
	}

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, sessionURL, uploadBody)
	if err != nil {
		return File{}, err
	}
	putReq.ContentLength = stat.Size()
	putReq.Header.Set("Content-Type", contentType)

	putResp, err := c.dataClient.Do(putReq)
	if err != nil {
		return File{}, fmt.Errorf("上传文件数据失败: %w", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(putResp.Body)
		return File{}, formatGDriveAPIError("上传文件", putResp.StatusCode, body)
	}

	var uploaded struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Size         string `json:"size"`
		ModifiedTime string `json:"modifiedTime"`
	}
	if err := json.NewDecoder(putResp.Body).Decode(&uploaded); err != nil {
		return File{
			Name:       remoteFileName,
			Size:       stat.Size(),
			ModifiedAt: time.Now().Format(time.RFC3339),
		}, nil
	}

	size, _ := strconv.ParseInt(uploaded.Size, 10, 64)
	if size == 0 {
		size = stat.Size()
	}
	modifiedAt := uploaded.ModifiedTime
	if modifiedAt == "" {
		modifiedAt = time.Now().Format(time.RFC3339)
	}

	return File{
		Name:       remoteFileName,
		Size:       size,
		ModifiedAt: modifiedAt,
	}, nil
}

func (c *Client) Download(ctx context.Context, remoteFileNameOrID, localDestPath string) error {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return err
	}
	folderID, err := c.getFolderID(ctx)
	if err != nil {
		return err
	}

	fileID := remoteFileNameOrID
	query := fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false", strings.ReplaceAll(folderID, "'", "\\'"), strings.ReplaceAll(remoteFileNameOrID, "'", "\\'"))
	searchParams := url.Values{}
	searchParams.Set("q", query)
	searchParams.Set("fields", "files(id,name)")
	searchParams.Set("spaces", "drive")
	searchURL := fmt.Sprintf("%s/files?%s", DriveAPIBase, searchParams.Encode())

	searchReq, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err == nil {
		searchReq.Header.Set("Authorization", "Bearer "+token)
		if searchResp, searchErr := c.controlClient.Do(searchReq); searchErr == nil {
			defer searchResp.Body.Close()
			if searchResp.StatusCode == http.StatusOK {
				var searchRes struct {
					Files []struct {
						ID string `json:"id"`
					} `json:"files"`
				}
				if json.NewDecoder(searchResp.Body).Decode(&searchRes) == nil && len(searchRes.Files) > 0 {
					fileID = searchRes.Files[0].ID
				}
			}
		}
	}

	downloadURL := fmt.Sprintf("%s/files/%s?alt=media", DriveAPIBase, url.PathEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.dataClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载文件网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatGDriveAPIError("下载文件", resp.StatusCode, body)
	}

	if err := os.MkdirAll(filepath.Dir(localDestPath), 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}

	tmpFile := localDestPath + ".tmp"
	out, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}

	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("保存下载文件失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("关闭下载文件失败: %w", closeErr)
	}

	if err := os.Rename(tmpFile, localDestPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("重命名完成文件失败: %w", err)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, remoteFileNameOrID string) error {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return err
	}
	folderID, err := c.getFolderID(ctx)
	if err != nil {
		return err
	}

	fileID := remoteFileNameOrID
	query := fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false", strings.ReplaceAll(folderID, "'", "\\'"), strings.ReplaceAll(remoteFileNameOrID, "'", "\\'"))
	searchParams := url.Values{}
	searchParams.Set("q", query)
	searchParams.Set("fields", "files(id,name)")
	searchParams.Set("spaces", "drive")
	searchURL := fmt.Sprintf("%s/files?%s", DriveAPIBase, searchParams.Encode())

	searchReq, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err == nil {
		searchReq.Header.Set("Authorization", "Bearer "+token)
		if searchResp, searchErr := c.controlClient.Do(searchReq); searchErr == nil {
			defer searchResp.Body.Close()
			if searchResp.StatusCode == http.StatusOK {
				var searchRes struct {
					Files []struct {
						ID string `json:"id"`
					} `json:"files"`
				}
				if json.NewDecoder(searchResp.Body).Decode(&searchRes) == nil && len(searchRes.Files) > 0 {
					fileID = searchRes.Files[0].ID
				}
			}
		}
	}

	deleteURL := fmt.Sprintf("%s/files/%s", DriveAPIBase, url.PathEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.controlClient.Do(req)
	if err != nil {
		return fmt.Errorf("删除文件网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return formatGDriveAPIError("删除文件", resp.StatusCode, body)
	}
	return nil
}

func formatGDriveAPIError(action string, statusCode int, body []byte) error {
	raw := strings.TrimSpace(string(body))
	if strings.Contains(raw, "SERVICE_DISABLED") || strings.Contains(raw, "accessNotConfigured") || strings.Contains(raw, "has not been used in project") {
		var parsed struct {
			Error struct {
				Details []struct {
					Metadata struct {
						ActivationURL string `json:"activationUrl"`
					} `json:"metadata"`
				} `json:"details"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &parsed)
		activationURL := ""
		for _, d := range parsed.Error.Details {
			if d.Metadata.ActivationURL != "" {
				activationURL = d.Metadata.ActivationURL
				break
			}
		}
		if activationURL != "" {
			return fmt.Errorf("%s失败: 您的 Google Cloud 项目尚未启用 Google Drive API。请在浏览器中打开此链接并点击「启用」: %s", action, activationURL)
		}
		return fmt.Errorf("%s失败: 您的 Google Cloud 项目尚未启用 Google Drive API，请前往控制台启用 Drive API 后重试", action)
	}

	// 如果返回的是 HTML 页面（如 400 Bad Request 机器页），不要直接把整个 HTML 标签输出到界面
	if strings.HasPrefix(raw, "<!") || strings.HasPrefix(raw, "<html") {
		msg := "请求格式有误或服务器拒绝"
		if strings.Contains(raw, "malformed or illegal request") {
			msg = "请求格式不合规 (malformed request)"
		}
		return fmt.Errorf("%s失败 (HTTP %d): %s", action, statusCode, msg)
	}

	// 如果是 JSON 错误，尝试解析出 message
	var jsonErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &jsonErr); err == nil && jsonErr.Error.Message != "" {
		return fmt.Errorf("%s失败 (HTTP %d): %s", action, statusCode, jsonErr.Error.Message)
	}

	return fmt.Errorf("%s失败 (HTTP %d): %s", action, statusCode, raw)
}
