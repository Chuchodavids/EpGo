package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Synthetic provider responses exercise the complete Update/publication path.
// reliabilityConfig installs a transport which rejects any unhandled request.
type partialSource struct {
	schedules       string
	scheduleMd5     string
	scheduleBodies  []string
	programs        string
	metadata        string
	status          map[string]int
	calls           map[string]int
	requested       []string
	programResponse func([]string) (int, string)
}

func (s *partialSource) install(t *testing.T) {
	t.Helper()
	s.calls = make(map[string]int)
	http.DefaultTransport = imageTransport(func(r *http.Request) (*http.Response, error) {
		path := strings.TrimPrefix(r.URL.Path, "/20141201/")
		s.calls[path]++
		code, body := 200, ""
		if c := s.status[path]; c != 0 {
			code = c
		}
		switch path {
		case "status":
			body = `{"lineups":[{"lineup":"TEST"}]}`
		case "lineups/TEST":
			var stations []map[string]string
			for _, ch := range Config.Station {
				stations = append(stations, map[string]string{"stationID": ch.ID, "name": "Synthetic " + ch.ID})
			}
			b, _ := json.Marshal(map[string]any{"stations": stations})
			body = string(b)
		case "schedules":
			b, _ := io.ReadAll(r.Body)
			s.scheduleBodies = append(s.scheduleBodies, string(b))
			body = s.schedules
		case "schedules/md5":
			body = s.scheduleMd5
			if body == "" {
				body = "{}"
			}
		case "programs":
			var ids []string
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Fatal(err)
			}
			s.requested = append(s.requested, ids...)
			body = s.programs
			if s.programResponse != nil {
				code, body = s.programResponse(ids)
			}
		case "metadata/programs/":
			body = s.metadata
			if body == "" {
				var ids []string
				if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
					t.Fatal(err)
				}
				var entries []map[string]any
				for _, id := range ids {
					entries = append(entries, map[string]any{"programID": id, "data": []any{}})
				}
				b, _ := json.Marshal(entries)
				body = string(b)
			}
		default:
			t.Fatalf("unexpected request %s", path)
		}
		return syntheticResponse(code, body), nil
	})
}

func partialSchedule(station string, day time.Time, programs ...string) string {
	return fmt.Sprintf(`{"stationID":%q,"programs":[%s],"metadata":{"startDate":%q}}`, station, strings.Join(programs, ","), day.Format("2006-01-02"))
}

func partialAiring(id, md5 string, start time.Time, duration int) string {
	return fmt.Sprintf(`{"programID":%q,"md5":%q,"airDateTime":%q,"duration":%d}`, id, md5, start.Format(time.RFC3339), duration)
}

func partialProgram(id, md5, title string) string {
	return fmt.Sprintf(`{"programID":%q,"md5":%q,"titles":[{"title120":%q}]}`, id, md5, title)
}

func cachePartialAiring(t *testing.T, station, id, title string, start time.Time) {
	t.Helper()
	Cache.Channel[station] = EPGoCache{StationID: station, Name: "Synthetic " + station}
	Cache.Schedule[station] = append(Cache.Schedule[station], EPGoCache{ProgramID: id, Md5: "old", AirDateTime: start, Duration: 3600})
	var program EPGoCache
	if err := json.Unmarshal([]byte(partialProgram(id, "old", title)), &program); err != nil {
		t.Fatal(err)
	}
	Cache.Program[id] = program
}

func partialGuide(t *testing.T) []Programme {
	t.Helper()
	b, err := os.ReadFile(Config.Files.XMLTV)
	if err != nil {
		t.Fatal(err)
	}
	var guide struct {
		Programs []Programme `xml:"programme"`
	}
	if err := xml.Unmarshal(b, &guide); err != nil {
		t.Fatal(err)
	}
	return guide.Programs
}

