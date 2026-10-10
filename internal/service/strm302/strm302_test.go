package strm302

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/logbuffer"
	"115strm/internal/store"
)

func TestCleanupMissingLocalMediaDeletesOrphans(t *testing.T) {
	batchDir := t.TempDir()
	cfg := config.Defaults()
	cfg.STRM302.BatchDir = batchDir
	cfg.Storage.DataDir = t.TempDir()

	logs, err := logbuffer.New(filepath.Join(t.TempDir(), "115strm.log"), 10)
	if err != nil {
		t.Fatalf("create log buffer: %v", err)
	}
	defer logs.Close()

	svc := New(cfg, nil, nil, logs, nil)
	writeTestFile(t, filepath.Join(batchDir, "Show", "keep.nfo"), "keep")
	writeTestFile(t, filepath.Join(batchDir, "Show", "old.nfo"), "old")
	writeTestFile(t, filepath.Join(batchDir, "Show", "old.ass"), "old")
	writeTestFile(t, filepath.Join(batchDir, "Show", "poster.jpg"), "old")
	writeTestFile(t, filepath.Join(batchDir, "Show", "movie.strm"), "strm")

	deleted, err := svc.cleanupMissingLocalMedia("/Show", []model.CloudFile{
		{Name: "keep.nfo", SourcePath: "/Show/keep.nfo"},
	}, "手动扫描")
	if err != nil {
		t.Fatalf("cleanup local media: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted %d orphan media files, want 3", deleted)
	}
	assertPathExists(t, filepath.Join(batchDir, "Show", "keep.nfo"))
	assertPathExists(t, filepath.Join(batchDir, "Show", "movie.strm"))
	assertPathMissing(t, filepath.Join(batchDir, "Show", "old.nfo"))
	assertPathMissing(t, filepath.Join(batchDir, "Show", "old.ass"))
	assertPathMissing(t, filepath.Join(batchDir, "Show", "poster.jpg"))
}

func TestCleanupMissingDoesNotMoveCurrentScanSTRMPath(t *testing.T) {
	batchDir := t.TempDir()
	cfg := config.Defaults()
	cfg.STRM302.BatchDir = batchDir
	cfg.Storage.DataDir = t.TempDir()

	logs, err := logbuffer.New(filepath.Join(t.TempDir(), "115strm.log"), 10)
	if err != nil {
		t.Fatalf("create log buffer: %v", err)
	}
	defer logs.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "115strm.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	localPath := filepath.Join(batchDir, "Movie", "example.strm")
	writeTestFile(t, localPath, "http://127.0.0.1:8585/api/strm302/redirect?pickcode=new-pickcode\n")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.DB.Exec(`
		INSERT INTO strm_files(relative_path, pickcode, file_name, source_path, local_path, source_type, companions_json, last_scan_id, generated_at)
		VALUES
			('Movie/example.strm', 'old-pickcode', 'example.mkv', 'Movie/example.mkv', ?, 'cd2', '[]', 'scan-old', ?),
			('Movie/example.strm', 'new-pickcode', 'example.mkv', 'Movie/example.mkv', ?, 'cd2', '[]', 'scan-new', ?)
	`, localPath, now, localPath, now)
	if err != nil {
		t.Fatalf("insert strm rows: %v", err)
	}

	svc := New(cfg, db, nil, logs, nil)
	deleted, err := svc.cleanupMissing("scan-new", "", []model.CloudFile{
		{Name: "example.mkv", SourcePath: "Movie/example.mkv", PickCode: "new-pickcode"},
	}, "manual")
	if err != nil {
		t.Fatalf("cleanup missing: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d, want 1 soft-deleted stale row", deleted)
	}
	assertPathExists(t, localPath)
}

func TestBuildGeneratePlanMarksPickcodeChangeForMediaInfo(t *testing.T) {
	svc, db, batchDir, closeLogs := newTestServiceWithStore(t)
	defer closeLogs()
	defer db.Close()

	localPath := filepath.Join(batchDir, "Movie", "example.strm")
	writeTestFile(t, localPath, "http://127.0.0.1:8585/api/strm302/redirect?pickcode=old-pickcode\n")
	if err := insertTestSTRMFile(db, localPath, "old-pickcode", "example.mkv", "Movie/example.mkv", "", "", 100, 1); err != nil {
		t.Fatalf("insert existing strm: %v", err)
	}

	plan := svc.buildGeneratePlan([]model.CloudFile{{
		Name:       "example.mkv",
		SourcePath: "Movie/example.mkv",
		PickCode:   "new-pickcode",
		Size:       100,
		ModTime:    1,
		SourceType: "cd2",
	}})
	if plan.Stats.VideoUpdated != 1 {
		t.Fatalf("VideoUpdated=%d, want 1", plan.Stats.VideoUpdated)
	}
	if plan.Stats.VideoMediaInfoNeeded != 1 {
		t.Fatalf("VideoMediaInfoNeeded=%d, want 1", plan.Stats.VideoMediaInfoNeeded)
	}
}

