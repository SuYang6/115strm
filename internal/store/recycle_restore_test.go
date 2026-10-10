package store

import (
	"path/filepath"
	"testing"
	"time"

	"115strm/internal/model"
)

func TestRecycleRestoreColumnMigratesOldDatabase(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := db.AddRecycleItem(&model.RecycleItem{DeletedFileName: "file", DeletedRelativePath: "file", RecyclePath: "recycle/file", DeletedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`ALTER TABLE recycle_items DROP COLUMN restored_at`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	items, total, err := db.ListRecycleItems(0, 0)
	if err != nil || total != 1 || len(items) != 1 || items[0].RestoredAt != nil {
		t.Fatalf("旧数据迁移: items=%v total=%d err=%v", items, total, err)
	}
}
