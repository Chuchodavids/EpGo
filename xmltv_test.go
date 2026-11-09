package main

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// resetState clears the package globals the XMLTV and cache code read.
func resetState(t *testing.T) {
	t.Helper()
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	Config = config{}
	Token = ""
	Cache.Channel = map[string]EPGoCache{}
	Cache.Schedule = map[string][]EPGoCache{}
	Cache.Program = map[string]EPGoCache{}
	Cache.Metadata = map[string]EPGoCache{}
}

func TestAudioStereo(t *testing.T) {
	tests := []struct {
		props []string
		want  string
	}{
		{nil, ""},
		{[]string{"cc"}, ""},
		{[]string{"cc", "SAP", "subtitled"}, ""},
		{[]string{"stereo", "cc"}, "stereo"},
		{[]string{"cc", "stereo"}, "stereo"},
		{[]string{"DD 5.1", "stereo"}, "dolby digital"},
		{[]string{"stereo", "DD 5.1"}, "dolby digital"},
		{[]string{"Atmos"}, "dolby digital"},
		{[]string{"Dolby", "stereo"}, "dolby"},
		{[]string{"mono"}, "mono"},
		{[]string{"SAP", "dubbed"}, "mono"},
		{[]string{"dvs"}, "stereo"},
	}
	for _, tt := range tests {
		if got := audioStereo(tt.props); got != tt.want {
			t.Errorf("audioStereo(%q) = %q, want %q", tt.props, got, tt.want)
		}
	}
}

