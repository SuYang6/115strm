package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/auditlog"
	"115strm/internal/service/strm302"
	"115strm/internal/util"
)

func (s *Server) login(c *gin.Context) {
	var req struct{ Username, Password string }
	if err := c.BindJSON(&req); err != nil {
		fail(c, 400, err)
		return
	}
	token, user, err := s.Auth.Login(req.Username, req.Password, c.ClientIP())
	if err != nil {
		fail(c, http.StatusUnauthorized, err)
		return
	}
	ok(c, gin.H{"token": token, "user": user})
}

func (s *Server) me(c *gin.Context) { ok(c, currentUser(c)) }

func (s *Server) changePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.BindJSON(&req); err != nil {
		fail(c, 400, err)
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		fail(c, 400, errors.New("new password required"))
		return
	}
	if err := s.Auth.ChangePassword(currentUser(c).ID, req.OldPassword, req.NewPassword); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"changed": true})
}

func (s *Server) health(c *gin.Context) { ok(c, gin.H{"status": "ok", "service": "115strm"}) }

func (s *Server) restartService(c *gin.Context) {
	user := currentUser(c)
	username := ""
	if user != nil {
		username = user.Username
	}
	ok(c, gin.H{"restarting": true})
	go func() {
		time.Sleep(800 * time.Millisecond)
		s.Logs.Warn("用户 %s 请求重启服务", username)
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			if err := p.Signal(os.Interrupt); err == nil {
				return
			}
		}
		os.Exit(0)
	}()
}

func (s *Server) healthDirs(c *gin.Context) {
	dirs := []string{s.Config.STRM302.BatchDir, s.Config.Storage.DataDir, filepath.Dir(s.Config.Storage.DBFile), filepath.Dir(s.Config.Storage.LogFile)}
	out := []gin.H{}
	for _, dir := range dirs {
		err := os.MkdirAll(dir, 0755)
		writable := err == nil
		if writable {
			test := filepath.Join(dir, ".115strm-write-test")
			if e := os.WriteFile(test, []byte("ok"), 0644); e != nil {
				writable = false
			} else {
				_ = os.Remove(test)
			}
		}
		out = append(out, gin.H{"path": dir, "writable": writable})
	}
	ok(c, out)
}

func (s *Server) dashboard(c *gin.Context) {
	stats, err := s.DB.DashboardStats()
	if err != nil {
		fail(c, 500, err)
		return
	}
	stats["115"] = s.Driver115.Status()
	if requestStats, e := s.DB.Pan115RequestStats(c.Query("day")); e == nil {
		stats["pan115_request_stats"] = requestStats
	}
	ok(c, stats)
}

func (s *Server) getSettings(c *gin.Context) { ok(c, s.Config) }

func (s *Server) updateSettings(c *gin.Context) {
	var cfg config.Config
	if err := c.BindJSON(&cfg); err != nil {
		fail(c, 400, err)
		return
	}
	if err := config.Save(s.ConfigPath, &cfg); err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, gin.H{"restart_required": true})
}

func (s *Server) qrcodeStart(c *gin.Context) {
	session, image, err := s.Driver115.StartQRCode()
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, gin.H{"uid": session.UID, "time": session.Time, "sign": session.Sign, "qrcode": session.QrcodeContent, "image": image})
}

func (s *Server) qrcodeStatus(c *gin.Context) {
	status, err := s.Driver115.QRCodeStatus()
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, status)
}

