package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	tmdbURL      = "https://api.themoviedb.org/3/%s"
	tmdbImageURL = "https://image.tmdb.org/t/p/w94_and_h141_bestv2%s"
)

const (
	badgeNew  = "ᴺᵉʷ"
	badgeLive = "ᴸᶦᵛᵉ"
)

var (
	httpClient = &http.Client{}
	cacheMu    sync.Mutex
)

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	PosterPath  string `json:"poster_path"`
	ID          int    `json:"id"`
	Title       string `json:"title"`
	MediaType   string `json:"media_type"`
	ReleaseDate string `json:"release_date"`
}

type posterCache map[string]string

// SearchItem searches TMDB for a poster image URL, using a file-based cache.
func SearchItem(logger *slog.Logger, searchTerm, mediaType, apiKey, cacheFile string) (string, error) {
	searchTerm = strings.ReplaceAll(searchTerm, badgeNew, "")
	searchTerm = strings.ReplaceAll(searchTerm, badgeLive, "")
	searchTerm = strings.TrimSpace(searchTerm)

	endpoint := searchEndpoint(mediaType)
	cacheKey := searchTerm + "-" + mediaType

	cache, err := loadCache(cacheFile)
	if err != nil {
		return "", fmt.Errorf("loading cache: %w", err)
	}

	if url, ok := cache[cacheKey]; ok {
		return fmt.Sprintf(tmdbImageURL, url), nil
	}

	posterPath, err := searchTMDB(searchTerm, endpoint, apiKey)
	if err != nil {
		return "", err
	}
	if posterPath == "" {
		return "", nil
	}

	cache[cacheKey] = posterPath
	if err := saveCache(cacheFile, cache); err != nil {
		logger.Error("error saving TMDB cache", "error", err)
	}

	return fmt.Sprintf(tmdbImageURL, posterPath), nil
}

func searchEndpoint(mediaType string) string {
	switch mediaType {
	case "SH", "EP":
		return "search/tv"
	case "MV":
		return "search/movie"
	default:
		return "search/multi"
	}
}

func searchTMDB(query, endpoint, apiKey string) (string, error) {
	url := fmt.Sprintf(tmdbURL, endpoint)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	q := req.URL.Query()
	q.Add("query", query)
	q.Add("language", "en")
	q.Add("page", "1")
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("TMDB returned %d", resp.StatusCode)
	}

	var r searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	for _, result := range r.Results {
		if result.PosterPath != "" {
			return result.PosterPath, nil
		}
	}
	return "", nil
}

func loadCache(cacheFile string) (posterCache, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	f, err := os.Open(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return posterCache{}, nil
		}
		return nil, err
	}
	defer f.Close()

	cache := posterCache{}
	if err := json.NewDecoder(f).Decode(&cache); err != nil && err != io.EOF {
		return nil, err
	}
	return cache, nil
}

func saveCache(cacheFile string, cache posterCache) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	f, err := os.OpenFile(cacheFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cache)
}
