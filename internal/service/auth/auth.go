package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"115strm/internal/config"
	"115strm/internal/model"
	"115strm/internal/service/logbuffer"
	"115strm/internal/store"
	"115strm/internal/util"
)

const jwtSettingKey = "jwt_secret"

type Service struct {
	db        *store.Store
	cfg       *config.Config
	logs      *logbuffer.Buffer
	jwtSecret string
	mu        sync.Mutex
	failures  map[string]failure
}

type failure struct {
	Count     int
	LockedTil time.Time
}

func New(db *store.Store, cfg *config.Config, logs *logbuffer.Buffer) (*Service, error) {
	s := &Service{db: db, cfg: cfg, logs: logs, failures: map[string]failure{}}
	secret, ok, err := db.GetSetting(jwtSettingKey)
	if err != nil {
		return nil, err
	}
	if !ok || secret == "" {
		raw, err := util.RandomBytes(64)
		if err != nil {
			return nil, err
		}
		secret = fmt.Sprintf("%x", raw)
		if err := db.SetSetting(jwtSettingKey, secret); err != nil {
			return nil, err
		}
	}
	s.jwtSecret = secret
	if err := s.ensureAdmin(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) JWTSecret() string { return s.jwtSecret }

func (s *Service) ensureAdmin() error {
	count, err := s.db.UserCount()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	password, err := util.RandomString(16)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.db.CreateUser("admin", string(hash)); err != nil {
		return err
	}
	path := filepath.Join(s.cfg.Storage.DataDir, ".initial_password")
	if err := os.WriteFile(path, []byte("admin:"+password+"\n"), 0600); err != nil {
		return err
	}
	s.logs.Info("初始管理员密码已写入 %s", path)
	fmt.Printf("115strm 初始管理员密码：admin / %s\n", password)
	return nil
}

func (s *Service) Login(username, password, ip string) (string, *model.User, error) {
	if s.isLocked(ip) {
		return "", nil, errors.New("too many login failures, locked for 15 minutes")
	}
	u, err := s.db.UserByUsername(username)
	if err != nil {
		return "", nil, err
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.recordFailure(ip)
		return "", nil, errors.New("invalid username or password")
	}
	s.clearFailure(ip)
	if err := s.db.TouchUserLogin(u.ID); err != nil {
		return "", nil, err
	}
	token, err := s.issueToken(u)
	if err != nil {
		return "", nil, err
	}
	s.logs.Info("用户 %s 已登录", username)
	return token, u, nil
}

func (s *Service) issueToken(u *model.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":      u.ID,
		"username": u.Username,
		"exp":      time.Now().Add(12 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}

func (s *Service) ParseToken(tokenString string) (*model.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	idFloat, ok := claims["sub"].(float64)
	if !ok {
		return nil, errors.New("invalid token subject")
	}
	return s.db.UserByID(int64(idFloat))
}

func (s *Service) ChangePassword(userID int64, oldPassword, newPassword string) error {
	u, err := s.db.UserByID(userID)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("user not found")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)) != nil {
		return errors.New("old password invalid")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.UpdatePassword(userID, string(hash))
}

func (s *Service) isLocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[ip]
	return !f.LockedTil.IsZero() && time.Now().Before(f.LockedTil)
}

func (s *Service) recordFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[ip]
	f.Count++
	if f.Count >= 5 {
		f.LockedTil = time.Now().Add(time.Duration(s.cfg.Security.LoginLockMinutes) * time.Minute)
	}
	s.failures[ip] = f
}

func (s *Service) clearFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, ip)
}