func TestBuildGeneratePlanDoesNotMarkMTimeOnlyChangeForMediaInfo(t *testing.T) {
	svc, db, batchDir, closeLogs := newTestServiceWithStore(t)
	defer closeLogs()
	defer db.Close()

	localPath := filepath.Join(batchDir, "Movie", "example.strm")
	writeTestFile(t, localPath, "http://127.0.0.1:8585/api/strm302/redirect?pickcode=same-pickcode\n")
	if err := insertTestSTRMFile(db, localPath, "same-pickcode", "example.mkv", "Movie/example.mkv", "file-1", "sha-1", 100, 1); err != nil {
		t.Fatalf("insert existing strm: %v", err)
	}

	plan := svc.buildGeneratePlan([]model.CloudFile{{
		ID:         "file-1",
		Name:       "example.mkv",
		SourcePath: "Movie/example.mkv",
		PickCode:   "same-pickcode",
		Sha1:       "sha-1",
		Size:       100,
		CID:        "cid-1",
		ModTime:    2,
		SourceType: "cd2",
	}})
	if plan.Stats.VideoUpdated != 1 {
		t.Fatalf("VideoUpdated=%d, want 1", plan.Stats.VideoUpdated)
	}
	if plan.Stats.VideoMediaInfoNeeded != 0 {
		t.Fatalf("VideoMediaInfoNeeded=%d, want 0", plan.Stats.VideoMediaInfoNeeded)
	}
}

func newTestServiceWithStore(t *testing.T) (*Service, *store.Store, string, func()) {
	t.Helper()
	batchDir := t.TempDir()
	cfg := config.Defaults()
	cfg.STRM302.BatchDir = batchDir
	cfg.Storage.DataDir = t.TempDir()

	logs, err := logbuffer.New(filepath.Join(t.TempDir(), "115strm.log"), 10)
	if err != nil {
		t.Fatalf("create log buffer: %v", err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "115strm.db"))
	if err != nil {
		_ = logs.Close()
		t.Fatalf("open store: %v", err)
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		_ = logs.Close()
		t.Fatalf("migrate store: %v", err)
	}
	return New(cfg, db, nil, logs, nil), db, batchDir, func() { _ = logs.Close() }
}

func insertTestSTRMFile(db *store.Store, localPath, pickcode, fileName, sourcePath, cloudFileID, sha1 string, size, sourceMTime int64) error {
	_, err := db.DB.Exec(`
		INSERT INTO strm_files(relative_path, pickcode, file_name, size, cloud_file_id, sha1, source_path, source_mtime, local_path, local_exists, source_type, companions_json, last_scan_id, generated_at)
		VALUES('Movie/example.strm', ?, ?, ?, ?, ?, ?, ?, ?, 1, 'cd2', '[]', 'scan-old', ?)
	`, pickcode, fileName, size, cloudFileID, sha1, sourcePath, sourceMTime, localPath, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, stat err=%v", path, err)
	}
}

func TestCompanionMediaInfoPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"strm returns sibling json", "/opt/Media/电影/调音师 (2026)/Tuner.2025.strm", "/opt/Media/电影/调音师 (2026)/Tuner.2025-mediainfo.json"},
		{"empty returns empty", "", ""},
		{"non-strm returns empty", "/opt/Media/电影/foo.nfo", ""},
		{"uppercase STRM", "/opt/Media/foo.STRM", "/opt/Media/foo-mediainfo.json"},
		{"trailing space stripped", "  /opt/Media/x.strm  ", "/opt/Media/x-mediainfo.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := companionMediaInfoPath(c.in)
			if got != c.want {
				t.Fatalf("companionMediaInfoPath(%q)=%q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestIsMediaInfoCompanion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"basic", "foo-mediainfo.json", true},
		{"uppercase", "FOO-MEDIAINFO.JSON", true},
		{"non-json", "foo.nfo", false},
		{"other json", "config.json", false},
		{"empty", "", false},
		{"only suffix word", "-mediainfo.json", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isMediaInfoCompanion(c.in); got != c.want {
				t.Fatalf("isMediaInfoCompanion(%q)=%v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsProtectedDirName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"single letter A", "A", true},
		{"single digit 0", "0", true},
		{"digit 9", "9", true},
		{"group 0-9", "0-9", true},
		{"show folder", "暗影蜘蛛侠", false},
		{"season folder", "S 1", false},
		{"empty", "", true},
		{"two letters", "AB", false},
		{"two digits", "10", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isProtectedDirName(c.in); got != c.want {
				t.Fatalf("isProtectedDirName(%q)=%v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestMissingSTRMDecisionAlwaysMovesOnMissing(t *testing.T) {
	svc, db, batchDir, closeLogs := newTestServiceWithStore(t)
	defer closeLogs()
	defer db.Close()

	localPath := filepath.Join(batchDir, "Movie", "example.strm")
	writeTestFile(t, localPath, "http://127.0.0.1:8585/api/strm302/redirect?pickcode=p\n")

	f := model.STRMFile{
		ID:         1,
		LocalPath:  localPath,
		SourcePath: "Movie/example.mkv",
		PickCode:   "p",
	}
	safety := cleanupSafetyState{sourcePaths: map[string]struct{}{}, localPaths: map[string]struct{}{}}

	if got := svc.missingSTRMDecision(f, safety); got != "move" {
		t.Fatalf("fresh missing got %q want %q", got, "move")
	}
	f.MissingSinceScanID = "scan-old"
	if got := svc.missingSTRMDecision(f, safety); got != "move" {
		t.Fatalf("with prior MissingSinceScanID got %q want %q", got, "move")
	}
}

func TestCleanupMissingMovesCompanionMediaInfo(t *testing.T) {
	batchDir := t.TempDir()
	cfg := config.Defaults()
	cfg.STRM302.BatchDir = batchDir
	cfg.Storage.DataDir = t.TempDir()

	logs, err := logbuffer.New(filepath.Join(t.TempDir(), "115strm.log"), 10)
	if err != nil {
		t.Fatalf("create log buffer: %v", err)
	}
	defer logs.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "115strm.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	strmPath := filepath.Join(batchDir, "Movie", "example.strm")
	jsonPath := filepath.Join(batchDir, "Movie", "example-mediainfo.json")
	writeTestFile(t, strmPath, "http://127.0.0.1:8585/api/strm302/redirect?pickcode=p\n")
	writeTestFile(t, jsonPath, `{"duration": 100}`)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.DB.Exec(`
		INSERT INTO strm_files(relative_path, pickcode, file_name, source_path, local_path, source_type, companions_json, last_scan_id, generated_at)
		VALUES('Movie/example.strm', 'p', 'example.mkv', 'Movie/example.mkv', ?, 'cd2', '[]', 'scan-old', ?)
		`, strmPath, now)
	if err != nil {
		t.Fatalf("insert strm: %v", err)
	}

	svc := New(cfg, db, nil, logs, nil)
	deleted, err := svc.cleanupMissing("scan-new", "", []model.CloudFile{}, "manual")
	if err != nil {
		t.Fatalf("cleanup missing: %v", err)
	}
	if deleted < 1 {
		t.Fatalf("deleted=%d, want >= 1", deleted)
	}
	assertPathMissing(t, strmPath)
	assertPathMissing(t, jsonPath)
}
func TestCleanupAncestorEmptyDirsStopsAtTwoLevelsAndBoundaries(t *testing.T) {
	batchDir := t.TempDir()
	cfg := config.Defaults()
	cfg.STRM302.BatchDir = batchDir
	cfg.Storage.DataDir = t.TempDir()

	logs, err := logbuffer.New(filepath.Join(t.TempDir(), "115strm.log"), 10)
	if err != nil {
		t.Fatalf("create log buffer: %v", err)
	}
	defer logs.Close()
	svc := New(cfg, nil, nil, logs, nil)

	t.Run("S1 empty deleted, S2 keeps show alive, A preserved", func(t *testing.T) {
		root := filepath.Join(batchDir, "tvcase1")
		show := filepath.Join(root, "A", "暗影蜘蛛侠")
		s1 := filepath.Join(show, "S 1")
		s2 := filepath.Join(show, "S 2")
		for _, d := range []string{s1, s2} {
			if err := os.MkdirAll(d, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", d, err)
			}
		}
		writeTestFile(t, filepath.Join(s2, "S02E02.strm"), "x")

		strmInS1 := filepath.Join(s1, "S01E01.strm")
		deleted, err := svc.cleanupAncestorEmptyDirs(strmInS1, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted=%d, want 1 (S1)", deleted)
		}
		assertPathMissing(t, s1)
		assertPathExists(t, show)
		assertPathExists(t, filepath.Join(root, "A"))
	})

	t.Run("full show cleanup via two STRM deletes", func(t *testing.T) {
		root := filepath.Join(batchDir, "tvcase2")
		show := filepath.Join(root, "A", "暗影蜘蛛侠")
		s1 := filepath.Join(show, "S 1")
		s2 := filepath.Join(show, "S 2")
		for _, d := range []string{s1, s2} {
			if err := os.MkdirAll(d, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", d, err)
			}
		}
		strm1 := filepath.Join(s1, "S01E01.strm")
		strm2 := filepath.Join(s2, "S02E02.strm")
		writeTestFile(t, strm1, "x")
		writeTestFile(t, strm2, "x")

		if err := os.Remove(strm1); err != nil {
			t.Fatalf("remove strm1: %v", err)
		}
		d1, err := svc.cleanupAncestorEmptyDirs(strm1, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup S1: %v", err)
		}
		if d1 != 1 {
			t.Fatalf("S1 cleanup deleted=%d, want 1", d1)
		}
		assertPathMissing(t, s1)
		assertPathExists(t, show)

		if err := os.Remove(strm2); err != nil {
			t.Fatalf("remove strm2: %v", err)
		}
		d2, err := svc.cleanupAncestorEmptyDirs(strm2, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup S2: %v", err)
		}
		if d2 != 2 {
			t.Fatalf("S2 cleanup deleted=%d, want 2 (S2 + show)", d2)
		}
		assertPathMissing(t, s2)
		assertPathMissing(t, show)
		assertPathExists(t, filepath.Join(root, "A"))
		assertPathExists(t, root)
	})

	t.Run("movie folder deleted, 电影 root preserved", func(t *testing.T) {
		// 电影 必须是 BatchDir 直接子目录，才能触发 parentAbs == rootAbs 边界保护
		movieRoot := filepath.Join(batchDir, "电影")
		movie := filepath.Join(movieRoot, "调音师 (2026)")
		if err := os.MkdirAll(movie, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", movie, err)
		}
		strm := filepath.Join(movie, "Tuner.2025.strm")
		writeTestFile(t, strm, "x")
		if err := os.Remove(strm); err != nil {
			t.Fatalf("remove strm: %v", err)
		}

		deleted, err := svc.cleanupAncestorEmptyDirs(strm, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted=%d, want 1 (movie folder)", deleted)
		}
		assertPathMissing(t, movie)
		assertPathExists(t, movieRoot)
	})

	t.Run("letter folder with single STRM preserves letter folder", func(t *testing.T) {
		root := filepath.Join(batchDir, "moviecase2")
		letter := filepath.Join(root, "电影", "A")
		movie := filepath.Join(letter, "调音师 (2026)")
		if err := os.MkdirAll(movie, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", movie, err)
		}
		strm := filepath.Join(movie, "Tuner.2025.strm")
		writeTestFile(t, strm, "x")
		if err := os.Remove(strm); err != nil {
			t.Fatalf("remove strm: %v", err)
		}

		deleted, err := svc.cleanupAncestorEmptyDirs(strm, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted=%d, want 1 (movie folder only)", deleted)
		}
		assertPathMissing(t, movie)
		assertPathExists(t, letter)
	})

	t.Run("0-9 grouping folder preserved", func(t *testing.T) {
		root := filepath.Join(batchDir, "moviecase3")
		group := filepath.Join(root, "电影", "0-9")
		movie := filepath.Join(group, "Movie (2026)")
		if err := os.MkdirAll(movie, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", movie, err)
		}
		strm := filepath.Join(movie, "Movie.2026.strm")
		writeTestFile(t, strm, "x")
		if err := os.Remove(strm); err != nil {
			t.Fatalf("remove strm: %v", err)
		}

		deleted, err := svc.cleanupAncestorEmptyDirs(strm, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted=%d, want 1 (movie folder only)", deleted)
		}
		assertPathMissing(t, movie)
		assertPathExists(t, group)
	})

	t.Run("non-empty stops walk-up immediately", func(t *testing.T) {
		root := filepath.Join(batchDir, "nonemptycase")
		show := filepath.Join(root, "暗影蜘蛛侠")
		s1 := filepath.Join(show, "S 1")
		s2 := filepath.Join(show, "S 2")
		if err := os.MkdirAll(s2, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", s2, err)
		}
		writeTestFile(t, filepath.Join(s2, "S02E02.strm"), "x")
		strm1 := filepath.Join(s1, "S01E01.strm")
		writeTestFile(t, strm1, "x")
		if err := os.Remove(strm1); err != nil {
			t.Fatalf("remove strm1: %v", err)
		}

		deleted, err := svc.cleanupAncestorEmptyDirs(strm1, maxAncestorCleanupLevels)
		if err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted=%d, want 1 (S1 only, S2 keeps show alive)", deleted)
		}
		assertPathMissing(t, s1)
		assertPathExists(t, show)
	})
}