func TestPartialUpdateKeepsFailedDaysAndReplacesSuccessfulDays(t *testing.T) {
	filename := reliabilityConfig(t)
	Config.Options.Schedule = 2
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}, {ID: "2", Lineup: "TEST"}}
	if err := Config.Save(); err != nil {
		t.Fatal(err)
	}
	Cache.Init()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	tomorrow := today.AddDate(0, 0, 1)
	cachePartialAiring(t, "1", "EP000000010001", "Cancelled", today.Add(12*time.Hour))
	cachePartialAiring(t, "1", "EP000000010002", "Cached tomorrow", tomorrow.Add(12*time.Hour))
	cachePartialAiring(t, "2", "EP000000020001", "Cached station", today.Add(12*time.Hour))
	cachePartialAiring(t, "2", "EP000000020002", "Cached range end", tomorrow.Add(12*time.Hour))
	cachePartialAiring(t, "1", "EP000000090001", "Outside window", today.AddDate(0, 0, -1))
	cachePartialAiring(t, "9", "EP000000090002", "Deselected", today.Add(12*time.Hour))
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	source := &partialSource{
		schedules: "[" + partialSchedule("1", today, partialAiring("EP000000010003", "new", today.Add(13*time.Hour), 3600)) + "," +
			fmt.Sprintf(`{"stationID":"2","code":7100,"requestedDate":%q},{"stationID":"2","code":7020,"requestedDate":%q,"maxDate":%q}]`, today.Format("2006-01-02"), tomorrow.Format("2006-01-02"), today.Format("2006-01-02")),
		programs: "[" + partialProgram("EP000000010003", "new", "Fresh correction") + "]",
	}
	source.install(t)
	var logs bytes.Buffer
	logger = slog.New(slog.NewTextHandler(&logs, nil))
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatalf("healthy update held back by another day/station: %v", err)
	}
	programmes := partialGuide(t)
	titles := make(map[string]bool)
	for _, p := range programmes {
		titles[p.Title[0].Value] = true
	}
	for _, want := range []string{"Fresh correction", "Cached tomorrow", "Cached station", "Cached range end"} {
		if !titles[want] {
			t.Errorf("missing %s from guide: %v", want, titles)
		}
	}
	if len(programmes) != 4 || titles["Cancelled"] || titles["Outside window"] || titles["Deselected"] {
		t.Fatalf("stale/duplicate airings survived: %v", titles)
	}
	disk := diskCache(t)
	if len(disk.Schedule["1"]) != 2 || len(disk.Schedule["2"]) != 2 || len(disk.Channel) != 2 || len(disk.Program) != 4 {
		t.Fatalf("bounded fallback cache was discarded/not pruned: %+v", disk.Schedule)
	}
	if !strings.Contains(logs.String(), "published with gaps") || !strings.Contains(logs.String(), "cached_schedule_days=3") {
		t.Fatalf("partial publication not visible: %s", &logs)
	}
	if source.calls["schedules"] != 1 || source.calls["programs"] != 1 {
		t.Fatal("partial results caused extra provider requests")
	}
	// Simulate a new process. Normal post-publication cleanup must leave fallback
	// schedules available, and the next pass must not append duplicate airings.
	Cache = cache{}
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if len(partialGuide(t)) != 4 || source.calls["programs"] != 1 {
		t.Fatal("fallback failed after reload, or unchanged programmes were fetched again")
	}
}

func TestPartialUpdateProgrammeFailuresUseCacheOrLeaveGap(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	source := &partialSource{
		schedules: "[" + partialSchedule("1", today,
			partialAiring("EP000000010001", "new", today.Add(12*time.Hour), 3600),
			partialAiring("EP000000010002", "new", today.Add(13*time.Hour), 3600),
			partialAiring("EP000000010003", "new", today.Add(14*time.Hour), 3600)) + "]",
		programs: `[{"programID":"EP000000010001","code":6001},{"programID":"EP000000010002","code":6000},` + partialProgram("EP000000010003", "new", "Fresh title") + "]",
	}
	source.install(t)
	var logs bytes.Buffer
	logger = slog.New(slog.NewTextHandler(&logs, nil))
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	programmes := partialGuide(t)
	if len(programmes) != 2 || programmes[0].Title[0].Value != "Synthetic" || programmes[1].Title[0].Value != "Fresh title" {
		t.Fatalf("missing/failed programme suppressed healthy data or emitted placeholder: %+v", programmes)
	}
	if programmes[0].Start != today.Add(12*time.Hour).Format("20060102150405 +0000") {
		t.Fatal("cached title did not receive the fresh airtime")
	}
	if diskCache(t).Program["EP000000010001"].Md5 != "old" {
		t.Fatal("stale programme falsely marked refreshed")
	}
	if !strings.Contains(logs.String(), "cached_programs=1") || !strings.Contains(logs.String(), "missing_programs=1") {
		t.Fatalf("programme gaps not reported: %s", &logs)
	}
	// A later normal run should ask for just the two failed programme IDs.
	source.programs = "[" + partialProgram("EP000000010001", "new", "Recovered cached") + "," + partialProgram("EP000000010002", "new", "Recovered missing") + "]"
	source.requested = nil
	Cache = cache{}
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if len(partialGuide(t)) != 3 || len(source.requested) != 2 || ContainsString(source.requested, "EP000000010003") != -1 {
		t.Fatalf("failed programme data not retried selectively: %v", source.requested)
	}
}

