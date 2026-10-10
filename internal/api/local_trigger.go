package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"115strm/internal/service/auditlog"
)

func NewLocalTriggerRouter(dep Dependencies) *gin.Engine {
	if strings.EqualFold(dep.Config.Server.LogLevel, "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	s := &Server{Dependencies: dep}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/test", s.localTriggerTest)
	r.POST("/test", s.localTriggerTest)
	r.GET("/tasks", s.localTriggerTasks)
	r.POST("/tasks/:id/run", s.localTriggerRunTask)
	r.GET("/tasks/:id/status", s.localTriggerTaskStatus)
	r.GET("/tasks/:id/logs", s.localTriggerTaskLogs)
	return r
}

func (s *Server) localTriggerTest(c *gin.Context) {
	if s.Logs != nil {
		s.Logs.Info("本机触发测试接口已调用 remote=%s", c.ClientIP())
	}
	if s.Audit != nil {
		s.Audit.Event(auditlog.TypeTask, "本机触发测试接口已调用")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "测试日志已写入"})
}

func (s *Server) localTriggerTasks(c *gin.Context) {
	tasks, err := s.Scheduler.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

func (s *Server) localTriggerRunTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	task, err := s.Scheduler.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	logManualTaskRun(s, id, task.Name, "本机触发接口")
	run, err := s.Scheduler.RunNow(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"running": true, "id": id, "name": task.Name, "run": run})
}

func (s *Server) localTriggerTaskStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	status, err := s.Scheduler.Status(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if status != nil && status.STRMJob != nil {
		s.attachSTRMJobRequestStats(status.STRMJob)
	}
	c.JSON(http.StatusOK, gin.H{"data": status})
}

func (s *Server) localTriggerTaskLogs(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	logs, err := s.Scheduler.Logs(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

func logManualTaskRun(s *Server, id int64, name, source string) {
	if s == nil || s.Logs == nil {
		return
	}
	if name == "" {
		name = "-"
	}
	if source == "" {
		source = "手动"
	}
	message := "手动执行定时任务 id=" + strconv.FormatInt(id, 10) + " name=" + name + " source=" + source
	s.Logs.Info(message)
	if s.Audit != nil {
		s.Audit.Event(auditlog.TypeTask, message)
	}
}
