package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	BaseURL  string
	Username string
	Password string
	Token    string
	Service  string
}

func Load() (Config, error) {
	loadDotEnv()
	c := Config{
		BaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("MOODLE_URL")), "/"),
		Username: strings.TrimSpace(os.Getenv("MOODLE_USERNAME")),
		Password: os.Getenv("MOODLE_PASSWORD"),
		Token:    strings.TrimSpace(os.Getenv("MOODLE_TOKEN")),
		Service:  strings.TrimSpace(os.Getenv("MOODLE_SERVICE")),
	}
	if c.Service == "" {
		c.Service = "moodle_mobile_app"
	}
	if c.Token == "" && (c.Username == "" || c.Password == "") {
		applySavedLogin(&c)
	}
	if c.BaseURL == "" {
		return c, fmt.Errorf("MOODLE_URL is required (or run: mcd login)")
	}
	if c.Token == "" && (c.Username == "" || c.Password == "") {
		return c, fmt.Errorf("set MOODLE_TOKEN or MOODLE_USERNAME + MOODLE_PASSWORD, or run: mcd login")
	}
	return c, nil
}

// loadDotEnv reads $MCD_ENV_FILE if set (MCP hosts start the server in an
// arbitrary cwd), else the first .env found in cwd or up to two parents.
func loadDotEnv() {
	paths := []string{
		".env",
		filepath.Join("..", ".env"),
		filepath.Join("..", "..", ".env"),
	}
	if p := os.Getenv("MCD_ENV_FILE"); p != "" {
		paths = []string{p}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
		return
	}
}
