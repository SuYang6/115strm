package strm302

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"115strm/internal/model"
	"115strm/internal/util"
)

func (s *Service) hydratePickcodes(jobID int64, req GenerateRequest, files []model.CloudFile, progress func(done, total int, currentPath, message string)) ([]model.CloudFile, int, error) {
	source := normalizeSourceName(req.ScanSource)
	if source == "" {
		source = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	if source == "" || source == "115" {
		return files, 0, nil
	}

	unresolved := map[string][]int{}
	for i := range files {
		if files[i].IsDir || !s.needsPickcode(files[i].Name) || strings.TrimSpace(files[i].PickCode) != "" {
			continue
		}
		if s.fillPickcodeFromCache(&files[i]) {
			continue
		}
		dir := normalizeSourcePath(cloudFileSourceDir(files[i].SourcePath))
		unresolved[dir] = append(unresolved[dir], i)
	}
	if len(unresolved) == 0 {
		return files, 0, nil
	}
	unresolvedTotal := unresolvedFileCount(unresolved)
	if progress != nil {
		progress(0, unresolvedTotal, "", fmt.Sprintf("正在补齐 CD2/OpenList 文件 pickcode：0/%d", unresolvedTotal))
	}
	if !s.shouldResolvePickcodeFrom115(req) {
		return files, unresolvedTotal, fmt.Errorf("%d 个文件缺少 pickcode，且未启用 115 补齐", unresolvedTotal)
	}
	rootCID, rootSourcePath := s.pickcodeRoot(req)
	if strings.TrimSpace(rootCID) == "" || rootCID == "0" {
		return files, unresolvedTotal, errors.New("使用 CD2/OpenList 扫描时需要配置 115 根目录 CID，用于补齐 pickcode")
	}

	dirCID := map[string]string{normalizeSourcePath(rootSourcePath): strings.TrimSpace(rootCID)}
	dirs := make([]string, 0, len(unresolved))
	for dir := range unresolved {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	if s.logs != nil {
		s.logs.Warn("CD2/OpenList 扫描有 %d 个文件缺少 pickcode，需要按 %d 个目录向 115 补齐；如果数量很大，通常是扫描根目录和旧 STRM 路径层级不一致", unresolvedTotal, len(dirs))
	}

	missing := 0
	done := 0
	cacheScanID := newScanID(jobID)
	missingErr := errors.New("115 目录中未按文件名和大小匹配到 pickcode")
	for dirIndex, dir := range dirs {
		if s.isStopRequested(jobID) {
			return files, unresolvedTotal, errScanStopped
		}
		if progress != nil {
			progress(done, unresolvedTotal, dir, fmt.Sprintf("正在补齐 CD2/OpenList 文件 pickcode：%d/%d，目录 %d/%d", done, unresolvedTotal, dirIndex+1, len(dirs)))
		}
		cid, err := s.resolve115CIDForSourceDir(jobID, strings.TrimSpace(rootCID), rootSourcePath, dir, dirCID)
		if err != nil {
			s.recordHydrateFailures(req, files, unresolved[dir], err)
			return files, unresolvedTotal, err
		}
		children, err := s.driver.BrowseForJobNamed(jobID, cid, dir)
		if err != nil {
			s.recordHydrateFailures(req, files, unresolved[dir], err)
			return files, unresolvedTotal, err
		}
		byNameSize := map[string]model.CloudFile{}
		for _, child := range children {
			if child.IsDir {
				continue
			}
			child.SourcePath = util.SlashJoin(dir, child.Name)
			byNameSize[fileMatchKey(child.Name, child.Size)] = child
		}
		for _, idx := range unresolved[dir] {
			if s.isStopRequested(jobID) {
				return files, unresolvedTotal, errScanStopped
			}
			match, ok := byNameSize[fileMatchKey(files[idx].Name, files[idx].Size)]
			if !ok || strings.TrimSpace(match.PickCode) == "" {
				missing++
				if files[idx].SourceType == "" {
					files[idx].SourceType = source
				}
				s.recordSTRMFailure(failureHydratePickcode, req, files[idx], missingErr)
				continue
			}
			files[idx].ID = match.ID
			files[idx].PickCode = match.PickCode
			files[idx].CID = match.CID
			files[idx].Sha1 = match.Sha1
			files[idx].ThumbURL = match.ThumbURL
			if match.ModTime > 0 {
				files[idx].ModTime = match.ModTime
			}
			if files[idx].SourceType == "" {
				files[idx].SourceType = source
			}
			if s.db != nil {
				if err := s.db.UpsertScanCacheFile(files[idx], cacheScanID); err != nil && s.logs != nil {
					s.logs.Warn("写入 pickcode 补齐缓存失败：%s：%v", files[idx].SourcePath, err)
				}
			}
			done++
			if progress != nil {
				progress(done, unresolvedTotal, files[idx].SourcePath, fmt.Sprintf("正在补齐 CD2/OpenList 文件 pickcode：%d/%d，目录 %d/%d", done, unresolvedTotal, dirIndex+1, len(dirs)))
			}
		}
	}
	if missing > 0 {
		if s.logs != nil {
			s.logs.Warn("%d 个文件未能从 115 目录中匹配到 pickcode，已写入失败列表并跳过这些文件，其它文件继续同步", missing)
		}
	}
	return files, missing, nil
}

func (s *Service) recordHydrateFailures(req GenerateRequest, files []model.CloudFile, indexes []int, err error) {
	for _, idx := range indexes {
		if idx < 0 || idx >= len(files) {
			continue
		}
		s.recordSTRMFailure(failureHydratePickcode, req, files[idx], err)
	}
}

func (s *Service) pickcodeRoot(req GenerateRequest) (string, string) {
	source := normalizeSourceName(req.ScanSource)
	if source == "" {
		source = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	if source == "115" {
		return strings.TrimSpace(req.CID), req.SourcePath
	}
	cid := strings.TrimSpace(req.CID)
	if cid == "" || cid == "0" {
		cid = strings.TrimSpace(s.cfg.STRM302.RootCID)
	}
	return cid, ""
}

func (s *Service) fillPickcodeFromCache(f *model.CloudFile) bool {
	sourcePath := normalizeSourcePath(f.SourcePath)
	if sourcePath == "" {
		return false
	}
	if cached, err := s.db.ScanCacheBySourcePath(sourcePath); err == nil && cached != nil && cacheMatchesCloudFile(cached, *f) && cached.PickCode != "" {
		f.PickCode = cached.PickCode
		f.CID = cached.CID
		f.ID = cached.CloudFileID
		f.Sha1 = cached.Sha1
		if f.ModTime == 0 {
			f.ModTime = cached.SourceMTime
		}
		return true
	}
	if existing, err := s.db.STRMFileBySourcePath(sourcePath); err == nil && existing != nil && existing.Size == f.Size && existing.PickCode != "" {
		f.PickCode = existing.PickCode
		f.CID = existing.CID
		if f.ModTime == 0 {
			f.ModTime = existing.SourceMTime
		}
		return true
	}
	if existing, err := s.db.MediaFileBySourcePath(sourcePath); err == nil && existing != nil && existing.Size == f.Size && existing.PickCode != "" {
		f.PickCode = existing.PickCode
		f.CID = existing.CID
		if f.ModTime == 0 {
			f.ModTime = existing.SourceMTime
		}
		return true
	}
	return false
}

func cacheMatchesCloudFile(cached *model.ScanCacheFile, f model.CloudFile) bool {
	if cached == nil || cached.Size != f.Size {
		return false
	}
	if cached.Sha1 != "" && f.Sha1 != "" && !strings.EqualFold(cached.Sha1, f.Sha1) {
		return false
	}
	if cached.SourceMTime > 0 && f.ModTime > 0 && cached.SourceMTime != f.ModTime {
		return false
	}
	return true
}

func (s *Service) resolve115CIDForSourceDir(jobID int64, rootCID, rootSourcePath, targetDir string, cache map[string]string) (string, error) {
	root := normalizeSourcePath(rootSourcePath)
	target := normalizeSourcePath(targetDir)
	if cid := cache[target]; cid != "" {
		return cid, nil
	}
	segments, err := relativeSegments(root, target)
	if err != nil {
		return "", err
	}
	currentCID := rootCID
	currentPath := root
	if currentPath == "" {
		currentPath = ""
	}
	for _, segment := range segments {
		nextPath := normalizeSourcePath(util.SlashJoin(currentPath, segment))
		if cid := cache[nextPath]; cid != "" {
			currentCID = cid
			currentPath = nextPath
			continue
		}
		children, err := s.driver.BrowseForJobNamed(jobID, currentCID, currentPath)
		if err != nil {
			return "", err
		}
		found := ""
		for _, child := range children {
			if child.IsDir && child.Name == segment {
				found = child.ID
				if found == "" {
					found = child.CID
				}
				break
			}
		}
		if found == "" {
			return "", fmt.Errorf("115 目录中找不到子目录 %s（父目录 %s）", segment, currentPath)
		}
		cache[nextPath] = found
		currentCID = found
		currentPath = nextPath
	}
	cache[target] = currentCID
	return currentCID, nil
}

func (s *Service) shouldResolvePickcodeFrom115(req GenerateRequest) bool {
	source := normalizeSourceName(req.ScanSource)
	if source == "" {
		source = normalizeSourceName(s.cfg.STRM302.ScanSource)
	}
	return source != "115"
}

func (s *Service) needsPickcode(name string) bool {
	if s.cfg.STRM302.VideoExtSet()[strings.ToLower(filepath.Ext(name))] {
		return true
	}
	return s.mediaType(name) != ""
}

func fileMatchKey(name string, size int64) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" + fmt.Sprintf("%d", size)
}

func relativeSegments(root, target string) ([]string, error) {
	root = normalizeSourcePath(root)
	target = normalizeSourcePath(target)
	if root == target {
		return nil, nil
	}
	rel := target
	if root != "" {
		prefix := root + "/"
		if !strings.HasPrefix(target, prefix) {
			return nil, fmt.Errorf("输出相对目录 %s 不在 115 根目录 %s 下", target, root)
		}
		rel = strings.TrimPrefix(target, prefix)
	}
	if rel == "" {
		return nil, nil
	}
	return strings.Split(rel, "/"), nil
}

func unresolvedFileCount(groups map[string][]int) int {
	total := 0
	for _, items := range groups {
		total += len(items)
	}
	return total
}
