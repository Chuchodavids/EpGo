package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type imageTransport func(*http.Request) (*http.Response, error)

func (f imageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func imageTestConfig(t *testing.T) {
	t.Helper()
	oldConfig, oldToken, oldStopped := Config, Token, imageDownloadsStopped
	oldLogger, oldClient, oldLimit := logger, http.DefaultClient, Cache.ImageLimitUntil
	t.Cleanup(func() {
		Config, Token, imageDownloadsStopped = oldConfig, oldToken, oldStopped
		Cache.ImageLimitUntil = oldLimit
		logger, http.DefaultClient = oldLogger, oldClient
	})
	Config = config{}
	Config.Options.Images.Typed = true
	Config.Options.Images.Download = true
	Config.Options.Images.Path = t.TempDir()
	Config.Server.Address = "images.example"
	Config.Server.Port = "8080"
	Token = "test-secret-token"
	imageDownloadsStopped = false
	imageFailures = map[string]error{}
	Cache.ImageLimitUntil = 0
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	http.DefaultClient = &http.Client{Transport: imageTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected image request: %s", r.URL.Path)
		return nil, fmt.Errorf("network disabled in tests")
	})}
}

func readImageFixture(t *testing.T, filename string) *cache {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	// Only image inputs are needed; exported fixtures may redact token fields
	// to strings even though the runtime cache uses an integer expiry.
	var fixture struct {
		Metadata map[string]EPGoCache
		Program  map[string]EPGoCache
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return &cache{Metadata: fixture.Metadata, Program: fixture.Program}
}

func TestSelectProgrammeImagesRealSamples(t *testing.T) {
	c := readImageFixture(t, "testdata/programme-images.json")
	for _, tt := range []struct {
		id   string
		want map[string]string
	}{
		{"MV000371790000", map[string]string{
			"backdrop": "8325c7a63571a704bc619245b8fa4da0f5bc41c0800dbcb6a8a39d944c6deaf3.jpg",
			"poster":   "05f4db2c359b3ad877306d6e89c50a09b0ca3a7867d9fef3f7464cfbcb18f98a.jpg",
		}},
		{"EP002061390830", map[string]string{
			"still":    "d73e774200fe316c1e597b84d28f35593f65fbe72e18f731583d287a87f049b6.jpg",
			"backdrop": "ff44f63b1fcf9e5194bdddbbb6e1b4dba087e5658feb29d4385779c4f1365f64.jpg",
			"poster":   "090384b68ddd76ad1752526b28db328e20c8434f33105eb1774131ee08c06bd3.jpg",
		}},
		{"SH000199170000", map[string]string{
			"backdrop": "c29cd9fa2cec79445602fb566b4b374e1740924d47131a953d7b3f0797762d86.jpg",
			"poster":   "4134e476ca1b23b5c9c9e3ea3f29e87a972245d039132500736d5b39dd83d787.jpg",
		}},
		{"EP000021447124", map[string]string{}},
		{"EP000031285726", map[string]string{"backdrop": "d7a592ea0964d41a1683f951b6a977e3649438c7c0d50e1d97c7f746e37f2f3e.jpg"}},
		{"missing", map[string]string{}},
	} {
		t.Run(tt.id, func(t *testing.T) {
			got := make(map[string]string)
			for imageType, data := range c.selectProgrammeImages(tt.id) {
				got[imageType] = data.URI
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("selected images = %v, want %v", got, tt.want)
			}
		})
	}
	// The fixture contains series-only art under SH IDs, not EP IDs.
	c.Metadata["EP000199170001"] = c.Metadata["SH000199170000"]
	if got := c.selectProgrammeImages("EP000199170001"); len(got) != 2 || got["backdrop"].Tier != "Series" || got["poster"].Tier != "Series" {
		t.Fatalf("series-only episode fallback = %v", got)
	}
}

func TestSelectProgrammeImagesPreferences(t *testing.T) {
	data := []Data{
		{URI: "series.jpg", Category: "Iconic", Tier: "Series", Aspect: "16x9", Width: 1920, Height: 1080},
		{URI: "season-portrait.jpg", Category: "Iconic", Tier: "Season", Aspect: "2x3", Width: 120, Height: 180},
		{URI: "season-small.jpg", Category: "Iconic", Tier: "Season", Aspect: "16x9", Width: 240, Height: 135},
		{URI: "season-large.jpg", Category: "Iconic", Tier: "Season", Aspect: "16x9", Width: 960, Height: 540},
		{URI: "movie-landscape.jpg", Category: "Poster Art", Aspect: "16x9", Width: 1920, Height: 1080},
		{URI: "movie-portrait.jpg", Category: "Poster Art", Aspect: "2x3", Width: 480, Height: 720},
		{URI: "invalid.jpg", Category: "Iconic", Tier: "Episode", Aspect: "16x9"},
		{Category: "Iconic", Tier: "Episode", Aspect: "16x9", Width: 1920, Height: 1080},
	}
	c := &cache{Metadata: map[string]EPGoCache{"MVtest": {Data: data}}}
	got := c.selectProgrammeImages("MVtest")
	if got["backdrop"].URI != "season-large.jpg" || got["poster"].URI != "movie-portrait.jpg" || len(got) != 2 {
		t.Fatalf("aspect/size preferences = %v", got)
	}
	c.Metadata["MVtest"] = EPGoCache{Data: data[:2]}
	if got := c.selectProgrammeImages("MVtest"); got["backdrop"].URI != "season-portrait.jpg" {
		t.Fatalf("tier must take priority over aspect: %v", got)
	}
	c.Metadata["EPtest"] = EPGoCache{Data: []Data{
		{URI: "other-aspect.jpg", Category: "Iconic", Tier: "Episode", Aspect: "5x4", Width: 500, Height: 400},
	}}
	if got := c.selectProgrammeImages("EPtest"); got["still"].URI != "other-aspect.jpg" {
		t.Fatalf("other aspect fallback = %v", got)
	}
	c.Metadata["EPsports"] = EPGoCache{Data: []Data{
		{URI: "sports.jpg", Category: "Backdrop-Sports", Tier: "Team Event", Aspect: "16x9", Width: 1920, Height: 1080},
		{URI: "series-iconic.jpg", Category: "Iconic", Tier: "Series", Aspect: "16x9", Width: 240, Height: 135},
	}}
	if got := c.selectProgrammeImages("EPsports"); got["backdrop"].URI != "series-iconic.jpg" {
		t.Fatalf("Iconic must take priority over sports backdrops: %v", got)
	}
	c.Program = map[string]EPGoCache{"movie": {ShowType: "Feature Film"}}
	c.Metadata["movie"] = EPGoCache{Data: data}
	if got := c.selectProgrammeImages("movie"); got["poster"].URI != "movie-portrait.jpg" {
		t.Fatalf("movie show type fallback = %v", got)
	}
}

func TestProgrammeImageAttributes(t *testing.T) {
	imageTestConfig(t)
	for _, tt := range []struct {
		width, height int
		size, orient  string
	}{
		{120, 180, "1", "P"}, {200, 100, "2", "L"},
		{400, 200, "2", "L"}, {240, 401, "3", "P"}, {300, 300, "2", "L"},
	} {
		uri := fmt.Sprintf("%dx%d.jpg", tt.width, tt.height)
		if err := os.WriteFile(filepath.Join(Config.Options.Images.Path, uri), []byte("cached"), 0644); err != nil {
			t.Fatal(err)
		}
		c := &cache{Metadata: map[string]EPGoCache{"EPtest": {Data: []Data{
			{URI: uri, Category: "Iconic", Tier: "Episode", Width: tt.width, Height: tt.height},
		}}}}
		got := c.GetImages("EPtest")
		if len(got) != 1 || got[0].Size != tt.size || got[0].Orient != tt.orient || got[0].System != "schedulesdirect" || got[0].URL != "http://images.example:8080/"+uri {
			t.Fatalf("attributes for %s = %v", uri, got)
		}
	}
}

func TestProgrammeImagesOptIn(t *testing.T) {
	imageTestConfig(t)
	c := readImageFixture(t, "testdata/programme-images.json")
	for _, tt := range []struct{ typed, download bool }{{false, false}, {false, true}, {true, false}} {
		Config.Options.Images.Typed, Config.Options.Images.Download = tt.typed, tt.download
		if got := c.GetImages("EP002061390830"); len(got) != 0 {
			t.Fatalf("images without both options enabled: %v", got)
		}
	}
}

func TestIconWithoutDownloads(t *testing.T) {
	imageTestConfig(t)
	Config.Options.Images.Download = false
	c := &cache{Metadata: map[string]EPGoCache{"EPtest": {Data: []Data{
		{URI: "relative.jpg", Category: "Banner-L1", Aspect: "16x9", Width: 960, Height: 540},
	}}}}
	if got := c.GetIcon("EPtest"); len(got) != 0 {
		t.Fatalf("relative URI without downloading = %v", got)
	}
	metadata := c.Metadata["EPtest"]
	metadata.Data[0].URI = "https://public.example/image.jpg"
	c.Metadata["EPtest"] = metadata
	if got := c.GetIcon("EPtest"); len(got) != 1 || got[0].Src != metadata.Data[0].URI || got[0].Width != 960 || got[0].Height != 540 {
		t.Fatalf("absolute icon behavior changed: %v", got)
	}
}

func TestProgrammeImagesDownloadReuse(t *testing.T) {
	imageTestConfig(t)
	c := readImageFixture(t, "testdata/programme-images.json")
	requests := make(map[string]int)
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		requests[imageFilename(r.URL.Path)]++
		if r.URL.Query().Get("token") != Token {
			t.Error("missing download token")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("image bytes"))}, nil
	})
	for i := 0; i < 2; i++ {
		icons := c.GetIcon("EP002061390830")
		images := c.GetImages("EP002061390830")
		if len(icons) != 1 || len(images) != 3 || icons[0].Src != images[2].URL {
			t.Fatalf("icon/typed image sharing: icons=%v images=%v", icons, images)
		}
		for _, image := range images {
			if strings.Contains(image.URL, Token) || !strings.HasPrefix(image.URL, "http://images.example:8080/") {
				t.Fatalf("unsafe XMLTV URL: %s", image.URL)
			}
		}
	}
	if len(requests) != 3 {
		t.Fatalf("unique downloads = %v, want 3", requests)
	}
	for uri, count := range requests {
		if count != 1 {
			t.Fatalf("downloaded %s %d times", uri, count)
		}
	}
}