func TestProgramType(t *testing.T) {
	tests := map[string]string{
		"EP000000010002": "EP",
		"MV000000030000": "MV",
		"S":              "",
		"":               "",
	}
	for id, want := range tests {
		if got := programType(id); got != want {
			t.Errorf("programType(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestGetRequiredProgramIDs(t *testing.T) {
	resetState(t)

	Cache.Schedule["1"] = []EPGoCache{
		{ProgramID: "UPTODATE", Md5: "a1"},
		{ProgramID: "CHANGED", Md5: "b2"},
		{ProgramID: "LEGACY", Md5: "c1"},
		{ProgramID: "NOMD5", Md5: ""},
		{ProgramID: "MISSING", Md5: "e1"},
	}
	// Same programs airing on a second station must only be requested once.
	Cache.Schedule["2"] = []EPGoCache{
		{ProgramID: "CHANGED", Md5: "b2"},
		{ProgramID: "MISSING", Md5: "e1"},
	}

	Cache.Program["UPTODATE"] = EPGoCache{Md5: "a1"}
	Cache.Program["CHANGED"] = EPGoCache{Md5: "b1"}
	Cache.Program["LEGACY"] = EPGoCache{} // cached before md5 was stored
	Cache.Program["NOMD5"] = EPGoCache{Md5: "d1"}

	got := Cache.GetRequiredProgramIDs()
	sort.Strings(got)

	want := []string{"CHANGED", "LEGACY", "MISSING"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("GetRequiredProgramIDs() = %v, want %v", got, want)
	}
}

func TestGetSubTitle(t *testing.T) {
	resetState(t)

	var withEpisode, withShortDesc, emptyDesc EPGoCache
	withEpisode.EpisodeTitle150 = "Pilot"
	withShortDesc.Descriptions.Description100 = append(withShortDesc.Descriptions.Description100,
		struct {
			DescriptionLanguage string `json:"descriptionLanguage"`
			Description         string `json:"description"`
		}{"es", "Corto"})
	emptyDesc.Descriptions.Description100 = append(emptyDesc.Descriptions.Description100,
		struct {
			DescriptionLanguage string `json:"descriptionLanguage"`
			Description         string `json:"description"`
		}{"en", ""})

	Cache.Program["EP"] = withEpisode
	Cache.Program["SH"] = withShortDesc
	Cache.Program["EMPTY"] = emptyDesc

	if s := Cache.GetSubTitle("EP", "en"); s == nil || s.Value != "Pilot" || s.Lang != "en" {
		t.Errorf("episode title sub-title = %+v, want Pilot/en", s)
	}
	if s := Cache.GetSubTitle("SH", "en"); s == nil || s.Value != "Corto" || s.Lang != "es" {
		t.Errorf("short description sub-title = %+v, want Corto/es", s)
	}
	if s := Cache.GetSubTitle("EMPTY", "en"); s != nil {
		t.Errorf("empty description sub-title = %+v, want nil", s)
	}
	if s := Cache.GetSubTitle("UNKNOWN", "en"); s != nil {
		t.Errorf("unknown program sub-title = %+v, want nil", s)
	}
}

func TestGetCredits(t *testing.T) {
	resetState(t)

	var withCrew EPGoCache
	withCrew.Crew = append(withCrew.Crew, struct {
		BillingOrder string `json:"billingOrder"`
		Name         string `json:"name"`
		NameID       string `json:"nameId"`
		PersonID     string `json:"personId"`
		Role         string `json:"role"`
	}{Name: "D Director", Role: "Director"})
	Cache.Program["CREW"] = withCrew
	Cache.Program["NONE"] = EPGoCache{}

	if cr := Cache.GetCredits("CREW"); cr != nil {
		t.Errorf("credits disabled: got %+v, want nil", cr)
	}

	Config.Options.Credits = true
	if cr := Cache.GetCredits("CREW"); cr == nil || len(cr.Director) != 1 {
		t.Errorf("credits with crew = %+v, want one director", cr)
	}
	if cr := Cache.GetCredits("NONE"); cr != nil {
		t.Errorf("credits without cast or crew = %+v, want nil", cr)
	}
}

func TestGetIconShortURI(t *testing.T) {
	resetState(t)
	Token = "tok"
	Cache.Metadata["EP1"] = EPGoCache{Data: []Data{
		{Category: "Poster Art", Aspect: "2x3", URI: "a.jpg", Width: 10, Height: 15},
	}}

	icons := Cache.GetIcon("EP1")
	if len(icons) != 1 {
		t.Fatalf("GetIcon() returned %d icons, want 1", len(icons))
	}
	want := "https://json.schedulesdirect.org/20141201/image/a.jpg?token=tok"
	if icons[0].Src != want {
		t.Errorf("GetIcon() src = %q, want %q", icons[0].Src, want)
	}
}

func TestGetLogo(t *testing.T) {
	var ch EPGoCache
	if icon := ch.getLogo(); icon != nil {
		t.Errorf("getLogo() without URL = %+v, want nil", icon)
	}
	ch.Logo.URL = "https://example.com/logo.png"
	ch.Logo.Width = 100
	if icon := ch.getLogo(); icon == nil || icon.Src != ch.Logo.URL || icon.Width != 100 {
		t.Errorf("getLogo() = %+v, want logo URL and width", icon)
	}
}

func TestGetProgramXML(t *testing.T) {
	resetState(t)
	dir := t.TempDir()

	// A TMDB cache hit, so the TMDB fallback below needs no network.
	tmdbCache := filepath.Join(dir, "tmdb.json")
	data, _ := json.Marshal(map[string]string{"No EPG Info-MV": "/poster.jpg"})
	if err := os.WriteFile(tmdbCache, data, 0644); err != nil {
		t.Fatal(err)
	}

	Config.Station = []channel{{ID: "100", Lineup: "USA-TEST-X"}}
	Config.Options.Rating.Guidelines = true
	Config.Options.Images.Tmdb.Enable = true
	Config.Files.TmdbCacheFile = tmdbCache

	var episode EPGoCache
	episode.Titles = append(episode.Titles, struct {
		Title120 string `json:"title120"`
	}{"Some Show"})
	episode.EpisodeTitle150 = "Pilot"
	episode.Metadata = append(episode.Metadata, struct {
		Gracenote struct {
			Episode int `json:"episode"`
			Season  int `json:"season"`
		} `json:"Gracenote"`
	}{})
	episode.Metadata[0].Gracenote.Season = 2
	episode.Metadata[0].Gracenote.Episode = 3
	episode.ContentRating = append(episode.ContentRating, struct {
		Body    string `json:"body"`
		Code    string `json:"code"`
		Country string `json:"country"`
	}{"USA Parental Rating", "TV-14", "USA"})
	Cache.Program["EP000000010002"] = episode
	Cache.Metadata["EP000000010002"] = EPGoCache{Data: []Data{
		{Category: "Poster Art", Aspect: "2x3", URI: "https://example.com/p.jpg"},
	}}

	Cache.Schedule["100"] = []EPGoCache{
		{ProgramID: "EP000000010002", New: true, AudioProperties: []string{"stereo", "cc"}},
		// Not in the program cache (e.g. its download failed): used to panic
		// when the TMDB fallback indexed the empty episode-num list.
		{ProgramID: "MV000000030000", AudioProperties: []string{"cc"}},
	}

	programmes := getProgram(EPGoCache{StationID: "100", BroadcastLanguage: []string{"en"}})
	if len(programmes) != 2 {
		t.Fatalf("getProgram() returned %d programmes, want 2", len(programmes))
	}

	encode := func(p Programme) string {
		b, err := xml.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	ep := encode(programmes[0])
	for _, want := range []string{
		`<sub-title lang="en">Pilot</sub-title>`,
		`<icon src="https://example.com/p.jpg"></icon>`,
		`<stereo>stereo</stereo>`,
	} {
		if !strings.Contains(ep, want) {
			t.Errorf("episode XML missing %s:\n%s", want, ep)
		}
	}
	// XMLTV DTD order: icon before episode-num, new before rating.
	if strings.Index(ep, "<icon") > strings.Index(ep, "<episode-num") {
		t.Errorf("icon must come before episode-num:\n%s", ep)
	}
	if strings.Index(ep, "<new>") > strings.Index(ep, "<rating") {
		t.Errorf("new must come before rating:\n%s", ep)
	}

	mv := encode(programmes[1])
	if !strings.Contains(mv, `/poster.jpg"`) {
		t.Errorf("movie XML missing TMDB poster:\n%s", mv)
	}
	for _, unwanted := range []string{"<sub-title", "<credits", "<video", "<audio", `src=""`, `height="0"`} {
		if strings.Contains(mv, unwanted) {
			t.Errorf("movie XML contains empty element %s:\n%s", unwanted, mv)
		}
	}
}
