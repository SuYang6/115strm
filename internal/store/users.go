package store

import (
	"database/sql"
	"errors"
	"time"

	"115strm/internal/model"
)

func (s *Store) UserCount() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash string) error {
	_, err := s.DB.Exec(`INSERT INTO users(username, password_hash, created_at) VALUES(?, ?, ?)`, username, passwordHash, nowText())
	return err
}

func (s *Store) UserByUsername(username string) (*model.User, error) {
	row := s.DB.QueryRow(`SELECT id, username, password_hash, created_at, last_login_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func (s *Store) UserByID(id int64) (*model.User, error) {
	row := s.DB.QueryRow(`SELECT id, username, password_hash, created_at, last_login_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *Store) TouchUserLogin(id int64) error {
	_, err := s.DB.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, nowText(), id)
	return err
}

func (s *Store) UpdatePassword(id int64, passwordHash string) error {
	_, err := s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (*model.User, error) {
	var u model.User
	var created string
	var last sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &created, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		u.CreatedAt = t
	}
	u.LastLoginAt = parseTimePtr(last)
	return &u, nil
}
