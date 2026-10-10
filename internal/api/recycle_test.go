package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/auth"
	"115strm/internal/service/logbuffer"
	"115strm/internal/service/strm302"
	"115strm/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestRecycleAuthenticatedRestoreAndValidation(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.Storage.DataDir = root
	cfg.STRM302.BatchDir = filepath.Join(root, "Media")
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	logs, err := logbuffer.New(filepath.Join(root, "test.log"), 10)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte("recycle-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("tester", string(hash)); err != nil {
		t.Fatal(err)
	}
	authSvc, err := auth.New(db, cfg, logs)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authSvc.Login("tester", "recycle-test-password", "test")
	if err != nil {
		t.Fatal(err)
	}
	svc := strm302.New(cfg, db, nil, logs, nil)
	router := NewRouter(Dependencies{Config: cfg, DB: db, Logs: logs, Auth: authSvc, STRM: svc})
	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if response := request("POST", "/api/recycle/restore", `{"ids":[1]}`, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录恢复: %d", response.Code)
	}
	for _, body := range []string{`{}`, `{"ids":[]}`, `{"ids":[0]}`, `{"ids":[-1]}`, `{"ids":["1"]}`} {
		if response := request("POST", "/api/recycle/restore", body, true); response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, response.Code)
		}
	}
	source := filepath.Join(root, "recycle", "2026-10-08", "手动扫描", "电影", "A-mediainfo.json")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := db.AddRecycleItem(&model.RecycleItem{DeletedFileName: "A-mediainfo.json", DeletedRelativePath: "电影/A-mediainfo.json", OriginalPath: filepath.Join(cfg.STRM302.BatchDir, "电影", "A-mediainfo.json"), RecyclePath: source, DeletedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	response := request("POST", "/api/recycle/restore", `{"ids":[1,1]}`, true)
	var envelope struct {
		Data model.RecycleRestoreResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || envelope.Data.Requested != 1 || envelope.Data.Restored != 1 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	response = request("GET", "/api/recycle?limit=1000", "", true)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"total":0`) {
		t.Fatalf("恢复后列表: %s", response.Body.String())
	}
}
