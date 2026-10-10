package store

import (
	"database/sql"
	"time"

	"115strm/internal/model"
)

func (s *Store) ListTasks() ([]model.ScheduledTask, error) {
	rows, err := s.DB.Query(`SELECT id, name, type, cron_expr, params_json, enabled, last_run_at, last_status, last_error, next_run_at, timeout_seconds, retry_count, created_at FROM scheduled_tasks ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) TaskByID(id int64) (*model.ScheduledTask, error) {
	row := s.DB.QueryRow(`SELECT id, name, type, cron_expr, params_json, enabled, last_run_at, last_status, last_error, next_run_at, timeout_seconds, retry_count, created_at FROM scheduled_tasks WHERE id=?`, id)
	return scanTask(row)
}

func (s *Store) CreateTask(t *model.ScheduledTask) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO scheduled_tasks(name, type, cron_expr, params_json, enabled, timeout_seconds, retry_count, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.Type, t.CronExpr, t.ParamsJSON, boolInt(t.Enabled), t.TimeoutSeconds, t.RetryCount, nowText())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTask(t *model.ScheduledTask) error {
	_, err := s.DB.Exec(`UPDATE scheduled_tasks SET name=?, type=?, cron_expr=?, params_json=?, enabled=?, timeout_seconds=?, retry_count=? WHERE id=?`,
		t.Name, t.Type, t.CronExpr, t.ParamsJSON, boolInt(t.Enabled), t.TimeoutSeconds, t.RetryCount, t.ID)
	return err
}

func (s *Store) DeleteTask(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM scheduled_tasks WHERE id=?`, id)
	return err
}

func (s *Store) ToggleTask(id int64, enabled bool) error {
	_, err := s.DB.Exec(`UPDATE scheduled_tasks SET enabled=? WHERE id=?`, boolInt(enabled), id)
	return err
}

func (s *Store) UpdateTaskRunState(id int64, status, errMsg string, next *time.Time) error {
	_, err := s.DB.Exec(`UPDATE scheduled_tasks SET last_run_at=?, last_status=?, last_error=?, next_run_at=? WHERE id=?`,
		nowText(), status, errMsg, timePtrText(next), id)
	return err
}

func (s *Store) CreateTaskRun(taskID int64, strmJobID int64, started time.Time) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO task_runs(task_id, strm_job_id, started_at, status) VALUES(?, ?, ?, 'running')`, taskID, strmJobID, started.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SetTaskRunSTRMJob(runID int64, strmJobID int64) error {
	_, err := s.DB.Exec(`UPDATE task_runs SET strm_job_id=? WHERE id=?`, strmJobID, runID)
	return err
}

func (s *Store) FinishTaskRun(runID int64, status, message string, affected int, started time.Time) error {
	finished := time.Now().UTC()
	_, err := s.DB.Exec(`UPDATE task_runs SET finished_at=?, status=?, duration_ms=?, message=?, affected_count=? WHERE id=?`,
		finished.Format(time.RFC3339Nano), status, finished.Sub(started).Milliseconds(), message, affected, runID)
	return err
}

func (s *Store) MarkRunningTaskRunsStopped(message string) (int64, error) {
	if message == "" {
		message = "服务已重启，旧运行任务已停止"
	}
	res, err := s.DB.Exec(`UPDATE task_runs SET finished_at=?, status='stopped', message=? WHERE status='running'`,
		nowText(), message)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) TaskRuns(taskID int64, limit int) ([]model.TaskRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.Query(`SELECT id, task_id, strm_job_id, started_at, finished_at, status, duration_ms, message, affected_count FROM task_runs WHERE task_id=? ORDER BY started_at DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TaskRun
	for rows.Next() {
		var r model.TaskRun
		var started string
		var finished sql.NullString
		if err := rows.Scan(&r.ID, &r.TaskID, &r.STRMJobID, &started, &finished, &r.Status, &r.DurationMS, &r.Message, &r.AffectedCount); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, started); err == nil {
			r.StartedAt = t
		}
		r.FinishedAt = parseTimePtr(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LatestTaskRun(taskID int64) (*model.TaskRun, error) {
	row := s.DB.QueryRow(`SELECT id, task_id, strm_job_id, started_at, finished_at, status, duration_ms, message, affected_count FROM task_runs WHERE task_id=? ORDER BY started_at DESC LIMIT 1`, taskID)
	var r model.TaskRun
	var started string
	var finished sql.NullString
	if err := row.Scan(&r.ID, &r.TaskID, &r.STRMJobID, &started, &finished, &r.Status, &r.DurationMS, &r.Message, &r.AffectedCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339Nano, started); err == nil {
		r.StartedAt = t
	}
	r.FinishedAt = parseTimePtr(finished)
	return &r, nil
}

func scanTask(row scanner) (*model.ScheduledTask, error) {
	var t model.ScheduledTask
	var enabled int
	var lastRun, nextRun sql.NullString
	var created string
	if err := row.Scan(&t.ID, &t.Name, &t.Type, &t.CronExpr, &t.ParamsJSON, &enabled, &lastRun, &t.LastStatus, &t.LastError, &nextRun, &t.TimeoutSeconds, &t.RetryCount, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t.Enabled = enabled == 1
	t.LastRunAt = parseTimePtr(lastRun)
	t.NextRunAt = parseTimePtr(nextRun)
	if c, err := time.Parse(time.RFC3339Nano, created); err == nil {
		t.CreatedAt = c
	}
	return &t, nil
}
