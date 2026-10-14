package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cache : Cache file
var Cache cache
var ImageError bool = false

// Init : Inti cache
func (c *cache) Init() {

	if c.Schedule == nil {
		c.Schedule = make(map[string][]EPGoCache)
	}

	c.Channel = make(map[string]EPGoCache)

	if c.Program == nil {
		c.Program = make(map[string]EPGoCache)
	}

	if c.Metadata == nil {
		c.Metadata = make(map[string]EPGoCache)
	}

}

func (c *cache) Remove() {
	if len(Config.Files.Cache) != 0 {
		logger.Info("Remove Cache File", "filename", Config.Files.Cache)
		os.RemoveAll(Config.Files.Cache)
		c.Init()
	}
}

func (c *cache) AddStations(data *[]byte, lineup string) {

	c.Lock()
	defer c.Unlock()

	var epgoCache EPGoCache
	var sdData SDStation

	err := json.Unmarshal(*data, &sdData)
	if err != nil {
		logger.Error("unable to unmarshal the JSON", "error", err)
		return
	}

	var channelIDs = Config.GetChannelList(lineup)

	lcnMap := make(map[string]string)
	for _, m := range sdData.Map {
		if m.AtscMajor > 0 {
			lcnMap[m.StationID] = fmt.Sprintf("%d.%d", m.AtscMajor, m.AtscMinor)
		} else if m.Channel != "" {
			lcnMap[m.StationID] = m.Channel
		}
	}

	for _, sd := range sdData.Stations {

		if ContainsString(channelIDs, sd.StationID) != -1 {

			epgoCache.StationID = sd.StationID
			epgoCache.Name = sd.Name
			epgoCache.Callsign = sd.Callsign
			epgoCache.Affiliate = sd.Affiliate
			epgoCache.BroadcastLanguage = sd.BroadcastLanguage
			epgoCache.Logo = sd.Logo
			epgoCache.Lcn = lcnMap[sd.StationID]

			c.Channel[sd.StationID] = epgoCache

		}

	}

}

func (c *cache) AddSchedule(data *[]byte) {

	c.Lock()
	defer c.Unlock()

	var epgoCache EPGoCache
	var sdData []SDSchedule

	err := json.Unmarshal(*data, &sdData)
	if err != nil {
		logger.Error("unable to unmarshal the JSON", "error", err)
		return
	}

	for _, sd := range sdData {

		if _, ok := c.Schedule[sd.StationID]; !ok {
			c.Schedule[sd.StationID] = []EPGoCache{}
		}

		for _, p := range sd.Programs {

			epgoCache.AirDateTime = p.AirDateTime
			epgoCache.AudioProperties = p.AudioProperties
			epgoCache.Duration = p.Duration
			epgoCache.LiveTapeDelay = p.LiveTapeDelay
			epgoCache.New = p.New
			epgoCache.Md5 = p.Md5
			epgoCache.ProgramID = p.ProgramID
			epgoCache.Ratings = p.Ratings
			epgoCache.VideoProperties = p.VideoProperties

			c.Schedule[sd.StationID] = append(c.Schedule[sd.StationID], epgoCache)

		}

	}

}

func (c *cache) AddProgram(gzip *[]byte, wg *sync.WaitGroup) {

	c.Lock()

	defer func() {
		c.Unlock()
		wg.Done()
	}()

	b, err := decodeSDPayload(*gzip)
	if err != nil {
		logger.Error("unable to unzip programs", "error", err)
		return
	}

	var epgoCache EPGoCache
	var sdData []SDProgram

	err = json.Unmarshal(b, &sdData)
	if err != nil {
		logger.Error("unable to unmarshal the JSON", "error", err)
		return
	}

	for _, sd := range sdData {

		epgoCache.Descriptions = sd.Descriptions
		epgoCache.EpisodeTitle150 = sd.EpisodeTitle150
		epgoCache.Genres = sd.Genres

		epgoCache.HasEpisodeArtwork = sd.HasEpisodeArtwork
		epgoCache.HasImageArtwork = sd.HasImageArtwork
		epgoCache.HasSeriesArtwork = sd.HasSeriesArtwork
		epgoCache.Metadata = sd.Metadata
		epgoCache.OriginalAirDate = sd.OriginalAirDate
		epgoCache.ResourceID = sd.ResourceID
		epgoCache.ShowType = sd.ShowType
		epgoCache.Titles = sd.Titles
		epgoCache.ContentRating = sd.ContentRating
		epgoCache.Cast = sd.Cast
		epgoCache.Crew = sd.Crew
		epgoCache.Md5 = sd.Md5

		c.Program[sd.ProgramID] = epgoCache

	}

}

