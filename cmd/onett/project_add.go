package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bobbyrc/onett/internal/config"
)

// addProject adds a project to an existing config: `onett project add NAME --repo OWNER/NAME`.
// It runs without the daemon, because it only edits the config file; the daemon reads that file
// when it starts or reloads.
func addProject(e *env, args []string) error {
	fs := newFlagSet("project add", e)
	cfgFlag := fs.String("config", "", "config file to edit (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	repo := fs.String("repo", "", "the project's GitHub repository, owner/name (required)")
	dir := fs.String("path", "", "the project's local clone (default <data_dir>/repos/<name>)")
	branch := fs.String("branch", "", "the branch agents base their work on (default main)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "project add NAME --repo OWNER/NAME [--path DIR] [--branch BRANCH] [--config PATH]"); err != nil {
		return err
	}
	name := positional[0]
	if !projectName.MatchString(name) {
		return usagef("project name %q may use letters, digits, '.', '_' and '-'", name)
	}
	if *repo == "" {
		return usagef("--repo is required")
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; run \"onett init\" first", path)
	} else if err != nil {
		return err
	}
	out, err := config.AddProject(data, path, config.NewProject{Name: name, Repo: *repo, Path: *dir, Branch: *branch})
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(path, out); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "added project %s (%s) to %s\n", name, *repo, path)
	fmt.Fprintln(e.out, `run "onett config --reload" (or restart onett) to start using it`)
	return nil
}

// checkInsertion exists for the tests of the guard, which lives in package config.
func checkInsertion(before, after []byte, path, name string, setsSelf bool) error {
	return config.CheckInsertion(before, after, path, name, setsSelf)
}
