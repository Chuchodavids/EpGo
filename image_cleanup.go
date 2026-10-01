package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// imageExtensions are the files deleteUnusedImages may remove. Anything
// else in the image folder is left alone.
var imageExtensions = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".tmp": true}

// deleteUnusedImages deletes images that no programme has used for the
// configured number of days. prefetchImages marks every image the guide
// uses by updating its modification time. A run that references no images
// at all, for example because the schedule download failed, deletes nothing.
func deleteUnusedImages(used int) {
	days := Config.Options.Images.Cleanup
	if days <= 0 {
		return
	}
	if used == 0 {
		logger.Warn("No images used by the guide, skipping image cleanup")
		return
	}

	folder := imageFolder()
	entries, err := os.ReadDir(folder)
	if err != nil {
		logger.Error("unable to read the image folder", "error", err)
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	deleted := 0
	for _, e := range entries {
		if !e.Type().IsRegular() || !imageExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(folder, e.Name())); err != nil {
			logger.Warn("unable to delete unused image", "file", e.Name(), "error", err)
			continue
		}
		deleted++
	}
	logger.Info("Deleted unused images", "deleted", deleted, "days", days)
}
