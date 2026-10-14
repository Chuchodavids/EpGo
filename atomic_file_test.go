//go:build unix

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWriteAtomicKeepsSymlinkAndGroup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data", "guide.xml")
	link := filepath.Join(dir, "guide.xml")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	gid := os.Getgid()
	if os.Geteuid() == 0 {
		gid = 12345
		if err := os.Chown(target, -1, gid); err != nil {
			t.Fatal(err)
		}
	}

	if err := writeAtomic(link, 0644, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v %v", fi, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "new" {
		t.Fatalf("target = %q, %v", data, err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0640 || int(fi.Sys().(*syscall.Stat_t).Gid) != gid {
		t.Fatalf("mode %v gid %d, want 0640 gid %d", fi.Mode().Perm(), fi.Sys().(*syscall.Stat_t).Gid, gid)
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left: %s", e.Name())
		}
	}
}

func TestCopyFileRewritesInPlace(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	if err := os.WriteFile(from, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("older and longer"), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(to)
	if err := copyFile(from, to, 0600); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(to)
	if data, _ := os.ReadFile(to); string(data) != "new" || !os.SameFile(before, after) {
		t.Fatalf("in-place copy = %q same=%v", data, os.SameFile(before, after))
	}
	// The cache holds the token: a world-readable bind mount must be narrowed.
	if perm := after.Mode().Perm(); perm != 0600 {
		t.Fatalf("in-place copy left mode %o", perm)
	}
}
