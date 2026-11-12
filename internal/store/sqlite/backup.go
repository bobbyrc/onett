package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Backup writes a consistent copy of the database at src to the new file dst. It is safe while
// another process, such as the daemon, is writing src: SQLite's VACUUM INTO reads one snapshot,
// so the copy is the database as it was at one moment. The source is opened read-only and no
// migration runs.
//
// The copy holds password hashes and sessions, so it is created with mode 0600. Backup never
// overwrites: it fails when dst exists. It builds the copy in dst+".partial" and renames it into
// place at the end, so a file named dst is either empty (the name is reserved while the copy is
// built) or a complete database, never a half-written one. On failure it removes what it created.
func Backup(ctx context.Context, src, dst string) (err error) {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("no database at %s: %w", src, err)
	}

	// O_EXCL reserves the name, so two backups cannot race and nothing is overwritten.
	reserved, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists: a backup never overwrites a file", dst)
	}
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	_ = reserved.Close()
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()

	// We create the partial file ourselves, not SQLite, so it has mode 0600 from its first byte.
	// SQLite accepts an existing empty file as the VACUUM INTO target.
	partial := dst + ".partial"
	made, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s is left from an interrupted backup: delete it and run again", partial)
	}
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	_ = made.Close()
	defer func() {
		if err != nil {
			_ = os.Remove(partial)
			_ = os.Remove(partial + "-journal")
		}
	}()

	db, openErr := sql.Open("sqlite", "file:"+src+"?mode=ro&_pragma=busy_timeout(5000)")
	if openErr != nil {
		return fmt.Errorf("open sqlite %s: %w", src, openErr)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", partial); err != nil {
		return fmt.Errorf("back up %s: %w", src, err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("back up %s: %w", src, err)
	}
	if err := os.Rename(partial, dst); err != nil {
		return fmt.Errorf("finish backup: %w", err)
	}
	return nil
}
