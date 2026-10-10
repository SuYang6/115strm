package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type embyItemResponse struct {
	ID           string            `json:"Id"`
	Name         string            `json:"Name"`
	Path         string            `json:"Path"`
	MediaSources []embyMediaSource `json:"MediaSources"`
}

type embyMediaSource struct {
	ID   string `json:"Id"`
	Path string `json:"Path"`
}

type embyUserResponse struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

func (s *Server) embyDirect(c *gin.Context) {
	itemID := strings.TrimSpace(c.Param("item_id"))
	if itemID == "" {
		fail(c, http.StatusBadRequest, errors.New("empty emby item id"))
		return
	}
	target, err := s.resolveEmbyDirectTarget(itemID, queryMediaSourceID(c.Request.URL.Query()), c.Request.UserAgent(), c.Request.Host)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.Redirect(http.StatusFound, target)
}

func (s *Server) resolveEmbyDirectTarget(itemID, mediaSourceID, userAgent, requestHost string) (string, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", errors.New("empty emby item id")
	}
	item, err := s.fetchEmbyItem(itemID, requestHost)
	if err != nil {
		return "", err
	}
	strmPath, err := s.embyItemSTRMPath(item, mediaSourceID)
	if err != nil {
		return "", err
	}
	strmURL := strmPath
	if !isHTTPURL(strmPath) {
		strmURL, err = readSTRMURL(strmPath)
		if err != nil {
			return "", err
		}
	}
	pickcode, err := pickcodeFromSTRMURL(strmURL)
	if err != nil {
		return "", err
	}
	target, err := s.STRM.ResolveRedirect(pickcode, userAgent)
	if err != nil {
		return "", err
	}
	return target, nil
}

func (s *Server) fetchEmbyItem(itemID, requestHost string) (*embyItemResponse, error) {
	base := strings.TrimSpace(s.Config.Emby.BaseURL)
	apiKey := strings.TrimSpace(s.Config.Emby.APIKey)
	if base == "" || apiKey == "" {
		return nil, errors.New("emby base_url and api_key are required")
	}
	candidates, err := embyBaseURLCandidates(base, requestHost)
	if err != nil {
		return nil, err
	}
	var lastErr error
	client := &http.Client{Timeout: 10 * time.Second}
	for _, candidate := range candidates {
		item, err := fetchEmbyItemAt(client, candidate, apiKey, "/Items/"+url.PathEscape(itemID))
		if err == nil {
			return item, nil
		}
		lastErr = err

		users, err := fetchEmbyUsersAt(client, candidate, apiKey)
		if err != nil {
			lastErr = fmt.Errorf("%v; users lookup failed: %w", lastErr, err)
			continue
		}
		for _, user := range users {
			userID := strings.TrimSpace(user.ID)
			if userID == "" {
				continue
			}
			itemPath := "/Users/" + url.PathEscape(userID) + "/Items/" + url.PathEscape(itemID)
			item, err := fetchEmbyItemAt(client, candidate, apiKey, itemPath)
			if err == nil {
				if s.Logs != nil {
					s.Logs.Info("emby item lookup selected user=%s item=%s", userID, itemID)
				}
				return item, nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no emby base_url candidates")
	}
	return nil, lastErr
}

func fetchEmbyItemAt(client *http.Client, base *url.URL, apiKey, itemPath string) (*embyItemResponse, error) {
	var item embyItemResponse
	if err := embyGetJSON(client, base, apiKey, itemPath, map[string]string{"Fields": "Path,MediaSources"}, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func fetchEmbyUsersAt(client *http.Client, base *url.URL, apiKey string) ([]embyUserResponse, error) {
	var users []embyUserResponse
	if err := embyGetJSON(client, base, apiKey, "/Users", nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func embyGetJSON(client *http.Client, base *url.URL, apiKey, requestPath string, query map[string]string, out any) error {
	u := embyTargetURL(base, requestPath, "")
	q := u.Query()
	q.Set("api_key", apiKey)
	for key, value := range query {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", u.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s failed: %s", u.Host, u.Path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode %s failed: %w", u.Host, u.Path, err)
	}
	return nil
}

func (s *Server) embyProbe(c *gin.Context) {
	requestHost := c.DefaultQuery("host", c.Request.Host)
	candidates, err := embyBaseURLCandidates(s.Config.Emby.BaseURL, requestHost)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	out := make([]gin.H, 0, len(candidates))
	for _, candidate := range candidates {
		u := embyTargetURL(candidate, "/System/Info/Public", "")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				out = append(out, gin.H{"url": candidate.Redacted(), "probe": u.Redacted(), "status": resp.Status, "ok": resp.StatusCode >= 200 && resp.StatusCode < 500})
				_ = resp.Body.Close()
			} else {
				out = append(out, gin.H{"url": candidate.Redacted(), "probe": u.Redacted(), "error": err.Error(), "ok": false})
			}
		} else {
			out = append(out, gin.H{"url": candidate.Redacted(), "probe": u.Redacted(), "error": err.Error(), "ok": false})
		}
		cancel()
	}
	ok(c, gin.H{"request_host": requestHost, "base_url": s.Config.Emby.BaseURL, "candidates": out})
}

func (s *Server) embyItemSTRMPath(item *embyItemResponse, mediaSourceID string) (string, error) {
	path := strings.TrimSpace(item.Path)
	mediaSourceID = strings.TrimSpace(mediaSourceID)
	if mediaSourceID != "" {
		for _, source := range item.MediaSources {
			if source.ID == mediaSourceID && strings.TrimSpace(source.Path) != "" {
				path = strings.TrimSpace(source.Path)
				break
			}
		}
	}
	if path == "" && len(item.MediaSources) > 0 {
		path = strings.TrimSpace(item.MediaSources[0].Path)
	}
	if isHTTPURL(path) {
		return path, nil
	}
	path = s.rewriteEmbyPath(path)
	if strings.ToLower(filepath.Ext(path)) != ".strm" {
		return "", fmt.Errorf("emby item path is not a .strm file: %s", path)
	}
	return path, nil
}

func (s *Server) rewriteEmbyPath(path string) string {
	normalized := filepath.Clean(path)
	for _, rule := range s.Config.Emby.PathRewrites {
		from := strings.TrimSpace(rule.From)
		to := strings.TrimSpace(rule.To)
		if from == "" || to == "" {
			continue
		}
		from = filepath.Clean(from)
		if normalized == from {
			return filepath.Clean(to)
		}
		prefix := from + string(filepath.Separator)
		if strings.HasPrefix(normalized, prefix) {
			return filepath.Join(to, strings.TrimPrefix(normalized, prefix))
		}
	}
	return normalized
}

func readSTRMURL(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
	}
	return "", errors.New("empty strm file")
}

func pickcodeFromSTRMURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	pickcode := strings.TrimSpace(u.Query().Get("pickcode"))
	if pickcode == "" {
		return "", errors.New("strm url has no pickcode")
	}
	return pickcode, nil
}

func isHTTPURL(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

func queryMediaSourceID(values url.Values) string {
	for _, key := range []string{"media_source_id", "MediaSourceId", "mediasourceid"} {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			return value
		}
	}
	return ""
}
