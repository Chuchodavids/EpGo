package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func syntheticResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func diskCache(t *testing.T) *cache {
	t.Helper()
	var c cache
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	return &c
}

func TestReliabilityTransientRetries(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			reliabilityConfig(t)
			var sd SD
			sd.Init()
			sd.Req.URL, sd.Req.Type, sd.Req.Call = "http://synthetic.invalid/status", "GET", "status"
			calls := 0
			var sleeps []time.Duration
			sd.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
			sd.Client = &http.Client{Transport: imageTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if recover && calls == 3 {
					return syntheticResponse(200, `{"code":0}`), nil
				}
				return syntheticResponse(503, `unavailable`), nil
			})}
			err := sd.Connect()
			if (err == nil) != recover || calls != 3 || len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != 2*time.Second {
				t.Fatalf("err=%v calls=%d waits=%v", err, calls, sleeps)
			}
		})
	}
}

func TestReliabilityExpiredTokenRecoveryBounded(t *testing.T) {
	reliabilityConfig(t)
	syntheticCache(t)
	var sd SD
	sd.Init()
	logins, requests := 0, 0
	sd.Client = &http.Client{Transport: imageTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			logins++
			return syntheticResponse(200, fmt.Sprintf(`{"token":"synthetic-fresh","tokenExpires":%d}`, time.Now().Add(24*time.Hour).Unix())), nil
		}
		requests++
		return syntheticResponse(403, `{"code":4006}`), nil
	})}
	if err := sd.Status(); err == nil {
		t.Fatal("second expiry accepted")
	}
	if logins != 1 || requests != 2 {
		t.Fatalf("logins=%d requests=%d", logins, requests)
	}
}

func TestReliabilityCacheFailurePreservation(t *testing.T) {
	reliabilityConfig(t)
	syntheticCache(t)
	for _, bad := range []string{`null`, `{"Program":{"bad":`} {
		os.WriteFile(Config.Files.Cache, []byte(bad), 0600)
		if err := Cache.Open(); err == nil {
			t.Fatal("invalid cache accepted")
		}
		if Cache.Program["EP000000010001"].Md5 != "old" {
			t.Fatal("bad decode mutated memory")
		}
	}
	Config.Files.Cache = t.TempDir()
	if err := Cache.Save(); err == nil {
		t.Fatal("save error swallowed")
	}
	if err := Cache.CleanUp(); err == nil {
		t.Fatal("cleanup save error swallowed")
	}
	if err := Cache.saveToken(); err == nil {
		t.Fatal("renewal read error swallowed")
	}
}

func TestReliabilityConfigurePropagatesCacheReadError(t *testing.T) {
	filename := reliabilityConfig(t)
	if err := os.Mkdir(Config.Files.Cache, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Configure(filename); err == nil {
		t.Fatal("configure continued after cache read error")
	}
}

func TestReliabilityAtomicWriteFailure(t *testing.T) {
	name := filepath.Join(t.TempDir(), "guide.xml")
	os.WriteFile(name, []byte("previous"), 0600)
	sentinel := errors.New("synthetic serialization/write failure")
	err := writeAtomic(name, 0644, func(w io.Writer) error { io.WriteString(w, "partial"); return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(name)
	if string(b) != "previous" {
		t.Fatal("partial output published")
	}
	entries, _ := os.ReadDir(filepath.Dir(name))
	if len(entries) != 1 {
		t.Fatal("error leaked temporary file")
	}
}
