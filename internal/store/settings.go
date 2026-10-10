package store

import "encoding/json"

const MediaLibraryStatsSettingKey = "dashboard.media_library_stats"

type MediaLibraryStats struct {
	Available      bool   `json:"available"`
	Root           string `json:"root"`
	TVSTRMCount    int    `json:"tv_strm_count"`
	MovieSTRMCount int    `json:"movie_strm_count"`
	TotalSTRMCount int    `json:"total_strm_count"`
	UpdatedAt      string `json:"updated_at"`
}

func (s *Store) GetSetting(key string) (string, bool, error) {
	var value string
	err := s.DB.QueryRow(`SELECT value FROM settings_kv WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`
		INSERT INTO settings_kv(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, nowText())
	return err
}

func (s *Store) SetMediaLibraryStats(stats MediaLibraryStats) error {
	stats.Available = true
	raw, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	return s.SetSetting(MediaLibraryStatsSettingKey, string(raw))
}

func (s *Store) GetMediaLibraryStats() (MediaLibraryStats, bool, error) {
	raw, ok, err := s.GetSetting(MediaLibraryStatsSettingKey)
	if err != nil || !ok {
		return MediaLibraryStats{}, ok, err
	}
	var stats MediaLibraryStats
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		return MediaLibraryStats{}, false, err
	}
	return stats, true, nil
}
