package strm302

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/logbuffer"
	"115strm/internal/store"
)

func newRestoreTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Defaults()
	cfg.STRM302.BatchDir = filepath.Join(t.TempDir(), "Media")
	cfg.Storage.DataDir = t.TempDir()
	if err := os.MkdirAll(cfg.STRM302.BatchDir, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	logs, err := logbuffer.New(filepath.Join(t.TempDir(), "test.log"), 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logs.Close() })
	return New(cfg, db, nil, logs, nil)
}

func addRestoreFixture(t *testing.T, s *Service, rel, content string) model.RecycleItem {
	t.Helper()
	item := model.RecycleItem{
		DeletedFileName: filepath.Base(rel), DeletedRelativePath: filepath.ToSlash(rel),
		OriginalPath: filepath.Join(s.cfg.STRM302.BatchDir, rel),
		RecyclePath:  filepath.Join(s.recycleRoot(), "2026-10-08", "手动扫描", rel), DeletedAt: time.Now(),
	}
	writeTestFile(t, item.RecyclePath, content)
	if err := s.db.AddRecycleItem(&item); err != nil {
		t.Fatal(err)
	}
	if err := s.db.DB.QueryRow("SELECT MAX(id) FROM recycle_items").Scan(&item.ID); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestCleanupProtectsLocalMediaInfo(t *testing.T) {
	s := newRestoreTestService(t)
	s.cfg.STRM302.CompanionExtensions["metadata"] = []string{".json", ".nfo"}
	jsons := []string{"电影/影片 A/A-mediainfo.json", "电视/B/B-MEDIAINFO.JSON", "动漫/C/C-mediainfo.json"}
	for _, rel := range jsons {
		writeTestFile(t, filepath.Join(s.cfg.STRM302.BatchDir, filepath.FromSlash(rel)), `{"duration":100}`)
	}
	writeTestFile(t, filepath.Join(s.cfg.STRM302.BatchDir, "电影/影片 A/A.strm"), "video")
	if err := s.db.UpsertMediaFile(&model.MediaFile{RelativePath: jsons[0], PickCode: "json", FileName: "A-mediainfo.json", LocalPath: filepath.Join(s.cfg.STRM302.BatchDir, filepath.FromSlash(jsons[0])), SourcePath: jsons[0], MediaType: "metadata", LastScanID: "old"}); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.cleanupMissing("new", "", []model.CloudFile{{Name: "A.mkv", SourcePath: "电影/影片 A/A.mkv"}}, "手动扫描")
	if err != nil || deleted != 0 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	for _, rel := range jsons {
		if !fileExists(filepath.Join(s.cfg.STRM302.BatchDir, filepath.FromSlash(rel))) {
			t.Fatalf("JSON 被清理: %s", rel)
		}
	}
	orphan := filepath.Join(s.cfg.STRM302.BatchDir, "电影/影片 A/old.nfo")
	writeTestFile(t, orphan, "orphan")
	deleted, err = s.cleanupMissingLocalMedia("电影", nil, "手动扫描")
	if err != nil || deleted != 1 || fileExists(orphan) {
		t.Fatalf("普通媒体清理: deleted=%d err=%v", deleted, err)
	}
}

func TestMissingSTRMDoesNotRecycleSurvivingJSON(t *testing.T) {
	s := newRestoreTestService(t)
	path := filepath.Join(s.cfg.STRM302.BatchDir, "Movie", "example.strm")
	json := companionMediaInfoPath(path)
	writeTestFile(t, json, "metadata")
	_, err := s.db.DB.Exec(`INSERT INTO strm_files(relative_path,pickcode,file_name,source_path,local_path,last_scan_id,generated_at) VALUES('Movie/example.strm','p','example.mkv','Movie/example.mkv',?,'old',?)`, path, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.cleanupMissing("new", "", nil, "manual"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(json) {
		t.Fatal("不存在的 STRM 不应联动回收 JSON")
	}
}

func TestRestorePartialResultsAndIdempotency(t *testing.T) {
	s := newRestoreTestService(t)
	good := addRestoreFixture(t, s, "电影/影片 A/Season 1/A-mediainfo.json", "good")
	same := addRestoreFixture(t, s, "电视/B/B-mediainfo.json", "same")
	conflict := addRestoreFixture(t, s, "动漫/C/C-mediainfo.json", "source")
	missing := addRestoreFixture(t, s, "动漫/D/D-mediainfo.json", "missing")
	writeTestFile(t, same.OriginalPath, "same")
	writeTestFile(t, conflict.OriginalPath, "different")
	if err := os.Remove(missing.RecyclePath); err != nil {
		t.Fatal(err)
	}
	response := s.RestoreRecycleItems(context.Background(), []int64{good.ID, same.ID, conflict.ID, missing.ID, 99999})
	if response.Restored != 1 || response.AlreadyRestored != 1 || response.Conflicts != 1 || response.Failed != 2 {
		t.Fatalf("%+v", response)
	}
	data, err := os.ReadFile(good.OriginalPath)
	if err != nil || string(data) != "good" {
		t.Fatalf("目标错误: %q %v", data, err)
	}
	data, _ = os.ReadFile(conflict.OriginalPath)
	if string(data) != "different" {
		t.Fatal("覆盖了冲突目标")
	}
	if !fileExists(good.RecyclePath) {
		t.Fatal("暂存副本被删除")
	}
	_, total, err := s.db.ListRecycleItems(1000, 0)
	if err != nil || total != 2 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	again := s.RestoreRecycleItems(context.Background(), []int64{good.ID})
	if again.AlreadyRestored != 1 {
		t.Fatalf("重复恢复: %+v", again)
	}
	if err := os.Remove(good.OriginalPath); err != nil {
		t.Fatal(err)
	}
	if response := s.RestoreRecycleItems(context.Background(), []int64{good.ID}); response.Restored != 1 {
		t.Fatalf("目标再次缺失: %+v", response)
	}
}

func TestRestoreDatabaseFailureCanRetry(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	_, err := s.db.DB.Exec(`CREATE TRIGGER fail_restore BEFORE UPDATE OF restored_at ON recycle_items BEGIN SELECT RAISE(FAIL,'模拟写入失败'); END`)
	if err != nil {
		t.Fatal(err)
	}
	response := s.RestoreRecycleItems(context.Background(), []int64{item.ID})
	if response.Failed != 1 || !fileExists(item.OriginalPath) || !fileExists(item.RecyclePath) {
		t.Fatalf("%+v", response)
	}
	if _, err := s.db.DB.Exec(`DROP TRIGGER fail_restore`); err != nil {
		t.Fatal(err)
	}
	response = s.RestoreRecycleItems(context.Background(), []int64{item.ID})
	if response.AlreadyRestored != 1 {
		t.Fatalf("重试未协调状态: %+v", response)
	}
}

func TestRestoreRejectsEscapesAndSymlinks(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	outside := filepath.Join(t.TempDir(), "outside.json")
	if _, err := s.db.DB.Exec(`UPDATE recycle_items SET original_path=? WHERE id=?`, outside, item.ID); err != nil {
		t.Fatal(err)
	}
	response := s.RestoreRecycleItems(context.Background(), []int64{item.ID})
	if response.Results[0].Status != "invalid_path" || fileExists(outside) {
		t.Fatalf("越界恢复: %+v", response)
	}
	link := filepath.Join(s.cfg.STRM302.BatchDir, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Logf("系统不允许符号链接，跳过该子断言: %v", err)
		return
	}
	if err := validateRestorePath(s.cfg.STRM302.BatchDir, filepath.Join(link, "x.json")); err == nil {
		t.Fatal("符号链接未拒绝")
	}
}

func TestRestoreCancelledRequestKeepsFiles(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := s.RestoreRecycleItems(ctx, []int64{item.ID})
	if response.Failed != 1 || fileExists(item.OriginalPath) || !fileExists(item.RecyclePath) {
		t.Fatalf("%+v", response)
	}
}

func TestRestoreThousandJSONFilesAndPagination(t *testing.T) {
	s := newRestoreTestService(t)
	ids := make([]int64, 0, 1001)
	for i := 0; i < 1001; i++ {
		item := addRestoreFixture(t, s, fmt.Sprintf("分类%d/深层/影片%d-mediainfo.json", i%3, i), fmt.Sprintf(`{"index":%d}`, i))
		ids = append(ids, item.ID)
	}
	for _, capacity := range []int{50, 200, 500, 1000} {
		items, total, err := s.db.ListRecycleItems(capacity, 0)
		if err != nil || total != 1001 || len(items) != capacity {
			t.Fatalf("limit=%d len=%d total=%d err=%v", capacity, len(items), total, err)
		}
	}
	start := time.Now()
	response := s.RestoreRecycleItems(context.Background(), ids[:1000])
	t.Logf("1000 个 JSON 真实恢复耗时: %s", time.Since(start))
	if response.Restored != 1000 || response.Failed != 0 {
		t.Fatalf("restored=%d failed=%d", response.Restored, response.Failed)
	}
	_, total, err := s.db.ListRecycleItems(1000, 0)
	if err != nil || total != 1 {
		t.Fatalf("total=%d err=%v", total, err)
	}
}

func TestConcurrentRestoreDoesNotOverwrite(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	responses := make(chan model.RecycleRestoreResponse, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() { defer wait.Done(); responses <- s.RestoreRecycleItems(context.Background(), []int64{item.ID}) }()
	}
	wait.Wait()
	close(responses)
	var restored, already int
	for response := range responses {
		restored += response.Restored
		already += response.AlreadyRestored
	}
	if restored != 1 || already != 1 {
		t.Fatalf("restored=%d already=%d", restored, already)
	}
}

func TestValidateRestoreIDs(t *testing.T) {
	ids, err := ValidateRestoreIDs([]int64{2, 2, 1})
	if err != nil || len(ids) != 2 || ids[0] != 2 {
		t.Fatalf("%v %v", ids, err)
	}
	for _, invalid := range [][]int64{nil, {0}, {-1}} {
		if _, err := ValidateRestoreIDs(invalid); err == nil {
			t.Fatalf("接受无效输入 %v", invalid)
		}
	}
	tooMany := make([]int64, 1001)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	if _, err := ValidateRestoreIDs(tooMany); err == nil {
		t.Fatal("接受超过1000项")
	}
}

func TestRestoredBackupExpiryKeepsOriginal(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	if response := s.RestoreRecycleItems(context.Background(), []int64{item.ID}); response.Restored != 1 {
		t.Fatalf("%+v", response)
	}
	if _, err := s.db.DB.Exec(`UPDATE recycle_items SET deleted_at=? WHERE id=?`, time.Now().AddDate(0, 0, -4).UTC().Format(time.RFC3339Nano), item.ID); err != nil {
		t.Fatal(err)
	}
	count, err := s.CleanRecycleRecords(3)
	if err != nil || count != 1 || fileExists(item.RecyclePath) || !fileExists(item.OriginalPath) {
		t.Fatalf("清理副本影响原文件 count=%d err=%v", count, err)
	}
	if response := s.RestoreRecycleItems(context.Background(), []int64{item.ID}); response.AlreadyRestored != 1 {
		t.Fatalf("清理副本后重复请求: %+v", response)
	}
}

func TestRestoreOriginalNameAndLegacyRelativePath(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	renamed := item.RecyclePath + ".deleted-123456"
	if err := os.Rename(item.RecyclePath, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.DB.Exec(`UPDATE recycle_items SET recycle_path=?, original_path='' WHERE id=?`, renamed, item.ID); err != nil {
		t.Fatal(err)
	}
	response := s.RestoreRecycleItems(context.Background(), []int64{item.ID})
	if response.Restored != 1 || !fileExists(item.OriginalPath) || fileExists(item.OriginalPath+".deleted-123456") {
		t.Fatalf("%+v", response)
	}
}

func TestRestoreSTRMCoordinatesDeletedRecord(t *testing.T) {
	s := newRestoreTestService(t)
	cloud := model.CloudFile{Name: "Movie.mkv", SourcePath: "电影/Movie.mkv", PickCode: "video", Size: 10}
	if _, err := s.syncVideoFile(cloud, "old"); err != nil {
		t.Fatal(err)
	}
	file, err := s.db.STRMFileByPickcode("video")
	if err != nil || file == nil {
		t.Fatalf("%v", err)
	}
	if err := s.recycleDeletedFile(file.LocalPath, file.RelativePath, "缺失STRM", "manual", file.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.db.MarkSTRMDeleted(file.ID); err != nil {
		t.Fatal(err)
	}
	items, _, err := s.db.ListRecycleItems(50, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("%v", err)
	}
	response := s.RestoreRecycleItems(context.Background(), []int64{items[0].ID})
	if response.Restored != 1 {
		t.Fatalf("%+v", response)
	}
	file, err = s.db.STRMFileByID(file.ID)
	if err != nil || file.DeletedAt != nil || !file.LocalExists {
		t.Fatalf("业务状态未恢复: %+v %v", file, err)
	}
}

func TestCopyRestoreFileNeverOverwritesAndCleansTemps(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	writeTestFile(t, from, "source")
	writeTestFile(t, to, "existing")
	if err := copyRestoreFile(context.Background(), from, to, 0644); err == nil {
		t.Fatal("已存在目标应拒绝发布")
	}
	data, _ := os.ReadFile(to)
	if string(data) != "existing" {
		t.Fatal("目标被覆盖")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyRestoreFile(ctx, from, filepath.Join(dir, "cancelled"), 0644); err == nil {
		t.Fatal("取消请求未失败")
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".recycle-restore-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("临时文件未清理: %v %v", temps, err)
	}
}

func TestRestoreAndPurgeAreSerialized(t *testing.T) {
	s := newRestoreTestService(t)
	item := addRestoreFixture(t, s, "电影/A/A-mediainfo.json", "data")
	if _, err := s.db.DB.Exec(`UPDATE recycle_items SET deleted_at=? WHERE id=?`, time.Now().AddDate(0, 0, -4).UTC().Format(time.RFC3339Nano), item.ID); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	var response model.RecycleRestoreResponse
	var purgeErr error
	wait.Add(2)
	go func() { defer wait.Done(); response = s.RestoreRecycleItems(context.Background(), []int64{item.ID}) }()
	go func() { defer wait.Done(); _, purgeErr = s.CleanRecycleRecords(3) }()
	wait.Wait()
	if purgeErr != nil {
		t.Fatal(purgeErr)
	}
	if response.Restored == 1 && !fileExists(item.OriginalPath) {
		t.Fatal("恢复目标被清理")
	}
	if response.Restored == 0 && response.Results[0].Status != "purged" {
		t.Fatalf("非预期结果: %+v", response)
	}
}