func TestProgrammeImagesStopOnSDError(t *testing.T) {
	for code := range sdImageStopCodes {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			imageTestConfig(t)
			c := readImageFixture(t, "testdata/programme-images.json")
			requests := 0
			http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":%d,"response":"STOP"}`, code)))}, nil
			})
			if got := c.GetImages("EP002061390830"); len(got) != 0 {
				t.Fatalf("failed images must be omitted: %v", got)
			}
			if got := c.GetIcon("EP002061390830"); len(got) != 0 {
				t.Fatalf("uncached icon after stop = %v", got)
			}
			if requests != 1 || !imageDownloadsStopped {
				t.Fatalf("requests=%d stopped=%v", requests, imageDownloadsStopped)
			}
			files, err := os.ReadDir(Config.Options.Images.Path)
			if err != nil || len(files) != 0 {
				t.Fatalf("error response saved: files=%v err=%v", files, err)
			}
			poster := c.selectProgrammeImages("EP002061390830")["poster"]
			if err := os.WriteFile(filepath.Join(Config.Options.Images.Path, poster.URI), []byte("cached"), 0644); err != nil {
				t.Fatal(err)
			}
			if got := c.GetImages("EP002061390830"); len(got) != 1 || got[0].Type != "poster" || requests != 1 {
				t.Fatalf("cached image must survive stop: %v requests=%d", got, requests)
			}
		})
	}
}

