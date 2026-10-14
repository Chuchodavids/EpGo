package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Update publishes usable fresh data with cached fallback for failed portions.
func (sd *SD) Update(filename string) error {
	Config.File = strings.TrimSuffix(filename, filepath.Ext(filename))
	// Headless runs must not silently create an empty interactive config.
	if _, err := os.ReadFile(Config.File + ".yaml"); err != nil {
		return err
	}
	if err := Config.Open(); err != nil {
		return err
	}
	if err := sd.Init(); err != nil {
		return err
	}
	if err := Cache.Open(); err != nil {
		return err
	}
	if err := sd.Login(); err != nil {
		return err
	}
	if err := Cache.Save(); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	if err := sd.Status(); err != nil {
		return err
	}
	if err := sd.GetData(); err != nil {
		return err
	}
	runtime.GC()
	if err := createXMLTV(filename, sd.update.FetchStopped == nil); err != nil {
		return err
	}
	sd.update.log()
	if err := Cache.CleanUp(); err != nil {
		return err
	}
	runtime.GC()
	if Config.Server.Enable {
		go StartServer(imageFolder(), Config.Server.Port)
		select {}
	}
	return nil
}

// GetData stages a bounded guide window. Failures are scoped to station/dates
// and programme IDs; global session/account errors still stop provider calls.
// A fresh station/date replaces its old schedule, rather than appending to it.
func (sd *SD) GetData() error {
	if err := Cache.Open(); err != nil {
		return err
	}
	sd.update = updateSummary{}
	previousChannels, previousSchedules := Cache.Channel, Cache.Schedule
	Cache.Init()
	Cache.Schedule = make(map[string][]EPGoCache)
	if len(Config.Station) == 0 || Config.Options.Schedule <= 0 {
		return fmt.Errorf("no stations or schedule days configured")
	}
	for _, station := range Config.Station {
		if old, ok := previousChannels[station.ID]; ok {
			Cache.Channel[station.ID] = old
		} else {
			name := station.Name
			if name == "" {
				name = station.ID
			}
			Cache.Channel[station.ID] = EPGoCache{StationID: station.ID, Name: name, Callsign: name}
		}
	}
	var lineups []string
	for _, l := range sd.Resp.Status.Lineups {
		lineups = append(lineups, l.Lineup)
	}
	for _, id := range lineups {
		sd.Req.Parameter, sd.Req.Type = "/"+id, "GET"
		err := sd.update.FetchStopped
		if err == nil {
			err = sd.Lineups()
		}
		if err != nil {
			if stopFetching(err) {
				sd.update.FetchStopped = err
			}
			logger.Warn("Lineup unavailable; using cached/configured channels", "lineup", id, "error", err)
			sd.update.LineupFailures++
			continue
		}
		Cache.AddStations(&sd.Resp.Body, id)
	}

	// SD dates are UTC; local dates near midnight can omit today's UTC data.
	days := make([]string, Config.Options.Schedule)
	now := time.Now().UTC()
	for i := range days {
		days[i] = now.AddDate(0, 0, i).Format("2006-01-02")
	}
	freshDays := make(map[scheduleKey]bool)
	for start := 0; start < len(Config.Station); start += 5000 {
		end := min(start+5000, len(Config.Station))
		channels := append([]channel(nil), Config.Station[start:end]...)
		for i := range channels {
			channels[i].Date = days
		}
		var err error
		sd.Req.Data, err = json.Marshal(channels)
		if err != nil {
			return err
		}
		fresh, rangeEnds := map[scheduleKey]SDSchedule{}, map[scheduleKey]bool{}
		err = sd.update.FetchStopped
		if err == nil {
			err = sd.Schedule()
			if err == nil {
				fresh, rangeEnds, err = selectSchedules(sd.Resp.Body, channels)
			}
		}
		if err != nil {
			if stopFetching(err) {
				sd.update.FetchStopped = err
			}
			logger.Warn("Schedule batch unavailable; using cached days", "error", err)
		}
		for _, ch := range channels {
			for _, day := range days {
				key := scheduleKey{ch.ID, day}
				if schedule, ok := fresh[key]; ok {
					body, err := json.Marshal([]SDSchedule{schedule})
					if err != nil {
						return err
					}
					Cache.AddSchedule(&body)
					freshDays[key] = true
					sd.update.FreshScheduleDays++
					continue
				}
				var fallback []EPGoCache
				for _, airing := range previousSchedules[ch.ID] {
					if airingDate(airing) == day && airing.ProgramID != "" && !airing.AirDateTime.IsZero() && airing.Duration > 0 {
						airing.ScheduleDate = day
						fallback = append(fallback, airing)
					}
				}
				if len(fallback) > 0 {
					Cache.Schedule[ch.ID] = append(Cache.Schedule[ch.ID], fallback...)
					sd.update.CachedScheduleDays++
				} else if !rangeEnds[key] {
					sd.update.MissingScheduleDays++
				}
				if !rangeEnds[key] {
					logger.Warn("Schedule day unavailable", "station", ch.ID, "date", day, "cached_airings", len(fallback))
				}
			}
		}
	}

	freshPrograms := make(map[string]bool)
	for _, kind := range []string{"programs", "metadata"} {
		ids, limit := Cache.GetRequiredProgramIDs(), 5000
		sd.Req.URL = sd.BaseURL + "programs"
		if kind == "metadata" {
			if !hasUsableUpdate(freshDays, freshPrograms) {
				if sd.update.FetchStopped != nil {
					return fmt.Errorf("no usable fresh schedule or programme data; keeping previous guide: %w", sd.update.FetchStopped)
				}
				return fmt.Errorf("no usable fresh schedule or programme data; keeping previous guide")
			}
			ids, limit = Cache.GetRequiredMetaIDs(), 500
			sd.Req.URL = sd.BaseURL + "metadata/programs/"
		}
		for start := 0; start < len(ids); start += limit {
			batch := ids[start:min(start+limit, len(ids))]
			var err error
			sd.Req.Data, err = json.Marshal(batch)
			if err != nil {
				return err
			}
			var good map[string]json.RawMessage
			err = sd.update.FetchStopped
			if err == nil {
				err = sd.Program()
				if err == nil {
					good, err = selectPrograms(sd.Resp.Body, batch, kind == "metadata")
				}
			}
			if err != nil {
				if stopFetching(err) {
					sd.update.FetchStopped = err
				}
				logger.Warn("Data batch unavailable; using cached data", "kind", kind, "error", err)
			}
			var entries []json.RawMessage
			for _, id := range batch {
				if raw, ok := good[id]; ok {
					entries = append(entries, raw)
					if kind == "programs" {
						freshPrograms[id] = true
					}
				} else if kind == "programs" {
					cached := usableProgram(Cache.Program[id])
					if cached {
						sd.update.CachedPrograms++
					} else {
						sd.update.MissingPrograms++
					}
					logger.Warn("Programme unavailable", "programID", id, "cached", cached)
				} else {
					sd.update.MetadataFailures++
				}
			}
			body, err := json.Marshal(entries)
			if err != nil {
				return err
			}
			var wg sync.WaitGroup
			wg.Add(1)
			if kind == "programs" {
				Cache.AddProgram(&body, &wg)
			} else {
				Cache.AddMetadata(&body, &wg)
			}
		}
	}

	for station, airings := range Cache.Schedule {
		var fresh []EPGoCache
		for _, airing := range airings {
			if usableProgram(Cache.Program[airing.ProgramID]) {
				if freshDays[scheduleKey{station, airingDate(airing)}] {
					fresh = append(fresh, airing)
				}
			}
		}
		var merged []EPGoCache
		for _, airing := range airings {
			// A fresh late-night event may overlap tomorrow's cached schedule.
			// Prefer the fresh event without inventing new timings for stale ones.
			if !freshDays[scheduleKey{station, airingDate(airing)}] && overlapsAny(airing, fresh) {
				continue
			}
			merged = append(merged, airing)
		}
		sort.Slice(merged, func(i, j int) bool { return merged[i].AirDateTime.Before(merged[j].AirDateTime) })
		Cache.Schedule[station] = merged
	}
	return Cache.Save()
}
