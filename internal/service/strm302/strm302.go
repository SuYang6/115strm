package strm302

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/auditlog"
	"115strm/internal/service/driver115"
	"115strm/internal/service/logbuffer"
	"115strm/internal/store"
	"115strm/internal/util"
)

type Service struct {
	cfg            *config.Config
	db             *store.Store
	driver         *driver115.Service
	logs           *logbuffer.Buffer
	audit          *auditlog.Logger
	mu             sync.Mutex
	recycleMu      sync.RWMutex // 协调回收、恢复和永久清理，不复用任务状态锁。
	pauses         map[int64]struct{}
	stops          map[int64]struct{}
	lastEmbyNotify time.Time
}

type GenerateRequest struct {
	Type         string   `json:"type"`
	Pickcodes    []string `json:"pickcodes"`
	CID          string   `json:"cid"`
	SourcePath   string   `json:"source_path"`
	ScanSource   string   `json:"scan_source"`
	ScanMode     string   `json:"scan_mode"`
	RecycleLabel string   `json:"recycle_label,omitempty"`
}

type scanStats struct {
	VideoNew     int
	VideoUpdated int
	VideoSkipped int
	MediaNew     int
	MediaUpdated int
	MediaSkipped int
	Deleted      int
}

type generatePlan struct {
	Videos        []model.CloudFile
	MediaLocal    []model.CloudFile
	MediaDownload []model.CloudFile
	Stats         planStats
}

type planStats struct {
	VideoNew             int `json:"video_new"`
	VideoUpdated         int `json:"video_updated"`
	VideoSkipped         int `json:"video_skipped"`
	VideoMediaInfoNeeded int `json:"video_mediainfo_needed"`
	MediaLocal           int `json:"media_local"`
	MediaMoved           int `json:"media_moved"`
	MediaDownload        int `json:"media_download"`
	MediaSkipped         int `json:"media_skipped"`
	Deleted              int `json:"deleted"`
}

var errScanPaused = errors.New("STRM scan paused")
var errScanStopped = errors.New("STRM scan stopped")

func New(cfg *config.Config, db *store.Store, driver *driver115.Service, logs *logbuffer.Buffer, audit *auditlog.Logger) *Service {
	return &Service{cfg: cfg, db: db, driver: driver, logs: logs, audit: audit, pauses: map[int64]struct{}{}, stops: map[int64]struct{}{}}
}

func (s *Service) CreateGenerateJob(req GenerateRequest) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count, err := s.db.ActiveSTRMJobCount(); err != nil {
		return 0, err
	} else if count > 0 {
		return 0, errors.New("已有 STRM 扫描任务正在运行")
	}
	if req.Type == "" {
		if len(req.Pickcodes) > 0 {
			req.Type = "pickcode_list"
		} else {
			req.Type = "own_directory_recursive"
		}
	}
	if req.ScanSource == "" {
		req.ScanSource = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	if req.ScanSource == "" {
		req.ScanSource = "115"
	}
	if req.ScanMode == "" {
		req.ScanMode = normalizeModeName(s.cfg.STRM302.ScanMode)
	}
	if req.ScanMode == "" {
		req.ScanMode = "full"
	}
	if normalizeSourceName(req.ScanSource) != "115" && strings.TrimSpace(req.CID) == "" {
		req.CID = strings.TrimSpace(s.cfg.STRM302.RootCID)
	}
	if strings.TrimSpace(req.RecycleLabel) == "" {
		req.RecycleLabel = "手动扫描"
	}
	params, _ := json.Marshal(req)
	if req.Type == "own_directory_recursive" {
		_ = s.clearResumeCheckpoint()
	}
	jobID, err := s.db.CreateJob(req.Type, string(params))
	if err != nil {
		return 0, err
	}
	go s.runGenerateJob(jobID, req, nil)
	return jobID, nil
}

func (s *Service) CreateResumeJob() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count, err := s.db.ActiveSTRMJobCount(); err != nil {
		return 0, err
	} else if count > 0 {
		return 0, errors.New("已有 STRM 扫描任务正在运行")
	}
	cp, err := s.readResumeCheckpoint()
	if err != nil {
		return 0, err
	}
	if cp == nil {
		return 0, errors.New("no resumable STRM scan")
	}
	req := cp.Request
	if req.Type != "own_directory_recursive" {
		return 0, errors.New("only directory scans can be resumed")
	}
	if req.ScanSource == "" {
		req.ScanSource = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	if req.ScanMode == "" {
		req.ScanMode = normalizeModeName(s.cfg.STRM302.ScanMode)
	}
	if normalizeSourceName(req.ScanSource) != "115" && strings.TrimSpace(req.CID) == "" {
		req.CID = strings.TrimSpace(s.cfg.STRM302.RootCID)
	}
	if strings.TrimSpace(req.RecycleLabel) == "" {
		req.RecycleLabel = "手动扫描"
	}
	params, _ := json.Marshal(req)
	jobID, err := s.db.CreateJob(req.Type, string(params))
	if err != nil {
		return 0, err
	}
	cp.JobID = jobID
	cp.Error = ""
	cp.Status = "running"
	_ = s.writeResumeCheckpoint(cp)
	go s.runGenerateJob(jobID, req, cp)
	return jobID, nil
}

func (s *Service) PauseGenerateJob(jobID int64) (bool, error) {
	job, err := s.db.JobByID(jobID)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, errors.New("job not found")
	}
	if job.Status != "pending" && job.Status != "running" {
		return false, nil
	}
	s.mu.Lock()
	s.pauses[jobID] = struct{}{}
	s.mu.Unlock()
	return true, nil
}

func (s *Service) StopGenerateJob(jobID int64) (bool, error) {
	job, err := s.db.JobByID(jobID)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, errors.New("job not found")
	}
	if job.Status != "pending" && job.Status != "running" {
		return false, nil
	}
	s.mu.Lock()
	s.stops[jobID] = struct{}{}
	s.mu.Unlock()
	return true, nil
}

func (s *Service) ResumeState() (map[string]any, error) {
	cp, err := s.readResumeCheckpoint()
	if err != nil {
		return nil, err
	}
	if cp == nil {
		return map[string]any{"available": false}, nil
	}
	out := map[string]any{
		"available":  true,
		"checkpoint": cp.summary(),
	}
	if cp.JobID > 0 {
		if job, err := s.db.JobByID(cp.JobID); err == nil && job != nil {
			out["job"] = job
		}
	}
	return out, nil
}

