package store

import (
	"database/sql"
	"strings"
	"time"

	"115strm/internal/model"
)

func (s *Store) CreateJob(jobType, params string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO strm_jobs(type, status, params_json) VALUES(?, 'pending', ?)`, jobType, params)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateJob(id int64, status string, total, done, failed int, errMsg string, finish bool) error {
	return s.UpdateJobProgress(&model.STRMJob{
		ID:     id,
		Status: status,
		Total:  total,
		Done:   done,
		Failed: failed,
		Error:  errMsg,
	}, finish)
}

func (s *Store) UpdateJobProgress(job *model.STRMJob, finish bool) error {
	var started any
	if job.Status == "running" {
		started = nowText()
	}
	var finished any
	if finish {
		finished = nowText()
	}
	_, err := s.DB.Exec(`
		UPDATE strm_jobs SET
			status=?,
			phase=?,
			total=?,
			done=?,
			failed=?,
			scanned=?,
			plan_total=?,
			download_total=?,
			download_done=?,
			current_path=?,
			message=?,
			stats_json=?,
			error=?,
			started_at=COALESCE(started_at, ?),
			finished_at=COALESCE(?, finished_at)
		WHERE id=?
	`, job.Status, job.Phase, job.Total, job.Done, job.Failed, job.Scanned, job.PlanTotal, job.DownloadTotal, job.DownloadDone, job.CurrentPath, job.Message, nonEmptyJSON(job.StatsJSON), job.Error, started, finished, job.ID)
	return err
}

func (s *Store) JobByID(id int64) (*model.STRMJob, error) {
	row := s.DB.QueryRow(`SELECT id, type, status, phase, total, done, failed, scanned, plan_total, download_total, download_done, current_path, message, stats_json, params_json, started_at, finished_at, error FROM strm_jobs WHERE id=?`, id)
	var j model.STRMJob
	var started, finished sql.NullString
	if err := row.Scan(&j.ID, &j.Type, &j.Status, &j.Phase, &j.Total, &j.Done, &j.Failed, &j.Scanned, &j.PlanTotal, &j.DownloadTotal, &j.DownloadDone, &j.CurrentPath, &j.Message, &j.StatsJSON, &j.ParamsJSON, &started, &finished, &j.Error); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	j.StartedAt = parseTimePtr(started)
	j.FinishedAt = parseTimePtr(finished)
	return &j, nil
}

func (s *Store) ActiveSTRMJobCount() (int, error) {
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM strm_jobs WHERE status IN ('pending','running')`).Scan(&count)
	return count, err
}

func (s *Store) ActiveSTRMJob() (*model.STRMJob, error) {
	row := s.DB.QueryRow(`SELECT id, type, status, phase, total, done, failed, scanned, plan_total, download_total, download_done, current_path, message, stats_json, params_json, started_at, finished_at, error FROM strm_jobs WHERE status IN ('pending','running') ORDER BY id DESC LIMIT 1`)
	var j model.STRMJob
	var started, finished sql.NullString
	if err := row.Scan(&j.ID, &j.Type, &j.Status, &j.Phase, &j.Total, &j.Done, &j.Failed, &j.Scanned, &j.PlanTotal, &j.DownloadTotal, &j.DownloadDone, &j.CurrentPath, &j.Message, &j.StatsJSON, &j.ParamsJSON, &started, &finished, &j.Error); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	j.StartedAt = parseTimePtr(started)
	j.FinishedAt = parseTimePtr(finished)
	return &j, nil
}

func (s *Store) MarkActiveSTRMJobsStopped(message string) (int64, error) {
	if strings.TrimSpace(message) == "" {
		message = "服务已重启，旧运行任务已停止"
	}
	res, err := s.DB.Exec(`
		UPDATE strm_jobs SET
			status='stopped',
			phase='stopped',
			message=?,
			error='',
			finished_at=COALESCE(finished_at, ?)
		WHERE status IN ('pending','running')
	`, message, nowText())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func nonEmptyJSON(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}

func (s *Store) SetRedirectCache(pickcode, targetURL string, expiresAt time.Time) error {
	_, err := s.DB.Exec(`
		INSERT INTO redirect_cache(pickcode, target_url, expires_at) VALUES(?, ?, ?)
		ON CONFLICT(pickcode) DO UPDATE SET target_url=excluded.target_url, expires_at=excluded.expires_at
	`, pickcode, targetURL, expiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetRedirectCache(pickcode string) (string, bool, error) {
	var url, expires string
	if err := s.DB.QueryRow(`SELECT target_url, expires_at FROM redirect_cache WHERE pickcode=?`, pickcode).Scan(&url, &expires); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	t, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().After(t) {
		return "", false, nil
	}
	return url, true, nil
}
