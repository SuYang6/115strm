package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/auditlog"
	"115strm/internal/service/logbuffer"
)

func requestLogger(logs *logbuffer.Buffer, audit *auditlog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			status := c.Writer.Status()
			if status >= 400 {
				errText := strings.TrimSpace(c.Errors.String())
				if errText == "" {
					errText = http.StatusText(status)
				}
				if audit != nil {
					audit.APIError(c.Request.Method, c.Request.URL.Path, status, errText)
				}
				logs.Warn("%s %s %d %s %s", c.Request.Method, c.Request.URL.Path, status, time.Since(start).Round(time.Millisecond), errText)
			}
		}
	}
}

func cors(cfg *config.Config) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range cfg.Security.CORSAllowedOrigins {
		allowed[o] = true
	}
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token := ""
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			fail(c, http.StatusUnauthorized, nil)
			c.Abort()
			return
		}
		u, err := s.Auth.ParseToken(token)
		if err != nil || u == nil {
			fail(c, http.StatusUnauthorized, err)
			c.Abort()
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

func currentUser(c *gin.Context) *model.User {
	v, _ := c.Get("user")
	u, _ := v.(*model.User)
	return u
}
