package store

import (
	"path/filepath"
	"testing"
	"time"

	"115strm/internal/model"
)

func TestUpsertSTRMFileMergesSameSourcePathPickcodeChange(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "115strm.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	first := &model.STRMFile{
		RelativePath:   "Movie/example.strm",
		PickCode:       "old-pickcode",
		FileName:       "example.mkv",
		Size:           100,
		CID:            "cid",
		SourcePath:     "Movie/example.mkv",
		SourceMTime:    1,
		LocalPath:      filepath.Join(t.TempDir(), "example.strm"),
		LocalExists:    true,
		SourceType:     "cd2",
		CompanionsJSON: "[]",
		LastScanID:     "scan-old",
	}
	if err := db.UpsertSTRMFile(first); err != nil {
		t.Fatalf("insert old strm: %v", err)
	}

	second := *first
	second.PickCode = "new-pickcode"
	second.SourceMTime = 2
	second.LastScanID = "scan-new"
	if err := db.UpsertSTRMFile(&second); err != nil {
		t.Fatalf("merge new strm: %v", err)
	}

	files, err := db.ListSTRMFiles(10, 0)
	if err != nil {
		t.Fatalf("list strm: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("active strm count=%d, want 1", len(files))
	}
	if files[0].PickCode != "new-pickcode" {
		t.Fatalf("pickcode=%s, want new-pickcode", files[0].PickCode)
	}
	if files[0].PreviousPickCode != "old-pickcode" {
		t.Fatalf("previous_pickcode=%s, want old-pickcode", files[0].PreviousPickCode)
	}
	if files[0].LastScanID != "scan-new" {
		t.Fatalf("last_scan_id=%s, want scan-new", files[0].LastScanID)
	}

	old, err := db.STRMFileByPickcode("old-pickcode")
	if err != nil {
		t.Fatalf("query old pickcode: %v", err)
	}
	if old != nil {
		t.Fatalf("old pickcode still resolves to active row: %+v", old)
	}
}

func TestSTRMLocalPathReferencedByOther(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "115strm.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	localPath := filepath.Join(t.TempDir(), "example.strm")
	_, err = db.DB.Exec(`
		INSERT INTO strm_files(relative_path, pickcode, file_name, source_path, local_path, source_type, companions_json, last_scan_id, generated_at)
		VALUES
			('Movie/example.strm', 'old-pickcode', 'example.mkv', 'Movie/example.mkv', ?, 'cd2', '[]', 'scan-old', ?),
			('Movie/example.strm', 'new-pickcode', 'example.mkv', 'Movie/example.mkv', ?, 'cd2', '[]', 'scan-new', ?)
	`, localPath, now, localPath, now)
	if err != nil {
		t.Fatalf("insert duplicate local path rows: %v", err)
	}

	used, err := db.STRMLocalPathReferencedByOther(1, localPath)
	if err != nil {
		t.Fatalf("check local path reference: %v", err)
	}
	if !used {
		t.Fatal("expected local path to be referenced by another active STRM")
	}
}