func (s *Service) runGenerateJob(jobID int64, req GenerateRequest, resume *resumeCheckpoint) {
	progress := &model.STRMJob{
		ID:        jobID,
		Status:    "running",
		Phase:     "scanning",
		Message:   "正在扫描目录",
		StatsJSON: "{}",
	}
	saveProgress := func(finish bool) {
		_ = s.db.UpdateJobProgress(progress, finish)
	}
	saveProgress(false)

	var files []model.CloudFile
	unresolvedPickcodes := 0
	var err error
	cleanupMissing := false
	switch req.Type {
	case "pickcode_list":
		progress.Message = "正在按 pickcode 查询文件"
		progress.PlanTotal = len(req.Pickcodes)
		for idx, pc := range req.Pickcodes {
			if s.isStopRequested(jobID) {
				err = errScanStopped
				break
			}
			pc = strings.TrimSpace(pc)
			if pc == "" {
				continue
			}
			progress.CurrentPath = pc
			progress.Scanned = idx
			saveProgress(false)
			f, e := s.driver.SearchByPickcodeForJob(jobID, pc)
			if e != nil {
				s.recordSTRMFailure(failurePickcodeDetail, req, model.CloudFile{PickCode: pc, Name: pc, SourcePath: util.SlashJoin("_pickcode", pc)}, e)
				progress.Failed++
				progress.Error = e.Error()
				continue
			}
			files = append(files, *f)
			progress.Scanned = len(files)
			saveProgress(false)
		}
	case "own_directory_recursive":
		if strings.TrimSpace(req.CID) == "" || req.CID == "0" {
			err = errors.New("incremental or directory generation requires a non-root CID")
		} else {
			files, unresolvedPickcodes, err = s.scanDirectoryWithCheckpoint(jobID, req, resume, progress, saveProgress)
			cleanupMissing = err == nil && normalizeModeName(req.ScanMode) == "full"
		}
	default:
		err = fmt.Errorf("unsupported generate type: %s", req.Type)
	}
	if err != nil {
		if errors.Is(err, errScanPaused) {
			progress.Status = "paused"
			progress.Phase = "paused"
			progress.Message = "任务已暂停，可继续"
			progress.Error = ""
			saveProgress(true)
			s.clearPause(jobID)
			s.logs.Warn("STRM 任务 %d 已暂停", jobID)
			return
		}
		if errors.Is(err, errScanStopped) {
			progress.Status = "stopped"
			progress.Phase = "stopped"
			progress.Message = "任务已停止"
			progress.Error = ""
			saveProgress(true)
			s.clearPause(jobID)
			s.clearStop(jobID)
			if req.Type == "own_directory_recursive" {
				_ = s.clearResumeCheckpoint()
			}
			s.logs.Warn("STRM 任务 %d 已停止", jobID)
			return
		}
		progress.Status = "failed"
		progress.Phase = "failed"
		progress.Error = err.Error()
		saveProgress(true)
		s.clearPause(jobID)
		s.clearStop(jobID)
		s.logs.Error("STRM 任务 %d 失败：%v", jobID, err)
		return
	}
	s.clearPause(jobID)
	if s.isStopRequested(jobID) {
		progress.Status = "stopped"
		progress.Phase = "stopped"
		progress.Message = "任务已停止"
		progress.Error = ""
		saveProgress(true)
		s.clearStop(jobID)
		if req.Type == "own_directory_recursive" {
			_ = s.clearResumeCheckpoint()
		}
		s.logs.Warn("STRM 任务 %d 已停止", jobID)
		return
	}
	if req.Type == "own_directory_recursive" {
		_ = s.clearResumeCheckpoint()
	}

	scanID := newScanID(jobID)
	s.cacheScannedFiles(files, scanID)
	progress.Phase = "planning"
	progress.Message = "正在生成执行计划"
	progress.CurrentPath = ""
	progress.Scanned = len(files)
	progress.Done = 0
	progress.Total = 0
	progress.PlanTotal = 0
	saveProgress(false)

	plan := s.buildGeneratePlan(files)
	stats := plan.Stats
	progress.StatsJSON = statsJSON(stats)
	progress.PlanTotal = plan.Total()
	progress.Total = plan.Total()
	progress.DownloadTotal = len(plan.MediaDownload)
	progress.Message = "执行计划已生成"
	saveProgress(false)

	execute := func(phase, message string, items []model.CloudFile, download bool, fn func(model.CloudFile) (string, error)) bool {
		progress.Phase = phase
		progress.Message = message
		for _, f := range items {
			if s.isStopRequested(jobID) {
				return false
			}
			progress.CurrentPath = f.SourcePath
			saveProgress(false)
			_, e := fn(f)
			if e != nil {
				progress.Failed++
				progress.Error = e.Error()
				s.logs.Warn("生成 %s 失败：%v", f.Name, e)
			} else {
				progress.Done++
			}
			if download {
				progress.DownloadDone++
			}
			progress.StatsJSON = statsJSON(stats)
			saveProgress(false)
		}
		return true
	}

	if !execute("writing", "正在写入 STRM 并登记本地已有媒体", plan.Videos, false, func(f model.CloudFile) (string, error) {
		return s.syncVideoFile(f, scanID)
	}) {
		s.finishStoppedJob(jobID, req, progress, saveProgress)
		return
	}
	if !execute("writing", "正在登记本地已有媒体文件", plan.MediaLocal, false, func(f model.CloudFile) (string, error) {
		return s.syncMediaFile(f, scanID)
	}) {
		s.finishStoppedJob(jobID, req, progress, saveProgress)
		return
	}
	if !execute("downloading", "正在下载缺失媒体文件", plan.MediaDownload, true, func(f model.CloudFile) (string, error) {
		return s.syncMediaFile(f, scanID)
	}) {
		s.finishStoppedJob(jobID, req, progress, saveProgress)
		return
	}

	if cleanupMissing && progress.Failed == 0 && unresolvedPickcodes == 0 {
		if s.isStopRequested(jobID) {
			s.finishStoppedJob(jobID, req, progress, saveProgress)
			return
		}
		progress.Phase = "cleaning"
		progress.Message = "正在清理本次扫描中不存在的本地文件"
		progress.CurrentPath = ""
		saveProgress(false)
		if deleted, e := s.cleanupMissing(scanID, req.SourcePath, files, req.RecycleLabel); e != nil {
			progress.Failed++
			progress.Error = e.Error()
			s.logs.Warn("清理缺失文件失败：%v", e)
		} else {
			stats.Deleted = deleted
			progress.StatsJSON = statsJSON(stats)
		}
	} else if cleanupMissing && progress.Failed > 0 {
		s.logs.Warn("本次扫描有 %d 个失败项，跳过缺失文件清理", progress.Failed)
	} else if cleanupMissing && unresolvedPickcodes > 0 {
		s.logs.Warn("本次扫描有 %d 个文件未补齐 pickcode，跳过缺失文件清理", unresolvedPickcodes)
	} else if normalizeModeName(req.ScanMode) == "incremental" {
		s.logs.Info("本次为增量扫描，不执行缺失文件清理")
	}

	status := "success"
	if progress.Failed > 0 && progress.Done > 0 {
		status = "partial"
	} else if progress.Failed > 0 {
		status = "failed"
	}
	progress.Status = status
	progress.Phase = "finished"
	progress.Message = "任务已完成"
	progress.CurrentPath = ""
	saveProgress(true)
	s.clearStop(jobID)
	if status == "success" {
		s.refreshDashboardMediaLibraryStats()
	}
	s.notifyEmbyIfChanged(stats)
	s.logs.Info("STRM 任务 %d 完成：总数=%d 已处理=%d 失败=%d 视频[新建=%d 更新=%d 跳过=%d 需MediaInfo=%d] 媒体[本地已有=%d 移动=%d 下载=%d 跳过=%d] 清理删除=%d",
		jobID, progress.Total, progress.Done, progress.Failed, stats.VideoNew, stats.VideoUpdated, stats.VideoSkipped, stats.VideoMediaInfoNeeded, stats.MediaLocal, stats.MediaMoved, stats.MediaDownload, stats.MediaSkipped, stats.Deleted)
}

