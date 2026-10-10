package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"115strm/internal/model"
)

const strmSelectColumns = `id, relative_path, pickcode, previous_pickcode, file_name, size, cid, cloud_file_id, sha1, source_fingerprint, source_path, source_mtime, local_path, local_exists, source_type, companions_json, last_scan_id, missing_since_scan_id, replaced_by_id, generated_at, last_access_at, access_count, replaced_at, deleted_at`

func (s *Store) UpsertSTRMFile(f *model.STRMFile) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sourcePath := cleanSourcePrefix(f.SourcePath)
	sourceType := normalizeSourceType(f.SourceType)
	now := nowText()
	existingID, existingPickcode, err := findSTRMMergeTarget(tx, f.PickCode, sourceType, sourcePath, f.LocalPath)
	if err != nil {
		return err
	}
	if existingID > 0 {
		if err := markDuplicateSTRMRowsReplaced(tx, existingID, f.PickCode, sourceType, sourcePath, f.LocalPath, now); err != nil {
			return err
		}
		previousPickcode := f.PreviousPickCode
		replacedAt := any(nil)
		if existingPickcode != "" && existingPickcode != f.PickCode {
			previousPickcode = existingPickcode
			replacedAt = now
		}
		_, err = tx.Exec(`
			UPDATE strm_files SET
				relative_path=?,
				pickcode=?,
				previous_pickcode=?,
				file_name=?,
				size=?,
				cid=?,
				cloud_file_id=?,
				sha1=?,
				source_fingerprint=?,
				source_path=?,
				source_mtime=?,
				local_path=?,
				local_exists=?,
				source_type=?,
				companions_json=?,
				last_scan_id=?,
				missing_since_scan_id='',
				replaced_by_id=0,
				generated_at=?,
				replaced_at=COALESCE(?, replaced_at),
				deleted_at=NULL
			WHERE id=?
		`, f.RelativePath, f.PickCode, previousPickcode, f.FileName, f.Size, f.CID, f.CloudFileID, f.Sha1, f.SourceFingerprint, sourcePath, f.SourceMTime, f.LocalPath, boolInt(f.LocalExists), sourceType, f.CompanionsJSON, f.LastScanID, now, replacedAt, existingID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	_, err = tx.Exec(`
		INSERT INTO strm_files(relative_path, pickcode, previous_pickcode, file_name, size, cid, cloud_file_id, sha1, source_fingerprint, source_path, source_mtime, local_path, local_exists, source_type, companions_json, last_scan_id, missing_since_scan_id, replaced_by_id, generated_at, deleted_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 0, ?, NULL)
	`, f.RelativePath, f.PickCode, f.PreviousPickCode, f.FileName, f.Size, f.CID, f.CloudFileID, f.Sha1, f.SourceFingerprint, sourcePath, f.SourceMTime, f.LocalPath, boolInt(f.LocalExists), sourceType, f.CompanionsJSON, f.LastScanID, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) STRMFileByID(id int64) (*model.STRMFile, error) {
	row := s.DB.QueryRow(`SELECT `+strmSelectColumns+` FROM strm_files WHERE id=?`, id)
	return scanSTRM(row)
}

func (s *Store) STRMFileByPickcode(pickcode string) (*model.STRMFile, error) {
	row := s.DB.QueryRow(`SELECT `+strmSelectColumns+` FROM strm_files WHERE pickcode=?`, pickcode)
	return scanSTRM(row)
}

func (s *Store) STRMFileBySourcePath(sourcePath string) (*model.STRMFile, error) {
	sourcePath = cleanSourcePrefix(sourcePath)
	if sourcePath == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT `+strmSelectColumns+` FROM strm_files WHERE deleted_at IS NULL AND (source_path=? OR source_path=?) ORDER BY generated_at DESC LIMIT 1`, sourcePath, "/"+sourcePath)
	return scanSTRM(row)
}

func (s *Store) STRMFileByLocalPath(localPath string) (*model.STRMFile, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT `+strmSelectColumns+` FROM strm_files WHERE deleted_at IS NULL AND local_path=? ORDER BY generated_at DESC LIMIT 1`, localPath)
	return scanSTRM(row)
}

func (s *Store) ListSTRMFiles(limit, offset int) ([]model.STRMFile, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT `+strmSelectColumns+` FROM strm_files WHERE deleted_at IS NULL ORDER BY generated_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.STRMFile
	for rows.Next() {
		f, err := scanSTRM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) STRMFilesBySourcePrefix(prefix string) ([]model.STRMFile, error) {
	rows, err := s.DB.Query(`SELECT `+strmSelectColumns+` FROM strm_files WHERE deleted_at IS NULL AND source_path LIKE ?`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.STRMFile
	for rows.Next() {
		f, err := scanSTRM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) STRMFilesMissingInScan(scanID, sourcePrefix string) ([]model.STRMFile, error) {
	query := `SELECT ` + strmSelectColumns + ` FROM strm_files WHERE deleted_at IS NULL AND last_scan_id<>?`
	args := []any{scanID}
	sourcePrefix = cleanSourcePrefix(sourcePrefix)
	if sourcePrefix != "" {
		query += ` AND (source_path=? OR source_path LIKE ? OR source_path=? OR source_path LIKE ?)`
		args = append(args, sourcePrefix, sourcePrefix+"/%", "/"+sourcePrefix, "/"+sourcePrefix+"/%")
	}
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.STRMFile
	for rows.Next() {
		f, err := scanSTRM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) MarkSTRMDeleted(id int64) error {
	_, err := s.DB.Exec(`UPDATE strm_files SET deleted_at=?, local_exists=0 WHERE id=?`, nowText(), id)
	return err
}

func (s *Store) MarkSTRMMissing(id int64, scanID string) error {
	_, err := s.DB.Exec(`UPDATE strm_files SET missing_since_scan_id=CASE WHEN missing_since_scan_id='' THEN ? ELSE missing_since_scan_id END WHERE id=? AND deleted_at IS NULL`, scanID, id)
	return err
}

func (s *Store) STRMLocalPathReferencedByOther(strmID int64, localPath string) (bool, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return false, nil
	}
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM strm_files WHERE id<>? AND deleted_at IS NULL AND local_path=?`, strmID, localPath).Scan(&count)
	return count > 0, err
}

func (s *Store) RecordAccess(pickcode string) error {
	_, err := s.DB.Exec(`UPDATE strm_files SET last_access_at=?, access_count=access_count+1 WHERE pickcode=?`, nowText(), pickcode)
	return err
}

func (s *Store) AddRecycleRecord(strmID int64, name, relPath string) error {
	_, err := s.DB.Exec(`INSERT INTO recycle_items(strm_file_id, deleted_file_name, deleted_relative_path, deleted_at) VALUES(?, ?, ?, ?)`, strmID, name, relPath, nowText())
	return err
}

func (s *Store) CompanionLocalPathReferencedByOther(strmID int64, localPath string) (bool, error) {
	rows, err := s.DB.Query(`SELECT companions_json FROM strm_files WHERE id<>? AND deleted_at IS NULL`, strmID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		var companions []model.CompanionFile
		if err := json.Unmarshal([]byte(raw), &companions); err != nil {
			continue
		}
		for _, c := range companions {
			if c.LocalPath == localPath {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

func (s *Store) PurgeRecycleRecordsBefore(cutoff time.Time) (int64, error) {
	res, err := s.DB.Exec(`UPDATE recycle_items SET purged_at=? WHERE purged_at IS NULL AND deleted_at < ?`, nowText(), cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) DashboardStats() (map[string]any, error) {
	stats := map[string]any{}
	var strmTotal, monthTotal int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM strm_files WHERE deleted_at IS NULL`).Scan(&strmTotal)
	monthStart := time.Now().UTC().Format("2006-01") + "-01T00:00:00Z"
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM strm_files WHERE deleted_at IS NULL AND generated_at >= ?`, monthStart).Scan(&monthTotal)
	stats["strm_total"] = strmTotal
	stats["month_generated"] = monthTotal
	var latest sql.NullString
	_ = s.DB.QueryRow(`SELECT generated_at FROM strm_files WHERE deleted_at IS NULL ORDER BY generated_at DESC LIMIT 1`).Scan(&latest)
	if latest.Valid {
		stats["latest_generated"] = latest.String
	}
	var runningTasks int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM task_runs WHERE status='running'`).Scan(&runningTasks)
	stats["running_tasks"] = runningTasks
	if mediaStats, ok, err := s.GetMediaLibraryStats(); err == nil && ok {
		stats["media_library"] = mediaStats
	} else {
		stats["media_library"] = MediaLibraryStats{Available: false}
	}
	return stats, nil
}

func scanSTRM(row scanner) (*model.STRMFile, error) {
	var f model.STRMFile
	var generated string
	var lastAccess, replaced, deleted sql.NullString
	var localExists int
	if err := row.Scan(&f.ID, &f.RelativePath, &f.PickCode, &f.PreviousPickCode, &f.FileName, &f.Size, &f.CID, &f.CloudFileID, &f.Sha1, &f.SourceFingerprint, &f.SourcePath, &f.SourceMTime, &f.LocalPath, &localExists, &f.SourceType, &f.CompanionsJSON, &f.LastScanID, &f.MissingSinceScanID, &f.ReplacedByID, &generated, &lastAccess, &f.AccessCount, &replaced, &deleted); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339Nano, generated); err == nil {
		f.GeneratedAt = t
	}
	f.LocalExists = localExists == 1
	f.LastAccessAt = parseTimePtr(lastAccess)
	f.ReplacedAt = parseTimePtr(replaced)
	f.DeletedAt = parseTimePtr(deleted)
	return &f, nil
}

func findSTRMMergeTarget(tx *sql.Tx, pickcode, sourceType, sourcePath, localPath string) (int64, string, error) {
	row := tx.QueryRow(`
		SELECT id, pickcode FROM strm_files
		WHERE pickcode=? OR (deleted_at IS NULL AND source_type=? AND source_path=?) OR (deleted_at IS NULL AND source_path=?) OR (deleted_at IS NULL AND local_path=?)
		ORDER BY
			CASE
				WHEN pickcode=? THEN 0
				WHEN deleted_at IS NULL AND source_type=? AND source_path=? THEN 1
				WHEN deleted_at IS NULL AND source_path=? THEN 2
				WHEN deleted_at IS NULL AND local_path=? THEN 3
				ELSE 4
			END,
			deleted_at IS NOT NULL,
			generated_at DESC
		LIMIT 1
	`, pickcode, sourceType, sourcePath, sourcePath, localPath, pickcode, sourceType, sourcePath, sourcePath, localPath)
	var id int64
	var existingPickcode string
	if err := row.Scan(&id, &existingPickcode); err != nil {
		if err == sql.ErrNoRows {
			return 0, "", nil
		}
		return 0, "", err
	}
	return id, existingPickcode, nil
}

func markDuplicateSTRMRowsReplaced(tx *sql.Tx, keepID int64, pickcode, sourceType, sourcePath, localPath, now string) error {
	_, err := tx.Exec(`
		UPDATE strm_files SET deleted_at=?, local_exists=0, replaced_by_id=?
		WHERE id<>? AND deleted_at IS NULL AND (pickcode=? OR (source_type=? AND source_path=?) OR source_path=? OR local_path=?)
	`, now, keepID, keepID, pickcode, sourceType, sourcePath, sourcePath, localPath)
	return err
}

func cleanSourcePrefix(prefix string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimPrefix(prefix, "/"), "\\", "/"), "/")
}

func normalizeSourceType(sourceType string) string {
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	if sourceType == "" {
		return "115"
	}
	return sourceType
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
