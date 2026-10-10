package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server" json:"server"`
	Emby     EmbyConfig     `yaml:"emby" json:"emby"`
	Pan115   Pan115Config   `yaml:"pan115" json:"pan115"`
	STRM302  STRM302Config  `yaml:"strm302" json:"strm302"`
	Tasks    TasksConfig    `yaml:"tasks" json:"tasks"`
	Storage  StorageConfig  `yaml:"storage" json:"storage"`
	Security SecurityConfig `yaml:"security" json:"security"`
}

type ServerConfig struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	BaseURL  string `yaml:"base_url" json:"base_url"`
	LogLevel string `yaml:"log_level" json:"log_level"`
}

type EmbyConfig struct {
	BaseURL               string        `yaml:"base_url" json:"base_url"`
	APIKey                string        `yaml:"api_key" json:"api_key"`
	ProxyHost             string        `yaml:"proxy_host" json:"proxy_host"`
	ProxyPort             int           `yaml:"proxy_port" json:"proxy_port"`
	NotifyRefresh         bool          `yaml:"notify_refresh" json:"notify_refresh"`
	NotifyCooldownMinutes int           `yaml:"notify_cooldown_minutes" json:"notify_cooldown_minutes"`
	LibraryTaskID         string        `yaml:"library_task_id" json:"library_task_id"`
	MediaInfoTaskID       string        `yaml:"mediainfo_task_id" json:"mediainfo_task_id"`
	PathRewrites          []PathRewrite `yaml:"path_rewrites" json:"path_rewrites"`
}

type PathRewrite struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}

type Pan115Config struct {
	AuthMode     string `yaml:"auth_mode" json:"auth_mode"`
	QRCodeApp    string `yaml:"qrcode_app" json:"qrcode_app"`
	AccessToken  string `yaml:"access_token" json:"access_token"`
	RefreshToken string `yaml:"refresh_token" json:"refresh_token"`
}

type STRM302Config struct {
	BatchDir                 string              `yaml:"batch_dir" json:"batch_dir"`
	VideoExtensions          string              `yaml:"video_extensions" json:"video_extensions"`
	RootCID                  string              `yaml:"root_cid" json:"root_cid"`
	ScanSource               string              `yaml:"scan_source" json:"scan_source"`
	ScanMode                 string              `yaml:"scan_mode" json:"scan_mode"`
	ScanQPS                  float64             `yaml:"scan_qps" json:"scan_qps"`
	DetailThreads            int                 `yaml:"detail_threads" json:"detail_threads"`
	ScanUA                   string              `yaml:"scan_ua" json:"scan_ua"`
	CD2MountDir              string              `yaml:"cd2_mount_dir" json:"cd2_mount_dir"`
	OpenListBaseURL          string              `yaml:"openlist_base_url" json:"openlist_base_url"`
	OpenListToken            string              `yaml:"openlist_token" json:"openlist_token"`
	OpenListUsername         string              `yaml:"openlist_username" json:"openlist_username"`
	OpenListPassword         string              `yaml:"openlist_password" json:"openlist_password"`
	OpenListRootPath         string              `yaml:"openlist_root_path" json:"openlist_root_path"`
	OpenListPageSize         int                 `yaml:"openlist_page_size" json:"openlist_page_size"`
	OpenListRefresh          bool                `yaml:"openlist_refresh" json:"openlist_refresh"`
	APIRequestRetryCount     int                 `yaml:"api_request_retry_count" json:"api_request_retry_count"`
	APIRequestRetryDelays    []string            `yaml:"api_request_retry_delays" json:"api_request_retry_delays"`
	RedirectCacheTTL         int                 `yaml:"redirect_cache_ttl" json:"redirect_cache_ttl"`
	RedirectURLTTL           int                 `yaml:"redirect_url_ttl" json:"redirect_url_ttl"`
	RedirectFetchRetryCount  int                 `yaml:"redirect_fetch_retry_count" json:"redirect_fetch_retry_count"`
	RedirectFetchRetryDelays []string            `yaml:"redirect_fetch_retry_delays" json:"redirect_fetch_retry_delays"`
	DownloadCompanion        bool                `yaml:"download_companion" json:"download_companion"`
	CompanionMaxSize         int64               `yaml:"companion_max_size" json:"companion_max_size"`
	CompanionExtensions      map[string][]string `yaml:"companion_extensions" json:"companion_extensions"`
	CompanionKeywords        map[string][]string `yaml:"companion_keywords" json:"companion_keywords"`
}

