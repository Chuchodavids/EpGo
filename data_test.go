package main

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	testLineupJSON = `{"map":[{"stationID":"100","channel":"5"}],` +
		`"stations":[{"stationID":"100","name":"Test One","callsign":"TST1"}],` +
		`"metadata":{"lineup":"L1"}}`
	testScheduleJSON = `[{"stationID":"100","programs":[` +
		`{"programID":"EP000000010002","airDateTime":"2026-11-08T20:00:00Z","duration":1800,"md5":"m1"}]}]`
	testProgramsJSON = `[{"programID":"EP000000010002","md5":"m1","titles":[{"title120":"Some Show"}]}]`
)

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeSD serves the Schedules Direct endpoints GetData uses. Each handler can
// be replaced to simulate a failure.
type fakeSD struct {
	lineup, schedule http.HandlerFunc
}

func newFakeSD(t *testing.T) *fakeSD {
	return &fakeSD{
		lineup: func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(testLineupJSON))
		},
		schedule: func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(testScheduleJSON))
		},
	}
}

func (f *fakeSD) start(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/lineups/L1", func(w http.ResponseWriter, r *http.Request) { f.lineup(w, r) })
	mux.HandleFunc("/schedules", func(w http.ResponseWriter, r *http.Request) { f.schedule(w, r) })
	mux.HandleFunc("/programs", func(w http.ResponseWriter, r *http.Request) {
		w.Write(gzipped(t, testProgramsJSON))
	})
	mux.HandleFunc("/metadata/programs/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(gzipped(t, `[]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// setupGetData points the SD client at srv and writes a cache file with
// known content, returning its path and content.
func setupGetData(t *testing.T, srv *httptest.Server) (*SD, string, []byte) {
	t.Helper()
	resetState(t)

	cacheFile := filepath.Join(t.TempDir(), "cache.json")
	previous := []byte(`{"Channel":{},"Program":{"OLD":{"md5":"x"}},"Metadata":{},"Schedule":{}}`)
	if err := os.WriteFile(cacheFile, previous, 0644); err != nil {
		t.Fatal(err)
	}
	Config.Files.Cache = cacheFile
	Config.Options.Schedule = 1
	Config.Station = []channel{{Name: "Test One", ID: "100", Lineup: "L1"}}

	var sd SD
	if err := sd.Init(); err != nil {
		t.Fatal(err)
	}
	sd.BaseURL = srv.URL + "/"
	sd.Resp.Status.Lineups = append(sd.Resp.Status.Lineups, struct {
		Lineup   string `json:"lineup"`
		Modified string `json:"modified"`
		Name     string `json:"name"`
		URI      string `json:"uri"`
	}{Lineup: "L1"})

	return &sd, cacheFile, previous
}

func TestGetDataSuccess(t *testing.T) {
	srv := newFakeSD(t).start(t)
	sd, _, _ := setupGetData(t, srv)

	if err := sd.GetData(); err != nil {
		t.Fatalf("GetData() error = %v", err)
	}
	if n := len(Cache.Schedule["100"]); n != 1 {
		t.Errorf("schedule has %d airings, want 1", n)
	}
	if p, ok := Cache.Program["EP000000010002"]; !ok || p.Md5 != "m1" {
		t.Errorf("program = %+v (cached %v), want md5 m1", p, ok)
	}
	if Cache.Channel["100"].Lcn != "5" {
		t.Errorf("channel LCN = %q, want 5", Cache.Channel["100"].Lcn)
	}
}

// A failed or empty download must return an error and leave the cache file
// as it was, so the previous XMLTV file is kept and nothing is cleaned up.
func TestGetDataFailureKeepsCache(t *testing.T) {
	fail := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":5000,"message":"boom"}`, http.StatusInternalServerError)
	}

	tests := []struct {
		name  string
		setup func(f *fakeSD)
	}{
		{"lineup error", func(f *fakeSD) { f.lineup = fail }},
		{"schedule error", func(f *fakeSD) { f.schedule = fail }},
		{"empty schedule", func(f *fakeSD) {
			f.schedule = func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) }
		}},
		{"malformed schedule", func(f *fakeSD) {
			f.schedule = func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"code":1}`)) }
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeSD(t)
			tt.setup(f)
			sd, cacheFile, previous := setupGetData(t, f.start(t))

			if err := sd.GetData(); err == nil {
				t.Fatal("GetData() error = nil, want an error")
			}

			got, err := os.ReadFile(cacheFile)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, previous) {
				t.Errorf("cache file was modified:\n%s", got)
			}
		})
	}
}

// A retry in the same process (e.g. from the configure menu) must not append
// duplicate airings to a schedule left over from a failed attempt.
func TestGetDataRetryDoesNotDuplicate(t *testing.T) {
	srv := newFakeSD(t).start(t)
	sd, _, _ := setupGetData(t, srv)

	Cache.Schedule["100"] = []EPGoCache{{ProgramID: "EP000000010002"}}

	if err := sd.GetData(); err != nil {
		t.Fatalf("GetData() error = %v", err)
	}
	if n := len(Cache.Schedule["100"]); n != 1 {
		t.Errorf("schedule has %d airings after retry, want 1", n)
	}
}
