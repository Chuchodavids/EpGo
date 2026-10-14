package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestReliabilityRenewalDoesNotCommitStagedFetch(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	Cache.ImageLimitUntil = time.Now().Add(time.Hour).Unix()
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	capUntil := Cache.ImageLimitUntil
	oldGuide := "<tv><!-- committed --></tv>"
	os.WriteFile(Config.Files.XMLTV, []byte(oldGuide), 0644)
	metadataCalls, logins := 0, 0
	http.DefaultTransport = imageTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/20141201/status":
			return syntheticResponse(200, `{"lineups":[{"lineup":"TEST"}]}`), nil
		case "/20141201/lineups/TEST":
			return syntheticResponse(200, `{"stations":[{"stationID":"1","name":"Synthetic"}]}`), nil
		case "/20141201/schedules":
			return syntheticResponse(200, fmt.Sprintf(`[{"stationID":"1","programs":[{"programID":"EP000000010001","md5":"new","airDateTime":"%s","duration":60}],"metadata":{"startDate":"%s"}}]`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format("2006-01-02"))), nil
		case "/20141201/programs":
			return syntheticResponse(200, `[{"programID":"EP000000010001","md5":"new","titles":[{"title120":"Synthetic new"}]}]`), nil
		case "/20141201/metadata/programs/":
			metadataCalls++
			if Cache.Program["EP000000010001"].Md5 != "new" {
				t.Error("renewal was not tested after staging new program data")
			}
			if metadataCalls == 1 {
				return syntheticResponse(403, `{"code":4006}`), nil
			}
			if r.Header.Get("Token") != "renewed-synthetic" {
				t.Error("request did not use renewed token")
			}
			disk := diskCache(t)
			if disk.Program["EP000000010001"].Md5 != "old" || disk.Schedule["1"][0].Md5 != "old" {
				t.Error("token renewal prematurely committed staged guide data")
			}
			return syntheticResponse(200, `{"code":3000}`), nil
		case "/20141201/token":
			logins++
			return syntheticResponse(200, fmt.Sprintf(`{"code":0,"token":"renewed-synthetic","tokenExpires":%d}`, time.Now().Add(24*time.Hour).Unix())), nil
		default:
			return nil, fmt.Errorf("unexpected local request")
		}
	})
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatalf("later provider stop discarded usable staged data: %v", err)
	}
	disk := diskCache(t)
	if disk.Program["EP000000010001"].Md5 != "new" || disk.Schedule["1"][0].Md5 != "new" {
		t.Fatal("validated partial publication did not commit its data")
	}
	if disk.Token != "renewed-synthetic" || disk.ImageLimitUntil != capUntil {
		t.Fatal("renewal lost token/cap state")
	}
	if logins != 1 || metadataCalls != 2 {
		t.Fatalf("logins=%d metadata=%d", logins, metadataCalls)
	}
	guide, _ := os.ReadFile(Config.Files.XMLTV)
	if string(guide) == oldGuide || !strings.Contains(string(guide), "Synthetic new") {
		t.Fatal("validated partial publication lost fresh programme data")
	}
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

func syntheticImages(t *testing.T) {
	t.Helper()
	Config.Options.Images.Download, Config.Options.Images.Typed = true, true
	Config.Options.Images.Cleanup = 14
	os.MkdirAll(Config.Options.Images.Path, 0755)
	Cache.Metadata["EP000000010001"] = EPGoCache{Data: []Data{{URI: "selected.jpg", Category: "Poster Art", Aspect: "16x9", Width: 640, Height: 360}}}
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Config.Save(); err != nil {
		t.Fatal(err)
	}
}