func (s *Server) qrcodeLogin(c *gin.Context) {
	var req struct {
		App string `json:"app"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(c, 400, err)
		return
	}
	if err := s.Driver115.QRCodeLogin(req.App); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, s.Driver115.Status())
}

func (s *Server) import115Cookie(c *gin.Context) {
	var req struct {
		Cookie string `json:"cookie"`
	}
	if err := c.BindJSON(&req); err != nil {
		fail(c, 400, err)
		return
	}
	if err := s.Driver115.ImportCookie(req.Cookie); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, s.Driver115.Status())
}

func (s *Server) import115OpenToken(c *gin.Context) {
	var req struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.BindJSON(&req); err != nil {
		fail(c, 400, err)
		return
	}
	if err := s.Driver115.ImportOpenToken(req.AccessToken, req.RefreshToken); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, s.Driver115.Status())
}

func (s *Server) logout115(c *gin.Context) {
	if err := s.Driver115.Logout(); err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, gin.H{"logged_out": true})
}

func (s *Server) status115(c *gin.Context) { ok(c, s.Driver115.Status()) }

func (s *Server) pan115RequestStats(c *gin.Context) {
	stats, err := s.DB.Pan115RequestStats(c.Query("day"))
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, stats)
}

func (s *Server) pan115RequestStatsJob(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("job_id"), 10, 64)
	stats, err := s.DB.Pan115RequestStatsForJob(id)
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, stats)
}

func (s *Server) browse115(c *gin.Context) {
	files, err := s.Driver115.Browse(c.DefaultQuery("cid", "0"))
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, files)
}

func (s *Server) browseRecursive115(c *gin.Context) {
	cid := c.Query("cid")
	if cid == "" || cid == "0" {
		fail(c, 400, errors.New("non-root cid required"))
		return
	}
	files, err := s.Driver115.BrowseRecursive(cid, c.Query("source_path"), s.Config.Tasks.IncrementalMaxItems)
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, files)
}

func (s *Server) getSTRMConfig(c *gin.Context) { ok(c, s.Config.STRM302) }

func (s *Server) updateSTRMConfig(c *gin.Context) {
	var cfg config.STRM302Config
	if err := c.BindJSON(&cfg); err != nil {
		fail(c, 400, err)
		return
	}
	s.Config.STRM302 = cfg
	if err := config.Save(s.ConfigPath, s.Config); err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, gin.H{"restart_required": true})
}

func (s *Server) getSTRMLastInput(c *gin.Context) {
	cid, _, _ := s.DB.GetSetting("strm302.last_cid")
	sourcePath, _, _ := s.DB.GetSetting("strm302.last_source_path")
	scanSource, _, _ := s.DB.GetSetting("strm302.last_scan_source")
	scanMode, _, _ := s.DB.GetSetting("strm302.last_scan_mode")
	ok(c, gin.H{"cid": cid, "source_path": sourcePath, "scan_source": scanSource, "scan_mode": scanMode})
}

func (s *Server) generateSTRM(c *gin.Context) {
	var req strm302.GenerateRequest
	if err := c.BindJSON(&req); err != nil {
		fail(c, 400, err)
		return
	}
	req.RecycleLabel = "手动扫描"
	id, err := s.STRM.CreateGenerateJob(req)
	if err != nil {
		fail(c, 400, err)
		return
	}
	if req.Type == "own_directory_recursive" {
		_ = s.DB.SetSetting("strm302.last_cid", strings.TrimSpace(req.CID))
		_ = s.DB.SetSetting("strm302.last_source_path", strings.TrimSpace(req.SourcePath))
		_ = s.DB.SetSetting("strm302.last_scan_source", strings.TrimSpace(req.ScanSource))
		_ = s.DB.SetSetting("strm302.last_scan_mode", strings.TrimSpace(req.ScanMode))
	}
	ok(c, gin.H{"job_id": id})
}

func (s *Server) resumeSTRMState(c *gin.Context) {
	state, err := s.STRM.ResumeState()
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, state)
}

func (s *Server) currentSTRMJob(c *gin.Context) {
	job, err := s.STRM.ActiveJob()
	if err != nil {
		fail(c, 500, err)
		return
	}
	if job != nil {
		s.attachSTRMJobRequestStats(job)
	}
	ok(c, job)
}

func (s *Server) resumeSTRM(c *gin.Context) {
	id, err := s.STRM.CreateResumeJob()
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"job_id": id})
}

func (s *Server) pauseSTRM(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("job_id"), 10, 64)
	paused, err := s.STRM.PauseGenerateJob(id)
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"pause_requested": paused})
}

func (s *Server) stopSTRM(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("job_id"), 10, 64)
	stopped, err := s.STRM.StopGenerateJob(id)
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"stop_requested": stopped})
}

func (s *Server) generateStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("job_id"), 10, 64)
	job, err := s.STRM.Job(id)
	if err != nil {
		fail(c, 500, err)
		return
	}
	if job == nil {
		fail(c, 404, errors.New("job not found"))
		return
	}
	s.attachSTRMJobRequestStats(job)
	ok(c, job)
}

func (s *Server) attachSTRMJobRequestStats(job *model.STRMJob) {
	if job == nil {
		return
	}
	if requestStats, e := s.DB.Pan115RequestStatsForJob(job.ID); e == nil {
		requestStats["recent"] = s.Driver115.RecentRequests(job.ID)
		job.Pan115Requests = requestStats
	}
}

func (s *Server) listSTRMFailures(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	failures, err := s.STRM.ListFailures(limit)
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, failures)
}

func (s *Server) retrySTRMFailure(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := s.STRM.RetryFailure(id); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"retried": true})
}

func (s *Server) deleteSTRMFailure(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := s.STRM.DeleteFailure(id); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s *Server) listSTRM(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	files, err := s.STRM.List(limit, offset)
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, files)
}

func (s *Server) deleteSTRM(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := s.STRM.DeleteSTRM(id); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s *Server) redirect(c *gin.Context) {
	target, err := s.STRM.ResolveRedirect(c.Query("pickcode"), c.Request.UserAgent())
	if err != nil {
		fail(c, 502, err)
		return
	}
	c.Redirect(http.StatusFound, target)
}

func (s *Server) filesTree(c *gin.Context) {
	root := s.Config.STRM302.BatchDir
	var out []gin.H
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		info, _ := d.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		out = append(out, gin.H{"path": filepath.ToSlash(rel), "name": d.Name(), "is_dir": d.IsDir(), "size": size})
		if len(out) > 2000 {
			return filepath.SkipAll
		}
		return nil
	})
	ok(c, out)
}

func (s *Server) fileDownload(c *gin.Context) {
	p, err := util.SafeJoin(s.Config.STRM302.BatchDir, c.Query("path"))
	if err != nil {
		fail(c, 400, err)
		return
	}
	c.FileAttachment(p, filepath.Base(p))
}

func (s *Server) listRecycle(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	items, total, err := s.DB.ListRecycleItems(limit, offset)
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, gin.H{"items": items, "total": total})
}

// restoreRecycle 只接收记录 ID，路径和覆盖规则由服务端确定。
func (s *Server) restoreRecycle(c *gin.Context) {
	var req model.RecycleRestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ids, err := strm302.ValidateRestoreIDs(req.IDs)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, s.STRM.RestoreRecycleItems(c.Request.Context(), ids))
}

func (s *Server) listTasks(c *gin.Context) {
	tasks, err := s.Scheduler.List()
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, tasks)
}

func (s *Server) createTask(c *gin.Context) {
	var t model.ScheduledTask
	if err := c.BindJSON(&t); err != nil {
		fail(c, 400, err)
		return
	}
	id, err := s.Scheduler.Create(&t)
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"id": id})
}

func (s *Server) getTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	t, err := s.Scheduler.Get(id)
	if err != nil {
		fail(c, 500, err)
		return
	}
	if t == nil {
		fail(c, 404, errors.New("task not found"))
		return
	}
	ok(c, t)
}

func (s *Server) updateTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var t model.ScheduledTask
	if err := c.BindJSON(&t); err != nil {
		fail(c, 400, err)
		return
	}
	t.ID = id
	if err := s.Scheduler.Update(&t); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"updated": true})
}

func (s *Server) deleteTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := s.Scheduler.Delete(id); err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s *Server) runTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	task, err := s.Scheduler.Get(id)
	if err != nil {
		fail(c, 500, err)
		return
	}
	if task == nil {
		fail(c, 404, errors.New("task not found"))
		return
	}
	logManualTaskRun(s, id, task.Name, "页面")
	run, err := s.Scheduler.RunNow(id)
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"running": true, "run": run})
}

func (s *Server) toggleTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	_ = c.BindJSON(&req)
	if err := s.Scheduler.Toggle(id, req.Enabled); err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, gin.H{"enabled": req.Enabled})
}

func (s *Server) taskLogs(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	logs, err := s.Scheduler.Logs(id)
	if err != nil {
		fail(c, 500, err)
		return
	}
	ok(c, logs)
}

func (s *Server) taskStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	status, err := s.Scheduler.Status(id)
	if err != nil {
		fail(c, 400, err)
		return
	}
	if status != nil && status.STRMJob != nil {
		s.attachSTRMJobRequestStats(status.STRMJob)
	}
	ok(c, status)
}

func (s *Server) taskTypes(c *gin.Context)     { ok(c, s.Scheduler.Types()) }
func (s *Server) taskTemplates(c *gin.Context) { ok(c, s.Scheduler.Templates()) }

func (s *Server) logsList(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	ok(c, s.Logs.List(limit))
}

func (s *Server) logsStream(c *gin.Context) {
	ch, cancel := s.Logs.Subscribe()
	defer cancel()
	c.Stream(func(w io.Writer) bool {
		select {
		case e, ok := <-ch:
			if !ok {
				return false
			}
			b, _ := json.Marshal(e)
			c.SSEvent("log", string(b))
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (s *Server) auditLogsList(c *gin.Context) {
	if s.Audit == nil {
		ok(c, gin.H{"items": []auditlog.Entry{}, "total": 0})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "1000"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	result, err := s.Audit.Search(auditlog.Query{
		Range:   c.DefaultQuery("range", "today"),
		Date:    c.Query("date"),
		Start:   c.Query("start"),
		End:     c.Query("end"),
		Type:    c.Query("type"),
		Keyword: c.Query("keyword"),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		fail(c, 400, err)
		return
	}
	ok(c, result)
}

func (s *Server) auditLogsDownload(c *gin.Context) {
	if s.Audit == nil {
		c.String(http.StatusOK, "")
		return
	}
	entries, err := s.Audit.List(auditlog.Query{
		Range:   c.DefaultQuery("range", "today"),
		Date:    c.Query("date"),
		Start:   c.Query("start"),
		End:     c.Query("end"),
		Type:    c.Query("type"),
		Keyword: c.Query("keyword"),
		Limit:   50000,
	})
	if err != nil {
		fail(c, 400, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="115strm-audit.log"`)
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	for _, entry := range entries {
		b, _ := json.Marshal(entry)
		_, _ = c.Writer.Write(append(b, '\n'))
	}
}

func (s *Server) auditLogsStream(c *gin.Context) {
	if s.Audit == nil {
		c.Status(http.StatusNoContent)
		return
	}
	ch, cancel := s.Audit.Subscribe()
	defer cancel()
	c.Stream(func(w io.Writer) bool {
		select {
		case e, ok := <-ch:
			if !ok {
				return false
			}
			b, _ := json.Marshal(e)
			c.SSEvent("audit", string(b))
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
