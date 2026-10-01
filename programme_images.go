package main

import (
	"net/url"
	"path/filepath"
	"strings"
)

// programmeImageURL shares the icon download path, including file reuse and
// SD's stop-on-error handling. Never expose the authenticated download URL.
func programmeImageURL(uri, id string) string {
	if Config.Options.Images.Download {
		filename, err := downloadImage(uri)
		if err != nil {
			if Config.Options.SDDownloadErrors {
				logger.Warn("Could not download image", "programID", id, "error", err)
			}
			return ""
		}
		if Config.Options.Images.Local {
			return localImageURL(filename)
		}
		return "http://" + Config.Server.Address + ":" + Config.Server.Port + "/" + filename
	}
	// Relative SD image URIs require a token and cannot be written to XMLTV.
	if isAbsoluteURL(uri) {
		return uri
	}
	return ""
}

// localImageURL links an image file directly, for clients on the same machine.
func localImageURL(filename string) string {
	folder, err := filepath.Abs(imageFolder())
	if err != nil {
		folder = imageFolder()
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(folder, filename))}).String()
}

// selectProgrammeImages selects at most one image per type. Tier preference
// takes priority over aspect, then larger widths win, as in GetIcon.
func (c *cache) selectProgrammeImages(id string) map[string]Data {
	images := make(map[string]Data)
	bestScores := make(map[string]int)
	aspectPrefs := map[string]int{"16x9": 0, "2x3": 1, "4x3": 2, "3x4": 3, "2x1": 4, "1x1": 5}
	movie := strings.HasPrefix(id, "MV") || c.Program[id].ShowType == "Movie" || c.Program[id].ShowType == "Feature Film"

	for _, data := range c.Metadata[id].Data {
		if data.URI == "" || data.Width <= 0 || data.Height <= 0 {
			continue
		}
		var imageType string
		tierScore := 0
		switch {
		case data.Category == "Iconic" && data.Tier == "Episode":
			imageType = "still"
		case data.Category == "Iconic" && (data.Tier == "Season" || data.Tier == "Series"):
			imageType = "backdrop"
		case movie && data.Category == "Iconic" && data.Tier == "":
			imageType = "backdrop"
		case data.Category == "Backdrop-Sports":
			imageType = "backdrop"
			tierScore = 2
		case movie && data.Category == "Poster Art":
			imageType = "poster"
		case !movie && data.Category == "Banner-L1" && (data.Tier == "Season" || data.Tier == "Series"):
			imageType = "poster"
		default:
			continue
		}
		if data.Tier == "Series" && !(imageType == "poster" && movie) {
			tierScore = 1
		}
		aspectScore, ok := aspectPrefs[data.Aspect]
		if !ok {
			aspectScore = len(aspectPrefs)
		}
		if imageType == "poster" && movie {
			switch data.Aspect {
			case "2x3":
				aspectScore = 0
			case "16x9":
				aspectScore = 1
			}
		}
		score := tierScore*10 + aspectScore
		best, found := images[imageType]
		if !found || score < bestScores[imageType] || (score == bestScores[imageType] && data.Width > best.Width) {
			images[imageType] = data
			bestScores[imageType] = score
		}
	}
	return images
}

// GetImages : Optional typed programme images, served from downloaded files.
func (c *cache) GetImages(id string) (images []ProgrammeImage) {
	if !Config.Options.Images.Typed || !Config.Options.Images.Download {
		return
	}
	selected := c.selectProgrammeImages(id)
	for _, imageType := range []string{"still", "backdrop", "poster"} {
		data, ok := selected[imageType]
		if !ok {
			continue
		}
		imageURL := programmeImageURL(data.URI, id)
		if imageURL == "" {
			continue
		}
		orient := "L"
		largest := data.Width
		if data.Height > data.Width {
			orient = "P"
			largest = data.Height
		}
		size := "3"
		if largest < 200 {
			size = "1"
		} else if largest <= 400 {
			size = "2"
		}
		images = append(images, ProgrammeImage{Type: imageType, Size: size, Orient: orient, System: "schedulesdirect", URL: imageURL})
	}
	return
}
