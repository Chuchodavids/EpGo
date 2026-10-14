package main

import (
	"os"
	"testing"
)

func TestReliabilityCachePermissions(t *testing.T) {
	reliabilityConfig(t)
	syntheticCache(t)
	if err := os.Chmod(Config.Files.Cache, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Config.Files.Cache)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("cache permissions widened to %o", info.Mode().Perm())
	}
	Config.Files.Cache += ".new"
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(Config.Files.Cache)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("new cache permissions %o, want 600", info.Mode().Perm())
	}
}
