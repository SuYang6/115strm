package strm302

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"115strm/internal/model"
	"115strm/internal/util"
)

const resumeCheckpointVersion = 1

type scanDirCheckpoint struct {
	ID         string `json:"id"`
	CID        string `json:"cid"`
	SourcePath string `json:"source_path"`
}

type resumeCheckpoint struct {
	Version     int                 `json:"version"`
	JobID       int64               `json:"job_id"`
	Request     GenerateRequest     `json:"request"`
	PendingDirs []scanDirCheckpoint `json:"pending_dirs"`
	Files       []model.CloudFile   `json:"files"`
	DirsScanned int                 `json:"dirs_scanned"`
	CurrentPath string              `json:"current_path"`
	Status      string              `json:"status"`
	Error       string              `json:"error"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type resumeCheckpointSummary struct {
	JobID       int64     `json:"job_id"`
	CID         string    `json:"cid"`
	SourcePath  string    `json:"source_path"`
	Files       int       `json:"files"`
	PendingDirs int       `json:"pending_dirs"`
	DirsScanned int       `json:"dirs_scanned"`
	CurrentPath string    `json:"current_path"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Service) scanDirectoryWithCheckpoint(jobID int64, req GenerateRequest, resume *resumeCheckpoint, progress *model.STRMJob, saveProgress func(bool)) ([]model.CloudFile, int, error) {
	max := 0
	provider, err := s.treeProvider(req)
	if err != nil {
		return nil, 0, err
	}
	root, err := provider.Root(req)
	if err != nil {
		return nil, 0, err
	}

	cp := resume
	if cp == nil {
		cp = &resumeCheckpoint{
			Version:     resumeCheckpointVersion,
			JobID:       jobID,
			Request:     req,
			PendingDirs: []scanDirCheckpoint{root},
			Status:      "running",
			UpdatedAt:   time.Now(),
		}
	} else {
		cp.JobID = jobID
		cp.Error = ""
		cp.Status = "running"
		if cp.Version == 0 {
			cp.Version = resumeCheckpointVersion
		}
		if cp.Request.Type == "" {
			cp.Request = req
		}
	}
	if err := s.writeResumeCheckpoint(cp); err != nil {
		return nil, 0, err
	}

	for len(cp.PendingDirs) > 0 && withinScanLimit(len(cp.Files), max) {
		if s.isStopRequested(jobID) {
			cp.Status = "stopped"
			cp.Error = ""
			cp.UpdatedAt = time.Now()
			_ = s.writeResumeCheckpoint(cp)
			return nil, 0, errScanStopped
		}
		if s.isPauseRequested(jobID) {
			cp.Status = "paused"
			cp.Error = ""
			cp.UpdatedAt = time.Now()
			_ = s.writeResumeCheckpoint(cp)
			return nil, 0, errScanPaused
		}
		current := cp.PendingDirs[0]
		cp.PendingDirs = cp.PendingDirs[1:]
		if current.ID == "" {
			current.ID = current.CID
		}
		cp.CurrentPath = current.SourcePath
		cp.Status = "running"
		cp.UpdatedAt = time.Now()
		progress.Message = "正在扫描目录，已扫描 " + itoa(cp.DirsScanned) + " 个目录"
		progress.Scanned = len(cp.Files)
		progress.CurrentPath = current.SourcePath
		saveProgress(false)

		children, err := provider.List(context.Background(), jobID, current)
		if err != nil {
			cp.PendingDirs = append([]scanDirCheckpoint{current}, cp.PendingDirs...)
			cp.Status = "failed"
			cp.Error = err.Error()
			cp.UpdatedAt = time.Now()
			_ = s.writeResumeCheckpoint(cp)
			return nil, 0, err
		}
		if s.isStopRequested(jobID) {
			cp.Status = "stopped"
			cp.Error = ""
			cp.UpdatedAt = time.Now()
			_ = s.writeResumeCheckpoint(cp)
			return nil, 0, errScanStopped
		}
		if s.isPauseRequested(jobID) {
			cp.PendingDirs = append([]scanDirCheckpoint{current}, cp.PendingDirs...)
			cp.Status = "paused"
			cp.Error = ""
			cp.UpdatedAt = time.Now()
			_ = s.writeResumeCheckpoint(cp)
			return nil, 0, errScanPaused
		}

		cp.DirsScanned++
		for _, f := range children {
			f.SourcePath = util.SlashJoin(current.SourcePath, f.Name)
			if f.SourceType == "" {
				f.SourceType = provider.SourceType()
			}
			if f.IsDir {
				cp.PendingDirs = append(cp.PendingDirs, scanDirCheckpoint{
					ID:         f.ID,
					CID:        f.CID,
					SourcePath: f.SourcePath,
				})
				continue
			}
			cp.Files = append(cp.Files, f)
			progress.Scanned = len(cp.Files)
			progress.CurrentPath = f.SourcePath
			saveProgress(false)
			if !withinScanLimit(len(cp.Files), max) {
				break
			}
		}
		cp.CurrentPath = current.SourcePath
		cp.Status = "running"
		cp.UpdatedAt = time.Now()
		if err := s.writeResumeCheckpoint(cp); err != nil {
			return nil, 0, err
		}
	}
	progress.Scanned = len(cp.Files)
	progress.CurrentPath = ""
	progress.Phase = "hydrating"
	progress.Message = "正在补齐 CD2/OpenList 文件 pickcode"
	saveProgress(false)
	return s.hydratePickcodes(jobID, req, cp.Files, func(done, total int, currentPath, message string) {
		progress.Phase = "hydrating"
		progress.Message = message
		progress.CurrentPath = currentPath
		progress.Done = done
		progress.PlanTotal = total
		saveProgress(false)
	})
}

func (s *Service) checkpointPath() string {
	return filepath.Join(s.cfg.Storage.DataDir, "strm_resume_checkpoint.json")
}

func (s *Service) readResumeCheckpoint() (*resumeCheckpoint, error) {
	b, err := os.ReadFile(s.checkpointPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cp resumeCheckpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		return nil, err
	}
	if cp.Version != 0 && cp.Version != resumeCheckpointVersion {
		return nil, errors.New("unsupported STRM resume checkpoint version")
	}
	if cp.Request.Type == "" {
		return nil, errors.New("invalid STRM resume checkpoint")
	}
	return &cp, nil
}

func (s *Service) writeResumeCheckpoint(cp *resumeCheckpoint) error {
	if cp == nil {
		return nil
	}
	cp.Version = resumeCheckpointVersion
	cp.UpdatedAt = time.Now()
	path := s.checkpointPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".strm-resume-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func (s *Service) clearResumeCheckpoint() error {
	err := os.Remove(s.checkpointPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (cp *resumeCheckpoint) summary() resumeCheckpointSummary {
	if cp == nil {
		return resumeCheckpointSummary{}
	}
	return resumeCheckpointSummary{
		JobID:       cp.JobID,
		CID:         cp.Request.CID,
		SourcePath:  cp.Request.SourcePath,
		Files:       len(cp.Files),
		PendingDirs: len(cp.PendingDirs),
		DirsScanned: cp.DirsScanned,
		CurrentPath: cp.CurrentPath,
		Status:      cp.Status,
		Error:       cp.Error,
		UpdatedAt:   cp.UpdatedAt,
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
