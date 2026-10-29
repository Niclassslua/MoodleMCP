package config

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSavedLoginFillsConfigAndLogoutClearsIt(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("AppData", home)
	t.Setenv("MCD_ENV_FILE", home+"/none.env")
	for _, k := range []string{"MOODLE_URL", "MOODLE_USERNAME", "MOODLE_PASSWORD", "MOODLE_TOKEN"} {
		t.Setenv(k, "")
	}

	if err := SaveLogin("https://moodle.example", "alice", "tok"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://moodle.example" || c.Username != "alice" || c.Token != "tok" || c.Password != "" {
		t.Fatalf("unexpected config: %+v", c)
	}

	if err := Logout(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected error after logout")
	}
	if err := Logout(); err != nil {
		t.Fatalf("second logout should be a no-op: %v", err)
	}
}
