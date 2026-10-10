package strm302

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"115strm/internal/service/auditlog"
)

func (s *Service) notifyEmbyIfChanged(stats planStats) {
	changed := stats.changedCount()
	mediaInfoNeeded := stats.VideoMediaInfoNeeded
	if !s.cfg.Emby.NotifyRefresh {
		s.recordEmbyNotifyInfo("Emby 入库通知已跳过：配置已关闭；%s", embyNotifyStatsSummary(stats))
		return
	}
	if changed <= 0 {
		s.recordEmbyNotifyInfo("Emby 入库通知已跳过：本次没有需要 Emby 扫库的文件变化；%s", embyNotifyStatsSummary(stats))
		return
	}
	apiKey := strings.TrimSpace(s.cfg.Emby.APIKey)
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Emby.BaseURL), "/")
	if apiKey == "" || baseURL == "" {
		s.recordEmbyNotifyInfo("Emby 入库通知已跳过：emby.base_url 或 emby.api_key 为空；%s", embyNotifyStatsSummary(stats))
		return
	}
	cooldown := time.Duration(s.cfg.Emby.NotifyCooldownMinutes) * time.Minute
	if cooldown <= 0 {
		cooldown = 10 * time.Minute
	}
	s.mu.Lock()
	if !s.lastEmbyNotify.IsZero() && time.Since(s.lastEmbyNotify) < cooldown {
		remaining := cooldown - time.Since(s.lastEmbyNotify)
		s.mu.Unlock()
		s.recordEmbyNotifyInfo("Emby 入库通知已跳过：仍在冷却期，剩余 %s；%s", remaining.Round(time.Second), embyNotifyStatsSummary(stats))
		return
	}
	s.lastEmbyNotify = time.Now()
	s.mu.Unlock()

	libraryTaskID := strings.TrimSpace(s.cfg.Emby.LibraryTaskID)
	mediaInfoTaskID := strings.TrimSpace(s.cfg.Emby.MediaInfoTaskID)
	s.recordEmbyNotifyInfo("Emby 入库通知开始：检测到 %d 个变动，需 MediaInfo=%d，扫库任务ID=%s，MediaInfo任务ID=%s；%s", changed, mediaInfoNeeded, emptyAsDash(libraryTaskID), emptyAsDash(mediaInfoTaskID), embyNotifyStatsSummary(stats))
	go s.runEmbyNotifyFlow(baseURL, apiKey, libraryTaskID, mediaInfoTaskID, changed, mediaInfoNeeded)
}

func (p planStats) changedCount() int {
	return p.VideoNew + p.VideoUpdated + p.MediaLocal + p.MediaMoved + p.MediaDownload + p.Deleted
}

func embyNotifyStatsSummary(stats planStats) string {
	return fmt.Sprintf("STRM[新增=%d 更新=%d 跳过=%d 需MediaInfo=%d] 媒体[本地=%d 移动=%d 下载=%d 跳过=%d] 清理删除=%d",
		stats.VideoNew, stats.VideoUpdated, stats.VideoSkipped, stats.VideoMediaInfoNeeded, stats.MediaLocal, stats.MediaMoved, stats.MediaDownload, stats.MediaSkipped, stats.Deleted)
}

func (s *Service) runEmbyNotifyFlow(baseURL, apiKey, libraryTaskID, mediaInfoTaskID string, changed, mediaInfoNeeded int) {
	if libraryTaskID == "" {
		if err := s.refreshEmbyLibrary(baseURL, apiKey); err != nil {
			s.recordEmbyNotifyError(http.MethodPost, "/emby/Library/Refresh", err)
			return
		}
		s.recordEmbyNotifyInfo("Emby 入库通知已发送：检测到 %d 个变动，未配置扫库任务 ID，已使用通用刷新接口", changed)
		if mediaInfoNeeded > 0 {
			s.recordEmbyNotifyInfo("神医助手 MediaInfo 已跳过：未配置 Emby 扫库任务 ID，无法等待扫库完成")
		}
		return
	}

	if err := s.startEmbyScheduledTask(baseURL, apiKey, libraryTaskID); err != nil {
		s.recordEmbyNotifyError(http.MethodPost, "/emby/ScheduledTasks/Running/"+libraryTaskID, err)
		return
	}
	s.recordEmbyNotifyInfo("Emby 入库通知已发送：检测到 %d 个变动，已启动媒体库扫描任务 %s", changed, libraryTaskID)
	if mediaInfoNeeded <= 0 {
		s.recordEmbyNotifyInfo("神医助手 MediaInfo 未触发：本次需 MediaInfo=0")
		return
	}
	if mediaInfoTaskID == "" {
		s.recordEmbyNotifyInfo("神医助手 MediaInfo 已跳过：本次需 MediaInfo=%d，但未配置 mediainfo_task_id", mediaInfoNeeded)
		return
	}
	s.recordEmbyNotifyInfo("等待 Emby 媒体库扫描任务进入 Running：%s", libraryTaskID)
	if err := s.waitEmbyScheduledTaskIdle(baseURL, apiKey, libraryTaskID, 2*time.Hour, 5*time.Second); err != nil {
		s.recordEmbyNotifyError(http.MethodGet, "/emby/ScheduledTasks", err)
		return
	}
	s.recordEmbyNotifyInfo("Emby 媒体库扫描任务已完成：%s，准备启动神医助手 MediaInfo", libraryTaskID)
	if err := s.startEmbyScheduledTask(baseURL, apiKey, mediaInfoTaskID); err != nil {
		s.recordEmbyNotifyError(http.MethodPost, "/emby/ScheduledTasks/Running/"+mediaInfoTaskID, err)
		return
	}
	s.recordEmbyNotifyInfo("神医助手 MediaInfo 提取任务已启动：%s", mediaInfoTaskID)
}

