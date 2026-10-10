package store

import (
	"database/sql"
	"time"

	"115strm/internal/model"
)

func (s *Store) AddRecycleItem(item *model.RecycleItem) error {
	if item == nil {
		return nil
	}
	deletedAt := item.DeletedAt
	if deletedAt.IsZero() {
		deletedAt = time.Now()
	}
	var expiresAt any
	if item.ExpiresAt != nil {
		expiresAt = item.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.DB.Exec(`
		INSERT INTO recycle_items(
			strm_file_id, deleted_file_name, deleted_relative_path,
			original_path, recycle_path, size, reason, source_label,
			deleted_at, expires_at, purged_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
	`, item.STRMFileID, item.DeletedFileName, item.DeletedRelativePath, item.OriginalPath, item.RecyclePath, item.Size, item.Reason, item.SourceLabel, deletedAt.UTC().Format(time.RFC3339Nano), expiresAt)
	return err
}

func (s *Store) ListRecycleItems(limit, offset int) ([]model.RecycleItem, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM recycle_items WHERE purged_at IS NULL AND restored_at IS NULL AND recycle_path<>''`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(`
		SELECT id, strm_file_id, deleted_file_name, deleted_relative_path, original_path, recycle_path,
			size, reason, source_label, deleted_at, expires_at, purged_at, restored_at
		FROM recycle_items
		WHERE purged_at IS NULL AND restored_at IS NULL AND recycle_path<>''
		ORDER BY deleted_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanRecycleItems(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) ExpiredRecycleItems(now time.Time, limit int) ([]model.RecycleItem, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.DB.Query(`
		SELECT id, strm_file_id, deleted_file_name, deleted_relative_path, original_path, recycle_path,
			size, reason, source_label, deleted_at, expires_at, purged_at, restored_at
		FROM recycle_items
		WHERE purged_at IS NULL AND recycle_path<>'' AND expires_at IS NOT NULL AND expires_at<=?
		ORDER BY expires_at ASC, id ASC
		LIMIT ?
	`, now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecycleItems(rows)
}

func (s *Store) RecycleItemsDeletedBefore(cutoff time.Time, limit int) ([]model.RecycleItem, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.DB.Query(`
		SELECT id, strm_file_id, deleted_file_name, deleted_relative_path, original_path, recycle_path,
			size, reason, source_label, deleted_at, expires_at, purged_at, restored_at
		FROM recycle_items
		WHERE purged_at IS NULL AND recycle_path<>'' AND deleted_at<?
		ORDER BY deleted_at ASC, id ASC
		LIMIT ?
	`, cutoff.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecycleItems(rows)
}

func (s *Store) MarkRecycleItemPurged(id int64) error {
	_, err := s.DB.Exec(`UPDATE recycle_items SET purged_at=? WHERE id=?`, nowText(), id)
	return err
}

func scanRecycleItems(rows *sql.Rows) ([]model.RecycleItem, error) {
	out := make([]model.RecycleItem, 0)
	for rows.Next() {
		var item model.RecycleItem
		var deletedAt string
		var expiresAt, purgedAt, restoredAt sql.NullString
		if err := rows.Scan(&item.ID, &item.STRMFileID, &item.DeletedFileName, &item.DeletedRelativePath, &item.OriginalPath, &item.RecyclePath, &item.Size, &item.Reason, &item.SourceLabel, &deletedAt, &expiresAt, &purgedAt, &restoredAt); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, deletedAt); err == nil {
			item.DeletedAt = t
		}
		item.ExpiresAt = parseTimePtr(expiresAt)
		item.PurgedAt = parseTimePtr(purgedAt)
		item.RestoredAt = parseTimePtr(restoredAt)
		out = append(out, item)
	}
	return out, rows.Err()
}