func (c *cache) AddMetadata(gzip *[]byte, wg *sync.WaitGroup) {

	c.Lock()
	defer func() {
		c.Unlock()
		wg.Done()
	}()

	b, err := decodeSDPayload(*gzip)
	if err != nil {
		logger.Error("unable to unzip metadata", "error", err)
		return
	}

	var tmp = make([]interface{}, 0)

	var epgoCache EPGoCache

	err = json.Unmarshal(b, &tmp)
	if err != nil {
		logger.Error("unable to unmarshal the JSON", "error", err)
		return
	}

	for _, t := range tmp {

		var sdData SDMetadata

		jsonByte, _ := json.Marshal(t)
		err = json.Unmarshal(jsonByte, &sdData)
		if err != nil {

			var sdError SDError
			err = json.Unmarshal(jsonByte, &sdError)
			if err == nil {

				if Config.Options.SDDownloadErrors {
					err = fmt.Errorf("%s [SD API Error Code: %d] Program ID: %s", sdError.Data.Message, sdError.Data.Code, sdError.ProgramID)
					logger.Error("unable to unmarshal the JSON", "error", err)
				}

			} else {
				if Config.Options.SDDownloadErrors {
					logger.Error("unable to unmarshal the JSON", "error", err)
				}
			}

		} else {

			epgoCache.Data = sdData.Data
			c.Metadata[sdData.ProgramID] = epgoCache

		}

	}
}

func (c *cache) GetAllProgramIDs() (programIDs []string) {

	for _, channel := range c.Schedule {

		for _, schedule := range channel {

			if ContainsString(programIDs, schedule.ProgramID) == -1 {
				programIDs = append(programIDs, schedule.ProgramID)
			}

		}

	}

	return
}

// GetRequiredProgramIDs returns the programs that are not cached yet or
// whose data changed since they were cached. The schedule carries the md5 of
// the current program data, which SD changes when it updates a program, for
// example when an episode title is added after the program was announced.
func (c *cache) GetRequiredProgramIDs() (programIDs []string) {

	var seen = make(map[string]bool)

	for _, channel := range c.Schedule {

		for _, schedule := range channel {

			if seen[schedule.ProgramID] {
				continue
			}

			// Check every airing: any one of them may carry the changed md5.
			if p, ok := c.Program[schedule.ProgramID]; !ok || p.Md5 != schedule.Md5 {
				seen[schedule.ProgramID] = true
				programIDs = append(programIDs, schedule.ProgramID)
			}

		}

	}

	return
}

func (c *cache) GetRequiredMetaIDs() (metaIDs []string) {

	for id := range c.Program {

		if len(id) > 10 {

			if _, ok := c.Metadata[id]; !ok {
				metaIDs = append(metaIDs, id)
			}

		}
	}

	return
}

func (c *cache) GetIcon(id string) (i []Icon) {
	if bestIcon := c.selectIcon(id); bestIcon != nil {
		if imageURL := programmeImageURL(bestIcon.Src, id); imageURL != "" {
			bestIcon.Src = imageURL
			i = append(i, *bestIcon)
		}
	}
	return
}

