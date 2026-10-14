package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func prefetchFixture(t *testing.T) *cache {
	t.Helper()
	c := readImageFixture(t, "testdata/programme-images.json")
	c.Channel = map[string]EPGoCache{"fixture": {StationID: "fixture"}}
	c.Schedule = map[string][]EPGoCache{"fixture": {}}
	// Each programme twice: shared images must be requested once.
	for i := 0; i < 2; i++ {
		for id := range c.Metadata {
			c.Schedule["fixture"] = append(c.Schedule["fixture"], EPGoCache{ProgramID: id})
		}
	}
	return c
}

func TestPrefetchImagesOncePerFile(t *testing.T) {
	imageTestConfig(t)
	c := prefetchFixture(t)
	var mu sync.Mutex
	requests := map[string]int{}
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("jpeg"))}, nil
	})
	c.prefetchImages()
	files, err := os.ReadDir(Config.Options.Images.Path)
	if err != nil || len(files) == 0 || len(files) != len(requests) {
		t.Fatalf("files=%d requests=%d err=%v", len(files), len(requests), err)
	}
	for path, n := range requests {
		if n != 1 {
			t.Fatalf("%s requested %d times", path, n)
		}
	}
	// Writing the XMLTV file afterwards must not request anything.
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request after prefetch: %s", r.URL.Path)
		return nil, fmt.Errorf("network disabled")
	})
	for id := range c.Metadata {
		c.GetIcon(id)
		c.GetImages(id)
	}
}

func TestPrefetchImagesStopAndFailures(t *testing.T) {
	imageTestConfig(t)
	c := prefetchFixture(t)
	var mu sync.Mutex
	requests := 0
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":5002,"response":"MAX_IMAGE_DOWNLOADS"}`))}, nil
	})
	c.prefetchImages()
	if !imageDownloadsStopped || requests > imageDownloadWorkers {
		t.Fatalf("stopped=%v requests=%d", imageDownloadsStopped, requests)
	}

	imageTestConfig(t)
	requests = 0
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		mu.Unlock()
		return &http.Response{StatusCode: 404, Status: "404 Not Found", Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	})
	c.prefetchImages()
	unique := requests
	for id := range c.Metadata {
		c.GetIcon(id)
		c.GetImages(id)
	}
	if imageDownloadsStopped || unique == 0 || requests != unique {
		t.Fatalf("failed images requested again: stopped=%v first=%d total=%d", imageDownloadsStopped, unique, requests)
	}
}

func TestImageLimitRemembered(t *testing.T) {
	imageTestConfig(t)
	c := prefetchFixture(t)
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":5002,"response":"MAX_IMAGE_DOWNLOADS"}`))}, nil
	})
	c.prefetchImages()
	if want := imageLimitReset(time.Now()).Unix(); Cache.ImageLimitUntil != want {
		t.Fatalf("limit until %d, want %d", Cache.ImageLimitUntil, want)
	}

	// A later run on the same day must not request any image.
	imageDownloadsStopped = false
	imageFailures = map[string]error{}
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("image requested before the limit reset: %s", r.URL.Path)
		return nil, fmt.Errorf("network disabled")
	})
	c.prefetchImages()
	if !imageDownloadsStopped {
		t.Fatal("image downloads not stopped")
	}
}

func TestImageLimitReset(t *testing.T) {
	vienna := time.FixedZone("CEST", 2*3600)
	for now, want := range map[time.Time]string{
		time.Date(2026, 10, 1, 9, 12, 0, 0, vienna): "2026-10-02T00:00:00Z",
		time.Date(2026, 10, 2, 1, 30, 0, 0, vienna): "2026-10-02T00:00:00Z",
		time.Date(2026, 10, 2, 2, 30, 0, 0, vienna): "2026-10-03T00:00:00Z",
	} {
		if got := imageLimitReset(now).Format(time.RFC3339); got != want {
			t.Errorf("%s: got %s, want %s", now, got, want)
		}
	}
}