func TestPartialUpdateWithoutCachePublishesAvailableStation(t *testing.T) {
	filename := reliabilityConfig(t)
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}, {ID: "2", Lineup: "TEST"}}
	Config.Save()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	Cache.Save()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	source := &partialSource{
		schedules: "[" + partialSchedule("1", today, partialAiring("EP000000010001", "new", today.Add(12*time.Hour), 3600)) + "]",
		programs:  "[" + partialProgram("EP000000010001", "new", "First fresh programme") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if programmes := partialGuide(t); len(programmes) != 1 || programmes[0].Channel != "1.schedulesdirect.org" {
		t.Fatalf("cold cache discarded usable partial guide: %+v", programmes)
	}
}

func TestPartialUpdateOptionalMetadataFailure(t *testing.T) {
	for _, body := range []string{`broken JSON`, `[{"programID":"EP000000010001","data":{"code":6001}}]`} {
		t.Run(body, func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			today := time.Now().UTC().Truncate(24 * time.Hour)
			source := &partialSource{
				schedules: "[" + partialSchedule("1", today, partialAiring("EP000000010001", "new", today.Add(12*time.Hour), 3600)) + "]",
				programs:  "[" + partialProgram("EP000000010001", "new", "New title without art") + "]",
				metadata:  body,
			}
			source.install(t)
			var sd SD
			if err := sd.Update(filename); err != nil {
				t.Fatalf("artwork error blocked usable schedule: %v", err)
			}
			if p := partialGuide(t); len(p) != 1 || p[0].Title[0].Value != "New title without art" {
				t.Fatal("healthy text update was lost")
			}
		})
	}
}

func TestPartialUpdateExhaustedBatchRetries(t *testing.T) {
	for _, path := range []string{"programs", "metadata/programs/", "lineups/TEST"} {
		t.Run(path, func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			today := time.Now().UTC().Truncate(24 * time.Hour)
			source := &partialSource{
				schedules: "[" + partialSchedule("1", today, partialAiring("EP000000010001", "new", today.Add(12*time.Hour), 3600)) + "]",
				programs:  "[" + partialProgram("EP000000010001", "new", "Fresh batch") + "]",
				status:    map[string]int{path: 503},
			}
			source.install(t)
			delays := 0
			sd := SD{Sleep: func(time.Duration) { delays++ }}
			if err := sd.Update(filename); err != nil {
				t.Fatalf("batch failure blocked usable data: %v", err)
			}
			want := "Fresh batch"
			if path == "programs" {
				want = "Synthetic"
			}
			if p := partialGuide(t); len(p) != 1 || p[0].Title[0].Value != want {
				t.Fatalf("wrong fallback: %+v", p)
			}
			if source.calls[path] != 3 || delays != 2 {
				t.Fatalf("partial-update retries exceeded existing bound: calls=%v delays=%d", source.calls, delays)
			}
		})
	}
}