// selectIcon returns the preferred icon with its SD URI, without downloading it.
func (c *cache) selectIcon(id string) (bestIcon *Icon) {

	if m, ok := c.Metadata[id]; ok {
		// Define preferences for categories and aspect ratios. Lower index = higher preference.
		categoryPrefs := map[string]int{
			"Poster Art": 0,
			"Box Art":    1,
			"Banner-L1":  2,
			"Banner-L2":  3,
			"VOD Art":    4, // Fallback category
		}
		aspectPrefs := map[string]int{
			"16x9": 0,
			"2x3":  1,
			"4x3":  2,
			"3x4":  3,
			"2x1":  4,
			"1x1":  5,
		}

		bestScore := -1

		for _, iconData := range m.Data {
			catScore, catOK := categoryPrefs[iconData.Category]
			if !catOK {
				continue // Skip categories we don't care about
			}

			aspectScore, aspectOK := aspectPrefs[iconData.Aspect]
			if !aspectOK {
				continue // Skip aspect ratios we don't care about
			}

			currentScore := catScore*10 + aspectScore

			if bestScore == -1 || currentScore < bestScore || (currentScore == bestScore && iconData.Width > bestIcon.Width) {
				bestScore = currentScore

				bestIcon = &Icon{Src: iconData.URI, Height: iconData.Height, Width: iconData.Width}
			}
		}

	}
	return
}

func (c *cache) Open() (err error) {

	data, err := os.ReadFile(Config.Files.Cache)

	if err != nil {
		if os.IsNotExist(err) {
			c.Init()
			return nil
		}
		return err
	}

	var loaded *cache
	err = json.Unmarshal(data, &loaded)
	if err != nil {
		return
	}
	if loaded == nil {
		return fmt.Errorf("cache is null")
	}
	c.Lock()
	defer c.Unlock()
	c.Channel, c.Program, c.Metadata, c.Schedule = loaded.Channel, loaded.Program, loaded.Metadata, loaded.Schedule
	c.Token, c.TokenExpires, c.ImageLimitUntil = loaded.Token, loaded.TokenExpires, loaded.ImageLimitUntil

	return
}

func (c *cache) Save() (err error) {

	c.Lock()
	defer c.Unlock()

	data, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		return err
	}

	err = writeAtomic(Config.Files.Cache, 0600, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
	if err != nil {
		return
	}

	return
}

// A token can expire during a staged fetch. Persist credentials without
// publishing the in-progress schedules/programs along with them.
func (c *cache) saveToken() error {
	var committed cache
	if err := committed.Open(); err != nil {
		return err
	}
	c.RLock()
	committed.Token, committed.TokenExpires = c.Token, c.TokenExpires
	c.RUnlock()
	return committed.Save()
}

func (c *cache) CleanUp() error {

	var count int
	logger.Info("Clean up Cache", "filename", Config.Files.Cache)

	var programIDs = c.GetAllProgramIDs()

	for id := range c.Program {

		if ContainsString(programIDs, id) == -1 {

			count++
			delete(c.Program, id)
			delete(c.Metadata, id)

		}

	}

	c.Channel = make(map[string]EPGoCache)
	c.Schedule = make(map[string][]EPGoCache)

	logger.Info("Clean up Cache", "count", count)

	return c.Save()
}

// Get data from cache
func (c *cache) GetTitle(id, lang string) (t []Title) {

	if p, ok := c.Program[id]; ok {

		var title Title

		for _, s := range p.Titles {
			title.Value = s.Title120
			title.Lang = lang
			t = append(t, title)
		}

	}

	if len(t) == 0 {
		var title Title
		title.Value = "No EPG Info"
		title.Lang = "en"
		t = append(t, title)
	}

	return
}

func (c *cache) GetSubTitle(id, lang string) (s *SubTitle) {

	if p, ok := c.Program[id]; ok {

		if len(p.EpisodeTitle150) != 0 {

			s = &SubTitle{Value: p.EpisodeTitle150, Lang: lang}

		} else if !Config.Options.SubtitleEpisodeTitleOnly {

			for _, d := range p.Descriptions.Description100 {

				s = &SubTitle{Value: d.Description, Lang: d.DescriptionLanguage}

			}

		}

	}

	return
}

func (c *cache) GetDescs(id, subTitle string) (de []Desc) {

	if p, ok := c.Program[id]; ok {

		d := p.Descriptions

		var desc Desc

		for _, tmp := range d.Description1000 {

			switch Config.Options.SubtitleIntoDescription {

			case true:
				if len(subTitle) != 0 {
					desc.Value = fmt.Sprintf("[%s]\n%s", subTitle, tmp.Description)
					break
				}

				fallthrough
			case false:
				desc.Value = tmp.Description

			}

			desc.Lang = tmp.DescriptionLanguage

			de = append(de, desc)
		}

	}

	return
}

