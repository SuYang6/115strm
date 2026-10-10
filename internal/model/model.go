package model

import "time"

type User struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

type CloudFile struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"`
	Name       string `json:"name"`
	IsDir      bool   `json:"is_dir"`
	Size       int64  `json:"size"`
	PickCode   string `json:"pickcode"`
	Sha1       string `json:"sha1"`
	CID        string `json:"cid"`
	SourcePath string `json:"source_path"`
	SourceType string `json:"source_type"`
	ModTime    int64  `json:"mod_time"`
	ThumbURL   string `json:"thumb_url"`
}

type STRMFile struct {
	ID                 int64      `json:"id"`
	RelativePath       string     `json:"relative_path"`
	PickCode           string     `json:"pickcode"`
	PreviousPickCode   string     `json:"previous_pickcode,omitempty"`
	FileName           string     `json:"file_name"`
	Size               int64      `json:"size"`
	CID                string     `json:"cid"`
	CloudFileID        string     `json:"cloud_file_id,omitempty"`
	Sha1               string     `json:"sha1,omitempty"`
	SourceFingerprint  string     `json:"source_fingerprint,omitempty"`
	SourcePath         string     `json:"source_path"`
	SourceMTime        int64      `json:"source_mtime"`
	LocalPath          string     `json:"local_path"`
	LocalExists        bool       `json:"local_exists"`
	SourceType         string     `json:"source_type"`
	CompanionsJSON     string     `json:"companions_json"`
	LastScanID         string     `json:"last_scan_id"`
	MissingSinceScanID string     `json:"missing_since_scan_id,omitempty"`
	ReplacedByID       int64      `json:"replaced_by_id,omitempty"`
	GeneratedAt        time.Time  `json:"generated_at"`
	LastAccessAt       *time.Time `json:"last_access_at,omitempty"`
	AccessCount        int64      `json:"access_count"`
	ReplacedAt         *time.Time `json:"replaced_at,omitempty"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

type MediaFile struct {
	ID                 int64      `json:"id"`
	RelativePath       string     `json:"relative_path"`
	PickCode           string     `json:"pickcode"`
	PreviousPickCode   string     `json:"previous_pickcode,omitempty"`
	FileName           string     `json:"file_name"`
	Size               int64      `json:"size"`
	CID                string     `json:"cid"`
	CloudFileID        string     `json:"cloud_file_id,omitempty"`
	Sha1               string     `json:"sha1,omitempty"`
	SourceFingerprint  string     `json:"source_fingerprint,omitempty"`
	SourcePath         string     `json:"source_path"`
	SourceMTime        int64      `json:"source_mtime"`
	LocalPath          string     `json:"local_path"`
	LocalExists        bool       `json:"local_exists"`
	MediaType          string     `json:"media_type"`
	LastScanID         string     `json:"last_scan_id"`
	MissingSinceScanID string     `json:"missing_since_scan_id,omitempty"`
	ReplacedByID       int64      `json:"replaced_by_id,omitempty"`
	GeneratedAt        time.Time  `json:"generated_at"`
	ReplacedAt         *time.Time `json:"replaced_at,omitempty"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

type ScanCacheFile struct {
	ID                int64      `json:"id"`
	SourcePath        string     `json:"source_path"`
	PickCode          string     `json:"pickcode"`
	FileName          string     `json:"file_name"`
	Size              int64      `json:"size"`
	CID               string     `json:"cid"`
	CloudFileID       string     `json:"cloud_file_id,omitempty"`
	Sha1              string     `json:"sha1,omitempty"`
	SourceFingerprint string     `json:"source_fingerprint,omitempty"`
	SourceMTime       int64      `json:"source_mtime"`
	SourceType        string     `json:"source_type"`
	LastScanID        string     `json:"last_scan_id"`
	UpdatedAt         time.Time  `json:"updated_at"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
}

type RecycleItem struct {
	ID                  int64      `json:"id"`
	STRMFileID          int64      `json:"strm_file_id,omitempty"`
	DeletedFileName     string     `json:"deleted_file_name"`
	DeletedRelativePath string     `json:"deleted_relative_path"`
	OriginalPath        string     `json:"original_path"`
	RecyclePath         string     `json:"recycle_path"`
	Size                int64      `json:"size"`
	Reason              string     `json:"reason"`
	SourceLabel         string     `json:"source_label"`
	DeletedAt           time.Time  `json:"deleted_at"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	PurgedAt            *time.Time `json:"purged_at,omitempty"`
	RestoredAt          *time.Time `json:"restored_at,omitempty"`
}

// RecycleRestoreRequest 仅使用数据库记录 ID 指定恢复内容。
type RecycleRestoreRequest struct {
	IDs []int64 `json:"ids"`
}

// RecycleRestoreResult 保存单项恢复结果，便于部分成功后重试。
type RecycleRestoreResult struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"`
	RelativePath string `json:"relative_path"`
	Message      string `json:"message"`
}