func oldImage(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(Config.Options.Images.Path, name)
	if err := os.WriteFile(path, []byte("synthetic-image"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReliabilityCleanupPublicationBoundary(t *testing.T) {
	for _, scenario := range []string{"disabled", "write-error", "success"} {
		t.Run(scenario, func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			syntheticImages(t)
			old := oldImage(t, "previous-guide.jpg")
			selected := oldImage(t, "selected.jpg")
			if scenario == "disabled" {
				Config.Options.Images.Download = false
				Config.Save()
			}
			if scenario == "write-error" {
				os.Mkdir(Config.Files.XMLTV, 0755)
			}
			err := CreateXMLTV(filename)
			if (err != nil) != (scenario == "write-error") {
				t.Fatalf("unexpected result %v", err)
			}
			_, err = os.Stat(old)
			if (err == nil) != (scenario != "success") {
				t.Fatalf("previous image cleanup boundary: %v", err)
			}
			info, err := os.Stat(selected)
			if err != nil {
				t.Fatal("selected image removed")
			}
			if scenario != "disabled" && time.Since(info.ModTime()) > time.Minute {
				t.Fatal("selected image was not retained")
			}
		})
	}
}

func TestReliabilityImageCapPersistenceAndReset(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	syntheticImages(t)
	requests := 0
	http.DefaultClient = &http.Client{Transport: imageTransport(func(*http.Request) (*http.Response, error) {
		requests++
		resp := syntheticResponse(200, `{"code":5002}`)
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	})}
	os.Mkdir(Config.Files.XMLTV, 0755) // cap must survive a later publication failure
	if err := CreateXMLTV(filename); err == nil {
		t.Fatal("expected guide write error")
	}
	if diskCache(t).ImageLimitUntil != imageLimitReset(time.Now()).Unix() || requests != 1 {
		t.Fatalf("cap not saved, requests=%d", requests)
	}
	// A fresh invocation resets run state but must honor the persisted cap.
	Cache.Open()
	if _, err := Cache.prefetchImages(); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("cap was forgotten on next invocation")
	}
	Cache.ImageLimitUntil = time.Now().Add(-time.Second).Unix()
	Cache.Save()
	http.DefaultClient.Transport = imageTransport(func(*http.Request) (*http.Response, error) {
		requests++
		resp := syntheticResponse(200, "synthetic-image")
		resp.Header.Set("Content-Type", "image/jpeg")
		return resp, nil
	})
	if _, err := Cache.prefetchImages(); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || imagesStopped() {
		t.Fatalf("expired cap/run state did not reset: requests=%d", requests)
	}
}

func TestReliabilityImageCapWorkersAndSaveFailure(t *testing.T) {
	reliabilityConfig(t)
	syntheticCache(t)
	syntheticImages(t)
	for i := 0; i < imageDownloadWorkers; i++ {
		id := fmt.Sprintf("EP%012d", i+10)
		Cache.Schedule["1"] = append(Cache.Schedule["1"], EPGoCache{ProgramID: id})
		Cache.Metadata[id] = EPGoCache{Data: []Data{{URI: fmt.Sprintf("worker%d.jpg", i), Category: "Poster Art", Aspect: "16x9", Width: 640, Height: 360}}}
	}
	// First workers are all in flight when the cap arrives; serialize saves under
	// imageMu and report a failed persistence attempt, rather than return success.
	Config.Files.Cache = filepath.Join(t.TempDir(), "absent", "cache.json")
	var mu sync.Mutex
	requests := 0
	gate := make(chan struct{})
	http.DefaultClient = &http.Client{Transport: imageTransport(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		if requests == imageDownloadWorkers {
			close(gate)
		}
		mu.Unlock()
		<-gate
		resp := syntheticResponse(200, `{"code":5002}`)
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	})}
	if _, err := Cache.prefetchImages(); err == nil {
		t.Fatal("cap persistence error swallowed")
	}
	if requests != imageDownloadWorkers || !imagesStopped() {
		t.Fatalf("requests=%d", requests)
	}
}