// creditRoles maps SD cast and crew roles to XMLTV credits elements.
var creditRoles = map[string]string{
	"Director":     "director",
	"Actor":        "actor",
	"Voice":        "actor",
	"Guest Star":   "actor",
	"Guest Voice":  "actor",
	"Writer":       "writer",
	"Screenwriter": "writer",
	"Producer":     "producer",
	"Presenter":    "presenter",
	"Host":         "presenter",
	"Anchor":       "presenter",
	"Guest":        "guest",
	"Self":         "guest",
	"Contestant":   "guest",
}

func (c *cache) GetCredits(id string) *Credits {

	var cr Credits

	if Config.Options.Credits {

		if p, ok := c.Program[id]; ok {

			add := func(role, name, character string) {

				switch creditRoles[role] {

				case "director":
					cr.Director = append(cr.Director, Director{Value: name})

				case "actor":
					cr.Actor = append(cr.Actor, Actor{Value: name, Role: character})

				case "writer":
					cr.Writer = append(cr.Writer, Writer{Value: name})

				case "producer":
					cr.Producer = append(cr.Producer, Producer{Value: name})

				case "presenter":
					cr.Presenter = append(cr.Presenter, Presenter{Value: name})

				case "guest":
					cr.Guest = append(cr.Guest, Guest{Value: name})

				}

			}

			// Crew
			for _, crew := range p.Crew {
				add(crew.Role, crew.Name, "")
			}

			// Cast
			for _, cast := range p.Cast {
				add(cast.Role, cast.Name, cast.CharacterName)
			}

		}

	}

	if len(cr.Director)+len(cr.Actor)+len(cr.Writer)+len(cr.Producer)+len(cr.Presenter)+len(cr.Guest) == 0 {
		return nil
	}

	return &cr
}

// programTypes are the generic programme types derived from the SD program
// ID prefix. Clients like MythTV look for exactly these categories.
var programTypes = map[string]string{
	"MV": "movie",
	"EP": "series",
	"SH": "tvshow",
	"SP": "sports",
}

func (c *cache) GetCategory(id string) (ca []Category) {

	if p, ok := c.Program[id]; ok {

		seen := make(map[string]bool)
		add := func(value string) {
			if value != "" && !seen[value] {
				seen[value] = true
				ca = append(ca, Category{Value: value, Lang: "en"})
			}
		}

		for _, g := range p.Genres {
			add(g)
		}

		// SD show type, e.g. "Series", "Feature Film" or "Sports event"
		add(p.ShowType)

		if len(id) >= 2 {
			add(programTypes[id[:2]])
		}

	}

	return
}

func (c *cache) GetEpisodeNum(id string) (ep []EpisodeNum) {

	var seaseon, episode int

	if p, ok := c.Program[id]; ok {

		for _, m := range p.Metadata {

			seaseon = m.Gracenote.Season
			episode = m.Gracenote.Episode

			var episodeNum EpisodeNum

			if seaseon != 0 && episode != 0 {

				episodeNum.Value = fmt.Sprintf("%d.%d.", seaseon-1, episode-1)
				episodeNum.System = "xmltv_ns"
				ep = append(ep, episodeNum)
			}

		}

		if seaseon != 0 && episode != 0 {

			var episodeNum EpisodeNum
			episodeNum.Value = fmt.Sprintf("S%d E%d", seaseon, episode)
			episodeNum.System = "onscreen"
			ep = append(ep, episodeNum)

		}

		// ✅ Fixed dd_progid formatting
		if len(ep) == 0 {

			var episodeNum EpisodeNum

			switch id[0:2] {

			case "EP":
				// Correct format: keep prefix, add dot + episode part if present
				if len(id) > 10 {
					episodeNum.Value = fmt.Sprintf("%s.%s", id[:10], id[10:])
				} else {
					episodeNum.Value = id + ".0000"
				}

			case "SH", "MV":
				episodeNum.Value = id + ".0000"

			default:
				episodeNum.Value = id
			}

			episodeNum.System = "dd_progid"
			ep = append(ep, episodeNum)

		}

		if len(p.OriginalAirDate) > 0 {

			var episodeNum EpisodeNum
			episodeNum.Value = p.OriginalAirDate
			episodeNum.System = "original-air-date"
			ep = append(ep, episodeNum)

		}

	}

	return
}

