package strm302

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"115strm/internal/model"
	"115strm/internal/service/auditlog"
	"115strm/internal/util"
)

const autoRecycleKeepDays = 3

func (s *Service) recycleDeletedFile(path, relativePath, reason, label string, strmFileID int64) error {
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	return s.recycleDeletedFileLocked(path, relativePath, reason, label, strmFileID)
}

// recycleDeletedFileLocked 调用方持锁，避免和恢复或清理交错。
func (s *Service) recycleDeletedFileLocked(path, relativePath, reason, label string, strmFileID int64) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	stat, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return nil
	}
	rel := normalizeSourcePath(relativePath)
	if rel == "" {
		if derived, err := filepath.Rel(s.cfg.STRM302.BatchDir, path); err == nil {
			rel = normalizeSourcePath(derived)
		}
	}
	if rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	sourceLabel := sanitizeRecycleLabel(label)
	if sourceLabel == "" {
		sourceLabel = "手动扫描"
	}
	now := time.Now()
	targetRel := filepath.Join(now.Format("2006-01-02"), sourceLabel, filepath.FromSlash(rel))
	target, err := util.SafeJoin(s.recycleRoot(), targetRel)
	if err != nil {
		return err
	}
	target = uniqueRecyclePath(target)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if err := moveFileToRecycle(path, target, stat.Mode()); err != nil {
		return err
	}
	expiresAt := now.AddDate(0, 0, autoRecycleKeepDays)
	if s.db != nil {
		if err := s.db.AddRecycleItem(&model.RecycleItem{
			STRMFileID:          strmFileID,
			DeletedFileName:     filepath.Base(path),
			DeletedRelativePath: rel,
			OriginalPath:        path,
			RecyclePath:         target,
			Size:                stat.Size(),
			Reason:              reason,
			SourceLabel:         sourceLabel,
			DeletedAt:           now,
			ExpiresAt:           &expiresAt,
		}); err != nil {
			return err
		}
	}
	if s.audit != nil {
		s.audit.FileOp(auditlog.TypeRecycleMove, fmt.Sprintf("%s -> %s", path, target))
	}
	if s.logs != nil {
		s.logs.Info("已移入回收站 %s -> %s", path, target)
	}
	return nil
}

func (s *Service) recycleRoot() string {
	root := strings.TrimSpace(s.cfg.Storage.DataDir)
	if root == "" {
		root = "./data"
	}
	return filepath.Join(root, "recycle")
}

func sanitizeRecycleLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	label = replacer.Replace(label)
	label = strings.ReplaceAll(label, "..", "_")
	label = strings.Trim(label, " .")
	if label == "" {
		return ""
	}
	if len([]rune(label)) > 80 {
		runes := []rune(label)
		label = string(runes[:80])
	}
	return label
}

func uniqueRecyclePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	stamp := time.Now().Format("150405")
	for i := 0; i < 1000; i++ {
		candidate := path + ".deleted-" + stamp
		if i > 0 {
			candidate = fmt.Sprintf("%s.deleted-%s-%d", path, stamp, i)
		}
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return fmt.Sprintf("%s.deleted-%d", path, time.Now().UnixNano())
}

func moveFileToRecycle(from, to string, mode os.FileMode) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyFile(from, to, mode); err != nil {
		_ = os.Remove(to)
		return err
	}
	if err := os.Remove(from); err != nil {
		_ = os.Remove(to)
		return err
	}
	return nil
}

