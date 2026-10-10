package driver115

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	open115 "github.com/OpenListTeam/115-sdk-go"
	driver "github.com/SheltonZhu/115driver/pkg/driver"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/logbuffer"
	"115strm/internal/store"
	"115strm/internal/util"
)

var ErrNotLoggedIn = errors.New("115 not logged in")

type Service struct {
	cfg        *config.Config
	db         *store.Store
	secret     string
	logs       *logbuffer.Buffer
	mu         sync.Mutex
	rateMu     sync.Mutex
	recentMu   sync.Mutex
	client     *driver.Pan115Client
	openClient *open115.Client
	session    *driver.QRCodeSession
	credential *driver.Credential
	last115API time.Time
	recent115  map[int64][]map[string]string
}

func New(cfg *config.Config, _ string, db *store.Store, secret string, logs *logbuffer.Buffer) *Service {
	s := &Service{cfg: cfg, db: db, secret: secret, logs: logs}
	_ = s.loadOpenSettings()
	_ = s.loadCredential()
	return s
}

func (s *Service) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	authMode := s.authModeLocked()
	status := map[string]any{"auth_mode": authMode, "logged_in": s.client != nil && s.credential != nil}
	if authMode == "open_token" {
		status["logged_in"] = strings.TrimSpace(s.cfg.Pan115.AccessToken) != "" && strings.TrimSpace(s.cfg.Pan115.RefreshToken) != ""
	}
	if s.credential != nil {
		status["uid"] = s.credential.UID
	}
	return status
}

func (s *Service) StartQRCode() (*driver.QRCodeSession, string, error) {
	c := s.newClient()
	session, err := c.QRCodeStart()
	if err != nil {
		return nil, "", err
	}
	png, err := session.QRCode()
	if err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	s.session = session
	s.mu.Unlock()
	return session, "data:image/png;base64," + util.Base64(png), nil
}

func (s *Service) QRCodeStatus() (*driver.QRCodeStatus, error) {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session == nil {
		return nil, errors.New("qrcode session not started")
	}
	return s.newClient().QRCodeStatus(session)
}

func (s *Service) QRCodeLogin(app string) error {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session == nil {
		return errors.New("qrcode session not started")
	}
	loginApp, err := s.resolveQRCodeApp(app)
	if err != nil {
		return err
	}
	c := s.newClient()
	cred, err := c.QRCodeLoginWithApp(session, loginApp)
	if err != nil {
		return err
	}
	c.ImportCredential(cred)
	if err := c.CookieCheck(); err != nil {
		return err
	}
	if err := s.saveCredential(cred); err != nil {
		return err
	}
	s.mu.Lock()
	s.client = c
	s.openClient = nil
	s.credential = cred
	s.session = nil
	s.cfg.Pan115.AuthMode = "cookie"
	s.cfg.Pan115.AccessToken = ""
	s.cfg.Pan115.RefreshToken = ""
	s.mu.Unlock()
	if err := s.saveOpenSettings("", "", "cookie"); err != nil {
		return err
	}
	s.logs.Info("115 扫码登录完成，登录端：%s", loginApp)
	return nil
}

func (s *Service) ImportCookie(cookie string) error {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return errors.New("115 cookie required")
	}
	cred := &driver.Credential{}
	if err := cred.FromCookie(cookie); err != nil {
		return err
	}
	c := s.newClient()
	c.ImportCredential(cred)
	if err := c.CookieCheck(); err != nil {
		return err
	}
	if err := s.saveCredential(cred); err != nil {
		return err
	}
	s.mu.Lock()
	s.client = c
	s.openClient = nil
	s.credential = cred
	s.session = nil
	s.cfg.Pan115.AuthMode = "cookie"
	s.cfg.Pan115.AccessToken = ""
	s.cfg.Pan115.RefreshToken = ""
	s.mu.Unlock()
	if err := s.saveOpenSettings("", "", "cookie"); err != nil {
		return err
	}
	s.logs.Info("115 Cookie 已导入")
	return nil
}