func TestPartialUpdateInvalidDaysDoNotPoisonHealthyStation(t *testing.T) {
	filename := reliabilityConfig(t)
	Cache.Init()
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}, {ID: "2", Lineup: "TEST"}, {ID: "3", Lineup: "TEST"}}
	Config.Save()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	cachePartialAiring(t, "1", "EP000000010001", "Cached one", today.Add(12*time.Hour))
	cachePartialAiring(t, "2", "EP000000020001", "Cached two", today.Add(12*time.Hour))
	Cache.Save()
	one := partialSchedule("1", today, partialAiring("EP000000010002", "new", today.Add(12*time.Hour), 3600))
	bad := partialSchedule("1", today, `{"programID":"EP000000010002","md5":"new","airDateTime":"bad-time","duration":3600}`)
	two := partialSchedule("2", today, `{"programID":"EP000000020002","md5":"new","airDateTime":"bad-time","duration":3600}`)
	source := &partialSource{
		schedules: "[" + strings.Join([]string{one, bad, two, partialSchedule("3", today, partialAiring("EP000000030001", "new", today.Add(12*time.Hour), 3600))}, ",") + "]",
		programs:  "[" + partialProgram("EP000000030001", "new", "Healthy third") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if len(partialGuide(t)) != 3 || len(source.requested) != 1 || source.requested[0] != "EP000000030001" {
		t.Fatalf("bad partitions blocked healthy data or triggered requests for quarantined data: %v", source.requested)
	}
}

