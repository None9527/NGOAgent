package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = ".ngoagent"

// HomeDir returns the NGOAgent home directory (~/.ngoagent/).
func HomeDir() string {
	if env := os.Getenv("NGOAGENT_HOME"); env != "" {
		return env
	}

	home, err := os.UserHomeDir()
	if err != nil {
		if runtime.GOOS == "windows" {
			home = os.Getenv("USERPROFILE")
		} else {
			home = os.Getenv("HOME")
		}
	}
	return filepath.Join(home, appDirName)
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	return filepath.Join(HomeDir(), "config.yaml")
}

// ResolvePath expands ~ prefix and makes paths absolute relative to HomeDir.
func ResolvePath(path string) string {
	if path == "" {
		return ""
	}
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(HomeDir(), path)
}
