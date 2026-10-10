package store

import (
	"database/sql"
	"strings"
	"time"

	"115strm/internal/model"
)

func (s *Store) UpsertScanCacheFile(f model.CloudFile, scanID string) error {
	if strings.TrimSpace(f.PickCode) == "" || strings.TrimSpace(f.SourcePath) == "" {
		return nil
	}
	sourcePath := cleanSourcePrefix(f.SourcePath)
	if sourcePath == "" {
		return nil
	}
	_, err := s.DB.Exec(`
		INSERT INTO scan_cache_files(source_path, pickcode, file_name, size, cid, cloud_file_id, sha1, source_fingerprint, source_mtime, source_type, last_scan_id, updated_at, deleted_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(source_path) DO UPDATE SET
			pickcode=excluded.pickcode,
			file_name=excluded.file_name,
			size=excluded.size,
			cid=excluded.cid,
			cloud_file_id=excluded.cloud_file_id,
			sha1=excluded.sha1,
			source_fingerprint=excluded.source_fingerprint,
			source_mtime=excluded.source_mtime,
			source_type=excluded.source_type,
			last_scan_id=excluded.last_scan_id,
			updated_at=excluded.updated_at,
			deleted_at=NULL
	`, sourcePath, f.PickCode, f.Name, f.Size, f.CID, f.ID, f.Sha1, scanCacheFingerprint(f), f.ModTime, f.SourceType, scanID, nowText())
	return err
}

func (s *Store) ScanCacheBySourcePath(sourcePath string) (*model.ScanCacheFile, error) {
	sourcePath = cleanSourcePrefix(sourcePath)
	if sourcePath == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT id, source_path, pickcode, file_name, size, cid, cloud_file_id, sha1, source_fingerprint, source_mtime, source_type, last_scan_id, updated_at, deleted_at FROM scan_cache_files WHERE source_path=? AND deleted_at IS NULL`, sourcePath)
	return scanCache(row)
}

func (s *Store) MarkScanCacheMissing(scanID, sourcePrefix string) error {
	query := `UPDATE scan_cache_files SET deleted_at=?, last_scan_id=? WHERE deleted_at IS NULL AND last_scan_id<>?`
	args := []any{nowText(), scanID, scanID}
	sourcePrefix = cleanSourcePrefix(sourcePrefix)
	if sourcePrefix != "" {
		query += ` AND (source_path=? OR source_path LIKE ? OR source_path=? OR source_path LIKE ?)`
		args = append(args, sourcePrefix, sourcePrefix+"/%", "/"+sourcePrefix, "/"+sourcePrefix+"/%")
	}
	_, err := s.DB.Exec(query, args...)
	return err
}

func scanCache(row scanner) (*model.ScanCacheFile, error) {
	var f model.ScanCacheFile
	var updated string
	var deleted sql.NullString
	if err := row.Scan(&f.ID, &f.SourcePath, &f.PickCode, &f.FileName, &f.Size, &f.CID, &f.CloudFileID, &f.Sha1, &f.SourceFingerprint, &f.SourceMTime, &f.SourceType, &f.LastScanID, &updated, &deleted); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		f.UpdatedAt = t
	}
	f.DeletedAt = parseTimePtr(deleted)
	return &f, nil
}

func scanCacheFingerprint(f model.CloudFile) string {
	parts := []string{
		cleanSourcePrefix(f.SourcePath),
		f.Name,
		f.Sha1,
		f.ID,
		strings.TrimSpace(f.PickCode),
	}
	return strings.Join(parts, "|")
}
