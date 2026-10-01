package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteUnusedImages(t *testing.T) {
	imageTestConfig(t)
	c := prefetchFixture(t)
	Config.Options.Images.Cleanup = 7
	folder := Config.Options.Images.Path
	old := time.Now().AddDate(0, 0, -30)

	write := func(name string, mtime time.Time) string {
		file := filepath.Join(folder, name)
		if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return file
	}

	// Every image the guide uses already exists and is old.
	var used []string
	for id := range c.Metadata {
		if icon := c.selectIcon(id); icon != nil {
			used = append(used, write(imageFilename(icon.Src), old))
		}
		for _, data := range c.selectProgrammeImages(id) {
			used = append(used, write(imageFilename(data.URI), old))
		}
	}
	if len(used) == 0 {
		t.Fatal("fixture uses no images")
	}
	unusedOld := write("unused-old.jpg", old)
	unusedNew := write("unused-new.jpg", time.Now())
	otherOld := write("notes.txt", old)

	// No requests: all used images exist (the transport fails the test).
	c.prefetchImages()

	for _, file := range append(used, unusedNew, otherOld) {
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%s deleted: %v", filepath.Base(file), err)
		}
	}
	if _, err := os.Stat(unusedOld); !os.IsNotExist(err) {
		t.Errorf("unused old image not deleted: %v", err)
	}
}

func TestDeleteUnusedImagesSkipsEmptyGuide(t *testing.T) {
	imageTestConfig(t)
	Config.Options.Images.Cleanup = 7
	file := filepath.Join(Config.Options.Images.Path, "old.jpg")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}

	(&cache{}).prefetchImages()

	if _, err := os.Stat(file); err != nil {
		t.Fatalf("image deleted although the guide was empty: %v", err)
	}
}