func (s *Service) ImportOpenToken(accessToken, refreshToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	refreshToken = strings.TrimSpace(refreshToken)
	if accessToken == "" || refreshToken == "" {
		return errors.New("115 open access_token and refresh_token required")
	}
	openClient := s.newImportOpenClient(&accessToken, &refreshToken)
	ctx, cancel := s.contextWithTimeout()
	defer cancel()
	if _, err := openClient.UserInfo(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg.Pan115.AuthMode = "open_token"
	s.cfg.Pan115.AccessToken = accessToken
	s.cfg.Pan115.RefreshToken = refreshToken
	s.openClient = s.newOpenClientLocked()
	s.mu.Unlock()
	if err := s.saveOpenSettings(accessToken, refreshToken, "open_token"); err != nil {
		return err
	}
	s.logs.Info("115 Open Token 已导入")
	return nil
}

func (s *Service) Logout() error {
	s.mu.Lock()
	s.client = nil
	s.openClient = nil
	s.credential = nil
	s.session = nil
	s.cfg.Pan115.AuthMode = "cookie"
	s.cfg.Pan115.AccessToken = ""
	s.cfg.Pan115.RefreshToken = ""
	s.mu.Unlock()
	if err := s.saveOpenSettings("", "", "cookie"); err != nil {
		return err
	}
	if err := os.Remove(s.cfg.Storage.CookieFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.logs.Info("115 登录凭证已清除")
	return nil
}

func (s *Service) Browse(cid string) ([]model.CloudFile, error) {
	return s.browseForJob(0, cid, "")
}

func (s *Service) BrowseForJob(jobID int64, cid string) ([]model.CloudFile, error) {
	return s.browseForJob(jobID, cid, "")
}

func (s *Service) BrowseForJobNamed(jobID int64, cid string, name string) ([]model.CloudFile, error) {
	return s.browseForJob(jobID, cid, name)
}

func (s *Service) browseForJob(jobID int64, cid string, name string) ([]model.CloudFile, error) {
	if cid == "" {
		cid = "0"
	}
	if s.authMode() == "open_token" {
		return s.browseOpenForJobNamed(jobID, cid, name)
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	var files *[]driver.File
	if err := s.with115RetryNamed(jobID, "list", "目录列表", cookieListURL(cid), name, func() error {
		var e error
		files, e = c.List(cid)
		return e
	}); err != nil {
		return nil, err
	}
	parentPath := "/" + strings.Trim(strings.TrimSpace(cid), "/")
	if cid == "0" {
		parentPath = ""
	}
	out := make([]model.CloudFile, 0, len(*files))
	for _, f := range *files {
		out = append(out, cloudFileFromDriver(f, parentPath))
	}
	return out, nil
}

func (s *Service) BrowseRecursive(cid, sourcePath string, max int) ([]model.CloudFile, error) {
	return s.BrowseRecursiveWithProgress(cid, sourcePath, max, nil)
}

func (s *Service) BrowseRecursiveWithProgress(cid, sourcePath string, max int, progress func(dirs int, files int, currentPath string)) ([]model.CloudFile, error) {
	return s.BrowseRecursiveWithProgressForJob(0, cid, sourcePath, max, progress)
}

func (s *Service) BrowseRecursiveForJob(jobID int64, cid, sourcePath string, max int) ([]model.CloudFile, error) {
	return s.BrowseRecursiveWithProgressForJob(jobID, cid, sourcePath, max, nil)
}

func (s *Service) BrowseRecursiveWithProgressForJob(jobID int64, cid, sourcePath string, max int, progress func(dirs int, files int, currentPath string)) ([]model.CloudFile, error) {
	if max <= 0 {
		max = s.cfg.Tasks.IncrementalMaxItems
	}
	var out []model.CloudFile
	dirs := 0
	var walk func(string, string) error
	walk = func(currentCID, currentPath string) error {
		if len(out) >= max {
			return nil
		}
		dirs++
		if progress != nil {
			progress(dirs, len(out), currentPath)
		}
		files, err := s.browseForJob(jobID, currentCID, currentPath)
		if err != nil {
			return err
		}
		for _, f := range files {
			f.SourcePath = util.SlashJoin(currentPath, f.Name)
			if f.IsDir {
				if err := walk(f.ID, f.SourcePath); err != nil {
					return err
				}
				continue
			}
			out = append(out, f)
			if progress != nil {
				progress(dirs, len(out), f.SourcePath)
			}
			if len(out) >= max {
				return nil
			}
		}
		return nil
	}
	return out, walk(cid, sourcePath)
}

func (s *Service) SearchByPickcode(pickcode string) (*model.CloudFile, error) {
	return s.SearchByPickcodeForJob(0, pickcode)
}

func (s *Service) SearchByPickcodeForJob(jobID int64, pickcode string) (*model.CloudFile, error) {
	if s.authMode() == "open_token" {
		return s.searchOpenByPickcodeForJob(jobID, pickcode)
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	var result *driver.SearchResult
	err = s.with115RetryNamed(jobID, "search", "pickcode 搜索", cookieSearchURL(pickcode), pickcode, func() error {
		var e error
		result, e = c.Search(&driver.SearchOption{PickCode: pickcode, Limit: 1, Offset: 0})
		return e
	})
	if err == nil && result != nil && len(result.Files) > 0 {
		f := cloudFileFromDriver(result.Files[0], "/_pickcode")
		if f.SourcePath == "" || f.SourcePath == "/_pickcode/"+f.Name {
			f.SourcePath = util.SlashJoin("_pickcode", f.Name)
		}
		return &f, nil
	}
	var info *driver.DownloadInfo
	if err := s.with115RetryNamed(jobID, "download_url", "pickcode 直链兜底查询", cookieDownloadURL(pickcode), pickcode, func() error {
		var e error
		info, e = c.DownloadWithUA(pickcode, s.cfg.STRM302.ScanUA)
		return e
	}); err != nil {
		return nil, err
	}
	name := info.FileName
	if strings.TrimSpace(name) == "" {
		name = pickcode
	}
	return &model.CloudFile{
		Name:       name,
		Size:       int64(info.FileSize),
		PickCode:   pickcode,
		CID:        "_pickcode",
		SourcePath: util.SlashJoin("_pickcode", name),
		ModTime:    time.Now().Unix(),
	}, nil
}

func (s *Service) DownloadURL(pickcode string) (string, http.Header, error) {
	return s.DownloadURLWithUA(pickcode, s.cfg.STRM302.ScanUA)
}

func (s *Service) DownloadURLWithUA(pickcode, userAgent string) (string, http.Header, error) {
	return s.DownloadURLWithUAForJob(0, pickcode, userAgent)
}

func (s *Service) DownloadURLWithUAForJob(jobID int64, pickcode, userAgent string) (string, http.Header, error) {
	if s.authMode() == "open_token" {
		return s.openDownloadURLForJob(jobID, pickcode, userAgent)
	}
	c, err := s.requireClient()
	if err != nil {
		return "", nil, err
	}
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = s.cfg.STRM302.ScanUA
	}
	var info *driver.DownloadInfo
	if err := s.with115RetryNamed(jobID, "download_url", "下载直链", cookieDownloadURL(pickcode), pickcode, func() error {
		var e error
		info, e = c.DownloadWithUA(pickcode, userAgent)
		return e
	}); err != nil {
		return "", nil, err
	}
	if info == nil || info.Url.Url == "" {
		return "", nil, errors.New("empty 115 download url")
	}
	return info.Url.Url, info.Header, nil
}

func (s *Service) DownloadSmallFile(pickcode string, localPath string, maxSize int64) error {
	return s.DownloadSmallFileForJob(0, pickcode, localPath, maxSize)
}

func (s *Service) DownloadSmallFileForJob(jobID int64, pickcode string, localPath string, maxSize int64) error {
	targetURL, headers, size, err := s.smallFileDownloadInfoForJobNamed(jobID, pickcode, filepath.Base(localPath))
	if err != nil {
		return err
	}
	if size > maxSize {
		return errors.New("file exceeds companion size limit")
	}
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	for k, values := range headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	client := &http.Client{Timeout: time.Duration(s.cfg.Security.OutboundTimeoutSeconds) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("download failed: " + resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	tmp := localPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxSize+1)); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, localPath)
}

func (s *Service) smallFileDownloadInfo(pickcode string) (string, http.Header, int64, error) {
	return s.smallFileDownloadInfoForJob(0, pickcode)
}

func (s *Service) smallFileDownloadInfoForJob(jobID int64, pickcode string) (string, http.Header, int64, error) {
	return s.smallFileDownloadInfoForJobNamed(jobID, pickcode, pickcode)
}

func (s *Service) smallFileDownloadInfoForJobNamed(jobID int64, pickcode string, targetName string) (string, http.Header, int64, error) {
	if s.authMode() == "open_token" {
		return s.openDownloadInfoForJobNamed(jobID, pickcode, s.cfg.STRM302.ScanUA, targetName)
	}
	c, err := s.requireClient()
	if err != nil {
		return "", nil, 0, err
	}
	var info *driver.DownloadInfo
	if err := s.with115RetryNamed(jobID, "download_url", "小文件下载直链", cookieDownloadURL(pickcode), targetName, func() error {
		var e error
		info, e = c.DownloadWithUA(pickcode, s.cfg.STRM302.ScanUA)
		return e
	}); err != nil {
		return "", nil, 0, err
	}
	if info == nil || info.Url.Url == "" {
		return "", nil, 0, errors.New("empty 115 download url")
	}
	return info.Url.Url, info.Header, int64(info.FileSize), nil
}

func (s *Service) requireClient() (*driver.Pan115Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		if err := s.loadCredentialLocked(); err != nil {
			return nil, err
		}
	}
	if s.client == nil {
		return nil, ErrNotLoggedIn
	}
	return s.client, nil
}

func (s *Service) requireOpenClient() *open115.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openClient == nil {
		s.openClient = s.newOpenClientLocked()
	}
	return s.openClient
}