func TestPartialUpdateFreshLateEventWinsOverCachedNextDay(t *testing.T) {
	filename := reliabilityConfig(t)
	Cache.Init()
	Config.Options.Schedule = 2
	Config.Save()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	tomorrow := today.AddDate(0, 0, 1)
	cachePartialAiring(t, "1", "EP000000010001", "Superseded next day", tomorrow)
	cachePartialAiring(t, "1", "EP000000010002", "Unchanged next day", tomorrow.Add(time.Hour))
	Cache.Save()
	source := &partialSource{
		schedules: "[" + partialSchedule("1", today, partialAiring("EP000000010003", "new", today.Add(23*time.Hour), 7200)) + "]",
		programs:  "[" + partialProgram("EP000000010003", "new", "Late sports correction") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if p := partialGuide(t); len(p) != 2 || p[0].Title[0].Value != "Late sports correction" || p[1].Title[0].Value != "Unchanged next day" {
		t.Fatalf("cached overlap survived fresh correction: %+v", p)
	}
}

func TestPartialUpdateNoUsableDataKeepsPreviousGuide(t *testing.T) {
	for _, cold := range []bool{false, true} {
		t.Run(fmt.Sprint("cold=", cold), func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			if cold {
				Cache.Program = make(map[string]EPGoCache)
				Cache.Save()
			}
			source := &partialSource{schedules: `[]`, programs: `[]`}
			source.install(t)
			if err := os.WriteFile(Config.Files.XMLTV, []byte("<tv><!-- previous guide --></tv>"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(Config.Files.XMLTV)
			if err != nil {
				t.Fatal(err)
			}
			old, err := os.Stat(Config.Files.XMLTV)
			if err != nil {
				t.Fatal(err)
			}
			var sd SD
			if err := sd.Update(filename); err == nil || !strings.Contains(err.Error(), "no usable fresh") {
				t.Fatalf("empty/cached-only run should fail without republishing: %v", err)
			}
			after, _ := os.ReadFile(Config.Files.XMLTV)
			current, _ := os.Stat(Config.Files.XMLTV)
			if !bytes.Equal(before, after) || !old.ModTime().Equal(current.ModTime()) || source.calls["metadata/programs/"] != 0 {
				t.Fatal("total failure rewrote old guide or made pointless artwork requests")
			}
		})
	}
}

func TestPartialUpdateGlobalErrorsStopRequests(t *testing.T) {
	for _, code := range []int{3000, 4001, 4006, 4009, 5002, 5003, 5004, 9999, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			body := fmt.Sprintf(`[{"stationID":"1","code":%d}]`, code)
			status := make(map[string]int)
			if code == 429 {
				body, status["schedules"] = `throttled`, 429
			}
			source := &partialSource{schedules: body, status: status}
			source.install(t)
			if err := os.WriteFile(Config.Files.XMLTV, []byte("<tv><!-- previous guide --></tv>"), 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(Config.Files.XMLTV)
			var sd SD
			if err := sd.Update(filename); err == nil {
				t.Fatal("global provider failure was treated as a local gap")
			}
			after, _ := os.ReadFile(Config.Files.XMLTV)
			if !bytes.Equal(before, after) || source.calls["schedules"] != 1 || source.calls["programs"] != 0 || source.calls["metadata/programs/"] != 0 {
				t.Fatalf("requests continued after global error: %v", source.calls)
			}
		})
	}
}

func TestPartialUpdateProviderStopPublishesAlreadyFetchedData(t *testing.T) {
	for _, phase := range []string{"schedules", "programs", "metadata"} {
		t.Run(phase, func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			Config.Options.Images.Download, Config.Options.Images.Typed = true, true
			Config.Options.Images.Cleanup = 1
			if err := Config.Save(); err != nil {
				t.Fatal(err)
			}
			today := time.Now().UTC().Truncate(24 * time.Hour)
			cachePartialAiring(t, "1", "EP000000010002", "Other programme", today.Add(13*time.Hour))
			var art EPGoCache
			if err := json.Unmarshal([]byte(`{"data":[
				{"uri":"cached-poster.jpg","category":"Banner-L1","aspect":"2x3","tier":"Series","width":200,"height":300},
				{"uri":"missing-backdrop.jpg","category":"Iconic","aspect":"16x9","tier":"Series","width":300,"height":169}
			]}`), &art); err != nil {
				t.Fatal(err)
			}
			Cache.Metadata["EP000000010001"] = art
			if err := Cache.Save(); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(imageFolder(), 0700); err != nil {
				t.Fatal(err)
			}
			poster := filepath.Join(imageFolder(), imageFilename("cached-poster.jpg"))
			if err := os.WriteFile(poster, []byte("synthetic cached image"), 0600); err != nil {
				t.Fatal(err)
			}
			old := today.AddDate(0, 0, -30)
			if err := os.Chtimes(poster, old, old); err != nil {
				t.Fatal(err)
			}
			schedule := partialSchedule("1", today,
				partialAiring("EP000000010001", "new", today.Add(12*time.Hour), 3600),
				partialAiring("EP000000010002", "old", today.Add(13*time.Hour), 3600))
			source := &partialSource{
				schedules: "[" + schedule + "]",
				programs:  "[" + partialProgram("EP000000010001", "new", "Already fetched correction") + "]",
			}
			switch phase {
			case "schedules":
				source.schedules = "[" + schedule + `,{"code":3000}]`
			case "programs":
				source.programs = "[" + partialProgram("EP000000010001", "new", "Already fetched correction") + `,{"code":3000}]`
			case "metadata":
				source.metadata = `{"code":3000}`
			}
			source.install(t)
			var imageRequests atomic.Int32
			http.DefaultClient = &http.Client{Transport: imageTransport(func(*http.Request) (*http.Response, error) {
				imageRequests.Add(1)
				return nil, fmt.Errorf("image request after provider stop")
			})}
			var logs bytes.Buffer
			logger = slog.New(slog.NewTextHandler(&logs, nil))
			var sd SD
			if err := sd.Update(filename); err != nil {
				t.Fatalf("provider stop discarded usable fresh data: %v", err)
			}
			want := "Already fetched correction"
			if phase == "schedules" {
				want = "Synthetic"
			}
			p := partialGuide(t)
			if len(p) != 2 || p[0].Title[0].Value != want || len(p[0].Images) != 1 || p[0].Images[0].Type != "poster" {
				t.Fatalf("useful data or cached artwork lost after stop: %+v", p)
			}
			if phase == "schedules" && source.calls["programs"] != 0 || phase != "metadata" && source.calls["metadata/programs/"] != 0 || imageRequests.Load() != 0 {
				t.Fatalf("requests continued after stop: %v images=%d", source.calls, imageRequests.Load())
			}
			if _, err := os.Stat(poster); err != nil {
				t.Fatal("provider stop caused referenced cached artwork to be cleaned up")
			}
			if !strings.Contains(logs.String(), "published with gaps") || !strings.Contains(logs.String(), "fetch_stopped=") {
				t.Fatal("successful partial publication hid the provider stop")
			}
		})
	}
}