func (s *Service) GenerateOne(f model.CloudFile, all map[string][]model.CloudFile) error {
	_, err := s.syncVideoFile(f, "")
	return err
}

func (p generatePlan) Total() int {
	return len(p.Videos) + len(p.MediaLocal) + len(p.MediaDownload)
}

func (s *Service) buildGeneratePlan(files []model.CloudFile) generatePlan {
	videoExt := s.cfg.STRM302.VideoExtSet()
	plan := generatePlan{}
	for _, f := range files {
		if f.IsDir {
			continue
		}
		if videoExt[strings.ToLower(filepath.Ext(f.Name))] {
			if strings.TrimSpace(f.PickCode) == "" {
				continue
			}
			plan.Videos = append(plan.Videos, f)
			action, needsMediaInfo := s.planVideoAction(f)
			switch action {
			case "skipped":
				plan.Stats.VideoSkipped++
			case "updated":
				plan.Stats.VideoUpdated++
			default:
				plan.Stats.VideoNew++
			}
			if needsMediaInfo {
				plan.Stats.VideoMediaInfoNeeded++
			}
			continue
		}
		if s.mediaType(f.Name) == "" {
			continue
		}
		if strings.TrimSpace(f.PickCode) == "" {
			continue
		}
		switch s.planMediaAction(f) {
		case "download":
			plan.MediaDownload = append(plan.MediaDownload, f)
			plan.Stats.MediaDownload++
		case "moved":
			plan.MediaLocal = append(plan.MediaLocal, f)
			plan.Stats.MediaMoved++
		case "skipped":
			plan.MediaLocal = append(plan.MediaLocal, f)
			plan.Stats.MediaSkipped++
		default:
			plan.MediaLocal = append(plan.MediaLocal, f)
			plan.Stats.MediaLocal++
		}
	}
	return plan
}

func (s *Service) planVideoAction(f model.CloudFile) (string, bool) {
	_, localPath, err := s.videoTarget(f)
	if err != nil {
		return "new", true
	}
	existing := s.lookupExistingSTRMFile(f, localPath)
	if existing == nil {
		return "new", true
	}
	needsMediaInfo := videoNeedsMediaInfo(existing, f)
	if sameSnapshot(existing.FileName, existing.Size, existing.SourceMTime, existing.SourcePath, f.Name, f.Size, f.ModTime, f.SourcePath) && fileExists(localPath) && !needsMediaInfo {
		return "skipped", false
	}
	return "updated", needsMediaInfo
}

func (s *Service) lookupExistingSTRMFile(f model.CloudFile, localPath string) *model.STRMFile {
	existing, _ := s.db.STRMFileBySourcePath(f.SourcePath)
	if existing == nil {
		existing, _ = s.db.STRMFileByLocalPath(localPath)
	}
	if existing == nil {
		existing, _ = s.db.STRMFileByPickcode(f.PickCode)
	}
	return existing
}

func videoNeedsMediaInfo(existing *model.STRMFile, current model.CloudFile) bool {
	if existing == nil {
		return true
	}
	if strings.TrimSpace(existing.PickCode) != strings.TrimSpace(current.PickCode) {
		return true
	}
	if existing.Size != current.Size {
		return true
	}
	if existing.Sha1 != "" && current.Sha1 != "" && !strings.EqualFold(existing.Sha1, current.Sha1) {
		return true
	}
	if existing.CloudFileID != "" && current.ID != "" && existing.CloudFileID != current.ID {
		return true
	}
	return false
}

func (s *Service) planMediaAction(f model.CloudFile) string {
	_, localPath, err := s.mediaTarget(f)
	if err != nil {
		return "download"
	}
	existing, _ := s.db.MediaFileBySourcePath(f.SourcePath)
	if existing == nil {
		existing, _ = s.db.MediaFileByLocalPath(localPath)
	}
	if existing == nil {
		existing, _ = s.db.MediaFileByPickcode(f.PickCode)
	}
	if fileSizeEquals(localPath, f.Size) {
		if existing != nil && sameSnapshot(existing.FileName, existing.Size, existing.SourceMTime, existing.SourcePath, f.Name, f.Size, f.ModTime, f.SourcePath) {
			return "skipped"
		}
		return "local"
	}
	if existing != nil && existing.LocalPath != "" && existing.LocalPath != localPath && fileSizeEquals(existing.LocalPath, f.Size) {
		return "moved"
	}
	return "download"
}

func (s *Service) videoTarget(f model.CloudFile) (string, string, error) {
	relPath := strings.TrimPrefix(f.SourcePath, "/")
	if relPath == "" {
		relPath = util.SlashJoin("_pickcode", f.Name)
		relPath = strings.TrimPrefix(relPath, "/")
	}
	relPath = util.ReplaceExt(relPath, ".strm")
	localPath, err := util.SafeJoin(s.cfg.STRM302.BatchDir, relPath)
	return relPath, localPath, err
}