func (s *Service) newClient() *driver.Pan115Client {
	timeout := s.outboundTimeout()
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	c := driver.New(driver.WithClient(&http.Client{Transport: transport, Timeout: timeout}))
	c.SetUserAgent(s.cfg.STRM302.ScanUA)
	return c
}

func (s *Service) outboundTimeout() time.Duration {
	timeout := s.cfg.Security.OutboundTimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}
	return time.Duration(timeout) * time.Second
}

func (s *Service) with115Retry(jobID int64, action string, label string, targetURL string, fn func() error) error {
	return s.with115RetryNamed(jobID, action, label, targetURL, "", fn)
}

func (s *Service) with115RetryNamed(jobID int64, action string, label string, targetURL string, targetName string, fn func() error) error {
	attempts := s.cfg.STRM302.APIRequestRetryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	delays := s.cfg.STRM302.APIRequestDelays()
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		s.wait115Rate(jobID, action, label, targetURL, targetName)
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if attempt == attempts || !is115Retryable(lastErr) {
			return lastErr
		}
		delay := delays[len(delays)-1]
		if attempt-1 < len(delays) {
			delay = delays[attempt-1]
		}
		if s.logs != nil {
			s.logs.Warn("115 请求失败，%s 后重试（%s，第 %d/%d 次）：%v", delay, label, attempt+1, attempts, lastErr)
		}
		time.Sleep(delay)
	}
	return lastErr
}