type TasksConfig struct {
	DefaultTimeout      int    `yaml:"default_timeout" json:"default_timeout"`
	DefaultRetry        int    `yaml:"default_retry" json:"default_retry"`
	RecycleKeepDays     int    `yaml:"recycle_keep_days" json:"recycle_keep_days"`
	PatrolMaxItems      int    `yaml:"patrol_max_items" json:"patrol_max_items"`
	IncrementalMaxItems int    `yaml:"incremental_max_items" json:"incremental_max_items"`
	LocalTriggerEnabled bool   `yaml:"local_trigger_enabled" json:"local_trigger_enabled"`
	LocalTriggerHost    string `yaml:"local_trigger_host" json:"local_trigger_host"`
	LocalTriggerPort    int    `yaml:"local_trigger_port" json:"local_trigger_port"`
}

type StorageConfig struct {
	DataDir       string `yaml:"data_dir" json:"data_dir"`
	DBFile        string `yaml:"db_file" json:"db_file"`
	CookieFile    string `yaml:"cookie_file" json:"cookie_file"`
	LogFile       string `yaml:"log_file" json:"log_file"`
	AuditKeepDays int    `yaml:"audit_keep_days" json:"audit_keep_days"`
}

type SecurityConfig struct {
	CORSAllowedOrigins     []string `yaml:"cors_allowed_origins" json:"cors_allowed_origins"`
	LoginLockMinutes       int      `yaml:"login_lock_minutes" json:"login_lock_minutes"`
	OutboundTimeoutSeconds int      `yaml:"outbound_timeout_seconds" json:"outbound_timeout_seconds"`
}

