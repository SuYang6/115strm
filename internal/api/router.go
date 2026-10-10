package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"115strm/internal/config"
	"115strm/internal/service/auditlog"
	"115strm/internal/service/auth"
	"115strm/internal/service/driver115"
	"115strm/internal/service/logbuffer"
	"115strm/internal/service/scheduler"
	"115strm/internal/service/strm302"
	"115strm/internal/store"
	webdist "115strm/internal/web"
)

type Dependencies struct {
	ConfigPath string
	Config     *config.Config
	DB         *store.Store
	Logs       *logbuffer.Buffer
	Audit      *auditlog.Logger
	Auth       *auth.Service
	Driver115  *driver115.Service
	STRM       *strm302.Service
	Scheduler  *scheduler.Service
}

type Server struct {
	Dependencies
}

func NewRouter(dep Dependencies) *gin.Engine {
	if strings.EqualFold(dep.Config.Server.LogLevel, "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(dep.Logs, dep.Audit), cors(dep.Config))
	s := &Server{Dependencies: dep}

	api := r.Group("/api")
	api.POST("/auth/login", s.login)
	api.GET("/health", s.health)
	api.GET("/health/local-dirs", s.healthDirs)
	api.GET("/emby/probe", s.embyProbe)
	api.GET("/strm302/redirect", s.redirect)
	api.HEAD("/strm302/redirect", s.redirect)
	api.GET("/emby/direct/:item_id", s.embyDirect)
	api.HEAD("/emby/direct/:item_id", s.embyDirect)

	protected := api.Group("")
	protected.Use(s.authMiddleware())
	protected.GET("/auth/me", s.me)
	protected.PUT("/auth/password", s.changePassword)
	protected.GET("/dashboard/stats", s.dashboard)
	protected.GET("/settings", s.getSettings)
	protected.PUT("/settings", s.updateSettings)
	protected.POST("/system/restart", s.restartService)

	protected.POST("/115/qrcode/start", s.qrcodeStart)
	protected.GET("/115/qrcode/status", s.qrcodeStatus)
	protected.POST("/115/qrcode/login", s.qrcodeLogin)
	protected.POST("/115/cookie/import", s.import115Cookie)
	protected.POST("/115/open-token/import", s.import115OpenToken)
	protected.POST("/115/logout", s.logout115)
	protected.GET("/115/status", s.status115)
	protected.GET("/115/request-stats", s.pan115RequestStats)
	protected.GET("/115/request-stats/job/:job_id", s.pan115RequestStatsJob)
	protected.GET("/115/browse", s.browse115)
	protected.GET("/115/browse-recursive", s.browseRecursive115)

	protected.GET("/strm302/config", s.getSTRMConfig)
	protected.PUT("/strm302/config", s.updateSTRMConfig)
	protected.GET("/strm302/last-input", s.getSTRMLastInput)
	protected.POST("/strm302/generate", s.generateSTRM)
	protected.GET("/strm302/generate/current", s.currentSTRMJob)
	protected.GET("/strm302/generate/resume-state", s.resumeSTRMState)
	protected.POST("/strm302/generate/resume", s.resumeSTRM)
	protected.POST("/strm302/generate/pause/:job_id", s.pauseSTRM)
	protected.POST("/strm302/generate/stop/:job_id", s.stopSTRM)
	protected.GET("/strm302/generate/status/:job_id", s.generateStatus)
	protected.GET("/strm302/failures", s.listSTRMFailures)
	protected.POST("/strm302/failures/:id/retry", s.retrySTRMFailure)
	protected.DELETE("/strm302/failures/:id", s.deleteSTRMFailure)
	protected.GET("/strm302/list", s.listSTRM)
	protected.DELETE("/strm302/:id", s.deleteSTRM)

	protected.GET("/files/tree", s.filesTree)
	protected.GET("/files/download", s.fileDownload)
	protected.GET("/recycle", s.listRecycle)
	protected.POST("/recycle/restore", s.restoreRecycle)

	protected.GET("/tasks", s.listTasks)
	protected.POST("/tasks", s.createTask)
	protected.GET("/tasks/types", s.taskTypes)
	protected.GET("/tasks/templates", s.taskTemplates)
	protected.GET("/tasks/:id", s.getTask)
	protected.GET("/tasks/:id/status", s.taskStatus)
	protected.PUT("/tasks/:id", s.updateTask)
	protected.DELETE("/tasks/:id", s.deleteTask)
	protected.POST("/tasks/:id/run", s.runTask)
	protected.POST("/tasks/:id/toggle", s.toggleTask)
	protected.GET("/tasks/:id/logs", s.taskLogs)

	protected.GET("/logs", s.logsList)
	protected.GET("/logs/stream", s.logsStream)
	protected.GET("/audit/logs", s.auditLogsList)
	protected.GET("/audit/logs/download", s.auditLogsDownload)
	protected.GET("/audit/logs/stream", s.auditLogsStream)

	dist, _ := fs.Sub(webdist.Dist, "dist")
	indexHTML, _ := fs.ReadFile(dist, "index.html")
	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		cleanPath := strings.TrimPrefix(c.Request.URL.Path, "/")
		if cleanPath == "" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			return
		}
		if f, err := dist.Open(cleanPath); err == nil {
			if stat, statErr := f.Stat(); statErr == nil && !stat.IsDir() {
				_ = f.Close()
				c.FileFromFS(cleanPath, http.FS(dist))
				return
			}
			_ = f.Close()
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
	return r
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func fail(c *gin.Context, code int, err error) {
	msg := "error"
	if err != nil {
		msg = err.Error()
		_ = c.Error(err)
	}
	c.JSON(code, gin.H{"error": msg})
}
