package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// Saved login from `mcd login`: URL and username in the user config dir,
// the token in the OS keychain. The password is never stored.
const keyringService = "mcd"

type savedLogin struct {
	URL      string `json:"url"`
	Username string `json:"username"`
}

func loginPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcd", "login.json"), nil
}

// SaveLogin stores the token for url in the keychain and remembers url and username.
func SaveLogin(url, username, token string) error {
	p, err := loginPath()
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, url, token); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(savedLogin{URL: url, Username: username}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Logout removes the saved login. It is not an error if there is none.
func Logout() error {
	p, err := loginPath()
	if err != nil {
		return err
	}
	if s, err := readLogin(p); err == nil {
		if err := keyring.Delete(keyringService, s.URL); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readLogin(p string) (savedLogin, error) {
	var s savedLogin
	b, err := os.ReadFile(p)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// applySavedLogin fills URL, username and token from `mcd login` when the
// environment does not provide credentials.
func applySavedLogin(c *Config) {
	p, err := loginPath()
	if err != nil {
		return
	}
	s, err := readLogin(p)
	if err != nil || (c.BaseURL != "" && c.BaseURL != s.URL) {
		return
	}
	token, err := keyring.Get(keyringService, s.URL)
	if err != nil {
		return
	}
	c.BaseURL, c.Username, c.Token = s.URL, s.Username, token
}
