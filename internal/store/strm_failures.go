package store

import (
	"database/sql"
	"time"

	"115strm/internal/model"
)

func (s *Store) UpsertSTRMFailure(f *model.STRMFailure) error {
	if f == nil || f.FailureKey == "" || f.Operation == "" {
		return nil
	}
	now := nowText()
	_, err := s.DB.Exec(`
		INSERT INTO strm_failures(failure_key, operation, source_path, pickcode, file_name, size, cid, source_mtime, source_type, request_json, file_json, status, error, retry_count, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'failed', ?, 0, ?, ?)
		ON CONFLICT(failure_key) DO UPDATE SET
			operation=excluded.operation,
			source_path=excluded.source_path,
			pickcode=excluded.pickcode,
			file_name=excluded.file_name,
			size=excluded.size,
			cid=excluded.cid,
			source_mtime=excluded.source_mtime,
			source_type=excluded.source_type,
			request_json=excluded.request_json,
			file_json=excluded.file_json,
			status='failed',
			error=excluded.error,
			updated_at=excluded.updated_at
	`, f.FailureKey, f.Operation, f.SourcePath, f.PickCode, f.FileName, f.Size, f.CID, f.SourceMTime, f.SourceType, f.RequestJSON, f.FileJSON, f.Error, now, now)
	return err
}

func (s *Store) ListSTRMFailures(limit int) ([]model.STRMFailure, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.DB.Query(`SELECT id, failure_key, operation, source_path, pickcode, file_name, size, cid, source_mtime, source_type, request_json, file_json, status, error, retry_count, last_retry_at, created_at, updated_at FROM strm_failures ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.STRMFailure
	for rows.Next() {
		f, err := scanSTRMFailure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) STRMFailureByID(id int64) (*model.STRMFailure, error) {
	row := s.DB.QueryRow(`SELECT id, failure_key, operation, source_path, pickcode, file_name, size, cid, source_mtime, source_type, request_json, file_json, status, error, retry_count, last_retry_at, created_at, updated_at FROM strm_failures WHERE id=?`, id)
	return scanSTRMFailure(row)
}

func (s *Store) MarkSTRMFailureRetry(id int64, status, errText string) error {
	_, err := s.DB.Exec(`UPDATE strm_failures SET status=?, error=?, retry_count=retry_count+1, last_retry_at=?, updated_at=? WHERE id=?`, status, errText, nowText(), nowText(), id)
	return err
}

func (s *Store) DeleteSTRMFailure(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM strm_failures WHERE id=?`, id)
	return err
}

func scanSTRMFailure(row scanner) (*model.STRMFailure, error) {
	var f model.STRMFailure
	var lastRetry sql.NullString
	var created, updated string
	if err := row.Scan(&f.ID, &f.FailureKey, &f.Operation, &f.SourcePath, &f.PickCode, &f.FileName, &f.Size, &f.CID, &f.SourceMTime, &f.SourceType, &f.RequestJSON, &f.FileJSON, &f.Status, &f.Error, &f.RetryCount, &lastRetry, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	f.LastRetryAt = parseTimePtr(lastRetry)
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		f.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		f.UpdatedAt = t
	}
	return &f, nil
}
