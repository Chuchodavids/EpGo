package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
