package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the real menu and credential input, without a TTY or provider traffic.
func configureInput(t *testing.T, filename, input string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old; f.Close() }()
	return Configure(filename)
}

func TestReliabilityConfigureAuthenticationRecovery(t *testing.T) {
	for _, scenario := range []struct {
		name, password, input string
		logins, statuses      int
	}{
		{"saved-bad-account", "wrong", "5\n1\nsynthetic\ncorrect\n5\n0\n", 2, 2},
		{"bad-account-edit", "correct", "1\nsynthetic\nwrong\n5\n1\nsynthetic\ncorrect\n5\n0\n", 3, 3},
		{"empty-account-exit", "", "0\n", 0, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			filename := reliabilityConfig(t)
			if scenario.password != "" {
				Config.Account.Username = "synthetic"
				Config.Account.Password = SHA1(scenario.password)
			}
			if err := Config.Save(); err != nil {
				t.Fatal(err)
			}
			logins, statuses, schedules := 0, 0, 0
			http.DefaultTransport = imageTransport(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/20141201/token":
					logins++
					var account struct{ Username, Password string }
					if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
						return nil, err
					}
					if account.Username != "synthetic" || account.Password != SHA1("correct") {
						return syntheticResponse(200, `{"code":4003}`), nil
					}
					return syntheticResponse(200, fmt.Sprintf(`{"token":"corrected-synthetic","tokenExpires":%d}`, time.Now().Add(24*time.Hour).Unix())), nil
				case "/20141201/status":
					statuses++
					return syntheticResponse(200, `{"lineups":[{"lineup":"TEST"}]}`), nil
				case "/20141201/lineups/TEST":
					return syntheticResponse(200, `{"stations":[{"stationID":"1","name":"Synthetic"}]}`), nil
				case "/20141201/schedules":
					schedules++
					return syntheticResponse(200, fmt.Sprintf(`[{"stationID":"1","programs":[{"programID":"EP000000010001","md5":"synthetic","airDateTime":%q,"duration":60}],"metadata":{"startDate":%q}}]`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format("2006-01-02"))), nil
				case "/20141201/programs":
					return syntheticResponse(200, `[{"programID":"EP000000010001","md5":"synthetic","titles":[{"title120":"Synthetic"}]}]`), nil
				case "/20141201/metadata/programs/":
					return syntheticResponse(200, `[]`), nil
				default:
					return nil, fmt.Errorf("unexpected synthetic request %s", r.URL.Path)
				}
			})
			if err := configureInput(t, filename, scenario.input); err != nil {
				t.Fatalf("interactive credential recovery/exit failed: %v", err)
			}
			if logins != scenario.logins || statuses != scenario.statuses {
				t.Fatalf("logins=%d statuses=%d; want %d/%d", logins, statuses, scenario.logins, scenario.statuses)
			}
			if scenario.password != "" {
				if schedules != 1 {
					t.Fatalf("guide generation before correction or missing afterward: %d schedule requests", schedules)
				}
				if _, err := os.Stat(Config.Files.XMLTV); err != nil {
					t.Fatalf("no guide after corrected credentials: %v", err)
				}
				if err := Config.Open(); err != nil {
					t.Fatal(err)
				}
				if Config.Account.Password != SHA1("correct") || len(Config.Station) != 1 || Config.Station[0].ID != "1" || Config.Options.Schedule != 1 {
					t.Fatal("corrected account or existing configuration was lost")
				}
			}
		})
	}
}

func TestReliabilityConfigureFailedEditInvalidatesCachedToken(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	Config.Account.Username, Config.Account.Password = "synthetic", SHA1("correct")
	if err := Config.Save(); err != nil {
		t.Fatal(err)
	}
	logins, statuses := 0, 0
	http.DefaultTransport = imageTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/20141201/token":
			logins++
			return syntheticResponse(200, `{"code":4003}`), nil
		case "/20141201/status":
			statuses++
			return syntheticResponse(200, `{}`), nil
		default:
			return nil, fmt.Errorf("unexpected synthetic request %s", r.URL.Path)
		}
	})
	if err := configureInput(t, filename, "1\nsynthetic\nwrong\n0\n"); err != nil {
		t.Fatalf("cannot exit after failed edit: %v", err)
	}
	if c := diskCache(t); c.Token != "" || c.TokenExpires != 0 || c.Program["EP000000010001"].Md5 != "old" {
		t.Fatal("failed credential edit retained old token or lost programme cache")
	}
	if err := configureInput(t, filename, "0\n"); err != nil {
		t.Fatalf("cannot reopen configuration after failed edit: %v", err)
	}
	if logins != 2 || statuses != 1 {
		t.Fatalf("old token reused after credential edit: logins=%d statuses=%d", logins, statuses)
	}
}
