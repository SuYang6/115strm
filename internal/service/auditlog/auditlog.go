package auditlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	TypeAPIError        = "接口错误"
	TypeSTRMCreate      = "STRM创建"
	TypeSTRMUpdate      = "STRM更新"
	TypeSTRMDelete      = "STRM删除"
	TypeMetadataCreate  = "元数据创建"
	TypeMetadataUpdate  = "元数据更新"
	TypeMetadataDelete  = "元数据删除"
	TypeMetadataMove    = "元数据移动"
	TypeRecycleMove     = "回收站移入"
	TypeRecyclePurge    = "回收站清理"
	TypeRecycleRestore  = "回收站恢复"
	TypeDirectoryDelete = "空目录删除"
	TypeEmbyNotify      = "Emby通知"
	TypeTask            = "定时任务"
)

type Entry struct {
	Time   time.Time `json:"time"`
	Type   string    `json:"type"`
	Path   string    `json:"path,omitempty"`
	Method string    `json:"method,omitempty"`
	Status int       `json:"status,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type Query struct {
	Range   string
	Date    string
	Start   string
	End     string
	Type    string
	Keyword string
	Limit   int
	Offset  int
}

type Result struct {
	Items []Entry `json:"items"`
	Total int     `json:"total"`
}

type Logger struct {
	dir       string
	keepDays  int
	mu        sync.Mutex
	subs      map[chan Entry]struct{}
	lastClean time.Time
}

func New(logFile string, keepDays int) (*Logger, error) {
	dir := filepath.Dir(logFile)
	if strings.TrimSpace(dir) == "" || dir == "." {
		dir = "."
	}
	if keepDays <= 0 {
		keepDays = 14
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	l := &Logger{dir: dir, keepDays: keepDays, subs: map[chan Entry]struct{}{}}
	_ = l.Cleanup()
	return l, nil
}

func (l *Logger) FileOp(opType, path string) {
	if l == nil || strings.TrimSpace(path) == "" {
		return
	}
	l.Write(Entry{Time: time.Now(), Type: opType, Path: path})
}

func (l *Logger) Event(eventType, message string) {
	if l == nil || strings.TrimSpace(eventType) == "" || strings.TrimSpace(message) == "" {
		return
	}
	l.Write(Entry{Time: time.Now(), Type: eventType, Path: message})
}

func (l *Logger) APIError(method, path string, status int, errText string) {
	if l == nil {
		return
	}
	l.Write(Entry{Time: time.Now(), Type: TypeAPIError, Method: method, Path: path, Status: status, Error: sanitize(errText)})
}

func (l *Logger) Write(e Entry) {
	if l == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Type = strings.TrimSpace(e.Type)
	if e.Type == "" {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.lastClean) > time.Hour {
		_ = l.cleanupLocked()
	}
	f, err := os.OpenFile(l.dayPath(e.Time), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	for ch := range l.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (l *Logger) List(q Query) ([]Entry, error) {
	result, err := l.Search(q)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (l *Logger) Search(q Query) (Result, error) {
	if l == nil {
		return Result{Items: []Entry{}}, nil
	}
	if q.Limit <= 0 || q.Limit > 5000 {
		q.Limit = 1000
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	paths, err := l.queryPaths(q)
	if err != nil {
		return Result{}, err
	}
	out := []Entry{}
	for _, path := range paths {
		entries, err := readEntries(path)
		if err != nil {
			return Result{}, err
		}
		for _, e := range entries {
			if !matchEntry(e, q) {
				continue
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	total := len(out)
	if q.Offset >= len(out) {
		return Result{Items: []Entry{}, Total: total}, nil
	}
	end := q.Offset + q.Limit
	if end > len(out) {
		end = len(out)
	}
	return Result{Items: out[q.Offset:end], Total: total}, nil
}

func (l *Logger) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 64)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	cancel := func() {
		l.mu.Lock()
		if _, ok := l.subs[ch]; ok {
			delete(l.subs, ch)
			close(ch)
		}
		l.mu.Unlock()
	}
	return ch, cancel
}

func (l *Logger) Cleanup() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cleanupLocked()
}

func (l *Logger) cleanupLocked() error {
	l.lastClean = time.Now()
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -l.keepDays)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "audit-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "audit-"), ".log")
		t, err := time.ParseInLocation("2006-01-02", day, time.Local)
		if err == nil && t.Before(startOfDay(cutoff)) {
			_ = os.Remove(filepath.Join(l.dir, entry.Name()))
		}
	}
	return nil
}

func (l *Logger) queryPaths(q Query) ([]string, error) {
	var days []time.Time
	now := time.Now()
	if strings.TrimSpace(q.Start) != "" || strings.TrimSpace(q.End) != "" {
		start, end, err := queryTimeRange(q)
		if err != nil {
			return nil, err
		}
		for day := startOfDay(start); !day.After(startOfDay(end)); day = day.AddDate(0, 0, 1) {
			days = append(days, day)
		}
	} else {
		switch strings.ToLower(strings.TrimSpace(q.Range)) {
		case "7d", "seven":
			for i := 0; i < 7; i++ {
				days = append(days, now.AddDate(0, 0, -i))
			}
		case "14d", "two_weeks":
			for i := 0; i < 14; i++ {
				days = append(days, now.AddDate(0, 0, -i))
			}
		case "date":
			if strings.TrimSpace(q.Date) == "" {
				return nil, errors.New("date is required")
			}
			t, err := time.ParseInLocation("2006-01-02", q.Date, time.Local)
			if err != nil {
				return nil, err
			}
			days = append(days, t)
		default:
			days = append(days, now)
		}
	}
	paths := make([]string, 0, len(days))
	for _, day := range days {
		path := l.dayPath(day)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func (l *Logger) dayPath(t time.Time) string {
	return filepath.Join(l.dir, "audit-"+t.Format("2006-01-02")+".log")
}

func readEntries(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Entry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil {
			out = append(out, e)
		}
	}
	return out, scanner.Err()
}

func matchEntry(e Entry, q Query) bool {
	if q.Type != "" && q.Type != "全部" && e.Type != q.Type {
		return false
	}
	if start, end, err := queryTimeRange(q); err == nil {
		if e.Time.Before(start) || e.Time.After(end) {
			return false
		}
	}
	keyword := strings.ToLower(strings.TrimSpace(q.Keyword))
	if keyword == "" {
		return true
	}
	haystack := strings.ToLower(e.Type + " " + e.Path + " " + e.Method + " " + e.Error)
	return strings.Contains(haystack, keyword)
}

func queryTimeRange(q Query) (time.Time, time.Time, error) {
	if strings.TrimSpace(q.Start) == "" && strings.TrimSpace(q.End) == "" {
		return time.Time{}, time.Time{}, errors.New("empty time range")
	}
	start := strings.TrimSpace(q.Start)
	end := strings.TrimSpace(q.End)
	if start == "" {
		start = "1970-01-01 00:00:00"
	}
	if end == "" {
		end = time.Now().Format("2006-01-02 15:04:05")
	}
	startTime, err := parseQueryTime(start)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endTime, err := parseQueryTime(end)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if endTime.Before(startTime) {
		return time.Time{}, time.Time{}, errors.New("end time is before start time")
	}
	return startTime, endTime, nil
}

func parseQueryTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			if layout == "2006-01-02" {
				return startOfDay(t), nil
			}
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid time format")
}

func sanitize(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, token := range []string{"access_token", "refresh_token", "Authorization", "Cookie"} {
		raw = strings.ReplaceAll(raw, token, "[filtered]")
	}
	if len(raw) > 1000 {
		raw = raw[:1000]
	}
	return raw
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