func is115Retryable(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, token := range []string{
		"tls handshake timeout",
		"i/o timeout",
		"timeout awaiting response headers",
		"context deadline exceeded",
		"connection reset",
		"connection refused",
		"connection aborted",
		"unexpected eof",
		" eof",
		"temporary",
		"server misbehaving",
		"no such host",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func (s *Service) wait115Rate(jobID int64, action string, label string, targetURL string, targetName string) {
	qps := s.cfg.STRM302.ScanQPS
	if qps <= 0 {
		qps = 1
	}
	interval := time.Duration(float64(time.Second) / qps)
	if interval <= 0 {
		s.record115Request(jobID, action, label, targetURL, targetName)
		return
	}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	if !s.last115API.IsZero() {
		if wait := s.last115API.Add(interval).Sub(now); wait > 0 {
			time.Sleep(wait)
			now = time.Now()
		}
	}
	s.last115API = now
	s.record115Request(jobID, action, label, targetURL, targetName)
}

func (s *Service) record115Request(jobID int64, action string, label string, targetURL string, targetName string) {
	if s.db != nil {
		if err := s.db.Increment115Request(jobID, action); err != nil && s.logs != nil {
			s.logs.Warn("记录 115 请求统计失败：%v", err)
		}
	}
	s.recordRecent115Request(jobID, action, label, targetURL, targetName)
}

func (s *Service) recordRecent115Request(jobID int64, action string, label string, targetURL string, targetName string) {
	if strings.TrimSpace(targetURL) == "" {
		targetURL = "115://" + action
	}
	targetName = strings.TrimSpace(targetName)
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	if s.recent115 == nil {
		s.recent115 = map[int64][]map[string]string{}
	}
	item := map[string]string{
		"time":   time.Now().Format(time.RFC3339),
		"action": action,
		"label":  label,
		"url":    targetURL,
	}
	if targetName != "" {
		item["name"] = targetName
	}
	list := append([]map[string]string{item}, s.recent115[jobID]...)
	if len(list) > 5 {
		list = list[:5]
	}
	s.recent115[jobID] = list
	if jobID != 0 {
		global := append([]map[string]string{item}, s.recent115[0]...)
		if len(global) > 5 {
			global = global[:5]
		}
		s.recent115[0] = global
	}
}

func (s *Service) RecentRequests(jobID int64) []map[string]string {
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	source := s.recent115[jobID]
	out := make([]map[string]string, len(source))
	for i, item := range source {
		copyItem := map[string]string{}
		for k, v := range item {
			copyItem[k] = v
		}
		out[i] = copyItem
	}
	return out
}

func cookieListURL(cid string) string {
	v := url.Values{}
	v.Set("cid", strings.TrimSpace(cid))
	return "115-cookie://list?" + v.Encode()
}

func cookieSearchURL(pickcode string) string {
	v := url.Values{}
	v.Set("pickcode", strings.TrimSpace(pickcode))
	return "115-cookie://search?" + v.Encode()
}

func cookieDownloadURL(pickcode string) string {
	v := url.Values{}
	v.Set("pickcode", strings.TrimSpace(pickcode))
	return "115-cookie://download_url?" + v.Encode()
}

func openFilesURL(cid string, limit int64, offset int64) string {
	v := url.Values{}
	v.Set("aid", "1")
	v.Set("asc", "1")
	v.Set("cid", strings.TrimSpace(cid))
	v.Set("fc_mix", "0")
	v.Set("format", "json")
	v.Set("limit", fmt.Sprintf("%d", limit))
	v.Set("natsort", "0")
	v.Set("o", "file_name")
	v.Set("offset", fmt.Sprintf("%d", offset))
	v.Set("record_open_time", "1")
	v.Set("show_dir", "1")
	v.Set("snap", "0")
	return "https://proapi.115.com/open/ufile/files?" + v.Encode()
}

func openDownURL(pickcode string) string {
	v := url.Values{}
	v.Set("pickcode", strings.TrimSpace(pickcode))
	return "https://proapi.115.com/open/ufile/downurl?" + v.Encode()
}

func (s *Service) newOpenClientLocked() *open115.Client {
	return s.newOpenClient(s.cfg.Pan115.AccessToken, s.cfg.Pan115.RefreshToken)
}

func (s *Service) newImportOpenClient(accessToken, refreshToken *string) *open115.Client {
	c := open115.New(
		open115.WithAccessToken(strings.TrimSpace(*accessToken)),
		open115.WithRefreshToken(strings.TrimSpace(*refreshToken)),
		open115.WithOnRefreshToken(func(nextAccessToken string, nextRefreshToken string) {
			*accessToken = nextAccessToken
			*refreshToken = nextRefreshToken
		}),
	)
	c.SetUserAgent(s.cfg.STRM302.ScanUA)
	return c
}

func (s *Service) newOpenClient(accessToken, refreshToken string) *open115.Client {
	c := open115.New(
		open115.WithAccessToken(strings.TrimSpace(accessToken)),
		open115.WithRefreshToken(strings.TrimSpace(refreshToken)),
		open115.WithOnRefreshToken(func(accessToken string, refreshToken string) {
			s.mu.Lock()
			s.cfg.Pan115.AccessToken = accessToken
			s.cfg.Pan115.RefreshToken = refreshToken
			s.mu.Unlock()
			if err := s.saveOpenSettings(accessToken, refreshToken, "open_token"); err != nil {
				s.logs.Warn("保存刷新的 115 Open Token 失败：%v", err)
			}
		}),
	)
	c.SetUserAgent(s.cfg.STRM302.ScanUA)
	return c
}

func (s *Service) authMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authModeLocked()
}

func (s *Service) authModeLocked() string {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Pan115.AuthMode))
	switch mode {
	case "open", "open_token", "openapi", "open_api":
		return "open_token"
	default:
		return "cookie"
	}
}

