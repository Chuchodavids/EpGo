package tmdb

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSearchEndpoint(t *testing.T) {
	tests := []struct {
		name, mediaType, want string
	}{
		{"SH", "SH", "search/tv"},
		{"EP", "EP", "search/tv"},
		{"MV", "MV", "search/movie"},
		{"empty", "", "search/multi"},
		{"unknown", "XXX", "search/multi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchEndpoint(tt.mediaType)
			if got != tt.want {
				t.Errorf("searchEndpoint(%q) = %q, want %q", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestLoadCache_NonExistent(t *testing.T) {
	cache, err := loadCache("/nonexistent/path/tmdb_cache.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cache) != 0 {
		t.Errorf("expected empty cache, got %d entries", len(cache))
	}
}

func TestLoadSaveCache(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "test_cache.json")

	cache := posterCache{
		"Breaking Bad-SH":    "/poster1.jpg",
		"The Matrix-MV":      "/poster2.jpg",
	}
	if err := saveCache(cacheFile, cache); err != nil {
		t.Fatalf("saveCache: %v", err)
	}

	loaded, err := loadCache(cacheFile)
	if err != nil {
		t.Fatalf("loadCache: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 entries, got %d", len(loaded))
	}
	for k, v := range cache {
		if loaded[k] != v {
			t.Errorf("cache[%q] = %q, want %q", k, loaded[k], v)
		}
	}
}

func TestLoadCache_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(cacheFile, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	cache, err := loadCache(cacheFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cache) != 0 {
		t.Errorf("expected empty cache, got %d entries", len(cache))
	}
}

func TestSearchItem_CleansBadges(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if query == "Two and a Half Men" {
			json.NewEncoder(w).Encode(searchResponse{
				Results: []searchResult{{PosterPath: "/clean.jpg"}},
			})
			return
		}
		http.Error(w, "unexpected query", 400)
	}))
	defer ts.Close()

	origURL := tmdbURL
	tmdbURL = ts.URL + "/%s"
	defer func() { tmdbURL = origURL }()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache.json")

	url, err := SearchItem(logger, "Two and a Half Men ᴺᵉʷ", "SH", "key", cacheFile)
	if err != nil {
		t.Fatalf("SearchItem: %v", err)
	}
	expected := fmt.Sprintf(tmdbImageURL, "/clean.jpg")
	if url != expected {
		t.Errorf("url = %q, want %q", url, expected)
	}
}

func TestSearchItem_CacheHit(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache.json")

	cache := posterCache{"Seinfeld-SH": "/seinfeld.jpg"}
	if err := saveCache(cacheFile, cache); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	url, err := SearchItem(logger, "Seinfeld", "SH", "unused-key", cacheFile)
	if err != nil {
		t.Fatalf("SearchItem: %v", err)
	}
	expected := fmt.Sprintf(tmdbImageURL, "/seinfeld.jpg")
	if url != expected {
		t.Errorf("url = %q, want %q", url, expected)
	}
}

func TestSearchItem_CacheMiss(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "Breaking Bad" {
			json.NewEncoder(w).Encode(searchResponse{
				Results: []searchResult{{PosterPath: "/bb.jpg"}},
			})
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer ts.Close()

	origURL := tmdbURL
	tmdbURL = ts.URL + "/%s"
	defer func() { tmdbURL = origURL }()

	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache.json")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	url, err := SearchItem(logger, "Breaking Bad", "SH", "key", cacheFile)
	if err != nil {
		t.Fatalf("SearchItem: %v", err)
	}
	expected := fmt.Sprintf(tmdbImageURL, "/bb.jpg")
	if url != expected {
		t.Errorf("url = %q, want %q", url, expected)
	}

	// Verify it was persisted to cache.
	loaded, _ := loadCache(cacheFile)
	if loaded["Breaking Bad-SH"] != "/bb.jpg" {
		t.Error("cache miss entry was not persisted")
	}
}

func TestSearchItem_SkipsEmptyPoster(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(searchResponse{
			Results: []searchResult{
				{PosterPath: ""},
				{PosterPath: "/second.jpg"},
			},
		})
	}))
	defer ts.Close()

	origURL := tmdbURL
	tmdbURL = ts.URL + "/%s"
	defer func() { tmdbURL = origURL }()

	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache.json")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	url, err := SearchItem(logger, "Test", "MV", "key", cacheFile)
	if err != nil {
		t.Fatalf("SearchItem: %v", err)
	}
	expected := fmt.Sprintf(tmdbImageURL, "/second.jpg")
	if url != expected {
		t.Errorf("url = %q, want %q", url, expected)
	}
}

func TestSearchItem_NoResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(searchResponse{Results: nil})
	}))
	defer ts.Close()

	origURL := tmdbURL
	tmdbURL = ts.URL + "/%s"
	defer func() { tmdbURL = origURL }()

	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache.json")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	url, err := SearchItem(logger, "zzzdoesnotexistzzz", "SH", "key", cacheFile)
	if err != nil {
		t.Fatalf("SearchItem: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL for no results, got %q", url)
	}
}

func TestSearchItem_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", 500)
	}))
	defer ts.Close()

	origURL := tmdbURL
	tmdbURL = ts.URL + "/%s"
	defer func() { tmdbURL = origURL }()

	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache.json")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	_, err := SearchItem(logger, "Test", "SH", "key", cacheFile)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}
