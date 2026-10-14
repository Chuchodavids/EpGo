package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Only synthetic data and local transports. No test may contact SD.
func reliabilityConfig(t *testing.T) string {
	t.Helper()
	oldConfig := Config
	oldTransport := http.DefaultTransport
	oldClient := http.DefaultClient
	t.Cleanup(func() {
		Config = oldConfig
		Cache = cache{}
		Token = ""
		http.DefaultTransport = oldTransport
		http.DefaultClient = oldClient
		imageDownloadsStopped = false
		imageFailures = map[string]error{}
	})
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	Config = config{}
	Cache = cache{}
	dir := t.TempDir()
	Config.File = filepath.Join(dir, "config")
	Config.Files.Cache = filepath.Join(dir, "cache.json")
	Config.Files.XMLTV = filepath.Join(dir, "guide.xml")
	Config.Options.Schedule = 1
	Config.Options.Images.Path = filepath.Join(dir, "images")
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}}
	b, err := yaml.Marshal(Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Config.File+".yaml", b, 0600); err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = imageTransport(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("external network forbidden by test")
	})
	http.DefaultClient = &http.Client{Transport: http.DefaultTransport}
	return Config.File + ".yaml"
}

func syntheticCache(t *testing.T) {
	t.Helper()
	Cache.Init()
	Cache.Token = "synthetic-token"
	Cache.TokenExpires = time.Now().Add(24 * time.Hour).Unix()
	Cache.Channel["1"] = EPGoCache{StationID: "1", Name: "Synthetic"}
	Cache.Schedule["1"] = []EPGoCache{{ProgramID: "EP000000010001", Md5: "old", AirDateTime: time.Now().UTC(), Duration: 60}}
	var p EPGoCache
	if err := json.Unmarshal([]byte(`{"md5":"old","titles":[{"title120":"Synthetic"}]}`), &p); err != nil {
		t.Fatal(err)
	}
	Cache.Program["EP000000010001"] = p
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestReliabilityCacheReadError(t *testing.T) {
	reliabilityConfig(t)
	Config.Files.Cache = t.TempDir() // EISDIR is not a cache miss.
	if err := Cache.Open(); err == nil {
		t.Fatal("cache read error was swallowed")
	}
}

func TestReliabilityCacheReaderSnapshot(t *testing.T) {
	reliabilityConfig(t)
	syntheticCache(t)
	before, _ := os.ReadFile(Config.Files.Cache)
	f, err := os.Open(Config.Files.Cache)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	Cache.Program = map[string]EPGoCache{}
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := io.ReadAll(f)
	if string(after) != string(before) {
		t.Fatal("open cache reader observed replacement bytes, not original snapshot")
	}
}

func TestReliabilityGuideReaderSnapshot(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	old := "<tv><!-- previous complete guide --></tv>"
	if err := os.WriteFile(Config.Files.XMLTV, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(Config.Files.XMLTV)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := CreateXMLTV(filename); err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	if string(b) != old {
		t.Fatal("open guide reader observed replacement bytes, not original snapshot")
	}
}

func TestReliabilityGuideWriteReturnsError(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	if err := os.Mkdir(Config.Files.XMLTV, 0755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("guide write panicked instead of returning error: %v", r)
		}
	}()
	if err := CreateXMLTV(filename); err == nil {
		t.Fatal("guide write failure was swallowed")
	}
}