func (s *Service) contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.outboundTimeout())
}

func (s *Service) loadOpenSettings() error {
	if mode, ok, err := s.db.GetSetting("pan115.auth_mode"); err != nil {
		return err
	} else if ok && strings.TrimSpace(mode) != "" {
		s.cfg.Pan115.AuthMode = mode
	}
	if accessToken, ok, err := s.db.GetSetting("pan115.open_access_token"); err != nil {
		return err
	} else if ok && strings.TrimSpace(accessToken) != "" {
		s.cfg.Pan115.AccessToken = accessToken
	}
	if refreshToken, ok, err := s.db.GetSetting("pan115.open_refresh_token"); err != nil {
		return err
	} else if ok && strings.TrimSpace(refreshToken) != "" {
		s.cfg.Pan115.RefreshToken = refreshToken
	}
	if s.authMode() == "open_token" && s.cfg.Pan115.AccessToken != "" && s.cfg.Pan115.RefreshToken != "" {
		s.mu.Lock()
		s.openClient = s.newOpenClientLocked()
		s.mu.Unlock()
	}
	return nil
}

func (s *Service) saveOpenSettings(accessToken, refreshToken, mode string) error {
	if err := s.db.SetSetting("pan115.auth_mode", mode); err != nil {
		return err
	}
	if err := s.db.SetSetting("pan115.open_access_token", accessToken); err != nil {
		return err
	}
	return s.db.SetSetting("pan115.open_refresh_token", refreshToken)
}

