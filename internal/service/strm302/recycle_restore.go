package strm302

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"115strm/internal/model"
	"115strm/internal/service/auditlog"
)

// ValidateRestoreIDs 校验并去重，保留用户选择顺序。
func ValidateRestoreIDs(ids []int64) ([]int64, error) {
	seen := make(map[int64]bool)
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("恢复 ID 必须是正整数")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
		if len(unique) > 1000 {
			return nil, errors.New("每批最多恢复 1000 项")
		}
	}
	if len(unique) == 0 {
		return nil, errors.New("请选择需要恢复的文件")
	}
	return unique, nil
}

// RestoreRecycleItems 逐项处理；已提交的成功项不因后续失败回滚。
func (s *Service) RestoreRecycleItems(ctx context.Context, ids []int64) model.RecycleRestoreResponse {
	response := model.RecycleRestoreResponse{Requested: len(ids), Results: make([]model.RecycleRestoreResult, 0, len(ids))}
	for _, id := range ids {
		s.recycleMu.Lock()
		result := s.restoreRecycleItem(ctx, id)
		s.recycleMu.Unlock()
		response.Results = append(response.Results, result)
		switch result.Status {
		case "restored":
			response.Restored++
		case "already_restored":
			response.AlreadyRestored++
		case "conflict":
			response.Conflicts++
		default:
			response.Failed++
		}
		if s.audit != nil {
			s.audit.FileOp(auditlog.TypeRecycleRestore, fmt.Sprintf("id=%d %s %s：%s", id, result.RelativePath, result.Status, result.Message))
		}
	}
	return response
}

// restoreRecycleItem 调用方持锁，所有路径均从数据库取得。
func (s *Service) restoreRecycleItem(ctx context.Context, id int64) model.RecycleRestoreResult {
	result := model.RecycleRestoreResult{ID: id, Status: "failed"}
	fail := func(status, message string) model.RecycleRestoreResult {
		result.Status = status
		result.Message = message
		return result
	}
	if err := ctx.Err(); err != nil {
		return fail("failed", "请求已中断，请刷新核对已恢复记录")
	}
	item, err := s.db.RecycleItemByID(id)
	if err != nil {
		return fail("failed", err.Error())
	}
	if item == nil {
		return fail("not_found", "回收记录不存在")
	}
	result.RelativePath = item.DeletedRelativePath
	root := s.cfg.STRM302.BatchDir
	target := item.OriginalPath
	if target == "" {
		rel := filepath.FromSlash(item.DeletedRelativePath)
		if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
			return fail("invalid_path", "没有合法原路径")
		}
		target = filepath.Join(root, rel)
	}
	if err := validateRestorePath(root, target); err != nil {
		return fail("invalid_path", err.Error())
	}
	// 已恢复且目标仍在时，不依赖暂存备份是否已经到期清理。
	if item.RestoredAt != nil {
		if info, err := os.Lstat(target); err == nil && info.Mode().IsRegular() {
			return fail("already_restored", "记录已恢复，原路径文件仍存在")
		}
	}
	if item.PurgedAt != nil {
		return fail("purged", "暂存文件已永久清理")
	}
	if item.RecyclePath == "" {
		return fail("source_missing", "该记录没有暂存实体")
	}
	if err := validateRestorePath(s.recycleRoot(), item.RecyclePath); err != nil {
		return fail("invalid_path", err.Error())
	}
	sourceInfo, err := os.Lstat(item.RecyclePath)
	if errors.Is(err, os.ErrNotExist) {
		return fail("source_missing", "暂存文件不存在")
	}
	if err != nil {
		return fail("failed", err.Error())
	}
	if !sourceInfo.Mode().IsRegular() {
		return fail("invalid_path", "仅支持恢复普通文件")
	}
	already := false
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return fail("conflict", "原路径已有非普通文件，未覆盖")
		}
		same, err := equalRestoreFiles(ctx, item.RecyclePath, target)
		if err != nil {
			return fail("failed", err.Error())
		}
		if !same {
			return fail("conflict", "原路径已有不同内容文件，未覆盖")
		}
		already = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail("failed", err.Error())
	}
	if !already {
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fail("failed", err.Error())
		}
		if err := validateRestorePath(root, target); err != nil {
			return fail("invalid_path", err.Error())
		}
		if err := copyRestoreFile(ctx, item.RecyclePath, target, sourceInfo.Mode()); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fail("conflict", "恢复期间原路径出现文件，未覆盖")
			}
			return fail("failed", err.Error())
		}
	}
	if err := s.db.MarkRecycleItemRestored(*item, target); err != nil {
		return fail("failed", "文件已保留，数据库状态更新失败，可重试："+err.Error())
	}
	if s.logs != nil {
		s.logs.Info("回收站恢复 %s -> %s", item.RecyclePath, target)
	}
	if s.audit != nil {
		s.audit.FileOp(auditlog.TypeRecycleRestore, fmt.Sprintf("id=%d %s -> %s", id, item.RecyclePath, target))
	}
	if already {
		return fail("already_restored", "原路径内容一致，已协调恢复状态")
	}
	return fail("restored", "恢复成功，暂存副本按原期限保留")
}

// validateRestorePath 同时检查目录边界及现存路径组件，拒绝符号链接逃逸。
func validateRestorePath(root, path string) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(path) == "" {
		return errors.New("恢复根目录或路径为空")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return errors.New("路径不在允许的根目录内")
	}
	for current := pathAbs; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("恢复路径不能包含符号链接")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

type restoreContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r restoreContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func restoreHash(ctx context.Context, path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, restoreContextReader{ctx, file}); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

func equalRestoreFiles(ctx context.Context, from, to string) (bool, error) {
	a, err := os.Stat(from)
	if err != nil {
		return false, err
	}
	b, err := os.Stat(to)
	if err != nil {
		return false, err
	}
	if a.Size() != b.Size() {
		return false, nil
	}
	ah, err := restoreHash(ctx, from)
	if err != nil {
		return false, err
	}
	bh, err := restoreHash(ctx, to)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ah, bh), nil
}

// copyRestoreFile 在目标文件系统内暂存并校验，以硬链接原子发布，绝不覆盖目标。
// 不支持硬链接的文件系统返回错误并保留源，不回退到会覆盖目标的 rename。
func copyRestoreFile(ctx context.Context, from, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(to), ".recycle-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := io.Copy(temp, restoreContextReader{ctx, source}); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	same, err := equalRestoreFiles(ctx, from, temp.Name())
	if err != nil {
		return err
	}
	if !same {
		return errors.New("暂存源发生变化或复制校验失败，未发布目标")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Link(temp.Name(), to)
}
