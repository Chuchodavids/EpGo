package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// writeAtomic publishes only a completely written, closed file. Readers that
// already opened the old inode keep a consistent snapshot. Temporary files
// live on the same filesystem; failures never truncate the previous version.
// The exception is a file bind-mounted on its own, which is rewritten in place
// without these guarantees. File sync is not a full power-loss guarantee: the
// directory is not synced.
func writeAtomic(filename string, mode os.FileMode, write func(io.Writer) error) error {
	// Replace the file a symlink points to, not the symlink itself.
	if target, err := filepath.EvalSymlinks(filename); err == nil {
		filename = target
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err := os.Stat(filename)
	if err == nil {
		mode &= info.Mode().Perm() // never widen an existing restrictive mode
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if info != nil {
		keepOwner(f, info)
	}
	if err = write(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	err = os.Rename(f.Name(), filename)
	if errors.Is(err, syscall.EBUSY) {
		// A file bind-mounted on its own (docker -v ./guide.xml:...) cannot
		// be replaced. Fall back to rewriting it in place.
		return copyFile(f.Name(), filename, mode)
	}
	return err
}

// copyFile rewrites an existing file in place. Its permissions are narrowed to
// mode before anything is written, so a secret never lands in a file that is
// more readable than requested.
func copyFile(from, to string, mode os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	info, err := dst.Stat()
	if err == nil && info.Mode().Perm()&^mode != 0 {
		err = dst.Chmod(info.Mode().Perm() & mode)
	}
	if err == nil {
		err = dst.Truncate(0)
	}
	if err == nil {
		_, err = io.Copy(dst, src)
	}
	if err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