func TestReliabilityScheduleDocumentedShapes(t *testing.T) {
	reliabilityConfig(t)
	now := time.Now().UTC()
	today, tomorrow := now.Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02")
	good := func(station, date string) string {
		return fmt.Sprintf(`{"stationID":%q,"programs":[{"programID":"SH000000010000","md5":"synthetic-md5","airDateTime":%q,"duration":86400}],"metadata":{"startDate":%q,"code":0}}`, station, date+"T00:00:00Z", date)
	}
	rangeEnd := fmt.Sprintf(`{"stationID":"1","code":7020,"requestedDate":%q,"minDate":%q,"maxDate":%q}`, tomorrow, today, today)
	requested := []channel{{ID: "1", Date: []string{today, tomorrow}}, {ID: "2", Date: []string{today, tomorrow}}}
	body := "[" + good("1", today) + "," + rangeEnd + "," + good("2", today) + "," + good("2", tomorrow) + "]"
	if good, rangeEnds, err := selectSchedules([]byte(body), requested); err != nil || len(good) != 3 || len(rangeEnds) != 1 {
		t.Fatalf("good=%d rangeEnds=%d err=%v", len(good), len(rangeEnds), err)
	}
	// Embedded SDSchedule must expose both stationID and programs through JSON.
	for _, body := range []string{
		`[{"stationID":"1","programs":[],"metadata":{"startDate":"` + today + `"}}]`,
		`[{"stationID":"1","code":7100,"retryTime":"synthetic"}]`,
		`[{"stationID":"1","programs":[],"metadata":{"code":2201,"startDate":"` + today + `"}}]`,
		`[{"stationID":"1","code":7020,"requestedDate":"` + today + `","maxDate":"2000-01-01"}]`,
		"[" + good("1", today) + "," + good("1", today) + "]",
		`[]`,
	} {
		if good, rangeEnds, err := selectSchedules([]byte(body), []channel{{ID: "1", Date: []string{today}}}); err != nil || len(good) != 0 || len(rangeEnds) != 0 {
			t.Fatalf("invalid schedule accepted or escalated to global failure: %s (err=%v)", body, err)
		}
	}
}

func TestReliabilityMetadataIdentityAcrossRefreshAndAirings(t *testing.T) {
	reliabilityConfig(t)
	syntheticCache(t)
	id := "EP000000010001"
	oldIdentity := Cache.GetEpisodeNum(id, nil)[0]
	Cache.Schedule["1"] = append(Cache.Schedule["1"], Cache.Schedule["1"][0])
	Cache.Schedule["1"][1].AirDateTime = Cache.Schedule["1"][0].AirDateTime.Add(time.Hour)
	for i := range Cache.Schedule["1"] {
		Cache.Schedule["1"][i].Md5 = "new"
	}
	if ids := Cache.GetRequiredProgramIDs(); len(ids) != 1 || ids[0] != id {
		t.Fatalf("repeat refresh not deduplicated: %v", ids)
	}
	body := []byte(`[{"programID":"EP000000010001","md5":"new","titles":[{"title120":"Synthetic series"}],"episodeTitle150":"Synthetic episode","descriptions":{"description1000":[{"description":"Synthetic long description","descriptionLanguage":"en"}]},"metadata":[{"Gracenote":{"season":2,"episode":3}},{"TVMaze":{"season":9,"episode":99}}]}]`)
	if good, _, err := selectPrograms(body, []string{id}, false); err != nil || len(good) != 1 {
		t.Fatalf("good=%d err=%v", len(good), err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	Cache.AddProgram(&body, &wg)
	wg.Wait()
	if len(Cache.GetRequiredProgramIDs()) != 0 {
		t.Fatal("refreshed programme still requested")
	}
	programmes := getProgram(Cache.Channel["1"])
	if len(programmes) != 2 || programmes[0].Start == programmes[1].Start {
		t.Fatal("repeated airings lost")
	}
	for _, p := range programmes {
		if p.SubTitle == nil || p.SubTitle.Value != "Synthetic episode" || len(p.Desc) != 1 || p.Desc[0].Value != "Synthetic long description" {
			t.Fatal("metadata semantics lost")
		}
		if len(p.EpisodeNums) < 3 || p.EpisodeNums[0] != oldIdentity || p.EpisodeNums[1].System != "xmltv_ns" || p.EpisodeNums[1].Value != "1.2." || p.EpisodeNums[2].Value != "S2 E3" {
			t.Fatalf("identity/order/numbering: %+v", p.EpisodeNums)
		}
		out, err := xml.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Index(string(out), `system="dd_progid"`) > strings.Index(string(out), `system="xmltv_ns"`) {
			t.Fatal("TVH would overwrite true episode with dd ordinal")
		}
	}
	var decoded []SDProgram
	if err := json.Unmarshal(body, &decoded); err != nil || len(decoded) != 1 {
		t.Fatal(err)
	}
}