func TestPartialUpdateMalformedOrDuplicateProgrammeKeepsOtherResults(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint("duplicate=", duplicate), func(t *testing.T) {
			filename := reliabilityConfig(t)
			syntheticCache(t)
			today := time.Now().UTC().Truncate(24 * time.Hour)
			bad := `{"programID":"EP000000010001","md5":"new","titles":123}`
			if duplicate {
				bad = partialProgram("EP000000010001", "new", "Ambiguous duplicate") + "," + bad
			}
			source := &partialSource{
				schedules: "[" + partialSchedule("1", today,
					partialAiring("EP000000010001", "new", today.Add(12*time.Hour), 3600),
					partialAiring("EP000000010002", "new", today.Add(13*time.Hour), 3600)) + "]",
				programs: "[" + bad + "," + partialProgram("EP000000010002", "new", "Good sibling") + "]",
			}
			source.install(t)
			var sd SD
			if err := sd.Update(filename); err != nil {
				t.Fatal(err)
			}
			if p := partialGuide(t); len(p) != 2 || p[0].Title[0].Value != "Synthetic" || p[1].Title[0].Value != "Good sibling" {
				t.Fatalf("bad programme contaminated sibling data or old cache: %+v", p)
			}
		})
	}
}

func TestPartialUpdateContinuesAfterFailedProgrammeBatch(t *testing.T) {
	filename := reliabilityConfig(t)
	Cache.Init()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var airings []string
	var old EPGoCache
	if err := json.Unmarshal([]byte(partialProgram("unused", "old", "Cached batch")), &old); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5001; i++ {
		id := fmt.Sprintf("EP%012d", i)
		Cache.Program[id] = old
		airings = append(airings, partialAiring(id, "new", today.Add(time.Duration(i)*time.Second), 1))
	}
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	source := &partialSource{
		schedules: "[" + partialSchedule("1", today, airings...) + "]",
		programResponse: func(ids []string) (int, string) {
			if len(ids) == 5000 {
				return 503, "temporary batch failure"
			}
			if len(ids) != 1 {
				t.Fatalf("unexpected batch size %d", len(ids))
			}
			return 200, "[" + partialProgram(ids[0], "new", "Healthy later batch") + "]"
		},
	}
	source.install(t)
	sd := SD{Sleep: func(time.Duration) {}}
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	p := partialGuide(t)
	if len(p) != 5001 || p[5000].Title[0].Value != "Healthy later batch" || source.calls["programs"] != 4 {
		t.Fatalf("later batch lost/retried excessively: programmes=%d calls=%d", len(p), source.calls["programs"])
	}
}

func TestPartialUpdateProgrammeCorrectionWithCachedSchedule(t *testing.T) {
	filename := reliabilityConfig(t)
	syntheticCache(t)
	Cache.Schedule["1"][0].Md5 = "new"
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	source := &partialSource{
		schedules: `[]`,
		programs:  "[" + partialProgram("EP000000010001", "new", "Recovered programme correction") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if p := partialGuide(t); len(p) != 1 || p[0].Title[0].Value != "Recovered programme correction" {
		t.Fatal("useful programme correction was blocked by schedule failure")
	}
}

func TestScheduleMd5SkipsUnchangedDays(t *testing.T) {
	filename := reliabilityConfig(t)
	Config.Options.Schedule = 2
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}}
	if err := Config.Save(); err != nil {
		t.Fatal(err)
	}
	Cache.Init()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	tomorrow := today.AddDate(0, 0, 1)
	day := func(d time.Time, md5, id string) string {
		return fmt.Sprintf(`{"stationID":"1","programs":[%s],"metadata":{"startDate":%q,"md5":%q}}`,
			partialAiring(id, "p", d.Add(12*time.Hour), 3600), d.Format("2006-01-02"), md5)
	}
	md5s := func(a, b string) string {
		return fmt.Sprintf(`{"1":{%q:{"code":0,"md5":%q},%q:{"code":0,"md5":%q}}}`, today.Format("2006-01-02"), a, tomorrow.Format("2006-01-02"), b)
	}
	source := &partialSource{
		schedules: "[" + day(today, "today-1", "EP000000010001") + "," + day(tomorrow, "tomorrow-1", "EP000000010002") + "]",
		programs:  "[" + partialProgram("EP000000010001", "p", "Today") + "," + partialProgram("EP000000010002", "p", "Tomorrow") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if got := diskCache(t).ScheduleMd5["1"]; got[today.Format("2006-01-02")] != "today-1" || got[tomorrow.Format("2006-01-02")] != "tomorrow-1" {
		t.Fatalf("schedule md5 not cached: %v", got)
	}

	// Only tomorrow changed: today comes from the cache.
	source.scheduleMd5 = md5s("today-1", "tomorrow-2")
	source.schedules = "[" + day(tomorrow, "tomorrow-2", "EP000000010003") + "]"
	source.programs = "[" + partialProgram("EP000000010003", "p", "Tomorrow changed") + "]"
	source.scheduleBodies = nil
	var logs bytes.Buffer
	logger = slog.New(slog.NewTextHandler(&logs, nil))
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if len(source.scheduleBodies) != 1 || strings.Contains(source.scheduleBodies[0], today.Format("2006-01-02")) || !strings.Contains(source.scheduleBodies[0], tomorrow.Format("2006-01-02")) {
		t.Fatalf("unchanged day requested again: %v", source.scheduleBodies)
	}
	titles := map[string]bool{}
	for _, p := range partialGuide(t) {
		titles[p.Title[0].Value] = true
	}
	if len(titles) != 2 || !titles["Today"] || !titles["Tomorrow changed"] {
		t.Fatalf("guide = %v", titles)
	}
	if !strings.Contains(logs.String(), "unchanged_schedule_days=1") || strings.Contains(logs.String(), "with gaps") {
		t.Fatalf("unchanged day reported as a gap: %s", &logs)
	}

	// Nothing changed: no schedule download, the guide is still published.
	source.scheduleMd5 = md5s("today-1", "tomorrow-2")
	calls := source.calls["schedules"]
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if source.calls["schedules"] != calls || len(partialGuide(t)) != 2 {
		t.Fatalf("schedules requested %d times, guide %v", source.calls["schedules"]-calls, partialGuide(t))
	}
}