func (s *Service) mediaTarget(f model.CloudFile) (string, string, error) {
	relPath := strings.TrimPrefix(f.SourcePath, "/")
	if relPath == "" {
		relPath = strings.TrimPrefix(util.SlashJoin("_pickcode", f.Name), "/")
	}
	localPath, err := util.SafeJoin(s.cfg.STRM302.BatchDir, relPath)
	return relPath, localPath, err
}

func statsJSON(stats planStats) string {
	b, err := json.Marshal(stats)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (s *Service) syncVideoFile(f model.CloudFile, scanID string) (string, error) {
	// 扫描写入保持既有并发，恢复与回收持排他锁。
	s.recycleMu.RLock()
	defer s.recycleMu.RUnlock()
	if strings.TrimSpace(f.PickCode) == "" {
		return "", errors.New("empty pickcode")
	}
	relPath, localPath, err := s.videoTarget(f)
	if err != nil {
		return "", err
	}
	action := "new"
	existing := s.lookupExistingSTRMFile(f, localPath)
	if existing != nil {
		action = "updated"
		if sameSnapshot(existing.FileName, existing.Size, existing.SourceMTime, existing.SourcePath, f.Name, f.Size, f.ModTime, f.SourcePath) && fileExists(localPath) && !videoNeedsMediaInfo(existing, f) {
			action = "skipped"
		}
	}
	if action != "skipped" {
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			return "", err
		}
		strmURL := s.redirectURL(f.PickCode, f.Name)
		if err := os.WriteFile(localPath, []byte(strmURL+"\n"), 0644); err != nil {
			return "", err
		}
		if s.audit != nil {
			if action == "new" {
				s.audit.FileOp(auditlog.TypeSTRMCreate, localPath)
			} else {
				s.audit.FileOp(auditlog.TypeSTRMUpdate, localPath)
			}
		}
		if existing != nil && existing.LocalPath != "" && existing.LocalPath != localPath {
			_ = os.Remove(existing.LocalPath)
		}
	}
	record := &model.STRMFile{
		RelativePath:      strings.ReplaceAll(relPath, string(os.PathSeparator), "/"),
		PickCode:          f.PickCode,
		PreviousPickCode:  previousPickcode(existing, f.PickCode),
		FileName:          f.Name,
		Size:              f.Size,
		CID:               f.CID,
		CloudFileID:       f.ID,
		Sha1:              f.Sha1,
		SourceFingerprint: sourceFingerprint(f),
		SourcePath:        normalizeSourcePath(f.SourcePath),
		SourceMTime:       f.ModTime,
		LocalPath:         localPath,
		LocalExists:       true,
		SourceType:        fileSourceType(f),
		CompanionsJSON:    "[]",
		LastScanID:        scanID,
	}
	if err := s.db.UpsertSTRMFile(record); err != nil {
		return "", err
	}
	if action == "skipped" {
		s.logs.Info("已刷新 STRM 元数据 %s", relPath)
	} else {
		s.logs.Info("已生成 STRM %s", relPath)
	}
	return action, nil
}

func (s *Service) DeleteSTRM(id int64) error {
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	f, err := s.db.STRMFileByID(id)
	if err != nil {
		return err
	}
	if f == nil {
		return errors.New("STRM not found")
	}
	_ = os.Remove(f.LocalPath)
	if s.audit != nil {
		s.audit.FileOp(auditlog.TypeSTRMDelete, f.LocalPath)
	}
	var comps []model.CompanionFile
	_ = json.Unmarshal([]byte(f.CompanionsJSON), &comps)
	for _, c := range comps {
		if c.LocalPath != "" {
			used, _ := s.db.CompanionLocalPathReferencedByOther(id, c.LocalPath)
			if !used {
				_ = os.Remove(c.LocalPath)
				if s.audit != nil {
					s.audit.FileOp(auditlog.TypeMetadataDelete, c.LocalPath)
				}
			}
		}
	}
	if err := s.db.MarkSTRMDeleted(id); err != nil {
		return err
	}
	if err := s.db.AddRecycleRecord(id, f.FileName, f.RelativePath); err != nil {
		return err
	}
	s.notifyEmbyIfChanged(planStats{Deleted: 1})
	return nil
}

func (s *Service) List(limit, offset int) ([]model.STRMFile, error) {
	return s.db.ListSTRMFiles(limit, offset)
}

func (s *Service) Job(id int64) (*model.STRMJob, error) {
	return s.db.JobByID(id)
}

func (s *Service) ActiveJob() (*model.STRMJob, error) {
	return s.db.ActiveSTRMJob()
}

func (s *Service) HasActiveJob() bool {
	count, err := s.db.ActiveSTRMJobCount()
	return err == nil && count > 0
}

func (s *Service) GenerateDirectorySync(cid, sourcePath string, max int) (int, error) {
	return s.GenerateRequestSync(GenerateRequest{
		Type:       "own_directory_recursive",
		CID:        cid,
		SourcePath: sourcePath,
		ScanSource: "115",
		ScanMode:   "incremental",
	}, max)
}

