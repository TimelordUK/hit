// Package paths resolves where hit keeps its data and config (DESIGN §4.1).
package paths

import (
	"errors"
	"path/filepath"
)

// Env is the slice of the environment path resolution needs. Tests fill it by hand.
type Env struct {
	Getenv func(string) string
	GOOS   string
	Home   string
}

// HistoryFile is the file name inside the data dir.
const HistoryFile = "history.jsonl"

// DataDir: HIT_DATA_DIR, else %LOCALAPPDATA%\hit on Windows,
// else $XDG_DATA_HOME/hit, else ~/.local/share/hit.
func DataDir(e Env) (string, error) {
	if d := e.Getenv("HIT_DATA_DIR"); d != "" {
		return d, nil
	}
	if e.GOOS == "windows" {
		if d := e.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "hit"), nil
		}
		return "", errors.New("LOCALAPPDATA is not set; set HIT_DATA_DIR")
	}
	if d := e.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "hit"), nil
	}
	if e.Home == "" {
		return "", errors.New("home directory unknown; set HIT_DATA_DIR")
	}
	return filepath.Join(e.Home, ".local", "share", "hit"), nil
}

// ConfigFile: HIT_CONFIG (a file path), else %APPDATA%\hit\config.toml on Windows,
// else $XDG_CONFIG_HOME/hit/config.toml, else ~/.config/hit/config.toml.
func ConfigFile(e Env) (string, error) {
	if f := e.Getenv("HIT_CONFIG"); f != "" {
		return f, nil
	}
	if e.GOOS == "windows" {
		if d := e.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "hit", "config.toml"), nil
		}
		return "", errors.New("APPDATA is not set; set HIT_CONFIG")
	}
	if d := e.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "hit", "config.toml"), nil
	}
	if e.Home == "" {
		return "", errors.New("home directory unknown; set HIT_CONFIG")
	}
	return filepath.Join(e.Home, ".config", "hit", "config.toml"), nil
}

// History returns the path of the history file.
func History(e Env) (string, error) {
	d, err := DataDir(e)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, HistoryFile), nil
}