func TestMissingArtworkIsNotAGap(t *testing.T) {
	filename := reliabilityConfig(t)
	Config.Options.Schedule = 1
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}}
	if err := Config.Save(); err != nil {
		t.Fatal(err)
	}
	Cache.Init()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	source := &partialSource{
		schedules: "[" + partialSchedule("1", today, partialAiring("EP000000010001", "p", today.Add(12*time.Hour), 3600), partialAiring("EP000000010002", "p", today.Add(13*time.Hour), 3600)) + "]",
		programs:  "[" + partialProgram("EP000000010001", "p", "No artwork") + "," + partialProgram("EP000000010002", "p", "Broken artwork") + "]",
		metadata: `[{"programID":"EP000000010001","data":{"response":"INVALID_PROGRAMID","code":6000,"message":"Could not find requested programID."}},` +
			`{"programID":"EP000000010002","data":{"response":"ERROR","code":9999}}]`,
	}
	source.install(t)
	var logs bytes.Buffer
	logger = slog.New(slog.NewTextHandler(&logs, nil))
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "published with gaps") || !strings.Contains(logs.String(), "no_artwork=1 ") || !strings.Contains(logs.String(), "metadata_failures=1") {
		t.Fatalf("real metadata failure not reported: %s", &logs)
	}

	source.metadata = `[{"programID":"EP000000010001","data":{"code":6000}},{"programID":"EP000000010002","data":{"code":6000}}]`
	logs.Reset()
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `msg="EPG update published"`) || !strings.Contains(logs.String(), "no_artwork=2 ") || !strings.Contains(logs.String(), "metadata_failures=0") {
		t.Fatalf("missing artwork reported as a gap: %s", &logs)
	}
}

func md5TestSetup(t *testing.T) (string, time.Time, time.Time) {
	t.Helper()
	filename := reliabilityConfig(t)
	Config.Options.Schedule = 2
	Config.Station = []channel{{ID: "1", Lineup: "TEST"}}
	if err := Config.Save(); err != nil {
		t.Fatal(err)
	}
	Cache.Init()
	Cache.Token, Cache.TokenExpires = "synthetic-token", time.Now().Add(24*time.Hour).Unix()
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return filename, today, today.AddDate(0, 0, 1)
}

func md5Day(d time.Time, md5 string, airings ...string) string {
	return fmt.Sprintf(`{"stationID":"1","programs":[%s],"metadata":{"startDate":%q,"md5":%q}}`, strings.Join(airings, ","), d.Format("2006-01-02"), md5)
}

func md5Response(entries map[time.Time]string) string {
	var days []string
	for d, e := range entries {
		days = append(days, fmt.Sprintf("%q:%s", d.Format("2006-01-02"), e))
	}
	return `{"1":{` + strings.Join(days, ",") + `}}`
}

