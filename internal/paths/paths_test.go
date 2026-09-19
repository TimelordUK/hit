package paths

import (
	"path/filepath"
	"testing"
)

func env(goos string, vars map[string]string) Env {
	return Env{Getenv: func(k string) string { return vars[k] }, GOOS: goos, Home: "/home/u"}
}

func TestDataDirAndConfigFile(t *testing.T) {
	tests := []struct {
		name       string
		e          Env
		data, conf string
	}{
		{"overrides win everywhere",
			env("windows", map[string]string{"HIT_DATA_DIR": "T", "HIT_CONFIG": "T/c.toml", "LOCALAPPDATA": "L", "APPDATA": "A"}),
			"T", "T/c.toml"},
		{"windows",
			env("windows", map[string]string{"LOCALAPPDATA": "L", "APPDATA": "A"}),
			filepath.Join("L", "hit"), filepath.Join("A", "hit", "config.toml")},
		{"linux xdg",
			env("linux", map[string]string{"XDG_DATA_HOME": "D", "XDG_CONFIG_HOME": "C"}),
			filepath.Join("D", "hit"), filepath.Join("C", "hit", "config.toml")},
		{"linux default",
			env("linux", nil),
			filepath.Join("/home/u", ".local", "share", "hit"), filepath.Join("/home/u", ".config", "hit", "config.toml")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := DataDir(tc.e)
			if err != nil || d != tc.data {
				t.Errorf("DataDir = %q, %v; want %q", d, err, tc.data)
			}
			c, err := ConfigFile(tc.e)
			if err != nil || c != tc.conf {
				t.Errorf("ConfigFile = %q, %v; want %q", c, err, tc.conf)
			}
		})
	}
}

func TestMissingEnvIsAnError(t *testing.T) {
	e := env("windows", nil)
	if _, err := DataDir(e); err == nil {
		t.Error("DataDir: want error without LOCALAPPDATA")
	}
	if _, err := ConfigFile(e); err == nil {
		t.Error("ConfigFile: want error without APPDATA")
	}
	e = Env{Getenv: func(string) string { return "" }, GOOS: "linux"}
	if _, err := History(e); err == nil {
		t.Error("History: want error without home")
	}
}
