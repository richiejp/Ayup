package conf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"premai.io/Ayup/go/internal/terror"

	"github.com/joho/godotenv"
)

//User* functions were adapted from buildkit appdefaults

func UserRuntimeDir() string {
	//  pam_systemd sets XDG_RUNTIME_DIR but not other dirs.
	if xdgRuntimeDirs := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntimeDirs != "" {
		dirs := strings.Split(xdgRuntimeDirs, ":")
		return filepath.Join(dirs[0], "ayup")
	}

	return "/run/ayup"
}

// UserRoot typically returns /home/$USER/.local/share/ayup
func UserRoot() string {
	//  pam_systemd sets XDG_RUNTIME_DIR but not other dirs.
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome != "" {
		dirs := strings.Split(xdgDataHome, ":")
		return filepath.Join(dirs[0], "ayup")
	}
	home := os.Getenv("HOME")
	if home != "" {
		return filepath.Join(home, ".local", "share", "ayup")
	}
	return "/var/lib/ayup"
}

// UserConfigDir returns dir for storing config. /home/$USER/.config/ayup/
func UserConfigDir() string {
	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "ayup")
	}
	home := os.Getenv("HOME")
	if home != "" {
		return filepath.Join(home, ".config", "ayup")
	}
	return "/etc/ayup"
}

func InrootlessAddr() string {
	return filepath.Join(UserRuntimeDir(), "rootless.sock")
}

func confFilePath(ctx context.Context) (string, error) {
	confDir := UserConfigDir()

	if err := os.MkdirAll(confDir, 0700); err != nil {
		return "", terror.Errorf(ctx, "os MkdirAll: %w", err)
	}

	return filepath.Join(confDir, "env"), nil
}

func read(ctx context.Context, path string) (confMap map[string]string, err error) {
	confMap, err = godotenv.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		confMap = make(map[string]string)
	} else if err != nil {
		return nil, terror.Errorf(ctx, "godotenv Read: %w", err)
	}

	return confMap, nil
}

func write(ctx context.Context, path string, confMap map[string]string) error {
	text, err := godotenv.Marshal(confMap)
	if err != nil {
		return terror.Errorf(ctx, "godotenv Marshal: %w", err)
	}

	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
		0600,
	)
	if err != nil {
		return terror.Errorf(ctx, "os OpenFile: %w", err)
	}
	defer file.Close()

	if _, err = file.WriteString(text); err != nil {
		return terror.Errorf(ctx, "file WriteString: %w", err)
	}

	return nil
}

func Append(ctx context.Context, key string, val string) error {
	path, err := confFilePath(ctx)
	if err != nil {
		return err
	}

	confMap, err := read(ctx, path)
	if err != nil {
		return err
	}

	oldVal, ok := confMap[key]
	if !ok || oldVal == "" {
		confMap[key] = val
	} else {
		confMap[key] = fmt.Sprintf("%s,%s", oldVal, val)
	}

	return write(ctx, path, confMap)
}

func Set(ctx context.Context, key string, val string) error {
	path, err := confFilePath(ctx)
	if err != nil {
		return err
	}

	confMap, err := read(ctx, path)
	if err != nil {
		return err
	}

	confMap[key] = val

	return write(ctx, path, confMap)
}

// Adapted from buildkit appdefaults