func TestProgrammeImagesDownloadFailure(t *testing.T) {
	imageTestConfig(t)
	c := readImageFixture(t, "testdata/programme-images.json")
	requests := 0
	http.DefaultClient.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if strings.Contains(r.URL.Path, "d73e774") {
			return nil, fmt.Errorf("simulated transport failure")
		}
		return &http.Response{StatusCode: 404, Status: "404 Not Found", Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	})
	if got := c.GetImages("EP002061390830"); len(got) != 0 || imageDownloadsStopped || requests != 3 {
		t.Fatalf("ordinary failures: images=%v stopped=%v requests=%d", got, imageDownloadsStopped, requests)
	}
}

func TestTypedImagesConfig(t *testing.T) {
	imageTestConfig(t)
	data, err := os.ReadFile("sample-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(data, &Config); err != nil || Config.Options.Images.Typed {
		t.Fatalf("sample default: typed=%v err=%v", Config.Options.Images.Typed, err)
	}
	Config.File = filepath.Join(t.TempDir(), "config")
	// Old files must turn the new option off even when Config was reused.
	Config.Options.Images.Typed = true
	data = []byte(strings.ReplaceAll(string(data), "        Insert typed image tags into XML file: false\n", ""))
	if err = os.WriteFile(Config.File+".yaml", data, 0644); err != nil {
		t.Fatal(err)
	}
	if err = Config.Open(); err != nil || Config.Options.Images.Typed {
		t.Fatalf("old config migration: typed=%v err=%v", Config.Options.Images.Typed, err)
	}
	Config.Options.Images.Typed = true
	if err = Config.Save(); err != nil {
		t.Fatal(err)
	}
	Config.Options.Images.Typed = false
	if err = Config.Open(); err != nil || !Config.Options.Images.Typed {
		t.Fatalf("opt-in round trip: typed=%v err=%v", Config.Options.Images.Typed, err)
	}
	Config.InitConfig()
	if Config.Options.Images.Typed {
		t.Fatal("new config must default typed images off")
	}
}

