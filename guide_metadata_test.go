package main

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

func metadataTestCache(t *testing.T) *cache {
	t.Helper()
	var programs map[string]EPGoCache
	err := json.Unmarshal([]byte(`{
		"EP000000010001": {
			"episodeTitle150": "Folge 1",
			"genres": ["Drama", "Crime drama"],
			"showType": "Series",
			"descriptions": {"description100": [{"description": "Kurzbeschreibung.", "descriptionLanguage": "de"}]},
			"cast": [
				{"name": "Actor A", "characterName": "Kommissar", "role": "Actor"},
				{"name": "Guest Star B", "characterName": "Opfer", "role": "Guest Star"},
				{"name": "Host C", "role": "Host"},
				{"name": "Self D", "role": "Self"}
			],
			"crew": [
				{"name": "Director E", "role": "Director"},
				{"name": "Writer F", "role": "Screenwriter"},
				{"name": "Art Director G", "role": "Art Director"}
			]
		},
		"SH000000020000": {
			"genres": ["News"],
			"showType": "Series",
			"descriptions": {"description100": [{"description": "Die Nachrichten des Tages.", "descriptionLanguage": "de"}]}
		},
		"MV000000030000": {
			"genres": ["Drama"],
			"showType": "Feature Film"
		}
	}`), &programs)
	if err != nil {
		t.Fatal(err)
	}
	Config.Options.Credits = true
	Config.Options.SubtitleEpisodeTitleOnly = false
	return &cache{Program: programs}
}

func TestGetSubTitle(t *testing.T) {
	c := metadataTestCache(t)

	if s := c.GetSubTitle("EP000000010001", "de"); s == nil || s.Value != "Folge 1" || s.Lang != "de" {
		t.Fatalf("episode title: %+v", s)
	}
	if s := c.GetSubTitle("SH000000020000", "de"); s == nil || s.Value != "Die Nachrichten des Tages." {
		t.Fatalf("description fallback: %+v", s)
	}
	if s := c.GetSubTitle("MV000000030000", "de"); s != nil {
		t.Fatalf("no subtitle expected: %+v", s)
	}

	Config.Options.SubtitleEpisodeTitleOnly = true
	if s := c.GetSubTitle("SH000000020000", "de"); s != nil {
		t.Fatalf("episode titles only: %+v", s)
	}
	if s := c.GetSubTitle("EP000000010001", "de"); s == nil || s.Value != "Folge 1" {
		t.Fatalf("episode titles only, episode title: %+v", s)
	}
}

func TestGetCredits(t *testing.T) {
	c := metadataTestCache(t)

	cr := c.GetCredits("EP000000010001")
	if cr == nil {
		t.Fatal("no credits")
	}
	out, err := xml.Marshal(cr)
	if err != nil {
		t.Fatal(err)
	}
	want := `<Credits><director>Director E</director>` +
		`<actor role="Kommissar">Actor A</actor><actor role="Opfer">Guest Star B</actor>` +
		`<writer>Writer F</writer><presenter>Host C</presenter><guest>Self D</guest></Credits>`
	if string(out) != want {
		t.Fatalf("credits\n got %s\nwant %s", out, want)
	}

	if cr := c.GetCredits("SH000000020000"); cr != nil {
		t.Fatalf("empty credits must be nil: %+v", cr)
	}
	Config.Options.Credits = false
	if cr := c.GetCredits("EP000000010001"); cr != nil {
		t.Fatalf("credits disabled: %+v", cr)
	}
}

func TestGetCategory(t *testing.T) {
	c := metadataTestCache(t)

	for id, want := range map[string]string{
		"EP000000010001": "Drama,Crime drama,Series,series",
		"SH000000020000": "News,Series,tvshow",
		"MV000000030000": "Drama,Feature Film,movie",
	} {
		var got []string
		for _, ca := range c.GetCategory(id) {
			got = append(got, ca.Value)
		}
		if strings.Join(got, ",") != want {
			t.Errorf("%s: got %v, want %s", id, got, want)
		}
	}
}

func TestEmptyElementsOmitted(t *testing.T) {
	c := metadataTestCache(t)
	pro := Programme{
		Title:    []Title{{Value: "Film", Lang: "de"}},
		SubTitle: c.GetSubTitle("MV000000030000", "de"),
		Credits:  c.GetCredits("MV000000030000"),
	}
	out, err := xml.Marshal(pro)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"<sub-title", "<credits"} {
		if strings.Contains(string(out), tag) {
			t.Errorf("%s written for a programme without it: %s", tag, out)
		}
	}
}
