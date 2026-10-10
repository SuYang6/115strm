package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

func NewEmbyGateway(dep Dependencies) (http.Handler, error) {
	baseURL := strings.TrimSpace(dep.Config.Emby.BaseURL)
	if baseURL == "" {
		return nil, errors.New("emby base_url is required")
	}
	target, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid emby base_url: %s", baseURL)
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Forwarded-Proto", requestScheme(req))
		},
		Transport: &embyFallbackTransport{
			baseURL: strings.TrimSpace(dep.Config.Emby.BaseURL),
			logs:    dep.Logs,
			transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if dep.Logs != nil {
			dep.Logs.Warn("Emby 网关代理失败：path=%s err=%v", r.URL.Path, err)
		}
		http.Error(w, "emby gateway proxy failed: "+err.Error(), http.StatusBadGateway)
	}

	s := &Server{Dependencies: dep}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if itemID, ok := embyDirectItemID(r.URL.Path); ok {
			targetURL, err := s.resolveEmbyDirectTarget(itemID, queryMediaSourceID(r.URL.Query()), r.UserAgent(), r.Host)
			if err != nil {
				if dep.Logs != nil {
					dep.Logs.Warn("Emby 网关直链跳转失败：item=%s path=%s err=%v", itemID, r.URL.Path, err)
				}
				http.Error(w, "emby gateway direct redirect failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			if dep.Logs != nil {
				dep.Logs.Info("emby gateway direct redirect item=%s path=%s ua=%s", itemID, r.URL.Path, r.UserAgent())
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, targetURL, http.StatusFound)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

type embyFallbackTransport struct {
	baseURL   string
	logs      interface{ Info(string, ...any) }
	transport http.RoundTripper
}

func (t *embyFallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	candidates, err := embyBaseURLCandidates(t.baseURL, req.Host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i, candidate := range candidates {
		out := req.Clone(req.Context())
		out.URL = embyTargetURL(candidate, req.URL.Path, req.URL.RawQuery)
		out.Host = candidate.Host
		resp, err := t.transport.RoundTrip(out)
		if err == nil {
			if i > 0 && t.logs != nil {
				t.logs.Info("Emby 网关已切换后端地址：%s", candidate.Redacted())
			}
			return resp, nil
		}
		lastErr = fmt.Errorf("%s: %w", candidate.Host, err)
	}
	if lastErr == nil {
		lastErr = errors.New("no emby backend candidates")
	}
	return nil, lastErr
}

func embyDirectItemID(rawPath string) (string, bool) {
	cleanPath := "/" + strings.Trim(path.Clean("/"+rawPath), "/")
	parts := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(parts) > 0 && strings.EqualFold(parts[0], "emby") {
		parts = parts[1:]
	}
	if len(parts) < 3 || !strings.EqualFold(parts[0], "videos") {
		return "", false
	}
	itemID := strings.TrimSpace(parts[1])
	if itemID == "" {
		return "", false
	}
	action := strings.ToLower(parts[2])
	switch {
	case action == "stream", strings.HasPrefix(action, "stream."):
		return itemID, true
	case action == "original", strings.HasPrefix(action, "original."):
		return itemID, true
	default:
		return "", false
	}
}

func embyBaseURLCandidates(rawBaseURL, requestHost string) ([]*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid emby base_url: %s", rawBaseURL)
	}

	out := []*url.URL{cloneURL(base)}
	if isLoopbackHost(base.Hostname()) {
		if requestCandidate := sameRequestHostCandidate(base, requestHost); requestCandidate != nil {
			out = append(out, requestCandidate)
		}
		for _, host := range []string{"host.docker.internal", dockerDefaultGateway(), "172.17.0.1", "gateway.docker.internal"} {
			if host == "" {
				continue
			}
			out = append(out, cloneURLWithHost(base, net.JoinHostPort(host, embyURLPort(base))))
		}
	}
	return uniqueURLs(out), nil
}

func embyTargetURL(base *url.URL, reqPath, rawQuery string) *url.URL {
	u := cloneURL(base)
	u.Path = singleJoiningSlash(base.Path, reqPath)
	u.RawPath = ""
	u.RawQuery = rawQuery
	u.Fragment = ""
	return u
}

func sameRequestHostCandidate(base *url.URL, requestHost string) *url.URL {
	host := requestHost
	if h, _, err := net.SplitHostPort(requestHost); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" || isLoopbackHost(host) {
		return nil
	}
	return cloneURLWithHost(base, net.JoinHostPort(host, embyURLPort(base)))
}

func dockerDefaultGateway() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		n, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || n == 0 {
			continue
		}
		ip := net.IPv4(byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
		return ip.String()
	}
	return ""
}

func cloneURL(u *url.URL) *url.URL {
	cloned := *u
	return &cloned
}

func cloneURLWithHost(u *url.URL, host string) *url.URL {
	cloned := cloneURL(u)
	cloned.Host = host
	return cloned
}

func embyURLPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0"
}

func uniqueURLs(urls []*url.URL) []*url.URL {
	seen := map[string]bool{}
	out := make([]*url.URL, 0, len(urls))
	for _, u := range urls {
		key := u.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, u)
	}
	return out
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func requestScheme(req *http.Request) string {
	if req.TLS != nil {
		return "https"
	}
	if scheme := req.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}