func copyFile(from, to string, mode os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = dst.Close()
		if !ok {
			_ = os.Remove(to)
		}
	}()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func (s *Service) CleanRecycleRecords(keepDays int) (int, error) {
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	if keepDays <= 0 {
		keepDays = autoRecycleKeepDays
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	deleted := 0
	for {
		items, err := s.db.RecycleItemsDeletedBefore(cutoff, 1000)
		if err != nil {
			return deleted, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if err := s.purgeRecycleItem(item); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	if _, err := s.cleanupEmptyDirs(s.recycleRoot(), 1); err != nil {
		return deleted, err
	}
	return deleted, nil
}

func (s *Service) purgeRecycleItem(item model.RecycleItem) error {
	if strings.TrimSpace(item.RecyclePath) != "" {
		if err := validateRestorePath(s.recycleRoot(), item.RecyclePath); err != nil {
			return err
		}
		if err := os.Remove(item.RecyclePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if s.audit != nil {
			s.audit.FileOp(auditlog.TypeRecyclePurge, item.RecyclePath)
		}
	}
	if s.db != nil {
		if err := s.db.MarkRecycleItemPurged(item.ID); err != nil {
			return err
		}
	}
	if s.logs != nil {
		s.logs.Info("已清理过期回收文件 %s", item.RecyclePath)
	}
	return nil
}

func ensurePathInRoot(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(os.PathSeparator)) {
		return errors.New("path escapes recycle root")
	}
	return nil
}

func (s *Service) cleanupEmptyMediaDirs() (int, error) {
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	root := strings.TrimSpace(s.cfg.STRM302.BatchDir)
	if root == "" {
		root = "/opt/Media"
	}
	deleted := 0
	tvDeleted, err := s.cleanupEmptyDirs(filepath.Join(root, "电视"), 2)
	if err != nil {
		return deleted, err
	}
	deleted += tvDeleted
	movieDeleted, err := s.cleanupEmptyDirs(filepath.Join(root, "电影"), 1)
	if err != nil {
		return deleted, err
	}
	deleted += movieDeleted
	return deleted, nil
}

func (s *Service) cleanupEmptyDirs(root string, minDepth int) (int, error) {
	if minDepth < 1 {
		minDepth = 1
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	deleted := 0
	for _, dir := range dirs {
		depth, err := pathDepth(root, dir)
		if err != nil {
			return deleted, err
		}
		if depth < minDepth {
			continue
		}
		empty, err := isEmptyDir(dir)
		if err != nil {
			return deleted, err
		}
		if !empty {
			continue
		}
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return deleted, err
		}
		deleted++
		if s.audit != nil {
			s.audit.FileOp(auditlog.TypeDirectoryDelete, dir)
		}
		if s.logs != nil {
			s.logs.Info("已清理空目录 %s", dir)
		}
	}
	return deleted, nil
}

func pathDepth(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, err
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return 0, nil
	}
	return len(strings.Split(rel, string(os.PathSeparator))), nil
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

// maxAncestorCleanupLevels 每文件删除后向上清理空目录的最大层级数。
// 从被删文件的父目录开始向上计算，最多 2 级（例如：S 1 + 暗影蜘蛛侠；不会动到 A）。
const maxAncestorCleanupLevels = 2

// isMediaInfoCompanion 判断文件名是否为 Emby 神医助手 / TinyMediaManager 等工具
// 生成的 STRM 同名 MediaInfo 元数据：xxx-mediainfo.json。
// 仅匹配后缀，不引入 .json 的全局扩展名，避免误删用户自存 .json。
func isMediaInfoCompanion(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(name), "-mediainfo.json")
}

// isProtectedDirName 判断目录名是否为 115strm 不应越级清理的分类目录。
// 典型场景：用户用首字母或首数字做聚合目录，删空后不希望被自动清掉。
// 保护范围：
//   - 单字符目录名（单个字母 A-Z 或单个数字 0-9）
//   - 数字分组目录 "0-9"（多字符写法）
func isProtectedDirName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	if utf8.RuneCountInString(name) == 1 {
		return true
	}
	if name == "0-9" {
		return true
	}
	return false
}

// cleanupAncestorEmptyDirs 从 startPath 的父目录开始向上清理空目录，最多 maxLevels 级。
// 停止条件（满足任一即停）：
//  1. 目录非空
//  2. 目录等于 BatchDir 根
//  3. 目录的父目录等于 BatchDir 根（即处于 电视/电影 等顶层类别）
//  4. 目录名命中 isProtectedDirName（单字符 / "0-9"）
//  5. 已向上清理 maxLevels 次
func (s *Service) cleanupAncestorEmptyDirs(startPath string, maxLevels int) (int, error) {
	if maxLevels <= 0 {
		maxLevels = maxAncestorCleanupLevels
	}
	root := strings.TrimSpace(s.cfg.STRM302.BatchDir)
	if root == "" {
		root = "/opt/Media"
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	deleted := 0
	dir := filepath.Dir(startPath)
	for level := 0; level < maxLevels; level++ {
		dirAbs, err := filepath.Abs(dir)
		if err != nil {
			return deleted, err
		}
		if dirAbs == rootAbs {
			break
		}
		parentAbs, err := filepath.Abs(filepath.Dir(dirAbs))
		if err != nil {
			return deleted, err
		}
		if parentAbs == rootAbs {
			break
		}
		if isProtectedDirName(filepath.Base(dir)) {
			break
		}
		empty, err := isEmptyDir(dir)
		if err != nil {
			return deleted, err
		}
		if !empty {
			break
		}
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return deleted, err
		}
		deleted++
		if s.audit != nil {
			s.audit.FileOp(auditlog.TypeDirectoryDelete, dir)
		}
		if s.logs != nil {
			s.logs.Info("已清理空目录 %s", dir)
		}
		dir = filepath.Dir(dir)
	}
	return deleted, nil
}