func Defaults() *Config {
	return &Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 8585, LogLevel: "info"},
		Emby:   EmbyConfig{BaseURL: "http://127.0.0.1:8096", ProxyHost: "0.0.0.0", ProxyPort: 8097, NotifyRefresh: true, NotifyCooldownMinutes: 10},
		Pan115: Pan115Config{AuthMode: "cookie", QRCodeApp: "android"},
		STRM302: STRM302Config{
			BatchDir:                 "/opt/Media",
			VideoExtensions:          "mkv,mp4,avi,ts,m2ts,mov,wmv,flv,webm,iso,rmvb",
			RootCID:                  "2950294963365084864",
			ScanSource:               "115",
			ScanMode:                 "full",
			ScanQPS:                  0.3,
			DetailThreads:            3,
			ScanUA:                   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			CD2MountDir:              "/opt/115open/电视电影",
			OpenListBaseURL:          "http://host.docker.internal:5244",
			OpenListRootPath:         "/电视电影",
			OpenListPageSize:         200,
			APIRequestRetryCount:     4,
			APIRequestRetryDelays:    []string{"10s", "30s", "60s", "120s"},
			RedirectCacheTTL:         600,
			RedirectURLTTL:           3600,
			RedirectFetchRetryCount:  2,
			RedirectFetchRetryDelays: []string{"1s", "3s"},
			DownloadCompanion:        true,
			CompanionMaxSize:         52428800,
			CompanionExtensions: map[string][]string{
				"nfo":      {".nfo"},
				"image":    {".jpg", ".jpeg", ".png", ".webp"},
				"subtitle": {".srt", ".ass", ".ssa", ".vtt", ".sub"},
			},
			CompanionKeywords: map[string][]string{
				"poster": {"-poster", "-cover"},
				"fanart": {"-fanart", "-backdrop"},
				"thumb":  {"-thumb"},
			},
		},
		Tasks:    TasksConfig{DefaultTimeout: 3600, DefaultRetry: 0, RecycleKeepDays: 3, PatrolMaxItems: 10000, IncrementalMaxItems: 10000, LocalTriggerEnabled: true, LocalTriggerHost: "0.0.0.0", LocalTriggerPort: 8590},
		Storage:  StorageConfig{DataDir: "./data", DBFile: "./data/115strm.db", CookieFile: "./data/115.cookie", LogFile: "./data/115strm.log", AuditKeepDays: 14},
		Security: SecurityConfig{CORSAllowedOrigins: []string{}, LoginLockMinutes: 15, OutboundTimeoutSeconds: 60},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func (c *Config) applyDefaults() {
	d := Defaults()
	if c.Server.Host == "" {
		c.Server.Host = d.Server.Host
	}
	if c.Server.Port == 0 {
		c.Server.Port = d.Server.Port
	}
	if c.Server.LogLevel == "" {
		c.Server.LogLevel = d.Server.LogLevel
	}
	if strings.TrimSpace(c.Emby.BaseURL) == "" {
		c.Emby.BaseURL = d.Emby.BaseURL
	}
	if strings.TrimSpace(c.Emby.ProxyHost) == "" {
		c.Emby.ProxyHost = d.Emby.ProxyHost
	}
	if c.Emby.NotifyCooldownMinutes <= 0 {
		c.Emby.NotifyCooldownMinutes = d.Emby.NotifyCooldownMinutes
	}
	if strings.TrimSpace(c.Pan115.QRCodeApp) == "" {
		c.Pan115.QRCodeApp = d.Pan115.QRCodeApp
	}
	if strings.TrimSpace(c.Pan115.AuthMode) == "" {
		c.Pan115.AuthMode = d.Pan115.AuthMode
	}
	if c.STRM302.BatchDir == "" {
		c.STRM302.BatchDir = d.STRM302.BatchDir
	}
	if c.STRM302.VideoExtensions == "" {
		c.STRM302.VideoExtensions = d.STRM302.VideoExtensions
	}
	if strings.TrimSpace(c.STRM302.RootCID) == "" {
		c.STRM302.RootCID = d.STRM302.RootCID
	}
	c.STRM302.ScanSource = normalizeScanSource(c.STRM302.ScanSource)
	if c.STRM302.ScanSource == "" {
		c.STRM302.ScanSource = d.STRM302.ScanSource
	}
	c.STRM302.ScanMode = normalizeScanMode(c.STRM302.ScanMode)
	if c.STRM302.ScanMode == "" {
		c.STRM302.ScanMode = d.STRM302.ScanMode
	}
	if c.STRM302.ScanQPS <= 0 {
		c.STRM302.ScanQPS = d.STRM302.ScanQPS
	}
	if c.STRM302.DetailThreads <= 0 {
		c.STRM302.DetailThreads = d.STRM302.DetailThreads
	}
	if c.STRM302.ScanUA == "" {
		c.STRM302.ScanUA = d.STRM302.ScanUA
	}
	if c.STRM302.OpenListPageSize <= 0 {
		c.STRM302.OpenListPageSize = d.STRM302.OpenListPageSize
	}
	if c.STRM302.APIRequestRetryCount < 0 {
		c.STRM302.APIRequestRetryCount = d.STRM302.APIRequestRetryCount
	}
	if len(c.STRM302.APIRequestRetryDelays) == 0 {
		c.STRM302.APIRequestRetryDelays = d.STRM302.APIRequestRetryDelays
	}
	if c.STRM302.RedirectCacheTTL <= 0 {
		c.STRM302.RedirectCacheTTL = d.STRM302.RedirectCacheTTL
	}
	if c.STRM302.RedirectURLTTL <= 0 {
		c.STRM302.RedirectURLTTL = d.STRM302.RedirectURLTTL
	}
	if c.STRM302.RedirectFetchRetryCount < 0 {
		c.STRM302.RedirectFetchRetryCount = d.STRM302.RedirectFetchRetryCount
	}
	if len(c.STRM302.RedirectFetchRetryDelays) == 0 {
		c.STRM302.RedirectFetchRetryDelays = d.STRM302.RedirectFetchRetryDelays
	}
	if c.STRM302.CompanionMaxSize <= 0 {
		c.STRM302.CompanionMaxSize = d.STRM302.CompanionMaxSize
	}
	if len(c.STRM302.CompanionExtensions) == 0 {
		c.STRM302.CompanionExtensions = d.STRM302.CompanionExtensions
	}
	if len(c.STRM302.CompanionKeywords) == 0 {
		c.STRM302.CompanionKeywords = d.STRM302.CompanionKeywords
	}
	if c.Tasks.DefaultTimeout <= 0 {
		c.Tasks.DefaultTimeout = d.Tasks.DefaultTimeout
	}
	if c.Tasks.RecycleKeepDays <= 0 {
		c.Tasks.RecycleKeepDays = d.Tasks.RecycleKeepDays
	}
	if c.Tasks.PatrolMaxItems <= 0 {
		c.Tasks.PatrolMaxItems = d.Tasks.PatrolMaxItems
	}
	if c.Tasks.IncrementalMaxItems <= 0 {
		c.Tasks.IncrementalMaxItems = d.Tasks.IncrementalMaxItems
	}
	if strings.TrimSpace(c.Tasks.LocalTriggerHost) == "" {
		c.Tasks.LocalTriggerHost = d.Tasks.LocalTriggerHost
	}
	if c.Tasks.LocalTriggerPort <= 0 {
		c.Tasks.LocalTriggerPort = d.Tasks.LocalTriggerPort
	}
	if c.Storage.DataDir == "" {
		c.Storage.DataDir = d.Storage.DataDir
	}
	if c.Storage.DBFile == "" {
		c.Storage.DBFile = d.Storage.DBFile
	}
	if c.Storage.CookieFile == "" {
		c.Storage.CookieFile = d.Storage.CookieFile
	}
	if c.Storage.LogFile == "" {
		c.Storage.LogFile = d.Storage.LogFile
	}
	if c.Storage.AuditKeepDays <= 0 {
		c.Storage.AuditKeepDays = d.Storage.AuditKeepDays
	}
	if c.Security.LoginLockMinutes <= 0 {
		c.Security.LoginLockMinutes = d.Security.LoginLockMinutes
	}
	if c.Security.OutboundTimeoutSeconds <= 0 {
		c.Security.OutboundTimeoutSeconds = d.Security.OutboundTimeoutSeconds
	}
}

