package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/logbuffer"
	"115strm/internal/service/strm302"
	"115strm/internal/store"
)

type Service struct {
	cfg     *config.Config
	db      *store.Store
	strm    *strm302.Service
	logs    *logbuffer.Buffer
	cron    *cron.Cron
	parser  cron.Parser
	mu      sync.Mutex
	entry   map[int64]cron.EntryID
	running map[int64]bool
}

func New(cfg *config.Config, db *store.Store, strm *strm302.Service, logs *logbuffer.Buffer) *Service {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return &Service{
		cfg: cfg, db: db, strm: strm, logs: logs,
		cron:   cron.New(cron.WithParser(parser)),
		parser: parser,
		entry:  map[int64]cron.EntryID{}, running: map[int64]bool{},
	}
}

func (s *Service) Load() error {
	tasks, err := s.db.ListTasks()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Enabled {
			if err := s.schedule(task); err != nil {
				s.logs.Warn("定时任务 %s 执行失败：%v", task.Name, err)
			}
		}
	}
	s.cron.Start()
	return nil
}

func (s *Service) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

func (s *Service) List() ([]model.ScheduledTask, error)       { return s.db.ListTasks() }
func (s *Service) Get(id int64) (*model.ScheduledTask, error) { return s.db.TaskByID(id) }
func (s *Service) Logs(id int64) ([]model.TaskRun, error)     { return s.db.TaskRuns(id, 50) }

