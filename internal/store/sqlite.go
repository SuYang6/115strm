package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_login_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS settings_kv (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS strm_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			relative_path TEXT NOT NULL,
			pickcode TEXT NOT NULL UNIQUE,
			previous_pickcode TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			cid TEXT NOT NULL DEFAULT '',
			cloud_file_id TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			source_fingerprint TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL DEFAULT '',
			source_mtime INTEGER NOT NULL DEFAULT 0,
			local_path TEXT NOT NULL,
			local_exists INTEGER NOT NULL DEFAULT 1,
			source_type TEXT NOT NULL DEFAULT 'own',
			companions_json TEXT NOT NULL DEFAULT '[]',
			last_scan_id TEXT NOT NULL DEFAULT '',
			missing_since_scan_id TEXT NOT NULL DEFAULT '',
			replaced_by_id INTEGER NOT NULL DEFAULT 0,
			generated_at TEXT NOT NULL,
			last_access_at TEXT,
			access_count INTEGER NOT NULL DEFAULT 0,
			replaced_at TEXT,
			deleted_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_strm_files_source_path ON strm_files(source_path);`,
		`CREATE INDEX IF NOT EXISTS idx_strm_files_cid ON strm_files(cid);`,
		`CREATE TABLE IF NOT EXISTS media_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			relative_path TEXT NOT NULL,
			pickcode TEXT NOT NULL UNIQUE,
			previous_pickcode TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			cid TEXT NOT NULL DEFAULT '',
			cloud_file_id TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			source_fingerprint TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL DEFAULT '',
			source_mtime INTEGER NOT NULL DEFAULT 0,
			local_path TEXT NOT NULL,
			local_exists INTEGER NOT NULL DEFAULT 1,
			media_type TEXT NOT NULL DEFAULT '',
			last_scan_id TEXT NOT NULL DEFAULT '',
			missing_since_scan_id TEXT NOT NULL DEFAULT '',
			replaced_by_id INTEGER NOT NULL DEFAULT 0,
			generated_at TEXT NOT NULL,
			replaced_at TEXT,
			deleted_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_source_path ON media_files(source_path);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_cid ON media_files(cid);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_last_scan ON media_files(last_scan_id);`,
		`CREATE TABLE IF NOT EXISTS scan_cache_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_path TEXT NOT NULL UNIQUE,
			pickcode TEXT NOT NULL,
			file_name TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			cid TEXT NOT NULL DEFAULT '',
			cloud_file_id TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			source_fingerprint TEXT NOT NULL DEFAULT '',
			source_mtime INTEGER NOT NULL DEFAULT 0,
			source_type TEXT NOT NULL DEFAULT '',
			last_scan_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			deleted_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_scan_cache_files_pickcode ON scan_cache_files(pickcode);`,
		`CREATE INDEX IF NOT EXISTS idx_scan_cache_files_source_path ON scan_cache_files(source_path);`,
		`CREATE INDEX IF NOT EXISTS idx_scan_cache_files_last_scan ON scan_cache_files(last_scan_id);`,
		`CREATE TABLE IF NOT EXISTS strm_failures (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			failure_key TEXT NOT NULL UNIQUE,
			operation TEXT NOT NULL,
			source_path TEXT NOT NULL DEFAULT '',
			pickcode TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			cid TEXT NOT NULL DEFAULT '',
			source_mtime INTEGER NOT NULL DEFAULT 0,
			source_type TEXT NOT NULL DEFAULT '',
			request_json TEXT NOT NULL DEFAULT '{}',
			file_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'failed',
			error TEXT NOT NULL DEFAULT '',
			retry_count INTEGER NOT NULL DEFAULT 0,
			last_retry_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_strm_failures_status ON strm_failures(status);`,
		`CREATE INDEX IF NOT EXISTS idx_strm_failures_updated ON strm_failures(updated_at);`,
		`CREATE TABLE IF NOT EXISTS strm_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			phase TEXT NOT NULL DEFAULT '',
			total INTEGER NOT NULL DEFAULT 0,
			done INTEGER NOT NULL DEFAULT 0,
			failed INTEGER NOT NULL DEFAULT 0,
			scanned INTEGER NOT NULL DEFAULT 0,
			plan_total INTEGER NOT NULL DEFAULT 0,
			download_total INTEGER NOT NULL DEFAULT 0,
			download_done INTEGER NOT NULL DEFAULT 0,
			current_path TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			stats_json TEXT NOT NULL DEFAULT '{}',
			params_json TEXT NOT NULL DEFAULT '{}',
			started_at TEXT,
			finished_at TEXT,
			error TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS redirect_cache (
			pickcode TEXT PRIMARY KEY,
			target_url TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS pan115_request_stats (
			day TEXT NOT NULL,
			job_id INTEGER NOT NULL DEFAULT 0,
			action TEXT NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(day, job_id, action)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_pan115_request_stats_day ON pan115_request_stats(day);`,
		`CREATE INDEX IF NOT EXISTS idx_pan115_request_stats_job ON pan115_request_stats(job_id);`,
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			cron_expr TEXT NOT NULL,
			params_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_run_at TEXT,
			last_status TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			next_run_at TEXT,
			timeout_seconds INTEGER NOT NULL DEFAULT 3600,
			retry_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS task_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			strm_job_id INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			status TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			message TEXT NOT NULL DEFAULT '',
			affected_count INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS recycle_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strm_file_id INTEGER NOT NULL DEFAULT 0,
			deleted_file_name TEXT NOT NULL,
			deleted_relative_path TEXT NOT NULL,
			original_path TEXT NOT NULL DEFAULT '',
			recycle_path TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			source_label TEXT NOT NULL DEFAULT '',
			deleted_at TEXT NOT NULL,
			expires_at TEXT,
			purged_at TEXT
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.DB.Exec(stmt); err != nil {
			return err
		}
	}
	strmColumns := map[string]string{
		"previous_pickcode":     "TEXT NOT NULL DEFAULT ''",
		"cloud_file_id":         "TEXT NOT NULL DEFAULT ''",
		"sha1":                  "TEXT NOT NULL DEFAULT ''",
		"source_fingerprint":    "TEXT NOT NULL DEFAULT ''",
		"last_scan_id":          "TEXT NOT NULL DEFAULT ''",
		"missing_since_scan_id": "TEXT NOT NULL DEFAULT ''",
		"replaced_by_id":        "INTEGER NOT NULL DEFAULT 0",
		"replaced_at":           "TEXT",
	}
	for column, decl := range strmColumns {
		if err := s.ensureColumn("strm_files", column, decl); err != nil {
			return err
		}
	}
	mediaColumns := map[string]string{
		"previous_pickcode":     "TEXT NOT NULL DEFAULT ''",
		"cloud_file_id":         "TEXT NOT NULL DEFAULT ''",
		"sha1":                  "TEXT NOT NULL DEFAULT ''",
		"source_fingerprint":    "TEXT NOT NULL DEFAULT ''",
		"missing_since_scan_id": "TEXT NOT NULL DEFAULT ''",
		"replaced_by_id":        "INTEGER NOT NULL DEFAULT 0",
		"replaced_at":           "TEXT",
	}
	for column, decl := range mediaColumns {
		if err := s.ensureColumn("media_files", column, decl); err != nil {
			return err
		}
	}
	scanCacheColumns := map[string]string{
		"cloud_file_id":      "TEXT NOT NULL DEFAULT ''",
		"sha1":               "TEXT NOT NULL DEFAULT ''",
		"source_fingerprint": "TEXT NOT NULL DEFAULT ''",
	}
	for column, decl := range scanCacheColumns {
		if err := s.ensureColumn("scan_cache_files", column, decl); err != nil {
			return err
		}
	}
	jobColumns := map[string]string{
		"phase":          "TEXT NOT NULL DEFAULT ''",
		"scanned":        "INTEGER NOT NULL DEFAULT 0",
		"plan_total":     "INTEGER NOT NULL DEFAULT 0",
		"download_total": "INTEGER NOT NULL DEFAULT 0",
		"download_done":  "INTEGER NOT NULL DEFAULT 0",
		"current_path":   "TEXT NOT NULL DEFAULT ''",
		"message":        "TEXT NOT NULL DEFAULT ''",
		"stats_json":     "TEXT NOT NULL DEFAULT '{}'",
	}
	for column, decl := range jobColumns {
		if err := s.ensureColumn("strm_jobs", column, decl); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("task_runs", "strm_job_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	recycleColumns := map[string]string{
		"restored_at":   "TEXT",
		"original_path": "TEXT NOT NULL DEFAULT ''",
		"recycle_path":  "TEXT NOT NULL DEFAULT ''",
		"size":          "INTEGER NOT NULL DEFAULT 0",
		"reason":        "TEXT NOT NULL DEFAULT ''",
		"source_label":  "TEXT NOT NULL DEFAULT ''",
		"expires_at":    "TEXT",
	}
	for column, decl := range recycleColumns {
		if err := s.ensureColumn("recycle_items", column, decl); err != nil {
			return err
		}
	}
	if _, err := s.DB.Exec(`CREATE INDEX IF NOT EXISTS idx_strm_files_last_scan ON strm_files(last_scan_id);`); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`CREATE INDEX IF NOT EXISTS idx_strm_files_identity ON strm_files(source_type, source_path, deleted_at);`); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`CREATE INDEX IF NOT EXISTS idx_strm_files_local_path ON strm_files(local_path, deleted_at);`); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`CREATE INDEX IF NOT EXISTS idx_media_files_identity ON media_files(media_type, source_path, deleted_at);`); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`CREATE INDEX IF NOT EXISTS idx_media_files_local_path ON media_files(local_path, deleted_at);`); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureColumn(table, column, decl string) error {
	rows, err := s.DB.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.DB.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func timePtrText(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimePtr(raw sql.NullString) *time.Time {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil
	}
	return &t
}