func (s *Service) GenerateRequestSync(req GenerateRequest, max int) (int, error) {
	if req.Type == "" {
		req.Type = "own_directory_recursive"
	}
	if req.ScanSource == "" {
		req.ScanSource = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	if req.ScanMode == "" {
		req.ScanMode = normalizeModeName(s.cfg.STRM302.ScanMode)
	}
	if normalizeSourceName(req.ScanSource) != "115" && strings.TrimSpace(req.CID) == "" {
		req.CID = strings.TrimSpace(s.cfg.STRM302.RootCID)
	}
	if strings.TrimSpace(req.RecycleLabel) == "" {
		req.RecycleLabel = "手动扫描"
	}
	if strings.TrimSpace(req.CID) == "" || req.CID == "0" {
		return 0, errors.New("non-root CID is required")
	}
	files, unresolvedPickcodes, err := s.scanTreeForRequest(req, max)
	if err != nil {
		return 0, err
	}
	scanID := newScanID(0)
	s.cacheScannedFiles(files, scanID)
	plan := s.buildGeneratePlan(files)
	affected := 0
	hadFailure := false
	run := func(items []model.CloudFile, fn func(model.CloudFile) (string, error)) {
		for _, f := range items {
			if _, err := fn(f); err != nil {
				s.logs.Warn("增量生成 %s 失败：%v", f.SourcePath, err)
				hadFailure = true
				continue
			}
			affected++
		}
	}
	run(plan.Videos, func(f model.CloudFile) (string, error) { return s.syncVideoFile(f, scanID) })
	run(plan.MediaLocal, func(f model.CloudFile) (string, error) { return s.syncMediaFile(f, scanID) })
	run(plan.MediaDownload, func(f model.CloudFile) (string, error) { return s.syncMediaFile(f, scanID) })
	if !hadFailure {
		if normalizeModeName(req.ScanMode) == "full" && unresolvedPickcodes == 0 {
			if deleted, err := s.cleanupMissing(scanID, req.SourcePath, files, req.RecycleLabel); err != nil {
				return affected, err
			} else {
				plan.Stats.Deleted = deleted
				affected += deleted
			}
		} else if normalizeModeName(req.ScanMode) == "full" && unresolvedPickcodes > 0 {
			s.logs.Warn("本次扫描有 %d 个文件未补齐 pickcode，跳过缺失文件清理", unresolvedPickcodes)
		} else if s.logs != nil {
			s.logs.Info("本次为增量扫描，不执行缺失文件清理")
		}
	} else {
		s.logs.Warn("增量扫描存在失败项，跳过缺失文件清理")
	}
	s.notifyEmbyIfChanged(plan.Stats)
	return affected, nil
}

func (s *Service) scanTreeForRequest(req GenerateRequest, max int) ([]model.CloudFile, int, error) {
	provider, err := s.treeProvider(req)
	if err != nil {
		return nil, 0, err
	}
	root, err := provider.Root(req)
	if err != nil {
		return nil, 0, err
	}
	var files []model.CloudFile
	pending := []scanDirCheckpoint{root}
	for len(pending) > 0 && withinScanLimit(len(files), max) {
		current := pending[0]
		pending = pending[1:]
		if current.ID == "" {
			current.ID = current.CID
		}
		children, err := provider.List(context.Background(), 0, current)
		if err != nil {
			return nil, 0, err
		}
		for _, f := range children {
			f.SourcePath = util.SlashJoin(current.SourcePath, f.Name)
			if f.SourceType == "" {
				f.SourceType = provider.SourceType()
			}
			if f.IsDir {
				pending = append(pending, scanDirCheckpoint{ID: f.ID, CID: f.CID, SourcePath: f.SourcePath})
				continue
			}
			files = append(files, f)
			if !withinScanLimit(len(files), max) {
				break
			}
		}
	}
	return s.hydratePickcodes(0, req, files, nil)
}

func (s *Service) syncMediaFile(f model.CloudFile, scanID string) (string, error) {
	// 扫描写入保持既有并发，恢复与回收持排他锁。
	s.recycleMu.RLock()
	defer s.recycleMu.RUnlock()
	if strings.TrimSpace(f.PickCode) == "" {
		return "", errors.New("empty media pickcode")
	}
	mediaType := s.mediaType(f.Name)
	if mediaType == "" {
		return "", nil
	}
	relPath, localPath, err := s.mediaTarget(f)
	if err != nil {
		return "", err
	}
	statsJobID := jobIDFromScanID(scanID)

	action := "new"
	existing, _ := s.db.MediaFileBySourcePath(f.SourcePath)
	if existing == nil {
		existing, _ = s.db.MediaFileByLocalPath(localPath)
	}
	if existing == nil {
		existing, _ = s.db.MediaFileByPickcode(f.PickCode)
	}
	if existing != nil {
		action = "updated"
		if sameSnapshot(existing.FileName, existing.Size, existing.SourceMTime, existing.SourcePath, f.Name, f.Size, f.ModTime, f.SourcePath) && fileSizeEquals(localPath, f.Size) {
			action = "skipped"
		}
	}

	if action != "skipped" {
		auditType := ""
		switch {
		case fileSizeEquals(localPath, f.Size):
			action = "skipped"
		case existing != nil && existing.LocalPath != "" && existing.LocalPath != localPath && fileSizeEquals(existing.LocalPath, f.Size):
			if err := moveFile(existing.LocalPath, localPath); err != nil {
				if err := s.downloadMediaFile(f, localPath, statsJobID); err != nil {
					return "", err
				}
				auditType = auditlog.TypeMetadataUpdate
			} else {
				action = "updated"
				auditType = auditlog.TypeMetadataMove
			}
		default:
			if err := s.downloadMediaFile(f, localPath, statsJobID); err != nil {
				return "", err
			}
			if existing == nil {
				auditType = auditlog.TypeMetadataCreate
			} else {
				auditType = auditlog.TypeMetadataUpdate
			}
		}
		if auditType != "" && s.audit != nil {
			s.audit.FileOp(auditType, localPath)
		}
		if existing != nil && existing.LocalPath != "" && existing.LocalPath != localPath {
			_ = os.Remove(existing.LocalPath)
		}
	}

	record := &model.MediaFile{
		RelativePath:      strings.ReplaceAll(relPath, string(os.PathSeparator), "/"),
		PickCode:          f.PickCode,
		PreviousPickCode:  previousMediaPickcode(existing, f.PickCode),
		FileName:          f.Name,
		Size:              f.Size,
		CID:               f.CID,
		CloudFileID:       f.ID,
		Sha1:              f.Sha1,
		SourceFingerprint: sourceFingerprint(f),
		SourcePath:        normalizeSourcePath(f.SourcePath),
		SourceMTime:       f.ModTime,
		LocalPath:         localPath,
		LocalExists:       fileSizeEquals(localPath, f.Size),
		MediaType:         mediaType,
		LastScanID:        scanID,
	}
	if err := s.db.UpsertMediaFile(record); err != nil {
		return "", err
	}
	switch action {
	case "skipped":
		s.logs.Info("已刷新媒体元数据 %s", relPath)
	default:
		s.logs.Info("已同步媒体文件 %s", relPath)
	}
	return action, nil
}

func (s *Service) downloadMediaFile(f model.CloudFile, localPath string, jobID int64) error {
	if f.Size > s.cfg.STRM302.CompanionMaxSize {
		return fmt.Errorf("media file exceeds size limit: %s", f.Name)
	}
	return s.driver.DownloadSmallFileForJob(jobID, f.PickCode, localPath, s.cfg.STRM302.CompanionMaxSize)
}

func (s *Service) cleanupMissing(scanID, sourcePath string, scanned []model.CloudFile, recycleLabel string) (int, error) {
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	deleted := 0
	safety := s.cleanupSafety(scanned)
	strms, err := s.db.STRMFilesMissingInScan(scanID, sourcePath)
	if err != nil {
		return deleted, err
	}
	for _, f := range strms {
		switch s.missingSTRMDecision(f, safety) {
		case "move":
			strmExisted := fileExists(f.LocalPath)
			if err := s.recycleDeletedFileLocked(f.LocalPath, f.RelativePath, "缺失STRM", recycleLabel, f.ID); err != nil {
				return deleted, err
			}
			// 只有 STRM 真实移走后才允许联动，不以“源不存在返回 nil”当作移动成功。
			if companionPath := companionMediaInfoPath(f.LocalPath); strmExisted && !fileExists(f.LocalPath) && companionPath != "" && fileExists(companionPath) {
				if err := s.recycleDeletedFileLocked(companionPath, "", "STRM同名MediaInfo", recycleLabel, 0); err != nil {
					if s.logs != nil {
						s.logs.Warn("STRM 同名 MediaInfo 移入回收站失败 %s：%v", companionPath, err)
					}
				} else if s.logs != nil {
					s.logs.Info("已暂存 STRM 同名 MediaInfo %s", companionPath)
				}
			}
			if err := s.db.MarkSTRMDeleted(f.ID); err != nil {
				return deleted, err
			}
			deleted++
			s.logs.Info("已暂存缺失 STRM %s", f.RelativePath)
			if dirDeleted, derr := s.cleanupAncestorEmptyDirs(f.LocalPath, maxAncestorCleanupLevels); derr != nil {
				if s.logs != nil {
					s.logs.Warn("清理空目录失败 %s：%v", f.LocalPath, derr)
				}
			} else if dirDeleted > 0 && s.logs != nil {
				s.logs.Info("已联动清理空目录 %d 个", dirDeleted)
			}
		case "soft":
			if err := s.db.MarkSTRMDeleted(f.ID); err != nil {
				return deleted, err
			}
			deleted++
			s.logs.Info("已软删除被替换 STRM 记录 %s", f.RelativePath)
		default:
			if err := s.db.MarkSTRMMissing(f.ID, scanID); err != nil {
				return deleted, err
			}
			s.logs.Warn("STRM 首次缺失，仅标记不移动 %s", f.RelativePath)
		}
	}
	media, err := s.db.MediaFilesMissingInScan(scanID, sourcePath)
	if err != nil {
		return deleted, err
	}
	for _, f := range media {
		// 本地生成的 MediaInfo 即使曾被登记为普通媒体也必须保护。
		if isMediaInfoCompanion(f.FileName) || isMediaInfoCompanion(filepath.Base(f.LocalPath)) {
			continue
		}
		switch s.missingMediaDecision(f, safety) {
		case "move":
			if err := s.recycleDeletedFileLocked(f.LocalPath, f.RelativePath, "缺失媒体文件", recycleLabel, 0); err != nil {
				return deleted, err
			}
			if err := s.db.MarkMediaDeleted(f.ID); err != nil {
				return deleted, err
			}
			deleted++
			s.logs.Info("已暂存缺失媒体文件 %s", f.RelativePath)
			if dirDeleted, derr := s.cleanupAncestorEmptyDirs(f.LocalPath, maxAncestorCleanupLevels); derr != nil {
				if s.logs != nil {
					s.logs.Warn("清理空目录失败 %s：%v", f.LocalPath, derr)
				}
			} else if dirDeleted > 0 && s.logs != nil {
				s.logs.Info("已联动清理空目录 %d 个", dirDeleted)
			}
		case "soft":
			if err := s.db.MarkMediaDeleted(f.ID); err != nil {
				return deleted, err
			}
			deleted++
			s.logs.Info("已软删除被替换媒体记录 %s", f.RelativePath)
		default:
			if err := s.db.MarkMediaMissing(f.ID, scanID); err != nil {
				return deleted, err
			}
			s.logs.Warn("媒体文件首次缺失，仅标记不移动 %s", f.RelativePath)
		}
	}
	orphaned, err := s.cleanupMissingLocalMediaLocked(sourcePath, scanned, recycleLabel)
	if err != nil {
		return deleted, err
	}
	deleted += orphaned
	if err := s.db.MarkScanCacheMissing(scanID, sourcePath); err != nil {
		return deleted, err
	}
	return deleted, nil
}

func (s *Service) cleanupMissingLocalMedia(sourcePath string, scanned []model.CloudFile, recycleLabel string) (int, error) {
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	return s.cleanupMissingLocalMediaLocked(sourcePath, scanned, recycleLabel)
}

// cleanupMissingLocalMediaLocked 由持有回收操作锁的调用方执行。
func (s *Service) cleanupMissingLocalMediaLocked(sourcePath string, scanned []model.CloudFile, recycleLabel string) (int, error) {
	keep := make(map[string]struct{})
	for _, f := range scanned {
		if f.IsDir {
			continue
		}
		if s.mediaType(f.Name) == "" && !isMediaInfoCompanion(f.Name) {
			continue
		}
		relPath := normalizeSourcePath(f.SourcePath)
		if relPath == "" {
			continue
		}
		keep[relPath] = struct{}{}
	}

	root := s.cfg.STRM302.BatchDir
	scope := normalizeSourcePath(sourcePath)
	if scope != "" {
		var err error
		root, err = util.SafeJoin(s.cfg.STRM302.BatchDir, scope)
		if err != nil {
			return 0, err
		}
	}
	if !fileExists(root) {
		return 0, nil
	}

	deleted := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		// 不依赖云端保留名单；即使配置 .json 为媒体扩展名也不得回收 MediaInfo。
		if isMediaInfoCompanion(entry.Name()) || strings.HasPrefix(entry.Name(), ".recycle-restore-") {
			return nil
		}
		if s.mediaType(entry.Name()) == "" {
			return nil
		}
		relPath, err := filepath.Rel(s.cfg.STRM302.BatchDir, path)
		if err != nil {
			return err
		}
		relPath = normalizeSourcePath(relPath)
		if relPath == "" {
			return nil
		}
		if _, ok := keep[relPath]; ok {
			return nil
		}
		if err := s.recycleDeletedFileLocked(path, relPath, "本地孤儿媒体文件", recycleLabel, 0); err != nil {
			return err
		}
		deleted++
		s.logs.Info("已暂存缺失本地媒体文件 %s", relPath)
		if dirDeleted, derr := s.cleanupAncestorEmptyDirs(path, maxAncestorCleanupLevels); derr != nil && s.logs != nil {
			s.logs.Warn("清理空目录失败 %s：%v", path, derr)
		} else if dirDeleted > 0 && s.logs != nil {
			s.logs.Info("已联动清理空目录 %d 个", dirDeleted)
		}
		return nil
	})
	return deleted, err
}