func TestProgrammeImagesXML(t *testing.T) {
	imageTestConfig(t)
	c := readImageFixture(t, "testdata/programme-images.json")
	oldProgram, oldMetadata, oldSchedule := Cache.Program, Cache.Metadata, Cache.Schedule
	t.Cleanup(func() { Cache.Program, Cache.Metadata, Cache.Schedule = oldProgram, oldMetadata, oldSchedule })
	Cache.Program, Cache.Metadata = c.Program, c.Metadata
	ids := []string{"MV000371790000", "EP002061390830", "SH000199170000", "EP000021447124"}
	Cache.Schedule = map[string][]EPGoCache{"fixture": {}}
	for i, id := range ids {
		for _, data := range c.Metadata[id].Data {
			if err := os.WriteFile(filepath.Join(Config.Options.Images.Path, data.URI), []byte("cached"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		Cache.Schedule["fixture"] = append(Cache.Schedule["fixture"], EPGoCache{
			ProgramID: id, AirDateTime: time.Date(2026, 10, 1, 12+i, 0, 0, 0, time.UTC), Duration: 3600,
			New: true, VideoProperties: []string{"hdtv"}, AudioProperties: []string{"stereo"},
		})
	}
	programmes := getProgram(EPGoCache{StationID: "fixture"})
	if len(programmes) != 4 || len(programmes[1].Icon) != 1 || len(programmes[1].Images) != 3 {
		t.Fatalf("generated programmes = %v", programmes)
	}
	// Exercise rating/previously-shown order as well as the normal new branch.
	programmes[0].Rating = []Rating{{System: "test", Value: "PG"}}
	programmes[2].New = nil
	programmes[2].PreviouslyShown = &PreviouslyShown{Start: "20250101"}
	document := struct {
		XMLName    xml.Name    `xml:"tv"`
		Programmes []Programme `xml:"programme"`
	}{Programmes: programmes}
	data, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if strings.Contains(output, Token) || strings.Contains(output, sdImageURL) || strings.Count(output, "<image ") != 7 {
		t.Fatalf("unsafe/missing typed XML: %s", output)
	}
	for _, programme := range programmes {
		encoded, err := xml.Marshal(programme)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		if strings.Index(text, "<icon") > strings.Index(text, "<episode-num") {
			t.Fatalf("icon must precede episode-num: %s", text)
		}
		if len(programme.Images) > 0 && !strings.HasSuffix(text, "</image></programme>") {
			t.Fatalf("images must be last: %s", text)
		}
	}
	if filename := os.Getenv("EPGO_XMLTV_SAMPLE"); filename != "" {
		if err := os.WriteFile(filename, append([]byte(xml.Header), data...), 0644); err != nil {
			t.Fatal(err)
		}
	}
	Config.Options.Images.Typed = false
	without := getProgram(EPGoCache{StationID: "fixture"})
	for i := range without {
		if len(without[i].Images) != 0 || !reflect.DeepEqual(without[i].Icon, programmes[i].Icon) {
			t.Fatalf("opt-in altered icons: enabled=%v disabled=%v", programmes[i].Icon, without[i].Icon)
		}
	}
}

func TestImageFixtureStatistics(t *testing.T) {
	filename := os.Getenv("EPGO_IMAGE_FIXTURE")
	if filename == "" {
		t.Skip("set EPGO_IMAGE_FIXTURE to inspect a complete offline cache")
	}
	imageTestConfig(t)
	c := readImageFixture(t, filename)
	Config.Options.Images.Download = false
	counts := make(map[string]int)
	icons, typed := make(map[string]bool), make(map[string]bool)
	ids := make([]string, 0, len(c.Metadata))
	for id := range c.Metadata {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for imageType, data := range c.selectProgrammeImages(id) {
			counts[imageType]++
			typed[imageFilename(data.URI)] = true
		}
		// GetIcon only returns absolute URIs when downloads are disabled.
		// Prefix relative URIs on a copy to run the unchanged icon selector.
		metadata := c.Metadata[id]
		metadata.Data = append([]Data(nil), metadata.Data...)
		for i := range metadata.Data {
			if !isAbsoluteURL(metadata.Data[i].URI) {
				metadata.Data[i].URI = "https://fixture.invalid/" + metadata.Data[i].URI
			}
		}
		iconCache := &cache{Metadata: map[string]EPGoCache{id: metadata}}
		for _, icon := range iconCache.GetIcon(id) {
			icons[imageFilename(icon.Src)] = true
		}
	}
	extra := 0
	for filename := range typed {
		if !icons[filename] {
			extra++
		}
	}
	t.Logf("programmes=%d still=%d backdrop=%d poster=%d icon_files=%d typed_files=%d extra_files=%d total_files=%d",
		len(c.Metadata), counts["still"], counts["backdrop"], counts["poster"], len(icons), len(typed), extra, len(icons)+extra)
}

func TestLocalImageLinks(t *testing.T) {
	imageTestConfig(t)
	Config.Options.Images.Local = true
	if err := os.WriteFile(filepath.Join(Config.Options.Images.Path, "local.jpg"), []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}
	c := &cache{Metadata: map[string]EPGoCache{"EPtest": {Data: []Data{
		{URI: "local.jpg", Category: "Iconic", Tier: "Episode", Width: 960, Height: 540},
	}}}}
	got := c.GetImages("EPtest")
	want := "file://" + filepath.ToSlash(filepath.Join(Config.Options.Images.Path, "local.jpg"))
	if len(got) != 1 || got[0].URL != want {
		t.Fatalf("local image link = %v, want %s", got, want)
	}
}
