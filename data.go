package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Update : Update data from Schedules Direct and create the XMLTV file
func (sd *SD) Update(filename string) (err error) {

	Config.File = strings.TrimSuffix(filename, filepath.Ext(filename))

	// reads the Config file
	_, err = os.ReadFile(fmt.Sprintf("%s.yaml", Config.File))

	if err != nil {
		return
	}

	err = Config.Open()
	if err != nil {
		return
	}

	// loads default functions and variables
	err = sd.Init()
	if err != nil {
		return err
	}

	// load cache before login to check for cached token
	err = Cache.Open()
	if err != nil {
		logger.Error("unable to open the cache", "error", err)
		return err
	}

	err = sd.Login()
	if err != nil {
		return err
	}

	err = Cache.Save()
	if err != nil {
		logger.Warn("unable to save token to cache", "error", err)
	}

	err = sd.Status()
	if err != nil{
		return err
	}

	sd.GetData()

	runtime.GC()

	err = CreateXMLTV(filename)
	if err != nil {
		logger.Error("unable to create the XMLTV file", "error", err)
		return
	}

	Cache.CleanUp()

	runtime.GC()

	if Config.Server.Enable {
		imagePath := Config.Options.Images.Path
		if imagePath == "" {
			imagePath = "images"
		}
		go StartServer(imagePath, Config.Server.Port)
		select {}
	}

	return
}

// GetData : Get data from Schedules Direct
func (sd *SD) GetData() {

	var err error
	var wg sync.WaitGroup
	var count = 0

	err = Cache.Open()
	if err != nil {
		logger.Error("unable to open the cache", "error", err)
		return
	}
	Cache.Init()

	// Channel list
	Cache.Channel = make(map[string]EPGoCache)

	var lineup []string

	for _, l := range sd.Resp.Status.Lineups {
		lineup = append(lineup, l.Lineup)
	}

	for _, id := range lineup {

		sd.Req.Parameter = fmt.Sprintf("/%s", id)
		sd.Req.Type = "GET"

		sd.Lineups()

		Cache.AddStations(&sd.Resp.Body, id)

	}

	// Schedule
	logger.Info("Download Schedule", "days", Config.Options.Schedule)

	var limit = 5000

	var days = make([]string, 0)
	var channels = make([]interface{}, 0)

	for i := 0; i < Config.Options.Schedule; i++ {
		var nextDay = time.Now().Add(time.Hour * time.Duration(24*i))
		days = append(days, nextDay.Format("2006-01-02"))
	}

	for i, channel := range Config.Station {

		count++

		channel.Date = days
		channels = append(channels, channel)

		if count == limit || i == len(Config.Station)-1 {

			sd.Req.Data, err = json.Marshal(channels)
			if err != nil {
				logger.Error("unable to marshal the JSON", "error", err)
				return
			}

			sd.Schedule()

			wg.Add(1)
			go func() {

				Cache.AddSchedule(&sd.Resp.Body)

				wg.Done()

			}()

			count = 0
			channels = make([]interface{}, 0)

		}

	}

	wg.Wait()

	// Program and Metadata
	count = 0
	sd.Req.Data = []byte{}

	var types = []string{"programs", "metadata"}
	var programIds = Cache.GetRequiredProgramIDs()
	var allIDs = Cache.GetAllProgramIDs()
	var programs = make([]interface{}, 0)

	logger.Info("Download Program Informations", "new", len(programIds), "cached", len(allIDs)-len(programIds))

	for _, t := range types {

		switch t {
		case "metadata":
			sd.Req.URL = fmt.Sprintf("%smetadata/programs/", sd.BaseURL)
			sd.Req.Call = "metadata"
			programIds = Cache.GetRequiredMetaIDs()
			limit = 500
			logger.Info("Download missing Metadata", "count", len(programIds))

		case "programs":

			sd.Req.URL = fmt.Sprintf("%sprograms", sd.BaseURL)
			sd.Req.Call = "programs"
			limit = 5000

		}

		for i, p := range programIds {

			count++

			programs = append(programs, p)

			if count == limit || i == len(programIds)-1 {

				sd.Req.Data, err = json.Marshal(programs)
				if err != nil {
					logger.Error("unable to marshal the JSON", "error", err)
					return
				}

				err := sd.Program()
				if err != nil {
					logger.Error("unable to download the programs", "error", err)
				} else {
					wg.Add(1)

					switch t {
					case "metadata":
						go Cache.AddMetadata(&sd.Resp.Body, &wg)

					case "programs":
						go Cache.AddProgram(&sd.Resp.Body, &wg)

					}
				}

				count = 0
				programs = make([]interface{}, 0)
				wg.Wait()

			}

		}

	}

	err = Cache.Save()
	if err != nil {
		logger.Error("unable to save the JSON", "error", err)
		return
	}
}