func TestScheduleMd5RefetchesPrunedDay(t *testing.T) {
	filename, today, tomorrow := md5TestSetup(t)
	source := &partialSource{
		schedules: "[" + md5Day(today, "T1", partialAiring("EP000000010001", "p", today.Add(12*time.Hour), 3600)) + "," +
			md5Day(tomorrow, "D1", partialAiring("EP000000010002", "p", tomorrow, 3600)) + "]",
		programs: "[" + partialProgram("EP000000010001", "p", "Today") + "," + partialProgram("EP000000010002", "p", "Tomorrow") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}

	// Tomorrow is queued and falls back to the cache; today's fresh late event
	// overlaps and removes tomorrow's cached first airing.
	source.scheduleMd5 = md5Response(map[time.Time]string{today: `{"code":0,"md5":"T2"}`, tomorrow: `{"code":0,"md5":"D2"}`})
	source.schedules = "[" + md5Day(today, "T2", partialAiring("EP000000010003", "p", today.Add(23*time.Hour), 7200)) + "," +
		fmt.Sprintf(`{"stationID":"1","code":7100,"requestedDate":%q}]`, tomorrow.Format("2006-01-02"))
	source.programs = "[" + partialProgram("EP000000010003", "p", "Late") + "]"
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if md5, ok := diskCache(t).ScheduleMd5["1"][tomorrow.Format("2006-01-02")]; ok {
		t.Fatalf("pruned day kept md5 %q", md5)
	}

	// SD's md5 for tomorrow is back to the cached one: it must still be downloaded.
	source.scheduleMd5 = md5Response(map[time.Time]string{today: `{"code":0,"md5":"T2"}`, tomorrow: `{"code":0,"md5":"D1"}`})
	source.schedules = "[" + md5Day(tomorrow, "D1", partialAiring("EP000000010002", "p", tomorrow, 3600)) + "]"
	source.scheduleBodies = nil
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if len(source.scheduleBodies) != 1 || !strings.Contains(source.scheduleBodies[0], tomorrow.Format("2006-01-02")) || strings.Contains(source.scheduleBodies[0], today.Format("2006-01-02")) {
		t.Fatalf("schedule requests = %v", source.scheduleBodies)
	}
}

func TestScheduleMd5EmptyCachedDayIsDownloaded(t *testing.T) {
	filename, today, tomorrow := md5TestSetup(t)
	Cache.ScheduleMd5 = map[string]map[string]string{"1": {today.Format("2006-01-02"): "T1", tomorrow.Format("2006-01-02"): "D1"}}
	if err := Cache.Save(); err != nil {
		t.Fatal(err)
	}
	source := &partialSource{
		scheduleMd5: md5Response(map[time.Time]string{today: `{"code":0,"md5":"T1"}`, tomorrow: `{"code":0,"md5":"D1"}`}),
		schedules: "[" + md5Day(today, "T1", partialAiring("EP000000010001", "p", today.Add(12*time.Hour), 3600)) + "," +
			md5Day(tomorrow, "D1", partialAiring("EP000000010002", "p", tomorrow.Add(12*time.Hour), 3600)) + "]",
		programs: "[" + partialProgram("EP000000010001", "p", "Today") + "," + partialProgram("EP000000010002", "p", "Tomorrow") + "]",
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err != nil {
		t.Fatal(err)
	}
	if source.calls["schedules"] != 1 || len(partialGuide(t)) != 2 {
		t.Fatalf("days without cached airings were not downloaded: %v", source.scheduleBodies)
	}
}

func TestScheduleMd5NestedErrorStopsRequests(t *testing.T) {
	filename, today, tomorrow := md5TestSetup(t)
	source := &partialSource{
		scheduleMd5: md5Response(map[time.Time]string{today: `{"code":4001,"message":"expired"}`, tomorrow: `{"code":0,"md5":"D1"}`}),
	}
	source.install(t)
	var sd SD
	if err := sd.Update(filename); err == nil {
		t.Fatal("update without any data must fail")
	}
	if source.calls["schedules"] != 0 || source.calls["programs"] != 0 || source.calls["metadata/programs/"] != 0 {
		t.Fatalf("requests after an account error: %v", source.calls)
	}
}
