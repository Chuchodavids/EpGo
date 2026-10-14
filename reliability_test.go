package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestReliabilityNoUsableDataDoesNotPublish(t *testing.T) {
	for _, fail := range []string{"lineup", "schedule", "program", "partial-schedule", "result-program"} {
		t.Run(fail, func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			if fail == "program" || fail == "result-program" {
				Cache.Program = make(map[string]EPGoCache)
				if err := Cache.Save(); err != nil {
					t.Fatal(err)
				}
			}
			oldGuide := "<tv><!-- good guide --></tv>"
			os.WriteFile(Config.Files.XMLTV, []byte(oldGuide), 0644)
			http.DefaultTransport = imageTransport(func(r *http.Request) (*http.Response, error) {
				body := `{}`
				status := 200
				switch r.URL.Path {
				case "/20141201/status":
					body = `{"code":0,"lineups":[{"lineup":"TEST"}]}`
				case "/20141201/lineups/TEST":
					body = `{"stations":[{"stationID":"1","name":"Synthetic"}]}`
					if fail == "lineup" {
						body = `{"code":3000}`
					}
				case "/20141201/schedules":
					body = fmt.Sprintf(`[{"stationID":"1","programs":[{"programID":"EP000000010001","md5":"new","airDateTime":"%s","duration":60}],"metadata":{"startDate":"%s"}}]`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format("2006-01-02"))
					if fail == "schedule" {
						body = `{"code":4001}`
					}
					if fail == "partial-schedule" {
						body = `[]`
					}
				case "/20141201/programs":
					body = `[{"programID":"EP000000010001","md5":"new","titles":[{"title120":"New synthetic"}]}]`
					if fail == "program" {
						status = 400
						body = `{"code":4007}`
					}
					if fail == "result-program" {
						body = `[{"programID":"EP000000010001","code":4001}]`
					}
				case "/20141201/metadata/programs/":
					body = `[]`
				default:
					return nil, fmt.Errorf("unexpected synthetic request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var sd SD
			if err := sd.Update(filename); err == nil {
				t.Error("global failure/no fresh data reported success")
			}
			guide, _ := os.ReadFile(Config.Files.XMLTV)
			if string(guide) != oldGuide {
				t.Error("global failure/no fresh data replaced good guide")
			}
			var disk cache
			data, _ := os.ReadFile(Config.Files.Cache)
			if err := json.Unmarshal(data, &disk); err != nil {
				t.Fatal(err)
			}
			wantMD5 := "old"
			if fail == "program" || fail == "result-program" {
				wantMD5 = ""
			}
			if disk.Program["EP000000010001"].Md5 != wantMD5 {
				t.Error("global failure/no fresh data replaced good program cache")
			}
		})
	}
}

func TestReliabilityLoginRejectsResultErrors(t *testing.T) {
	for _, response := range []string{`{"code":4003}`, `{"code":3000}`, `{"code":0,"token":"","tokenExpires":9999999999}`, `{"code":0,"token":"expired","tokenExpires":1}`} {
		t.Run(response, func(t *testing.T) {
			reliabilityConfig(t)
			n := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n++; io.WriteString(w, response) }))
			defer s.Close()
			// Use a private real transport only to the local server.
			http.DefaultTransport = &http.Transport{}
			var sd SD
			sd.Init()
			sd.BaseURL = s.URL + "/"
			if err := sd.Login(); err == nil {
				t.Error("unusable token/API failure reported success")
			}
			if n > 2 {
				t.Errorf("unbounded retry: %d", n)
			}
			if strings.Contains(response, `"code":4003`) && n != 1 {
				t.Errorf("repeated invalid credentials %d times", n)
			}
		})
	}
}

func TestReliabilityProgramIdentity(t *testing.T) {
	reliabilityConfig(t)
	var c cache
	json.Unmarshal([]byte(`{"Program":{"EP000000010001":{"metadata":[{"Gracenote":{"season":2,"episode":3}},{"TVMaze":{"season":2,"episode":3}}]},"MV000000020000":{},"SH000000030000":{}}}`), &c)
	for id, want := range map[string]string{"EP000000010001": "EP00000001.0001", "MV000000020000": "MV00000002.0000", "SH000000030000": "SH00000003.0000"} {
		nums := c.GetEpisodeNum(id, nil)
		found := false
		for _, num := range nums {
			if num.System == "dd_progid" && num.Value == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing stable dd_progid %s: %+v", id, want, nums)
		}
	}
}