func (s *Service) browseOpen(cid string) ([]model.CloudFile, error) {
	return s.browseOpenForJobNamed(0, cid, "")
}

func (s *Service) browseOpenForJob(jobID int64, cid string) ([]model.CloudFile, error) {
	return s.browseOpenForJobNamed(jobID, cid, "")
}

func (s *Service) browseOpenForJobNamed(jobID int64, cid string, name string) ([]model.CloudFile, error) {
	client := s.requireOpenClient()
	const pageSize int64 = 1150
	var out []model.CloudFile
	for offset := int64(0); ; offset += pageSize {
		var resp *open115.GetFilesResp
		err := s.with115RetryNamed(jobID, "open_get_files", "OpenAPI 目录列表", openFilesURL(cid, pageSize, offset), name, func() error {
			requestCtx, requestCancel := s.contextWithTimeout()
			defer requestCancel()
			var e error
			resp, e = client.GetFiles(requestCtx, &open115.GetFilesReq{
				CID:     cid,
				Limit:   pageSize,
				Offset:  offset,
				ASC:     true,
				O:       "file_name",
				ShowDir: true,
			})
			return e
		})
		if err != nil {
			return nil, err
		}
		for _, f := range resp.Data {
			out = append(out, openCloudFile(f))
		}
		if int64(len(out)) >= resp.Count || len(resp.Data) == 0 {
			break
		}
	}
	return out, nil
}

func (s *Service) searchOpenByPickcode(pickcode string) (*model.CloudFile, error) {
	return s.searchOpenByPickcodeForJob(0, pickcode)
}

func (s *Service) searchOpenByPickcodeForJob(jobID int64, pickcode string) (*model.CloudFile, error) {
	var resp open115.DownURLResp
	if err := s.with115RetryNamed(jobID, "open_down_url", "OpenAPI pickcode 查询", openDownURL(pickcode), pickcode, func() error {
		ctx, cancel := s.contextWithTimeout()
		defer cancel()
		var e error
		resp, e = s.requireOpenClient().DownURL(ctx, pickcode, s.cfg.STRM302.ScanUA)
		return e
	}); err != nil {
		return nil, err
	}
	for fileID, item := range resp {
		name := strings.TrimSpace(item.FileName)
		if name == "" {
			name = pickcode
		}
		itemPickcode := strings.TrimSpace(item.PickCode)
		if itemPickcode == "" {
			itemPickcode = pickcode
		}
		return &model.CloudFile{
			ID:         fileID,
			Name:       name,
			Size:       item.FileSize,
			PickCode:   itemPickcode,
			Sha1:       item.Sha1,
			CID:        "_pickcode",
			SourcePath: util.SlashJoin("_pickcode", name),
			ModTime:    time.Now().Unix(),
		}, nil
	}
	return nil, errors.New("empty 115 open downurl response")
}

func (s *Service) openDownloadURL(pickcode, userAgent string) (string, http.Header, error) {
	return s.openDownloadURLForJob(0, pickcode, userAgent)
}

func (s *Service) openDownloadURLForJob(jobID int64, pickcode, userAgent string) (string, http.Header, error) {
	targetURL, headers, _, err := s.openDownloadInfoForJob(jobID, pickcode, userAgent)
	return targetURL, headers, err
}

func (s *Service) openDownloadInfo(pickcode, userAgent string) (string, http.Header, int64, error) {
	return s.openDownloadInfoForJob(0, pickcode, userAgent)
}

