package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

const (
	tmdbURL           = "https://api.themoviedb.org/3/%s"
	tmdbImageBase     = "https://image.tmdb.org/t/p"
	defaultPosterSize = "w500" // sharper default for Plex posters
)

// posterURL builds a full TMDb image URL from a path and size.
// If size is empty, defaultPosterSize is used.
func posterURL(path, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = defaultPosterSize
	}
	path = strings.TrimPrefix(path, "/")
	return fmt.Sprintf("%s/%s/%s", tmdbImageBase, size, path)
}

type Results struct {
	TmdbResults []TmdbResults `json:"results"`
}

type TmdbResults struct {
	BackdropPath     string  `json:"backdrop_path"`
	ID               int     `json:"id"`
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"original_title"`
	Overview         string  `json:"overview"`
	PosterPath       string  `json:"poster_path"`
	MediaType        string  `json:"media_type"`
	Adult            bool    `json:"adult"`
	OriginalLanguage string  `json:"original_language"`
	GenreIds         []int   `json:"genre_ids"`
	Popularity       float64 `json:"popularity"`
	ReleaseDate      string  `json:"release_date"`
	Video            bool    `json:"video"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
}

type ShowDetails struct {
	Adult            bool          `json:"adult"`
	BackdropPath     interface{}   `json:"backdrop_path"`
	CreatedBy        []interface{} `json:"created_by"`
	EpisodeRunTime   []interface{} `json:"episode_run_time"`
	FirstAirDate     string        `json:"first_air_date"`
	Genres           []interface{} `json:"genres"`
	Homepage         string        `json:"homepage"`
	ID               int           `json:"id"`
	InProduction     bool          `json:"in_production"`
	Languages        []string      `json:"languages"`
	LastAirDate      interface{}   `json:"last_air_date"`
	LastEpisodeToAir interface{}   `json:"last_episode_to_air"`
	Name             string        `json:"name"`
	NextEpisodeToAir interface{}   `json:"next_episode_to_air"`
	Networks         []struct {
		ID            int    `json:"id"`
		LogoPath      string `json:"logo_path"`
		Name          string `json:"name"`
		OriginCountry string `json:"origin_country"`
	} `json:"networks"`
	NumberOfEpisodes    int           `json:"number_of_episodes"`
	NumberOfSeasons     int           `json:"number_of_seasons"`
	OriginCountry       []interface{} `json:"origin_country"`
	OriginalLanguage    string        `json:"original_language"`
	OriginalName        string        `json:"original_name"`
	Overview            string        `json:"overview"`
	Popularity          float64       `json:"popularity"`
	PosterPath          interface{}   `json:"poster_path"`
	ProductionCompanies []interface{} `json:"production_companies"`
	ProductionCountries []struct {
		Iso31661 string `json:"iso_3166_1"`
		Name     string `json:"name"`
	} `json:"production_countries"`
	Seasons         []interface{} `json:"seasons"`
	SpokenLanguages []struct {
		EnglishName string `json:"english_name"`
		Iso6391     string `json:"iso_639_1"`
		Name        string `json:"name"`
	} `json:"spoken_languages"`
	Status      string `json:"status"`
	Tagline     string `json:"tagline"`
	Type        string `json:"type"`
	VoteAverage int    `json:"vote_average"`
	VoteCount   int    `json:"vote_count"`
}

// https://api.themoviedb.org/3/search/multi?query=two%20towers&include_adult=false&language=en-US&page=1
func SearchItem(logger *slog.Logger, searchTerm, mediaType, tmdbApiKey, imageCacheFile string) (string, error) {
	// 1) Clean search term
	searchTerm = strings.ReplaceAll(searchTerm, "ᴺᵉʷ", "")
	searchTerm = strings.ReplaceAll(searchTerm, "ᴸᶦᵛᵉ", "")
	searchTerm = strings.TrimSpace(searchTerm)

	// 2) Endpoint by media type
	var tmdbUrl string
	switch mediaType {
	case "SH", "EP":
		tmdbUrl = fmt.Sprintf(tmdbURL, "search/tv")
		mediaType = "SH"
	case "MV":
		tmdbUrl = fmt.Sprintf(tmdbURL, "search/movie")
		mediaType = "MV"
	default:
		tmdbUrl = fmt.Sprintf(tmdbURL, "search/multi")
		mediaType = "default"
	}

	// 3) Cache hit?
	if cachedPath, err := getImageURL(searchTerm+"-"+mediaType, imageCacheFile); err != nil {
		return "", fmt.Errorf("error checking cache: %w", err)
	} else if cachedPath != "" {
		return posterURL(cachedPath, ""), nil // default w500
	}

	// 4) Remote request
	token := "Bearer " + tmdbApiKey
	req, err := http.NewRequest(http.MethodGet, tmdbUrl, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	q := req.URL.Query()
	q.Add("query", searchTerm)
	q.Add("language", "en")
	q.Add("page", "1")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", token)
	req.Header.Set("accept", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", fmt.Errorf("error making TMDB request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tmdb response was non-200: %v", resp.Status)
	}

	var r Results
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("error decoding TMDB response: %w", err)
	}
	if len(r.TmdbResults) == 0 {
		return "", nil
	}

	posterPath := r.TmdbResults[0].PosterPath
	if posterPath == "" {
		return "", nil
	}

	// 5) Cache the *path* (not full URL)
	if err := addImageToCache(searchTerm+"-"+mediaType, posterPath, imageCacheFile); err != nil {
		logger.Error("error adding to cache", "error", err)
	}

	// 6) Return full URL with default w500 size
	return posterURL(posterPath, ""), nil
}

func getImageURL(name, cacheFile string) (string, error) {
	f, err := os.Open(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("error opening image cache file: %w", err)
	}
	defer f.Close()

	var cache []map[string]string
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cache); err != nil {
		if err != io.EOF {
			return "", fmt.Errorf("error decoding JSON: %w", err)
		}
	}

	for _, entry := range cache {
		if entry["name"] == name {
			return entry["url"], nil
		}
	}
	return "", nil
}

func addImageToCache(name, url, cacheFile string) error {
	entry := map[string]string{
		"name": name,
		"url":  url,
	}

	f, err := os.OpenFile(cacheFile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("error opening image cache file: %w", err)
	}
	defer f.Close()

	var cache []map[string]string
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cache); err != nil && err != io.EOF {
		return fmt.Errorf("error decoding JSON: %w", err)
	}

	// Avoid duplicates
	for _, e := range cache {
		if e["name"] == name && e["url"] == url {
			return nil
		}
	}
	cache = append(cache, entry)

	// Seek to start & truncate to avoid trailing bytes from previous content
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("error seeking to beginning of file: %w", err)
	}
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("error truncating file: %w", err)
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(cache); err != nil {
		return fmt.Errorf("error encoding JSON: %w", err)
	}
	return nil
}