// RecycleRestoreResponse 汇总本批次恢复结果。
type RecycleRestoreResponse struct {
	Requested       int                    `json:"requested"`
	Restored        int                    `json:"restored"`
	AlreadyRestored int                    `json:"already_restored"`
	Conflicts       int                    `json:"conflicts"`
	Failed          int                    `json:"failed"`
	Results         []RecycleRestoreResult `json:"results"`
}

type STRMFailure struct {
	ID          int64      `json:"id"`
	FailureKey  string     `json:"failure_key"`
	Operation   string     `json:"operation"`
	SourcePath  string     `json:"source_path"`
	PickCode    string     `json:"pickcode"`
	FileName    string     `json:"file_name"`
	Size        int64      `json:"size"`
	CID         string     `json:"cid"`
	SourceMTime int64      `json:"source_mtime"`
	SourceType  string     `json:"source_type"`
	RequestJSON string     `json:"request_json"`
	FileJSON    string     `json:"file_json"`
	Status      string     `json:"status"`
	Error       string     `json:"error"`
	RetryCount  int        `json:"retry_count"`
	LastRetryAt *time.Time `json:"last_retry_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CompanionFile struct {
	Name       string `json:"name"`
	PickCode   string `json:"pickcode"`
	SourcePath string `json:"source_path"`
	LocalPath  string `json:"local_path"`
	Size       int64  `json:"size"`
	ModTime    int64  `json:"mod_time"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type STRMJob struct {
	ID             int64          `json:"id"`
	Type           string         `json:"type"`
	Status         string         `json:"status"`
	Phase          string         `json:"phase"`
	Total          int            `json:"total"`
	Done           int            `json:"done"`
	Failed         int            `json:"failed"`
	Scanned        int            `json:"scanned"`
	PlanTotal      int            `json:"plan_total"`
	DownloadTotal  int            `json:"download_total"`
	DownloadDone   int            `json:"download_done"`
	CurrentPath    string         `json:"current_path"`
	Message        string         `json:"message"`
	StatsJSON      string         `json:"stats_json"`
	Pan115Requests map[string]any `json:"pan115_requests,omitempty"`
	ParamsJSON     string         `json:"params_json"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type ScheduledTask struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	CronExpr       string     `json:"cron_expr"`
	ParamsJSON     string     `json:"params_json"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	LastStatus     string     `json:"last_status"`
	LastError      string     `json:"last_error"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	RetryCount     int        `json:"retry_count"`
	CreatedAt      time.Time  `json:"created_at"`
}

type TaskRun struct {
	ID            int64      `json:"id"`
	TaskID        int64      `json:"task_id"`
	STRMJobID     int64      `json:"strm_job_id,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	Status        string     `json:"status"`
	DurationMS    int64      `json:"duration_ms"`
	Message       string     `json:"message"`
	AffectedCount int        `json:"affected_count"`
}

type TaskStatus struct {
	Task      *ScheduledTask `json:"task"`
	Running   bool           `json:"running"`
	LatestRun *TaskRun       `json:"latest_run,omitempty"`
	STRMJob   *STRMJob       `json:"strm_job,omitempty"`
}