func (s *Service) openDownloadInfoForJob(jobID int64, pickcode, userAgent string) (string, http.Header, int64, error) {
	return s.openDownloadInfoForJobNamed(jobID, pickcode, userAgent, pickcode)
}

func (s *Service) openDownloadInfoForJobNamed(jobID int64, pickcode, userAgent string, targetName string) (string, http.Header, int64, error) {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = s.cfg.STRM302.ScanUA
	}
	var resp open115.DownURLResp
	if err := s.with115RetryNamed(jobID, "open_down_url", "OpenAPI 下载直链", openDownURL(pickcode), targetName, func() error {
		ctx, cancel := s.contextWithTimeout()
		defer cancel()
		var e error
		resp, e = s.requireOpenClient().DownURL(ctx, pickcode, userAgent)
		return e
	}); err != nil {
		return "", nil, 0, err
	}
	for _, item := range resp {
		if item.URL.URL != "" {
			return item.URL.URL, http.Header{"User-Agent": []string{userAgent}}, item.FileSize, nil
		}
	}
	return "", nil, 0, errors.New("empty 115 open download url")
}

func (s *Service) resolveQRCodeApp(raw string) (driver.LoginApp, error) {
	app := strings.ToLower(strings.TrimSpace(raw))
	if app == "" {
		app = strings.ToLower(strings.TrimSpace(s.cfg.Pan115.QRCodeApp))
	}
	if app == "" {
		app = "android"
	}
	switch app {
	case "android":
		return driver.LoginAppAndroid, nil
	case "ios":
		return driver.LoginAppIOS, nil
	case "tv":
		return driver.LoginAppTV, nil
	case "web":
		return driver.LoginAppWeb, nil
	case "alipaymini":
		return driver.LoginAppAlipayMini, nil
	case "wechatmini":
		return driver.LoginAppWechatMini, nil
	case "qandroid":
		return driver.LoginQAppAndroid, nil
	default:
		return "", fmt.Errorf("unsupported 115 qrcode app: %s", raw)
	}
}

func (s *Service) loadCredential() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadCredentialLocked()
}

func (s *Service) loadCredentialLocked() error {
	b, err := os.ReadFile(s.cfg.Storage.CookieFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	plain, err := util.DecryptString(s.secret, strings.TrimSpace(string(b)))
	if err != nil {
		return err
	}
	cred := &driver.Credential{}
	if err := cred.FromCookie(plain); err != nil {
		return err
	}
	c := s.newClient()
	c.ImportCredential(cred)
	s.client = c
	s.credential = cred
	return nil
}

func (s *Service) saveCredential(cred *driver.Credential) error {
	enc, err := util.EncryptString(s.secret, cred.Cookie())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.Storage.CookieFile), 0755); err != nil {
		return err
	}
	return os.WriteFile(s.cfg.Storage.CookieFile, []byte(enc), 0600)
}

func cloudFileFromDriver(f driver.File, parentPath string) model.CloudFile {
	cid := f.ParentID
	if f.IsDirectory {
		cid = f.FileID
	}
	sourcePath := util.SlashJoin(parentPath, f.Name)
	return model.CloudFile{
		ID:         f.FileID,
		ParentID:   f.ParentID,
		Name:       f.Name,
		IsDir:      f.IsDirectory,
		Size:       f.Size,
		PickCode:   f.PickCode,
		Sha1:       f.Sha1,
		CID:        cid,
		SourcePath: sourcePath,
		ModTime:    f.UpdateTime.Unix(),
		ThumbURL:   f.ThumbURL,
	}
}

func openCloudFile(f open115.GetFilesResp_File) model.CloudFile {
	isDir := f.Fc == "0"
	cid := f.Pid
	if isDir {
		cid = f.Fid
	}
	modTime := f.Upt
	if modTime == 0 {
		modTime = f.Uet
	}
	if modTime == 0 {
		modTime = f.UpPt
	}
	name := f.Fn
	if name == "" {
		name = f.Fid
	}
	return model.CloudFile{
		ID:       f.Fid,
		ParentID: f.Pid,
		Name:     name,
		IsDir:    isDir,
		Size:     f.FS,
		PickCode: f.Pc,
		Sha1:     f.Sha1,
		CID:      cid,
		ModTime:  modTime,
		ThumbURL: f.Thumbnail,
	}
}
