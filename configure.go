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

	Cache.Open()

	if len(Config.Account.Username) != 0 || len(Config.Account.Password) != 0 {
		sd.Login()
		Cache.Save()
		sd.Status()
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
		if len(Config.Account.Username) == 0 || len(Config.Account.Password) == 0 {
			entry.account()
			err = sd.Login()
			if err != nil {
				os.RemoveAll(Config.File + ".yaml")
				os.Exit(0)
			}
			Cache.Save()
			sd.Status()

		}

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

		var selection = menu.Show()

		entry = menu.Entry[selection]

		switch selection {

		case 0:
			Config.Save()
			os.Exit(0)

		case 1:
			entry.account()
			Cache.Token = ""
			Cache.TokenExpires = 0
			sd.Login()
			Cache.Save()
			sd.Status()

		case 2:
			entry.addLineup(&sd)
			sd.Status()

		case 3:
			entry.removeLineup(&sd)
			sd.Status()

		case 4:
			entry.manageChannels(&sd)
			sd.Status()

		case 5:
			sd.Update(filename)

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

	if !bytes.Contains(data, []byte("Live and New icons")){
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
	c.Options.Credits = false
	Config.Options.Rating.Guidelines = true
	Config.Options.Rating.Countries = []string{"USA", "CHE", "DE"}
	Config.Options.Rating.CountryCodeAsSystem = false
	Config.Options.Rating.MaxEntries = 1
	Config.Options.LiveIcons = false
	// Options images
	c.Options.Images.Download = false
	c.Options.Images.Path = ""
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
