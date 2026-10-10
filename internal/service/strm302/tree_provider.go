package strm302

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"115strm/internal/model"
	"115strm/internal/util"
)

type treeProvider interface {
	SourceType() string
	Root(GenerateRequest) (scanDirCheckpoint, error)
	List(context.Context, int64, scanDirCheckpoint) ([]model.CloudFile, error)
}

func (s *Service) treeProvider(req GenerateRequest) (treeProvider, error) {
	source := normalizeSourceName(req.ScanSource)
	if source == "" {
		source = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	if source == "" {
		source = "115"
	}
	switch source {
	case "115":
		return pan115TreeProvider{s: s}, nil
	case "cd2":
		root := strings.TrimSpace(s.cfg.STRM302.CD2MountDir)
		if root == "" {
			return nil, errors.New("CD2 mount directory is not configured")
		}
		return localMountTreeProvider{root: root}, nil
	case "openlist":
		baseURL := strings.TrimSpace(s.cfg.STRM302.OpenListBaseURL)
		if baseURL == "" {
			return nil, errors.New("OpenList base URL is not configured")
		}
		return newOpenListTreeProvider(s), nil
	default:
		return nil, fmt.Errorf("unsupported scan source: %s", source)
	}
}

func normalizeSourceName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "115", "pan115", "open115":
		return "115"
	case "cd2", "local", "mount", "local_mount":
		return "cd2"
	case "openlist", "alist":
		return "openlist"
	default:
		return ""
	}
}

func normalizeModeName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "full":
		return "full"
	case "incremental", "increment", "inc":
		return "incremental"
	default:
		return "full"
	}
}

type pan115TreeProvider struct {
	s *Service
}

func (p pan115TreeProvider) SourceType() string { return "115" }

func (p pan115TreeProvider) Root(req GenerateRequest) (scanDirCheckpoint, error) {
	cid := strings.TrimSpace(req.CID)
	if cid == "" || cid == "0" {
		return scanDirCheckpoint{}, errors.New("non-root CID is required")
	}
	return scanDirCheckpoint{ID: cid, CID: cid, SourcePath: req.SourcePath}, nil
}

func (p pan115TreeProvider) List(_ context.Context, jobID int64, current scanDirCheckpoint) ([]model.CloudFile, error) {
	cid := strings.TrimSpace(current.ID)
	if cid == "" {
		cid = current.CID
	}
	files, err := p.s.driver.BrowseForJobNamed(jobID, cid, current.SourcePath)
	if err != nil {
		return nil, err
	}
	for i := range files {
		files[i].SourceType = p.SourceType()
	}
	return files, nil
}

type localMountTreeProvider struct {
	root string
}

func (p localMountTreeProvider) SourceType() string { return "cd2" }

func (p localMountTreeProvider) Root(req GenerateRequest) (scanDirCheckpoint, error) {
	root, err := util.SafeJoin(p.root, req.SourcePath)
	if err != nil {
		return scanDirCheckpoint{}, err
	}
	if stat, err := os.Stat(root); err != nil {
		return scanDirCheckpoint{}, err
	} else if !stat.IsDir() {
		return scanDirCheckpoint{}, fmt.Errorf("CD2 mount path is not a directory: %s", root)
	}
	return scanDirCheckpoint{ID: root, CID: req.CID, SourcePath: req.SourcePath}, nil
}