func (s *Service) Status(id int64) (*model.TaskStatus, error) {
	task, err := s.db.TaskByID(id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("task not found")
	}
	latest, err := s.db.LatestTaskRun(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	running := s.running[id]
	s.mu.Unlock()
	if latest != nil && latest.Status == "running" {
		running = true
	}
	status := &model.TaskStatus{Task: task, Running: running, LatestRun: latest}
	if latest != nil && latest.STRMJobID > 0 {
		if job, err := s.strm.Job(latest.STRMJobID); err == nil {
			status.STRMJob = job
		}
	}
	return status, nil
}

func (s *Service) Create(t *model.ScheduledTask) (int64, error) {
	if err := validateTask(t); err != nil {
		return 0, err
	}
	if _, err := s.parser.Parse(t.CronExpr); err != nil {
		return 0, err
	}
	if t.TimeoutSeconds <= 0 {
		t.TimeoutSeconds = s.cfg.Tasks.DefaultTimeout
	}
	id, err := s.db.CreateTask(t)
	if err != nil {
		return 0, err
	}
	t.ID = id
	if t.Enabled {
		err = s.schedule(*t)
	}
	return id, err
}

func (s *Service) Update(t *model.ScheduledTask) error {
	if err := validateTask(t); err != nil {
		return err
	}
	if _, err := s.parser.Parse(t.CronExpr); err != nil {
		return err
	}
	if err := s.db.UpdateTask(t); err != nil {
		return err
	}
	s.unschedule(t.ID)
	if t.Enabled {
		return s.schedule(*t)
	}
	return nil
}

func (s *Service) Delete(id int64) error {
	s.unschedule(id)
	return s.db.DeleteTask(id)
}

func (s *Service) Toggle(id int64, enabled bool) error {
	if err := s.db.ToggleTask(id, enabled); err != nil {
		return err
	}
	s.unschedule(id)
	if enabled {
		t, err := s.db.TaskByID(id)
		if err != nil || t == nil {
			return err
		}
		return s.schedule(*t)
	}
	return nil
}

func (s *Service) RunNow(id int64) (*model.TaskRun, error) {
	t, err := s.db.TaskByID(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("task not found")
	}
	return s.startRun(*t)
}

func (s *Service) Types() []map[string]string {
	return []map[string]string{
		{"type": "strm_scan", "label": "STRM 扫描"},
		{"type": "incremental", "label": "增量同步（兼容旧任务）"},
		{"type": "hash_patrol", "label": "Hash 巡检"},
		{"type": "recycle_clean", "label": "回收站清理"},
	}
}

func (s *Service) Templates() []model.ScheduledTask {
	return []model.ScheduledTask{
		{Name: "每日 CD2 增量扫描", Type: "strm_scan", CronExpr: "30 17 * * *", ParamsJSON: `{"scan_source":"cd2","scan_mode":"incremental","source_path":""}`, Enabled: true, TimeoutSeconds: 3600},
		{Name: "每周一 CD2 全量扫描", Type: "strm_scan", CronExpr: "0 5 * * 1", ParamsJSON: `{"scan_source":"cd2","scan_mode":"full","source_path":""}`, Enabled: false, TimeoutSeconds: 7200},
		{Name: "每周 Hash 巡检", Type: "hash_patrol", CronExpr: "0 3 * * 0", ParamsJSON: `{"prefix":""}`, Enabled: false, TimeoutSeconds: 3600},
		{Name: "每月回收站清理", Type: "recycle_clean", CronExpr: "0 4 1 * *", ParamsJSON: `{"keep_days":3}`, Enabled: false, TimeoutSeconds: 3600},
	}
}

func (s *Service) schedule(t model.ScheduledTask) error {
	id, err := s.cron.AddFunc(t.CronExpr, func() {
		if _, err := s.startRun(t); err != nil {
			s.logs.Warn("定时任务 %s 本次未启动：%v", t.Name, err)
		}
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.entry[t.ID] = id
	s.mu.Unlock()
	return nil
}

func (s *Service) unschedule(id int64) {
	s.mu.Lock()
	entryID, ok := s.entry[id]
	if ok {
		delete(s.entry, id)
	}
	s.mu.Unlock()
	if ok {
		s.cron.Remove(entryID)
	}
}

func (s *Service) startRun(t model.ScheduledTask) (*model.TaskRun, error) {
	s.mu.Lock()
	if s.running[t.ID] {
		s.mu.Unlock()
		return nil, fmt.Errorf("任务 %s 上一次运行仍未结束", t.Name)
	}
	s.running[t.ID] = true
	s.mu.Unlock()
	clearRunning := true
	defer func() {
		if clearRunning {
			s.mu.Lock()
			delete(s.running, t.ID)
			s.mu.Unlock()
		}
	}()

	start := time.Now()
	strmJobID := int64(0)
	if isSTRMScanTask(t.Type) {
		if s.strm.HasActiveJob() {
			return nil, errors.New("已有 STRM 扫描任务正在运行")
		}
		req, err := taskSTRMRequest(t, s.cfg)
		if err != nil {
			return nil, err
		}
		req.RecycleLabel = t.Name
		jobID, err := s.strm.CreateGenerateJob(req)
		if err != nil {
			return nil, err
		}
		strmJobID = jobID
	}
	runID, err := s.db.CreateTaskRun(t.ID, strmJobID, start)
	if err != nil {
		if strmJobID > 0 {
			_, _ = s.strm.StopGenerateJob(strmJobID)
		}
		return nil, err
	}
	run := &model.TaskRun{ID: runID, TaskID: t.ID, STRMJobID: strmJobID, StartedAt: start, Status: "running"}
	clearRunning = false
	go s.run(t, runID, start, strmJobID)
	return run, nil
}

func (s *Service) run(t model.ScheduledTask, runID int64, start time.Time, strmJobID int64) {
	defer func() {
		s.mu.Lock()
		delete(s.running, t.ID)
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSeconds)*time.Second)
	defer cancel()

	affected, err := s.execute(ctx, t, strmJobID)
	status, msg := "success", "ok"
	if err != nil {
		status, msg = "failed", err.Error()
		s.logs.Warn("任务 %s 执行失败：%v", t.Name, err)
	} else {
		s.logs.Info("任务 %s 执行完成，影响数量=%d", t.Name, affected)
	}
	_ = s.db.FinishTaskRun(runID, status, msg, affected, start)
	_ = s.db.UpdateTaskRunState(t.ID, status, msg, nil)
}

func (s *Service) execute(ctx context.Context, t model.ScheduledTask, strmJobID int64) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	switch t.Type {
	case "strm_scan", "incremental":
		if strmJobID <= 0 {
			return 0, errors.New("STRM job id is missing")
		}
		return s.waitSTRMJob(ctx, strmJobID)
	case "hash_patrol":
		var p struct {
			Prefix string `json:"prefix"`
		}
		_ = json.Unmarshal([]byte(t.ParamsJSON), &p)
		return s.strm.Patrol(p.Prefix, s.cfg.Tasks.PatrolMaxItems)
	case "recycle_clean":
		var p struct {
			KeepDays int `json:"keep_days"`
		}
		_ = json.Unmarshal([]byte(t.ParamsJSON), &p)
		if p.KeepDays <= 0 {
			p.KeepDays = s.cfg.Tasks.RecycleKeepDays
		}
		return s.strm.CleanRecycleRecords(p.KeepDays)
	default:
		return 0, errors.New("unsupported task type")
	}
}

func isSTRMScanTask(taskType string) bool {
	return taskType == "strm_scan" || taskType == "incremental"
}

func taskSTRMRequest(t model.ScheduledTask, cfg *config.Config) (strm302.GenerateRequest, error) {
	var p struct {
		StartCID   string `json:"start_cid"`
		CID        string `json:"cid"`
		SourcePath string `json:"source_path"`
		ScanSource string `json:"scan_source"`
		ScanMode   string `json:"scan_mode"`
	}
	_ = json.Unmarshal([]byte(t.ParamsJSON), &p)
	cid := strings.TrimSpace(p.CID)
	if cid == "" {
		cid = strings.TrimSpace(p.StartCID)
	}
	if cid == "" && cfg != nil {
		cid = strings.TrimSpace(cfg.STRM302.RootCID)
	}
	scanSource := p.ScanSource
	scanMode := p.ScanMode
	if t.Type == "incremental" {
		if strings.TrimSpace(scanSource) == "" {
			scanSource = "115"
		}
		if strings.TrimSpace(scanMode) == "" {
			scanMode = "incremental"
		}
	}
	return strm302.GenerateRequest{
		Type:       "own_directory_recursive",
		CID:        cid,
		SourcePath: p.SourcePath,
		ScanSource: scanSource,
		ScanMode:   scanMode,
	}, nil
}

func (s *Service) waitSTRMJob(ctx context.Context, jobID int64) (int, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		job, err := s.strm.Job(jobID)
		if err != nil {
			return 0, err
		}
		if job == nil {
			return 0, errors.New("STRM job not found")
		}
		affected := strmJobAffected(job)
		switch job.Status {
		case "pending", "running":
		case "success":
			return affected, nil
		case "partial":
			return affected, fmt.Errorf("STRM 任务部分成功：失败=%d %s", job.Failed, job.Error)
		case "failed":
			return affected, fmt.Errorf("STRM 任务失败：%s", job.Error)
		case "paused", "stopped":
			return affected, fmt.Errorf("STRM 任务%s：%s", job.Status, job.Message)
		default:
			if job.FinishedAt != nil {
				return affected, nil
			}
		}
		select {
		case <-ctx.Done():
			_, _ = s.strm.StopGenerateJob(jobID)
			return affected, ctx.Err()
		case <-ticker.C:
		}
	}
}

func strmJobAffected(job *model.STRMJob) int {
	if job == nil {
		return 0
	}
	stats := map[string]int{}
	_ = json.Unmarshal([]byte(job.StatsJSON), &stats)
	total := 0
	for _, key := range []string{"video_new", "video_updated", "media_local", "media_moved", "media_download", "deleted"} {
		total += stats[key]
	}
	if total > 0 {
		return total
	}
	return job.Done
}

func validateTask(t *model.ScheduledTask) error {
	if t.Name == "" || t.Type == "" || t.CronExpr == "" {
		return errors.New("name, type and cron_expr are required")
	}
	switch t.Type {
	case "strm_scan", "incremental", "hash_patrol", "recycle_clean":
		return nil
	default:
		return errors.New("unsupported task type")
	}
}
