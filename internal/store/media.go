package store

import (
	"database/sql"
	"strings"
	"time"

	"115strm/internal/model"
)

const mediaSelectColumns = `id, relative_path, pickcode, previous_pickcode, file_name, size, cid, cloud_file_id, sha1, source_fingerprint, source_path, source_mtime, local_path, local_exists, media_type, last_scan_id, missing_since_scan_id, replaced_by_id, generated_at, replaced_at, deleted_at`

func (s *Store) UpsertMediaFile(f *model.MediaFile) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sourcePath := cleanSourcePrefix(f.SourcePath)
	mediaType := strings.TrimSpace(f.MediaType)
	now := nowText()
	existingID, existingPickcode, err := findMediaMergeTarget(tx, f.PickCode, mediaType, sourcePath, f.LocalPath)
	if err != nil {
		return err
	}
	if existingID > 0 {
		if err := markDuplicateMediaRowsReplaced(tx, existingID, f.PickCode, mediaType, sourcePath, f.LocalPath, now); err != nil {
			return err
		}
		previousPickcode := f.PreviousPickCode
		replacedAt := any(nil)
		if existingPickcode != "" && existingPickcode != f.PickCode {
			previousPickcode = existingPickcode
			replacedAt = now
		}
		_, err = tx.Exec(`
			UPDATE media_files SET
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
				media_type=?,
				last_scan_id=?,
				missing_since_scan_id='',
				replaced_by_id=0,
				generated_at=?,
				replaced_at=COALESCE(?, replaced_at),
				deleted_at=NULL
			WHERE id=?
		`, f.RelativePath, f.PickCode, previousPickcode, f.FileName, f.Size, f.CID, f.CloudFileID, f.Sha1, f.SourceFingerprint, sourcePath, f.SourceMTime, f.LocalPath, boolInt(f.LocalExists), mediaType, f.LastScanID, now, replacedAt, existingID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	_, err = tx.Exec(`
		INSERT INTO media_files(relative_path, pickcode, previous_pickcode, file_name, size, cid, cloud_file_id, sha1, source_fingerprint, source_path, source_mtime, local_path, local_exists, media_type, last_scan_id, missing_since_scan_id, replaced_by_id, generated_at, deleted_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 0, ?, NULL)
	`, f.RelativePath, f.PickCode, f.PreviousPickCode, f.FileName, f.Size, f.CID, f.CloudFileID, f.Sha1, f.SourceFingerprint, sourcePath, f.SourceMTime, f.LocalPath, boolInt(f.LocalExists), mediaType, f.LastScanID, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MediaFileByPickcode(pickcode string) (*model.MediaFile, error) {
	row := s.DB.QueryRow(`SELECT `+mediaSelectColumns+` FROM media_files WHERE pickcode=?`, pickcode)
	return scanMedia(row)
}

func (s *Store) MediaFileBySourcePath(sourcePath string) (*model.MediaFile, error) {
	sourcePath = cleanSourcePrefix(sourcePath)
	if sourcePath == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT `+mediaSelectColumns+` FROM media_files WHERE deleted_at IS NULL AND (source_path=? OR source_path=?) ORDER BY generated_at DESC LIMIT 1`, sourcePath, "/"+sourcePath)
	return scanMedia(row)
}

func (s *Store) MediaFileByLocalPath(localPath string) (*model.MediaFile, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT `+mediaSelectColumns+` FROM media_files WHERE deleted_at IS NULL AND local_path=? ORDER BY generated_at DESC LIMIT 1`, localPath)
	return scanMedia(row)
}

func (s *Store) MediaFilesMissingInScan(scanID, sourcePrefix string) ([]model.MediaFile, error) {
	query := `SELECT ` + mediaSelectColumns + ` FROM media_files WHERE deleted_at IS NULL AND last_scan_id<>?`
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
	var out []model.MediaFile
	for rows.Next() {
		f, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) MarkMediaDeleted(id int64) error {
	_, err := s.DB.Exec(`UPDATE media_files SET deleted_at=?, local_exists=0 WHERE id=?`, nowText(), id)
	return err
}

func (s *Store) MarkMediaMissing(id int64, scanID string) error {
	_, err := s.DB.Exec(`UPDATE media_files SET missing_since_scan_id=CASE WHEN missing_since_scan_id='' THEN ? ELSE missing_since_scan_id END WHERE id=? AND deleted_at IS NULL`, scanID, id)
	return err
}

func (s *Store) MediaLocalPathReferencedByOther(mediaID int64, localPath string) (bool, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return false, nil
	}
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM media_files WHERE id<>? AND deleted_at IS NULL AND local_path=?`, mediaID, localPath).Scan(&count)
	return count > 0, err
}

func scanMedia(row scanner) (*model.MediaFile, error) {
	var f model.MediaFile
	var generated string
	var replaced, deleted sql.NullString
	var localExists int
	if err := row.Scan(&f.ID, &f.RelativePath, &f.PickCode, &f.PreviousPickCode, &f.FileName, &f.Size, &f.CID, &f.CloudFileID, &f.Sha1, &f.SourceFingerprint, &f.SourcePath, &f.SourceMTime, &f.LocalPath, &localExists, &f.MediaType, &f.LastScanID, &f.MissingSinceScanID, &f.ReplacedByID, &generated, &replaced, &deleted); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339Nano, generated); err == nil {
		f.GeneratedAt = t
	}
	f.LocalExists = localExists == 1
	f.ReplacedAt = parseTimePtr(replaced)
	f.DeletedAt = parseTimePtr(deleted)
	return &f, nil
}

func findMediaMergeTarget(tx *sql.Tx, pickcode, mediaType, sourcePath, localPath string) (int64, string, error) {
	row := tx.QueryRow(`
		SELECT id, pickcode FROM media_files
		WHERE pickcode=? OR (deleted_at IS NULL AND media_type=? AND source_path=?) OR (deleted_at IS NULL AND source_path=?) OR (deleted_at IS NULL AND local_path=?)
		ORDER BY
			CASE
				WHEN pickcode=? THEN 0
				WHEN deleted_at IS NULL AND media_type=? AND source_path=? THEN 1
				WHEN deleted_at IS NULL AND source_path=? THEN 2
				WHEN deleted_at IS NULL AND local_path=? THEN 3
				ELSE 4
			END,
			deleted_at IS NOT NULL,
			generated_at DESC
		LIMIT 1
	`, pickcode, mediaType, sourcePath, sourcePath, localPath, pickcode, mediaType, sourcePath, sourcePath, localPath)
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

func markDuplicateMediaRowsReplaced(tx *sql.Tx, keepID int64, pickcode, mediaType, sourcePath, localPath, now string) error {
	_, err := tx.Exec(`
		UPDATE media_files SET deleted_at=?, local_exists=0, replaced_by_id=?
		WHERE id<>? AND deleted_at IS NULL AND (pickcode=? OR (media_type=? AND source_path=?) OR source_path=? OR local_path=?)
	`, now, keepID, keepID, pickcode, mediaType, sourcePath, sourcePath, localPath)
	return err
}