func (p localMountTreeProvider) List(_ context.Context, _ int64, current scanDirCheckpoint) ([]model.CloudFile, error) {
	entries, err := os.ReadDir(current.ID)
	if err != nil {
		return nil, err
	}
	out := make([]model.CloudFile, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(current.ID, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		size := info.Size()
		if entry.IsDir() {
			size = 0
		}
		out = append(out, model.CloudFile{
			ID:         path,
			ParentID:   current.ID,
			Name:       entry.Name(),
			IsDir:      entry.IsDir(),
			Size:       size,
			CID:        current.CID,
			SourceType: p.SourceType(),
			ModTime:    info.ModTime().Unix(),
		})
	}
	return out, nil
}

type openListTreeProvider struct {
	s      *Service
	client *openListClient
}

func newOpenListTreeProvider(s *Service) openListTreeProvider {
	return openListTreeProvider{s: s, client: newOpenListClient(s)}
}

func (p openListTreeProvider) SourceType() string { return "openlist" }

func (p openListTreeProvider) Root(req GenerateRequest) (scanDirCheckpoint, error) {
	root := slashJoinOpenList(p.s.cfg.STRM302.OpenListRootPath, req.SourcePath)
	return scanDirCheckpoint{ID: root, CID: req.CID, SourcePath: req.SourcePath}, nil
}

func (p openListTreeProvider) List(ctx context.Context, _ int64, current scanDirCheckpoint) ([]model.CloudFile, error) {
	items, err := p.client.List(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	out := make([]model.CloudFile, 0, len(items))
	for _, item := range items {
		mtime := int64(0)
		if t, err := time.Parse(time.RFC3339, item.Modified); err == nil {
			mtime = t.Unix()
		}
		out = append(out, model.CloudFile{
			ID:         slashJoinOpenList(current.ID, item.Name),
			ParentID:   current.ID,
			Name:       item.Name,
			IsDir:      item.IsDir,
			Size:       item.Size,
			CID:        current.CID,
			SourceType: p.SourceType(),
			ModTime:    mtime,
			ThumbURL:   item.Thumb,
			Sha1:       item.HashInfo,
		})
	}
	return out, nil
}

type openListClient struct {
	baseURL  string
	token    string
	username string
	password string
	pageSize int
	refresh  bool
	client   *http.Client
}

type openListResp[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type openListLoginData struct {
	Token string `json:"token"`
}

type openListFileListData struct {
	Content []openListFile `json:"content"`
	Total   int64          `json:"total"`
}

type openListFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Modified string `json:"modified"`
	Thumb    string `json:"thumb"`
	HashInfo string `json:"hashinfo"`
}

func newOpenListClient(s *Service) *openListClient {
	timeout := time.Duration(s.cfg.Security.OutboundTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	pageSize := s.cfg.STRM302.OpenListPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	return &openListClient{
		baseURL:  strings.TrimRight(strings.TrimSpace(s.cfg.STRM302.OpenListBaseURL), "/"),
		token:    strings.TrimSpace(s.cfg.STRM302.OpenListToken),
		username: strings.TrimSpace(s.cfg.STRM302.OpenListUsername),
		password: strings.TrimSpace(s.cfg.STRM302.OpenListPassword),
		pageSize: pageSize,
		refresh:  s.cfg.STRM302.OpenListRefresh,
		client:   &http.Client{Timeout: timeout},
	}
}

func (c *openListClient) List(ctx context.Context, path string) ([]openListFile, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	var out []openListFile
	for page := 1; ; page++ {
		var resp openListResp[openListFileListData]
		req := map[string]any{
			"path":     normalizeOpenListPath(path),
			"page":     page,
			"per_page": c.pageSize,
			"refresh":  c.refresh,
			"password": "",
		}
		if err := c.post(ctx, "/api/fs/list", req, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Data.Content...)
		if len(resp.Data.Content) == 0 || int64(len(out)) >= resp.Data.Total {
			return out, nil
		}
	}
}

func (c *openListClient) ensureToken(ctx context.Context) error {
	if c.token != "" || c.username == "" || c.password == "" {
		return nil
	}
	var resp openListResp[openListLoginData]
	if err := c.post(ctx, "/api/auth/login", map[string]string{
		"username": c.username,
		"password": c.password,
	}, &resp); err != nil {
		return err
	}
	c.token = strings.TrimSpace(resp.Data.Token)
	if c.token == "" {
		return errors.New("OpenList login returned empty token")
	}
	return nil
}

func (c *openListClient) post(ctx context.Context, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "115strm/openlist")
	if c.token != "" {
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OpenList request failed: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	code, message := openListStatus(out)
	if code != 0 && code != http.StatusOK {
		if message == "" {
			message = fmt.Sprintf("code %d", code)
		}
		return errors.New("OpenList request failed: " + message)
	}
	return nil
}

func openListStatus(out any) (int, string) {
	b, err := json.Marshal(out)
	if err != nil {
		return 0, ""
	}
	var raw struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &raw)
	return raw.Code, raw.Message
}

func slashJoinOpenList(parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.Trim(strings.ReplaceAll(part, "\\", "/"), "/")
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

func normalizeOpenListPath(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
