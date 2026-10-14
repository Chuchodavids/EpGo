package main

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"sync"
	"testing"
	"time"
)

func extrasTestPrograms(t *testing.T) {
	t.Helper()
	oldProgram, oldSchedule, oldMetadata := Cache.Program, Cache.Schedule, Cache.Metadata
	t.Cleanup(func() { Cache.Program, Cache.Schedule, Cache.Metadata = oldProgram, oldSchedule, oldMetadata })

	var sdPrograms []SDProgram
	err := json.Unmarshal([]byte(`[
		{"programID": "MV000000010000", "md5": "a", "titles": [{"title120": "Film"}],
		 "movie": {"year": "2004", "duration": 5400,
		           "qualityRating": [{"ratingsBody": "Gracenote", "rating": "3.5", "minRating": "1", "maxRating": "4", "increment": ".5"}]}},
		{"programID": "MV000000020000", "md5": "a", "titles": [{"title120": "Numeric"}],
		 "movie": {"year": 1999, "qualityRating": [{"ratingsBody": "Gracenote", "rating": 2, "maxRating": 4}]}},
		{"programID": "EP000000030001", "md5": "a", "titles": [{"title120": "Series"}],
		 "originalAirDate": "1992-09-24", "multiPart": {"partNumber": 1, "totalParts": 2},
		 "metadata": [{"Gracenote": {"season": 6, "episode": 1}}]},
		{"programID": "EP000000040001", "md5": "a", "titles": [{"title120": "Miniseries"}],
		 "originalAirDate": "2026-10-13"}
	]`), &sdPrograms)
	if err != nil {
		t.Fatal(err)
	}
	Cache.Program = map[string]EPGoCache{}
	Cache.Metadata = map[string]EPGoCache{}
	for _, sd := range sdPrograms {
		data, _ := json.Marshal([]SDProgram{sd})
		var c cache
		c.Program = map[string]EPGoCache{}
		var wg sync.WaitGroup
		wg.Add(1)
		c.AddProgram(&data, &wg)
		Cache.Program[sd.ProgramID] = c.Program[sd.ProgramID]
	}

	var schedules []SDSchedule
	err = json.Unmarshal([]byte(`[{"stationID": "fixture", "metadata": {"startDate": "2026-10-14"}, "programs": [
		{"programID": "MV000000010000", "md5": "a", "airDateTime": "2026-10-14T12:00:00Z", "duration": 5400,
		 "premiere": true, "audioProperties": ["DD 5.1", "cc"], "signed": true},
		{"programID": "MV000000020000", "md5": "a", "airDateTime": "2026-10-14T14:00:00Z", "duration": 5400,
		 "isPremiereOrFinale": "Premiere", "audioProperties": ["stereo", "subtitled", "cc"]},
		{"programID": "EP000000030001", "md5": "a", "airDateTime": "2026-10-14T16:00:00Z", "duration": 1800,
		 "new": true, "isPremiereOrFinale": "Season Premiere"},
		{"programID": "EP000000040001", "md5": "a", "airDateTime": "2026-10-14T17:00:00Z", "duration": 1800,
		 "isPremiereOrFinale": "Series Finale", "multipart": {"partNumber": 3, "totalParts": 3}}
	]}]`), &schedules)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(schedules)
	Cache.Schedule = map[string][]EPGoCache{}
	Cache.AddSchedule(&data)
}

func TestGuideExtras(t *testing.T) {
	extrasTestPrograms(t)

	programmes := getProgram(EPGoCache{StationID: "fixture"})
	if len(programmes) != 4 {
		t.Fatalf("programmes = %d", len(programmes))
	}
	var got []string
	for _, p := range programmes {
		out, err := xml.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(out))
	}

	for i, want := range [][]string{
		{
			`<date>2004</date>`,
			`<premiere></premiere>`,
			`<subtitles type="teletext"></subtitles><subtitles type="deaf-signed"></subtitles>`,
			`<star-rating system="Gracenote"><value>3.5/4</value></star-rating>`,
			`<stereo>dolby digital</stereo>`,
		},
		{
			`<date>1999</date>`,
			`<star-rating system="Gracenote"><value>2/4</value></star-rating>`,
			`<subtitles type="onscreen"></subtitles><subtitles type="teletext"></subtitles>`,
			`<stereo>stereo</stereo>`,
			`<previously-shown></previously-shown>`,
		},
		{
			`<date>19920924</date>`,
			`<episode-num system="dd_progid">EP00000003.0001</episode-num><episode-num system="xmltv_ns">5.0.0/2</episode-num>`,
			`<premiere>Season Premiere</premiere><new></new>`,
		},
		{
			`<date>20261013</date>`,
			`<episode-num system="xmltv_ns">..2/3</episode-num>`,
			`<previously-shown start="20261013"></previously-shown>`,
		},
	} {
		for _, w := range want {
			if !strings.Contains(got[i], w) {
				t.Errorf("programme %d: missing %s\n%s", i, w, got[i])
			}
		}
	}

	// A premiere without the new flag is only trusted from SD's premiere flag.
	if strings.Contains(got[1], "<premiere") {
		t.Errorf("premiere text on an airing that is not new: %s", got[1])
	}
	for i, out := range got {
		if strings.Contains(out, "last-chance") {
			t.Errorf("programme %d: finales are not last-chance: %s", i, out)
		}
		if strings.Contains(out, "<stereo>mono</stereo>") {
			t.Errorf("programme %d: unknown audio properties must not mean mono: %s", i, out)
		}
		order := []string{"<credits", "<date", "<category", "<episode-num", "<audio", "<previously-shown", "<premiere", "<new", "<subtitles", "<rating", "<star-rating"}
		last := -1
		for _, tag := range order {
			if pos := strings.Index(out, tag); pos >= 0 {
				if pos < last {
					t.Errorf("programme %d: %s out of DTD order: %s", i, tag, out)
				}
				last = pos
			}
		}
	}
}

func TestCacheVersionRefreshesPrograms(t *testing.T) {
	oldFile := Config.Files.Cache
	t.Cleanup(func() { Config.Files.Cache = oldFile })
	Config.Files.Cache = t.TempDir() + "/cache.json"

	old := cache{
		Program: map[string]EPGoCache{"EP000000030001": {Md5: "a", Titles: []struct {
			Title120 string `json:"title120"`
		}{{"Kept"}}}},
		Schedule:    map[string][]EPGoCache{"1": {{ProgramID: "EP000000030001", Md5: "a", AirDateTime: time.Now()}}},
		ScheduleMd5: map[string]map[string]string{"1": {time.Now().UTC().Format("2006-01-02"): "s"}},
	}
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}

	var c cache
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	if c.ScheduleMd5 != nil {
		t.Fatalf("schedules cached by an older version lack new fields and must be downloaded again: %v", c.ScheduleMd5)
	}
	if c.Version != cacheVersion || len(c.GetRequiredProgramIDs()) != 1 || !usableProgram(c.Program["EP000000030001"]) {
		t.Fatalf("old cache must be refreshed but kept: version %d, required %v", c.Version, c.GetRequiredProgramIDs())
	}

	c.Program["EP000000030001"] = EPGoCache{Md5: "a", Titles: c.Program["EP000000030001"].Titles}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	var reopened cache
	if err := reopened.Open(); err != nil {
		t.Fatal(err)
	}
	if ids := reopened.GetRequiredProgramIDs(); len(ids) != 0 {
		t.Fatalf("current cache must not be refreshed again: %v", ids)
	}
}
