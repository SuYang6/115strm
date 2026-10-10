package strm302

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"115strm/internal/model"
	"115strm/internal/util"
)

const (
	failurePickcodeDetail  = "pickcode_detail"
	failureHydratePickcode = "hydrate_pickcode"
)

func (s *Service) ListFailures(limit int) ([]model.STRMFailure, error) {
	return s.db.ListSTRMFailures(limit)
}

func (s *Service) DeleteFailure(id int64) error {
	return s.db.DeleteSTRMFailure(id)
}

func (s *Service) RetryFailure(id int64) error {
	failure, err := s.db.STRMFailureByID(id)
	if err != nil {
		return err
	}
	if failure == nil {
		return errors.New("failure record not found")
	}
	err = s.retryFailure(*failure)
	if err != nil {
		_ = s.db.MarkSTRMFailureRetry(id, "failed", shortError(err))
		return err
	}
	return s.db.MarkSTRMFailureRetry(id, "succeeded", "重试成功，请确认结果后手动删除该记录")
}

func (s *Service) retryFailure(failure model.STRMFailure) error {
	switch failure.Operation {
	case failurePickcodeDetail:
		pickcode := strings.TrimSpace(failure.PickCode)
		if pickcode == "" {
			return errors.New("empty pickcode")
		}
		f, err := s.driver.SearchByPickcode(pickcode)
		if err != nil {
			return err
		}
		if strings.TrimSpace(f.SourcePath) == "" {
			f.SourcePath = util.SlashJoin("_pickcode", f.Name)
		}
		return s.syncRetriedCloudFile(*f)
	case failureHydratePickcode:
		var req GenerateRequest
		if err := json.Unmarshal([]byte(failure.RequestJSON), &req); err != nil {
			return err
		}
		file := model.CloudFile{
			Name:       failure.FileName,
			Size:       failure.Size,
			CID:        failure.CID,
			SourcePath: failure.SourcePath,
			SourceType: failure.SourceType,
			ModTime:    failure.SourceMTime,
			PickCode:   failure.PickCode,
		}
		if strings.TrimSpace(failure.FileJSON) != "" {
			_ = json.Unmarshal([]byte(failure.FileJSON), &file)
		}
		if err := s.hydrateSinglePickcode(0, req, &file); err != nil {
			return err
		}
		return s.syncRetriedCloudFile(file)
	default:
		return fmt.Errorf("unsupported failure operation: %s", failure.Operation)
	}
}

func (s *Service) hydrateSinglePickcode(jobID int64, req GenerateRequest, f *model.CloudFile) error {
	if f == nil {
		return errors.New("empty file")
	}
	if strings.TrimSpace(f.PickCode) != "" || s.fillPickcodeFromCache(f) {
		return nil
	}
	rootCID, rootSourcePath := s.pickcodeRoot(req)
	if strings.TrimSpace(rootCID) == "" || rootCID == "0" {
		return errors.New("使用 CD2/OpenList 扫描时需要配置 115 根目录 CID，用于补齐 pickcode")
	}
	dir := normalizeSourcePath(cloudFileSourceDir(f.SourcePath))
	cid, err := s.resolve115CIDForSourceDir(jobID, strings.TrimSpace(rootCID), rootSourcePath, dir, map[string]string{normalizeSourcePath(rootSourcePath): strings.TrimSpace(rootCID)})
	if err != nil {
		return err
	}
	children, err := s.driver.BrowseForJobNamed(jobID, cid, dir)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.IsDir || fileMatchKey(child.Name, child.Size) != fileMatchKey(f.Name, f.Size) {
			continue
		}
		f.ID = child.ID
		f.PickCode = child.PickCode
		f.CID = child.CID
		f.Sha1 = child.Sha1
		f.ThumbURL = child.ThumbURL
		if child.ModTime > 0 {
			f.ModTime = child.ModTime
		}
		if f.SourceType == "" {
			f.SourceType = normalizeSourceName(req.ScanSource)
		}
		if s.db != nil {
			_ = s.db.UpsertScanCacheFile(*f, newScanID(jobID))
		}
		return nil
	}
	return fmt.Errorf("115 目录中未匹配到文件：%s", f.SourcePath)
}

func (s *Service) syncRetriedCloudFile(f model.CloudFile) error {
	if strings.TrimSpace(f.PickCode) == "" {
		return errors.New("empty pickcode")
	}
	if s.cfg.STRM302.VideoExtSet()[strings.ToLower(filepath.Ext(f.Name))] {
		_, needsMediaInfo := s.planVideoAction(f)
		action, err := s.syncVideoFile(f, newScanID(0))
		if err == nil {
			stats := planStats{}
			switch action {
			case "new":
				stats.VideoNew = 1
			case "updated":
				stats.VideoUpdated = 1
			}
			if stats.VideoNew > 0 || stats.VideoUpdated > 0 {
				if needsMediaInfo {
					stats.VideoMediaInfoNeeded = 1
				}
				s.notifyEmbyIfChanged(stats)
			}
		}
		return err
	}
	if s.mediaType(f.Name) != "" {
		action, err := s.syncMediaFile(f, newScanID(0))
		if err == nil && action != "skipped" {
			s.notifyEmbyIfChanged(planStats{MediaDownload: 1})
		}
		return err
	}
	return fmt.Errorf("unsupported file type: %s", f.Name)
}

func (s *Service) recordSTRMFailure(operation string, req GenerateRequest, f model.CloudFile, err error) {
	if s == nil || s.db == nil || err == nil {
		return
	}
	if strings.TrimSpace(f.SourcePath) == "" {
		if strings.TrimSpace(f.PickCode) != "" {
			f.SourcePath = util.SlashJoin("_pickcode", f.PickCode)
		} else if strings.TrimSpace(f.Name) != "" {
			f.SourcePath = f.Name
		}
	}
	reqJSON, _ := json.Marshal(req)
	fileJSON, _ := json.Marshal(f)
	record := &model.STRMFailure{
		FailureKey:  failureKey(operation, f),
		Operation:   operation,
		SourcePath:  normalizeSourcePath(f.SourcePath),
		PickCode:    strings.TrimSpace(f.PickCode),
		FileName:    f.Name,
		Size:        f.Size,
		CID:         f.CID,
		SourceMTime: f.ModTime,
		SourceType:  fileSourceType(f),
		RequestJSON: string(reqJSON),
		FileJSON:    string(fileJSON),
		Error:       shortError(err),
	}
	if e := s.db.UpsertSTRMFailure(record); e != nil && s.logs != nil {
		s.logs.Warn("写入 STRM 失败记录失败：%v", e)
	}
}

func failureKey(operation string, f model.CloudFile) string {
	path := normalizeSourcePath(f.SourcePath)
	if path == "" {
		path = strings.TrimSpace(f.Name)
	}
	return operation + "|" + path + "|" + strings.TrimSpace(f.PickCode) + "|" + strings.TrimSpace(f.Name) + "|" + fmt.Sprintf("%d", f.Size)
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 1000 {
		text = text[:1000]
	}
	return text
}
