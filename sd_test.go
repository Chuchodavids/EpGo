package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The API returns the existing token unless newToken is set, even when that
// token expires seconds later.
func TestLoginRequestsNewTokenWhenExistingExpiresSoon(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	Cache = cache{Token: "old", TokenExpires: time.Now().Add(20 * time.Second).Unix()}
	t.Cleanup(func() { Cache = cache{} })

	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, data)
		token, expires := "old", time.Now().Add(20*time.Second)
		if data["newToken"] == true {
			token, expires = "new", time.Now().Add(24*time.Hour)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "token": token, "tokenExpires": expires.Unix()})
	}))
	defer server.Close()

	var sd SD
	if err := sd.Init(); err != nil {
		t.Fatal(err)
	}
	sd.BaseURL = server.URL + "/"
	if err := sd.Login(); err != nil {
		t.Fatal(err)
	}

	if len(requests) != 2 || requests[0]["newToken"] != nil || requests[1]["newToken"] != true {
		t.Fatalf("token requests = %v", requests)
	}
	if sd.Token != "new" || Token != "new" || Cache.Token != "new" {
		t.Fatalf("token = %q, global %q, cache %q", sd.Token, Token, Cache.Token)
	}
}

func TestLoginKeepsCachedTokenValidForRun(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	Cache = cache{Token: "cached", TokenExpires: time.Now().Add(2 * time.Hour).Unix()}
	t.Cleanup(func() { Cache = cache{} })

	var sd SD
	if err := sd.Init(); err != nil {
		t.Fatal(err)
	}
	sd.BaseURL = "http://127.0.0.1:1/"
	if err := sd.Login(); err != nil || sd.Token != "cached" {
		t.Fatalf("token = %q, error %v", sd.Token, err)
	}
}
