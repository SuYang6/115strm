package strm302

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

func (s *Service) ResolveRedirect(pickcode, userAgent string) (string, error) {
	if pickcode == "" {
		return "", errors.New("empty pickcode")
	}
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = s.cfg.STRM302.ScanUA
	}
	cacheKey := redirectCacheKey(s.redirectAuthMode(), pickcode, userAgent)
	if cached, ok, err := s.db.GetRedirectCache(cacheKey); err != nil {
		return "", err
	} else if ok {
		_ = s.db.RecordAccess(pickcode)
		return cached, nil
	}
	delays := s.cfg.STRM302.RetryDelays()
	attempts := s.cfg.STRM302.RedirectFetchRetryCount + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			idx := i - 1
			if idx >= len(delays) {
				idx = len(delays) - 1
			}
			time.Sleep(delays[idx])
		}
		target, _, err := s.driver.DownloadURLWithUA(pickcode, userAgent)
		if err == nil {
			if err := validateRedirectTarget(target); err != nil {
				return "", err
			}
			expires := time.Now().Add(time.Duration(s.cfg.STRM302.RedirectCacheTTL) * time.Second)
			_ = s.db.SetRedirectCache(cacheKey, target, expires)
			_ = s.db.RecordAccess(pickcode)
			return target, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func validateRedirectTarget(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Hostname())
	if host == "115.com" || strings.HasSuffix(host, ".115.com") ||
		host == "115cdn.com" || strings.HasSuffix(host, ".115cdn.com") ||
		host == "115cdn.net" || strings.HasSuffix(host, ".115cdn.net") ||
		host == "anxia.com" || strings.HasSuffix(host, ".anxia.com") {
		return nil
	}
	return errors.New("redirect target is outside 115 allowlist")
}

func (s *Service) redirectAuthMode() string {
	switch strings.ToLower(strings.TrimSpace(s.cfg.Pan115.AuthMode)) {
	case "open", "open_token", "openapi", "open_api":
		return "open_token"
	default:
		return "cookie"
	}
}

func redirectCacheKey(authMode, pickcode, userAgent string) string {
	return "auth=" + authMode + "|pc=" + pickcode + "|ua=" + userAgent
}