type cleanupSafetyState struct {
	sourcePaths map[string]struct{}
	localPaths  map[string]struct{}
}

func (s *Service) cleanupSafety(scanned []model.CloudFile) cleanupSafetyState {
	state := cleanupSafetyState{
		sourcePaths: map[string]struct{}{},
		localPaths:  map[string]struct{}{},
	}
	for _, f := range scanned {
		if f.IsDir {
			continue
		}
		sourcePath := normalizeSourcePath(f.SourcePath)
		if sourcePath != "" {
			state.sourcePaths[sourcePath] = struct{}{}
		}
		if s.cfg.STRM302.VideoExtSet()[strings.ToLower(filepath.Ext(f.Name))] {
			if _, localPath, err := s.videoTarget(f); err == nil && localPath != "" {
				state.localPaths[localPath] = struct{}{}
			}
			continue
		}
		if s.mediaType(f.Name) != "" {
			if _, localPath, err := s.mediaTarget(f); err == nil && localPath != "" {
				state.localPaths[localPath] = struct{}{}
			}
		}
	}
	return state
}

func (s *Service) missingSTRMDecision(f model.STRMFile, safety cleanupSafetyState) string {
	if _, ok := safety.sourcePaths[normalizeSourcePath(f.SourcePath)]; ok {
		return "soft"
	}
	if _, ok := safety.localPaths[f.LocalPath]; ok {
		return "soft"
	}
	if used, err := s.db.STRMLocalPathReferencedByOther(f.ID, f.LocalPath); used || err != nil {
		return "soft"
	}
	if f.LocalPath != "" && fileExists(f.LocalPath) && !strmFileContainsPickcode(f.LocalPath, f.PickCode) {
		return "soft"
	}
	// 第一次缺失即删除：不再走两遍安全网（MarkSTRMMissing 仍保留作死代码兼容）。
	return "move"
}

