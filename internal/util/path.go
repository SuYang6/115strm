package util

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func SafeJoin(base, rel string) (string, error) {
	if base == "" {
		return "", errors.New("empty base path")
	}
	cleanRel := filepath.Clean(strings.TrimPrefix(rel, "/"))
	if cleanRel == "." {
		cleanRel = ""
	}
	target := filepath.Join(base, cleanRel)
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != baseAbs && !strings.HasPrefix(targetAbs, baseAbs+string(os.PathSeparator)) {
		return "", errors.New("path escapes base directory")
	}
	return targetAbs, nil
}

func SlashJoin(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

func ReplaceExt(name, ext string) string {
	old := filepath.Ext(name)
	return strings.TrimSuffix(name, old) + ext
}