func (s *Service) startEmbyScheduledTask(baseURL, apiKey, taskID string) error {
	requestPath := "/ScheduledTasks/Running/" + url.PathEscape(taskID)
	u, err := embyAPIURL(baseURL, requestPath, apiKey)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", apiKey)
	resp, err := s.embyHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Emby scheduled task failed: %s", resp.Status)
	}
	return nil
}

func (s *Service) waitEmbyScheduledTaskIdle(baseURL, apiKey, taskID string, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	seenRunning := false
	for {
		state, err := s.fetchEmbyScheduledTaskState(baseURL, apiKey, taskID)
		if err != nil {
			return err
		}
		if strings.EqualFold(state, "Running") {
			if !seenRunning {
				s.recordEmbyNotifyInfo("Emby 媒体库扫描任务已进入 Running：%s", taskID)
			}
			seenRunning = true
		} else if seenRunning {
			return nil
		}
		if time.Now().After(deadline) {
			if !seenRunning {
				return fmt.Errorf("waiting for Emby scheduled task %s to enter Running timed out; latest state=%s", taskID, emptyAsDash(state))
			}
			return fmt.Errorf("waiting for Emby scheduled task %s to finish timed out; latest state=%s", taskID, emptyAsDash(state))
		}
		time.Sleep(interval)
	}
}

func (s *Service) fetchEmbyScheduledTaskState(baseURL, apiKey, taskID string) (string, error) {
	u, err := embyAPIURL(baseURL, "/ScheduledTasks", apiKey)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Emby-Token", apiKey)
	resp, err := s.embyHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Emby scheduled task list failed: %s", resp.Status)
	}
	var tasks []struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State string `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return "", err
	}
	for _, task := range tasks {
		if strings.EqualFold(task.ID, taskID) {
			return task.State, nil
		}
	}
	return "", fmt.Errorf("Emby scheduled task not found: %s", taskID)
}

func (s *Service) refreshEmbyLibrary(baseURL, apiKey string) error {
	u, err := embyAPIURL(baseURL, "/Library/Refresh", apiKey)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", apiKey)
	resp, err := s.embyHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Emby refresh failed: %s", resp.Status)
	}
	return nil
}

func (s *Service) embyHTTPClient() *http.Client {
	timeout := time.Duration(s.cfg.Security.OutboundTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func embyAPIURL(baseURL, apiPath, apiKey string) (string, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	cleanBasePath := strings.TrimRight(u.Path, "/")
	apiPath = "/" + strings.TrimLeft(apiPath, "/")
	if cleanBasePath == "" || cleanBasePath == "/" {
		u.Path = "/emby" + apiPath
	} else if strings.EqualFold(strings.Trim(cleanBasePath, "/"), "emby") || strings.HasSuffix(strings.ToLower(cleanBasePath), "/emby") {
		u.Path = cleanBasePath + apiPath
	} else {
		u.Path = cleanBasePath + "/emby" + apiPath
	}
	q := u.Query()
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *Service) recordEmbyNotifyError(method, path string, err error) {
	if s.audit != nil {
		s.audit.APIError(method, path, http.StatusBadGateway, err.Error())
	}
	if s.logs != nil {
		s.logs.Warn("Emby 入库通知失败：%v", err)
	}
}

func (s *Service) recordEmbyNotifyInfo(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if s.audit != nil {
		s.audit.Event(auditlog.TypeEmbyNotify, message)
	}
	if s.logs != nil {
		s.logs.Info("%s", message)
	}
}

func emptyAsDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