func (s *Service) missingMediaDecision(f model.MediaFile, safety cleanupSafetyState) string {
	if _, ok := safety.sourcePaths[normalizeSourcePath(f.SourcePath)]; ok {
		return "soft"
	}
	if _, ok := safety.localPaths[f.LocalPath]; ok {
		return "soft"
	}
	if used, _ := s.db.MediaLocalPathReferencedByOther(f.ID, f.LocalPath); used {
		return "soft"
	}
	// 第一次缺失即删除：不再走两遍安全网（MarkMediaMissing 仍保留作死代码兼容）。
	return "move"
}

func strmFileContainsPickcode(path, pickcode string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	raw := strings.TrimSpace(string(b))
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.Contains(raw, "pickcode="+url.QueryEscape(pickcode)) || strings.Contains(raw, "pickcode="+pickcode)
	}
	return u.Query().Get("pickcode") == pickcode
}

func (s *Service) Patrol(prefix string, max int) (int, error) {
	if max <= 0 {
		max = s.cfg.Tasks.PatrolMaxItems
	}
	files, err := s.db.ListSTRMFiles(max, 0)
	if err != nil {
		return 0, err
	}
	checked := 0
	for _, f := range files {
		if prefix != "" && !strings.HasPrefix(f.RelativePath, strings.TrimPrefix(prefix, "/")) {
			continue
		}
		_, err := s.driver.SearchByPickcode(f.PickCode)
		if err != nil {
			s.logs.Warn("Hash 巡检发现缺失或变更：%s：%v", f.RelativePath, err)
		}
		checked++
	}
	return checked, nil
}

func (s *Service) redirectURL(pickcode, fileName string) string {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	if base == "" {
		host := s.cfg.Server.Host
		if host == "0.0.0.0" || host == "" {
			host = "localhost"
		}
		base = fmt.Sprintf("http://%s:%d", host, s.cfg.Server.Port)
	}
	v := url.Values{}
	v.Set("pickcode", pickcode)
	if fileName != "" {
		v.Set("file_name", fileName)
	}
	return base + "/api/strm302/redirect?" + v.Encode()
}

func companionPool(files []model.CloudFile) map[string][]model.CloudFile {
	out := map[string][]model.CloudFile{}
	for _, f := range files {
		if f.IsDir {
			continue
		}
		dir := cloudFileSourceDir(f.SourcePath)
		out[dir] = append(out[dir], f)
	}
	return out
}

func (s *Service) downloadCompanions(video model.CloudFile, all map[string][]model.CloudFile, localDir string) []model.CompanionFile {
	if !s.cfg.STRM302.DownloadCompanion {
		return nil
	}
	dir := cloudFileSourceDir(video.SourcePath)
	var out []model.CompanionFile
	seen := map[string]bool{}
	for _, searchDir := range companionSearchDirs(dir) {
		for _, candidate := range all[searchDir] {
			key := candidate.PickCode
			if key == "" {
				key = candidate.SourcePath
			}
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			companion := s.downloadCompanionCandidate(video, candidate, localDir)
			if companion != nil {
				out = append(out, *companion)
			}
		}
	}
	return out
}

func (s *Service) downloadCompanionCandidate(video model.CloudFile, candidate model.CloudFile, fallbackDir string) *model.CompanionFile {
	// 扫描写入保持既有并发，恢复与回收持排他锁。
	s.recycleMu.RLock()
	defer s.recycleMu.RUnlock()
	if candidate.PickCode == video.PickCode || candidate.IsDir || candidate.Size > s.cfg.STRM302.CompanionMaxSize {
		return nil
	}
	if !s.isCompanion(candidate.Name) {
		return nil
	}
	localPath, err := s.companionLocalPath(candidate, fallbackDir)
	status := "skipped"
	errText := ""
	if err != nil {
		status = "failed"
		errText = err.Error()
	} else if stat, statErr := os.Stat(localPath); statErr != nil || stat.Size() != candidate.Size {
		if err := s.driver.DownloadSmallFile(candidate.PickCode, localPath, s.cfg.STRM302.CompanionMaxSize); err != nil {
			status = "failed"
			errText = err.Error()
		} else {
			status = "downloaded"
		}
	}
	return &model.CompanionFile{
		Name: candidate.Name, PickCode: candidate.PickCode, SourcePath: candidate.SourcePath,
		LocalPath: localPath, Size: candidate.Size, ModTime: candidate.ModTime, Status: status, Error: errText,
	}
}

