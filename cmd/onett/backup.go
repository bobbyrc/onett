package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bobbyrc/onett/internal/secrets"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

// cmdBackup writes a consistent copy of the database to a new file. Like `user reset-password`
// it reads the database file itself, so it needs shell access to the daemon's machine: the
// copy holds password hashes and sessions, which the API token must never be able to fetch. It
// works whether or not the daemon is running.
func cmdBackup(e *env, args []string) error {
	fs := newFlagSet("backup", e)
	cfgFlag := fs.String("config", "", "config file (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "backup FILE [--config P]"); err != nil {
		return err
	}
	// An agent run carries these variables. An agent must not take a backup.
	if os.Getenv("ONETT_RUN_ID") != "" || os.Getenv("ONETT_ROLE") != "" {
		return errors.New("backup is refused inside an agent run: the copy holds password hashes and sign-in sessions, so only a person takes one")
	}
	dst, err := filepath.Abs(positional[0])
	if err != nil {
		return err
	}
	repo, err := enclosingRepo(dst)
	if err != nil {
		return err
	}
	if repo != "" {
		return fmt.Errorf("%s is inside the git repository or worktree %s: the copy holds password hashes, so write it where it cannot be committed or read by an agent", dst, repo)
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if err := secrets.New(filepath.Join(dir, "secrets.env")).Load(); err != nil {
		return err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	src := filepath.Join(cfg.Server.DataDir, "onett.db")
	if err := sqlite.Backup(context.Background(), src, dst); err != nil {
		return err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Backed up %s to %s (%d bytes).\nIt holds password hashes and sign-in sessions: keep it private.\n", src, dst, info.Size())
	return nil
}

// enclosingRepo returns the nearest directory at or above the parent of path that contains an
// entry named .git (a directory in a repository, a file in a worktree), or "" when there is none.
// The caller refuses such a destination, so a backup is never committed or left where an agent
// works. It also fails when the parent directory does not exist.
func enclosingRepo(path string) (string, error) {
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("backup directory: %w", err)
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
