package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// imageDownloadWorkers is the number of parallel image downloads. One request
// takes most of a second, mostly for SD's redirect, so a serial first run with
// thousands of images takes hours. SD's daily image limit still applies: the
// first stop code ends all workers.
const imageDownloadWorkers = 4

// prefetchImages downloads every image the XMLTV file will reference before it
// is written. Writing it then only finds existing files or known failures.
func (c *cache) prefetchImages() (int, error) {
	return c.prefetchImagesWithNetwork(true)
}

func (c *cache) prefetchImagesWithNetwork(allowNetwork bool) (int, error) {
	if !Config.Options.Images.Download {
		return 0, nil
	}
	imageMu.Lock()
	imageDownloadsStopped = !allowNetwork
	imageFailures = map[string]error{}
	imageStateError = nil
	imageMu.Unlock()
	seen := make(map[string]bool)
	var uris []string
	now := time.Now()
	cached := 0
	var touchErr error
	add := func(uri string) {
		name := imageFilename(uri)
		if uri == "" || seen[name] {
			return
		}
		seen[name] = true
		// Existing images are marked as used, which is what the cleanup
		// goes by, and need no download.
		if err := os.Chtimes(filepath.Join(imageFolder(), name), now, now); err == nil {
			cached++
			return
		} else if !os.IsNotExist(err) {
			touchErr = fmt.Errorf("unable to retain selected image %s: %w", name, err)
			return
		}
		uris = append(uris, uri)
	}
	for _, channel := range c.Channel {
		for _, s := range c.Schedule[channel.StationID] {
			if icon := c.selectIcon(s.ProgramID); icon != nil {
				add(icon.Src)
			}
			if Config.Options.Images.Typed {
				for _, data := range c.selectProgrammeImages(s.ProgramID) {
					add(data.URI)
				}
			}
		}
	}
	if touchErr != nil {
		return 0, touchErr
	}
	Cache.RLock()
	until := time.Unix(Cache.ImageLimitUntil, 0)
	Cache.RUnlock()
	if now.Before(until) {
		imageMu.Lock()
		imageDownloadsStopped = true
		imageMu.Unlock()
		logger.Warn("The daily image limit was reached in an earlier run, no images will be requested before it resets", "reset", until)
	}
	logger.Info("Downloading images", "images", len(uris), "cached", cached, "workers", imageDownloadWorkers)

	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done, failed := 0, 0
	for w := 0; w < imageDownloadWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uri := range jobs {
				_, err := downloadImage(uri)
				mu.Lock()
				done++
				if err != nil {
					failed++
				}
				if done%500 == 0 {
					logger.Info("Downloading images", "done", done, "failed", failed, "images", len(uris))
				}
				mu.Unlock()
			}
		}()
	}
	for _, uri := range uris {
		if imagesStopped() {
			break
		}
		jobs <- uri
	}
	close(jobs)
	wg.Wait()
	logger.Info("Images downloaded", "done", done, "failed", failed, "images", len(uris), "stopped", imagesStopped())

	imageMu.Lock()
	err := imageStateError
	imageMu.Unlock()
	return len(seen), err
}
