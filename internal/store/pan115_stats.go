package store

import (
	"strings"
	"time"
)

func (s *Store) Increment115Request(jobID int64, action string) error {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "unknown"
	}
	day := time.Now().Format("2006-01-02")
	_, err := s.DB.Exec(`
		INSERT INTO pan115_request_stats(day, job_id, action, request_count, updated_at)
		VALUES(?, ?, ?, 1, ?)
		ON CONFLICT(day, job_id, action) DO UPDATE SET
			request_count=pan115_request_stats.request_count+1,
			updated_at=excluded.updated_at
	`, day, jobID, action, nowText())
	return err
}

func (s *Store) Pan115RequestStats(day string) (map[string]any, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	rows, err := s.DB.Query(`
		SELECT action, SUM(request_count)
		FROM pan115_request_stats
		WHERE day=?
		GROUP BY action
		ORDER BY action
	`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats, err := scan115RequestRows(rows)
	if err != nil {
		return nil, err
	}
	stats["day"] = day
	return stats, nil
}

func (s *Store) Pan115RequestStatsForJob(jobID int64) (map[string]any, error) {
	rows, err := s.DB.Query(`
		SELECT action, SUM(request_count)
		FROM pan115_request_stats
		WHERE job_id=?
		GROUP BY action
		ORDER BY action
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats, err := scan115RequestRows(rows)
	if err != nil {
		return nil, err
	}
	stats["job_id"] = jobID
	return stats, nil
}

type requestRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scan115RequestRows(rows requestRows) (map[string]any, error) {
	byAction := map[string]int64{}
	var total int64
	for rows.Next() {
		var action string
		var count int64
		if err := rows.Scan(&action, &count); err != nil {
			return nil, err
		}
		byAction[action] = count
		total += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"total":     total,
		"by_action": byAction,
	}, nil
}