func (s *Service) companionLocalPath(candidate model.CloudFile, fallbackDir string) (string, error) {
	rel := strings.TrimPrefix(candidate.SourcePath, "/")
	if rel != "" {
		return util.SafeJoin(s.cfg.STRM302.BatchDir, rel)
	}
	return util.SafeJoin(fallbackDir, candidate.Name)
}

func companionSearchDirs(dir string) []string {
	dir = strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	if dir == "" || dir == "." {
		return []string{""}
	}
	var out []string
	for {
		out = append(out, dir)
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == "." || parent == "/" || parent == dir {
			out = append(out, "")
			return out
		}
		dir = parent
	}
}

func cloudFileSourceDir(sourcePath string) string {
	dir := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(sourcePath, "/")))
	if dir == "." || dir == "/" {
		return ""
	}
	return strings.Trim(dir, "/")
}

func (s *Service) isCompanion(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	for _, group := range s.cfg.STRM302.CompanionExtensions {
		for _, allowed := range group {
			if ext == strings.ToLower(allowed) {
				return true
			}
		}
	}
	return false
}

func (s *Service) mediaType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return ""
	}
	for group, allowedList := range s.cfg.STRM302.CompanionExtensions {
		for _, allowed := range allowedList {
			if ext == strings.ToLower(allowed) {
				return group
			}
		}
	}
	return ""
}

func (s *scanStats) countVideo(action string) {
	switch action {
	case "new":
		s.VideoNew++
	case "skipped":
		s.VideoSkipped++
	case "updated":
		s.VideoUpdated++
	}
}

func (s *scanStats) countMedia(action string) {
	switch action {
	case "new":
		s.MediaNew++
	case "skipped":
		s.MediaSkipped++
	case "updated":
		s.MediaUpdated++
	}
}

func (s *Service) isPauseRequested(jobID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pauses[jobID]
	return ok
}

func (s *Service) isStopRequested(jobID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.stops[jobID]
	return ok
}

func (s *Service) clearPause(jobID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pauses, jobID)
}

func (s *Service) clearStop(jobID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stops, jobID)
}

func (s *Service) finishStoppedJob(jobID int64, req GenerateRequest, progress *model.STRMJob, saveProgress func(bool)) {
	progress.Status = "stopped"
	progress.Phase = "stopped"
	progress.Message = "任务已停止"
	progress.Error = ""
	progress.CurrentPath = ""
	saveProgress(true)
	s.clearPause(jobID)
	s.clearStop(jobID)
	if req.Type == "own_directory_recursive" {
		_ = s.clearResumeCheckpoint()
	}
	if s.logs != nil {
		s.logs.Warn("STRM 任务 %d 已停止", jobID)
	}
}

func newScanID(jobID int64) string {
	if jobID > 0 {
		return fmt.Sprintf("job-%d-%d", jobID, time.Now().UnixNano())
	}
	return fmt.Sprintf("sync-%d", time.Now().UnixNano())
}

func jobIDFromScanID(scanID string) int64 {
	if !strings.HasPrefix(scanID, "job-") {
		return 0
	}
	rest := strings.TrimPrefix(scanID, "job-")
	idx := strings.Index(rest, "-")
	if idx <= 0 {
		return 0
	}
	var jobID int64
	if _, err := fmt.Sscanf(rest[:idx], "%d", &jobID); err != nil {
		return 0
	}
	return jobID
}

func sameSnapshot(oldName string, oldSize int64, oldMTime int64, oldSourcePath string, newName string, newSize int64, newMTime int64, newSourcePath string) bool {
	return oldName == newName &&
		oldSize == newSize &&
		oldMTime == newMTime &&
		normalizeSourcePath(oldSourcePath) == normalizeSourcePath(newSourcePath)
}

func previousPickcode(existing *model.STRMFile, current string) string {
	if existing == nil || existing.PickCode == "" || existing.PickCode == current {
		return ""
	}
	return existing.PickCode
}

func previousMediaPickcode(existing *model.MediaFile, current string) string {
	if existing == nil || existing.PickCode == "" || existing.PickCode == current {
		return ""
	}
	return existing.PickCode
}

func sourceFingerprint(f model.CloudFile) string {
	return strings.Join([]string{
		fileSourceType(f),
		normalizeSourcePath(f.SourcePath),
		f.ID,
		f.Sha1,
		fmt.Sprintf("%d", f.Size),
		fmt.Sprintf("%d", f.ModTime),
	}, "|")
}

func normalizeSourcePath(path string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimPrefix(path, "/"), "\\", "/"), "/")
}

// companionMediaInfoPath 返回 STRM 同目录、同基名、扩展名替换为 -mediainfo.json 的兄弟路径。
// 这是 Emby 神医助手 / TinyMediaManager 的通用约定：xxx.strm <-> xxx-mediainfo.json。
// 入参不是 .strm 或为空时返回空字符串。
func companionMediaInfoPath(localPath string) string {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return ""
	}
	if strings.ToLower(filepath.Ext(localPath)) != ".strm" {
		return ""
	}
	base := strings.TrimSuffix(localPath, filepath.Ext(localPath))
	return base + "-mediainfo.json"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func fileSizeEquals(path string, size int64) bool {
	if path == "" {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir() && stat.Size() == size
}

func moveFile(from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	if fileExists(to) {
		if err := os.Remove(to); err != nil {
			return err
		}
	}
	return os.Rename(from, to)
}

func (s *Service) cacheScannedFiles(files []model.CloudFile, scanID string) {
	for _, f := range files {
		if f.IsDir || strings.TrimSpace(f.PickCode) == "" || !s.needsPickcode(f.Name) {
			continue
		}
		f.SourcePath = normalizeSourcePath(f.SourcePath)
		if f.SourceType == "" {
			f.SourceType = "115"
		}
		if err := s.db.UpsertScanCacheFile(f, scanID); err != nil && s.logs != nil {
			s.logs.Warn("写入扫描缓存失败：%s：%v", f.SourcePath, err)
		}
	}
}

func fileSourceType(f model.CloudFile) string {
	source := normalizeSourceName(f.SourceType)
	if source == "" {
		return "115"
	}
	return source
}

func withinScanLimit(count, max int) bool {
	return max <= 0 || count < max
}
