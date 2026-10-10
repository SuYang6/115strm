package store

import (
	"errors"

	"115strm/internal/model"
)

// RecycleItemByID 包括已恢复、已清理记录，供幂等检查使用。
func (s *Store) RecycleItemByID(id int64) (*model.RecycleItem, error) {
	rows, err := s.DB.Query(`SELECT id, strm_file_id, deleted_file_name, deleted_relative_path,
		original_path, recycle_path, size, reason, source_label, deleted_at, expires_at, purged_at, restored_at
		FROM recycle_items WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanRecycleItems(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

// MarkRecycleItemRestored 在同一事务中协调业务记录，不复活被替代的记录。
func (s *Store) MarkRecycleItemRestored(item model.RecycleItem, target string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if item.STRMFileID > 0 {
		_, err = tx.Exec(`UPDATE strm_files SET deleted_at=NULL, local_exists=1, missing_since_scan_id=''
			WHERE id=? AND local_path=? AND deleted_at IS NOT NULL AND replaced_by_id=0
			AND generated_at<=? AND NOT EXISTS (
				SELECT 1 FROM strm_files AS other WHERE other.id<>? AND other.local_path=? AND other.deleted_at IS NULL
			)`, item.STRMFileID, target, item.DeletedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), item.STRMFileID, target)
	} else {
		// 仅恢复唯一且未被替代的旧媒体记录；JSON 通常没有媒体业务记录。
		_, err = tx.Exec(`UPDATE media_files SET deleted_at=NULL, local_exists=1, missing_since_scan_id=''
			WHERE local_path=? AND deleted_at IS NOT NULL AND replaced_by_id=0 AND generated_at<=?
			AND (SELECT COUNT(*) FROM media_files WHERE local_path=? AND deleted_at IS NOT NULL AND replaced_by_id=0)=1
			AND NOT EXISTS (SELECT 1 FROM media_files AS other WHERE other.local_path=? AND other.deleted_at IS NULL)`,
			target, item.DeletedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), target, target)
	}
	if err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE recycle_items SET restored_at=? WHERE id=? AND purged_at IS NULL`, nowText(), item.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("回收记录已清理或不存在")
	}
	return tx.Commit()
}
