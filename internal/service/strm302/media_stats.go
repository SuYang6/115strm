package strm302

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"115strm/internal/store"
)

func (s *Service) refreshDashboardMediaLibraryStats() {
	stats, err := collectDashboardMediaLibraryStats(s.cfg.STRM302.BatchDir)
	if err != nil {
		if s.logs != nil {
			s.logs.Warn("刷新首页媒体统计失败：%v", err)
		}
		return
	}
	if err := s.db.SetMediaLibraryStats(stats); err != nil {
		if s.logs != nil {
			s.logs.Warn("保存首页媒体统计失败：%v", err)
		}
		return
	}
	if s.logs != nil {
		s.logs.Info("已刷新首页媒体统计：电视剧=%d 电影=%d", stats.TVSTRMCount, stats.MovieSTRMCount)
	}
	if deleted, err := s.cleanupEmptyMediaDirs(); err != nil {
		if s.logs != nil {
			s.logs.Warn("清理空媒体目录失败：%v", err)
		}
	} else if deleted > 0 && s.logs != nil {
		s.logs.Info("已清理空媒体目录 %d 个", deleted)
	}
	if purged, err := s.CleanRecycleRecords(autoRecycleKeepDays); err != nil {
		if s.logs != nil {
			s.logs.Warn("清理过期回收站失败：%v", err)
		}
	} else if purged > 0 && s.logs != nil {
		s.logs.Info("已清理过期回收文件 %d 个", purged)
	}
}

func collectDashboardMediaLibraryStats(root string) (store.MediaLibraryStats, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/opt/Media"
	}
	tvCount, err := countSTRMFiles(filepath.Join(root, "电视"))
	if err != nil {
		return store.MediaLibraryStats{}, err
	}
	movieCount, err := countSTRMFiles(filepath.Join(root, "电影"))
	if err != nil {
		return store.MediaLibraryStats{}, err
	}
	return store.MediaLibraryStats{
		Available:      true,
		Root:           root,
		TVSTRMCount:    tvCount,
		MovieSTRMCount: movieCount,
		TotalSTRMCount: tvCount + movieCount,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func countSTRMFiles(dir string) (int, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}
	count := 0
	err = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".strm") {
			count++
		}
		return nil
	})
	return count, err
}