func EnsureRuntimeDirs(cfg *Config) error {
	for _, dir := range []string{
		cfg.Storage.DataDir,
		filepath.Dir(cfg.Storage.DBFile),
		filepath.Dir(cfg.Storage.CookieFile),
		filepath.Dir(cfg.Storage.LogFile),
		cfg.STRM302.BatchDir,
	} {
		if dir == "." || strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (c STRM302Config) VideoExtSet() map[string]bool {
	out := map[string]bool{}
	for _, raw := range strings.Split(c.VideoExtensions, ",") {
		ext := strings.TrimSpace(strings.ToLower(raw))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out[ext] = true
	}
	return out
}

func (c STRM302Config) RetryDelays() []time.Duration {
	var out []time.Duration
	for _, raw := range c.RedirectFetchRetryDelays {
		d, err := time.ParseDuration(raw)
		if err == nil {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return []time.Duration{time.Second, 3 * time.Second}
	}
	return out
}

func (c STRM302Config) APIRequestDelays() []time.Duration {
	var out []time.Duration
	for _, raw := range c.APIRequestRetryDelays {
		d, err := time.ParseDuration(raw)
		if err == nil {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return []time.Duration{2 * time.Second, 5 * time.Second}
	}
	return out
}

func normalizeScanSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "115", "pan115", "open115":
		return "115"
	case "cd2", "local", "mount", "local_mount":
		return "cd2"
	case "openlist", "alist":
		return "openlist"
	default:
		return ""
	}
}

func normalizeScanMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "full", "全量":
		return "full"
	case "incremental", "increment", "inc", "增量":
		return "incremental"
	default:
		return ""
	}
}
