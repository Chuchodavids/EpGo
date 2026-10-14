package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Configure : Configure config file
func Configure(filename string) (err error) {

	var menu Menu
	var entry Entry
	var sd SD

	Config.File = strings.TrimSuffix(filename, filepath.Ext(filename))

	err = Config.Open()
	if err != nil {
		return
	}

	err = sd.Init()
	if err != nil {
		return err
	}

	if err = Cache.Open(); err != nil {
		return err
	}

	// Interactive authentication failures must leave Account/Exit reachable.
	// Storage failures still abort; headless Update continues to fail on either.
	authenticated := false
	authenticate := func() error {
		authenticated = false
		if err := sd.Login(); err != nil {
			logger.Error("unable to authenticate; edit Account or exit", "error", err)
			return nil
		}
		if err := Cache.Save(); err != nil {
			return err
		}
		if err := sd.Status(); err != nil {
			logger.Error("unable to get account status; edit Account or exit", "error", err)
			return nil
		}
		authenticated = true
		return nil
	}
	if Config.Account.Username != "" && Config.Account.Password != "" {
		if err = authenticate(); err != nil {
			return err
		}
	}

	for {

		menu.Entry = make(map[int]Entry)

		menu.Headline = fmt.Sprintf("%s [%s.yaml]", getMsg(0000), Config.File)
		menu.Select = getMsg(0001)

		// Exit
		entry.Key = 0
		entry.Value = getMsg(0010)
		menu.Entry[0] = entry

		// Account
		entry.Key = 1
		entry.Value = getMsg(0011)
		menu.Entry[1] = entry

		if authenticated {
			// Add Lineup
			entry.Key = 2
			entry.Value = getMsg(0012)
			menu.Entry[2] = entry

			// Remove Lineup
			entry.Key = 3
			entry.Value = getMsg(0013)
			menu.Entry[3] = entry

			// Manage Channels
			entry.Key = 4
			entry.Value = getMsg(0014)
			menu.Entry[4] = entry

			// Create XMLTV file
			entry.Key = 5
			entry.Value = fmt.Sprintf("%s [%s]", getMsg(0016), Config.Files.XMLTV)
			menu.Entry[5] = entry
		}

		var selection = menu.Show()

		entry = menu.Entry[selection]

		switch selection {

		case 0:
			return Config.Save()

		case 1:
			entry.account()
			Cache.Token = ""
			Cache.TokenExpires = 0
			sd.Token, Token = "", ""
			// Do not reuse the previous account's token after a failed edit/exit.
			if err = Cache.Save(); err != nil {
				return err
			}
			if err = authenticate(); err != nil {
				return err
			}

		case 2:
			entry.addLineup(&sd)
			if err = sd.Status(); err != nil {
				return err
			}

		case 3:
			entry.removeLineup(&sd)
			if err = sd.Status(); err != nil {
				return err
			}

		case 4:
			entry.manageChannels(&sd)
			if err = sd.Status(); err != nil {
				return err
			}

		case 5:
			if err = sd.Update(filename); err != nil {
				return err
			}

		}

	}

}

func (c *config) Open() (err error) {

	data, err := os.ReadFile(fmt.Sprintf("%s.yaml", c.File))
	var rmCacheFile, newOptions bool

	if err != nil {
		// File is missing, create new config file (YAML)
		c.InitConfig()
		err = c.Save()
		if err != nil {
			return
		}

		return nil
	}

	// Open config file and convert Yaml to Struct (config)
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return
	}

	/*
	   New config options
	*/

	// Credits tag
	if !bytes.Contains(data, []byte("credits tag")) {

		rmCacheFile = true
		newOptions = true

		Config.Options.Credits = true

		logger.Info("update config file", "filename", Config.File+".yaml")
	}

	// Rating tag
	if !bytes.Contains(data, []byte("Rating:")) {

		newOptions = true

		Config.Options.Rating.Guidelines = true
		Config.Options.Rating.Countries = []string{}
		Config.Options.Rating.CountryCodeAsSystem = false
		Config.Options.Rating.MaxEntries = 1

		logger.Info("update config file", "filename", Config.File+".yaml")

	}

	if !bytes.Contains(data, []byte("Live and New icons")) {
		newOptions = true
		Config.Options.LiveIcons = false
	}

	if !bytes.Contains(data, []byte("The MovieDB cache")) {
		newOptions = true
		Config.Files.TmdbCacheFile = ""
	}

	// SD errors
	if !bytes.Contains(data, []byte("download errors")) {

		newOptions = true
		Config.Options.SDDownloadErrors = false

		logger.Info("update config file", "filename", Config.File+".yaml")
	}

	if !bytes.Contains(data, []byte("The MovieDB:")) {
		newOptions = true
		Config.Options.Images.Tmdb.Enable = false
		Config.Options.Images.Tmdb.ApiKey = ""
	}

	if !bytes.Contains(data, []byte("Images:")) {
		newOptions = true
		Config.Options.Images.Download = false
		Config.Options.Images.Path = ""
	}

	if !bytes.Contains(data, []byte("Server:")) {
		newOptions = true
		Config.Server.Enable = false
		Config.Server.Address = "localhost"
		Config.Server.Port = "80"
	}

	if !bytes.Contains(data, []byte("Insert typed image tags into XML file:")) {
		newOptions = true
		c.Options.Images.Typed = false
	}

	if c.Server.Address == "" {
		c.Server.Address = "localhost"
		newOptions = true
	}

	if c.Server.Port == "" {
		c.Server.Port = "80"
		newOptions = true
	}

	if newOptions {
		err = c.Save()
		if err != nil {
			return
		}

	}

	if rmCacheFile {
		Cache.Remove()
	}

	return
}

func (c *config) Save() (err error) {

	data, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}

	err = os.WriteFile(fmt.Sprintf("%s.yaml", c.File), data, 0644)
	if err != nil {
		return
	}

	return
}

func (c *config) InitConfig() {

	// Files
	c.Files.Cache = fmt.Sprintf("%s_cache.json", c.File)
	c.Files.XMLTV = fmt.Sprintf("%s.xml", c.File)
	c.Files.TmdbCacheFile = fmt.Sprintf("%s_tmdb_cache.json", c.File)

	// Server
	c.Server.Enable = false
	c.Server.Address = "localhost"
	c.Server.Port = "80"

	// Options
	c.Options.Schedule = 7
	c.Options.SubtitleIntoDescription = false
	c.Options.SubtitleEpisodeTitleOnly = false
	c.Options.Credits = false
	Config.Options.Rating.Guidelines = true
	Config.Options.Rating.Countries = []string{"USA", "CHE", "DE"}
	Config.Options.Rating.CountryCodeAsSystem = false
	Config.Options.Rating.MaxEntries = 1
	Config.Options.LiveIcons = false
	// Options images
	c.Options.Images.Download = false
	c.Options.Images.Typed = false
	c.Options.Images.Path = ""
	c.Options.Images.Cleanup = 0
	c.Options.Images.Local = false
	c.Options.Images.Tmdb.Enable = false
	c.Options.Images.Tmdb.ApiKey = ""

}

func (c *config) GetChannelList(lineup string) (list []string) {

	for _, channel := range c.Station {

		switch len(lineup) {

		case 0:
			list = append(list, channel.ID)

		default:
			if lineup == channel.Lineup {
				list = append(list, channel.ID)
			}

		}

	}

	return
}

func (c *config) GetLineupCountry(id string) (countryCode string) {

	for _, channel := range c.Station {

		if id == channel.ID {
			countryCode = strings.Split(channel.Lineup, "-")[0]
			return
		}

	}

	return
}