func (c *cache) GetPreviouslyShown(id string) (prev *PreviouslyShown) {

	prev = &PreviouslyShown{}

	if p, ok := c.Program[id]; ok {
		prev.Start = p.OriginalAirDate
	}

	return
}

func (c *cache) GetRating(id, countryCode string) (ra []Rating) {

	if !Config.Options.Rating.Guidelines {
		return
	}

	var add = func(code, body, country string) {

		switch Config.Options.Rating.CountryCodeAsSystem {

		case true:
			ra = append(ra, Rating{Value: code, System: country})

		case false:
			ra = append(ra, Rating{Value: code, System: body})

		}

	}

	if p, ok := c.Program[id]; ok {

		switch len(Config.Options.Rating.Countries) {

		case 0:
			for _, r := range p.ContentRating {

				if len(ra) == Config.Options.Rating.MaxEntries && Config.Options.Rating.MaxEntries != 0 {
					return
				}

				if countryCode == r.Country {
					add(r.Code, r.Body, r.Country)
				}

			}

			for _, r := range p.ContentRating {

				if len(ra) == Config.Options.Rating.MaxEntries && Config.Options.Rating.MaxEntries != 0 {
					return
				}

				if countryCode != r.Country {
					add(r.Code, r.Body, r.Country)
				}

			}

		default:
			for _, cCode := range Config.Options.Rating.Countries {

				for _, r := range p.ContentRating {

					if len(ra) == Config.Options.Rating.MaxEntries && Config.Options.Rating.MaxEntries != 0 {
						return
					}

					if cCode == r.Country {

						add(r.Code, r.Body, r.Country)

					}

				}

			}

		}

	}

	return
}

// imageDownloadsStopped is set when Schedules Direct tells us to stop
// requesting images (daily limit reached, token or account problem).
// Every further request would count against the account, so no more image
// requests are sent for the rest of the run.
var imageDownloadsStopped bool

// imageFailures remembers images that failed in this run, so programmes
// sharing an image do not request it again. Both are guarded by imageMu
// because images are downloaded in parallel.
var imageFailures = map[string]error{}
var imageMu sync.Mutex

func imagesStopped() bool {
	imageMu.Lock()
	defer imageMu.Unlock()
	return imageDownloadsStopped
}

const sdImageURL = "https://json.schedulesdirect.org/20141201/image/"

func isAbsoluteURL(uri string) bool {
	return strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
}

// imageFilename returns the local file name for an image URI. SD image URIs
// are content hashes, so programs sharing an image (e.g. all episodes of a
// series) share one file and it is only downloaded once.
func imageFolder() string {
	if Config.Options.Images.Path == "" {
		return "images"
	}
	return Config.Options.Images.Path
}

func imageFilename(uri string) string {
	if u, err := url.Parse(uri); err == nil {
		uri = u.Path
	}
	return path.Base(uri)
}

// sdImageStopCodes are Schedules Direct error codes after which image
// downloads must stop for the current run.
var sdImageStopCodes = map[int]bool{
	1004: true, // TOKEN_MISSING
	4001: true, // ACCOUNT_EXPIRED
	4003: true, // INVALID_USER
	4004: true, // ACCOUNT_LOCKOUT
	4005: true, // JSON_ACCOUNT_ACCESS_DISABLED
	4006: true, // TOKEN_EXPIRED
	4007: true, // APPLICATION_DISABLED
	4008: true, // ACCOUNT_INACTIVE
	4009: true, // TOO_MANY_LOGINS
	4010: true, // TOO_MANY_UNIQUE_IPS
	5002: true, // MAX_IMAGE_DOWNLOADS
	5003: true, // MAX_IMAGE_DOWNLOADS_TRIAL
}

// sdImageLimitCodes are the daily image limit errors. The limit stays reached
// until SD resets the counter, so later runs that day must not ask again.
var sdImageLimitCodes = map[int]bool{
	5002: true, // MAX_IMAGE_DOWNLOADS
	5003: true, // MAX_IMAGE_DOWNLOADS_TRIAL
}

