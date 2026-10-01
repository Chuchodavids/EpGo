package main

import "sync"

// imageDownloadWorkers is the number of parallel image downloads. One request
// takes most of a second, mostly for SD's redirect, so a serial first run with
// thousands of images takes hours. SD's daily image limit still applies: the
// first stop code ends all workers.
const imageDownloadWorkers = 4

// prefetchImages downloads every image the XMLTV file will reference before it
// is written. Writing it then only finds existing files or known failures.
func (c *cache) prefetchImages() {
	if !Config.Options.Images.Download {
		return
	}
	seen := make(map[string]bool)
	var uris []string
	add := func(uri string) {
		if name := imageFilename(uri); uri != "" && !seen[name] {
			seen[name] = true
			uris = append(uris, uri)
		}
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
	logger.Info("Downloading images", "images", len(uris), "workers", imageDownloadWorkers)

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
}