// imageLimitReset returns when SD resets the daily image counter (00:00Z).
func imageLimitReset(now time.Time) time.Time {
	return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

// downloadImage downloads an image into the image folder and returns the
// file name. Existing files and failed images are not requested again.
func downloadImage(uri string) (string, error) {
	filename, err := fetchImage(uri)
	if err != nil && filename != "" {
		imageMu.Lock()
		if !imageDownloadsStopped {
			imageFailures[filename] = err
		}
		imageMu.Unlock()
		return "", err
	}
	return filename, err
}

// fetchImage returns the image file name, also with an error, so that
// downloadImage can remember the failure.
func fetchImage(uri string) (string, error) {

	folderImage := imageFolder()

	// Create the "images" folder if it doesn't exist
	if _, err := os.Stat(folderImage); os.IsNotExist(err) {
		err = os.MkdirAll(folderImage, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create images folder: %w", err)
		}
	}

	filename := imageFilename(uri)
	if filename == "" || filename == "." || filename == "/" {
		return "", fmt.Errorf("invalid image uri: %q", uri)
	}
	filePath := filepath.Join(folderImage, filename)
	if _, err := os.Stat(filePath); err == nil {
		return filename, nil
	}

	imageMu.Lock()
	failure, failed := imageFailures[filename]
	stopped := imageDownloadsStopped
	imageMu.Unlock()
	if failed {
		return "", failure
	}
	if stopped {
		return "", fmt.Errorf("image downloads stopped for this run")
	}

	imageURL := uri
	if !isAbsoluteURL(uri) {
		// The token goes into the query string rather than a header, so it is
		// not forwarded when SD redirects to the storage bucket.
		imageURL = sdImageURL + uri + "?token=" + url.QueryEscape(Token)
	}

	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return filename, fmt.Errorf("failed to create image request for %s: %w", filename, err)
	}
	req.Header.Set("User-Agent", AppName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return filename, fmt.Errorf("failed to download image %s: %w", filename, redactToken(err))
	}
	defer resp.Body.Close()

	// SD reports errors (including the daily image limit) as JSON, sometimes
	// with HTTP 200. Only save responses that are actually images.
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		var sdErr struct {
			Code     int    `json:"code"`
			Response string `json:"response"`
			Message  string `json:"message"`
		}
		if json.Unmarshal(body, &sdErr) == nil && sdErr.Code != 0 {
			if sdImageStopCodes[sdErr.Code] {
				imageMu.Lock()
				imageDownloadsStopped = true
				if sdImageLimitCodes[sdErr.Code] {
					Cache.ImageLimitUntil = imageLimitReset(time.Now()).Unix()
				}
				imageMu.Unlock()
				logger.Warn("Schedules Direct returned an error, no more images will be requested in this run",
					"code", sdErr.Code, "response", sdErr.Response, "message", sdErr.Message)
			}
			return filename, fmt.Errorf("schedules direct error %d (%s) for image %s", sdErr.Code, sdErr.Response, filename)
		}
		return filename, fmt.Errorf("unexpected response for image %s: %s, content type %q", filename, resp.Status, resp.Header.Get("Content-Type"))
	}

	// Write to a temporary file first so an interrupted download never
	// leaves a truncated image that would be served forever.
	tmp, err := os.CreateTemp(folderImage, filename+".*.tmp")
	if err != nil {
		return filename, fmt.Errorf("failed to create file: %w", err)
	}
	_, err = io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return filename, fmt.Errorf("failed to save image %s: %w", filename, err)
	}
	if err = os.Chmod(tmp.Name(), 0644); err != nil {
		os.Remove(tmp.Name())
		return filename, fmt.Errorf("failed to save image %s: %w", filename, err)
	}
	if err = os.Rename(tmp.Name(), filePath); err != nil {
		os.Remove(tmp.Name())
		return filename, fmt.Errorf("failed to save image %s: %w", filename, err)
	}

	return filename, nil
}

// redactToken removes the SD token from errors that contain the request URL.
func redactToken(err error) error {
	if Token == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), Token, "REDACTED"))
}
