package main

import (
	"encoding/json"
	"errors"
	"time"
)

type scheduleKey struct{ station, date string }

type updateSummary struct {
	FreshScheduleDays, CachedScheduleDays, MissingScheduleDays        int
	CachedPrograms, MissingPrograms, MetadataFailures, LineupFailures int
	FetchStopped                                                      error
}

func (s updateSummary) log() {
	log := logger.Info
	message := "EPG update published"
	if s.FetchStopped != nil || s.CachedScheduleDays+s.MissingScheduleDays+s.CachedPrograms+s.MissingPrograms+s.MetadataFailures+s.LineupFailures > 0 {
		log, message = logger.Warn, "EPG update published with gaps"
	}
	log(message, "fresh_schedule_days", s.FreshScheduleDays, "cached_schedule_days", s.CachedScheduleDays,
		"missing_schedule_days", s.MissingScheduleDays, "cached_programs", s.CachedPrograms,
		"missing_programs", s.MissingPrograms, "metadata_failures", s.MetadataFailures, "lineup_failures", s.LineupFailures,
		"fetch_stopped", s.FetchStopped)
}

// Account/session, throttling and unknown API errors must stop further requests.
// Only documented resource-specific failures may fall back and continue. Network
// errors and exhausted transient HTTP retries affect the current batch only.
func stopFetching(err error) bool {
	if errors.Is(err, errSDSession) {
		return true
	}
	var api *sdAPIError
	if errors.As(err, &api) {
		switch api.Code {
		case 2101, 2102, 2105, 2106, 2200, 2201, 5000, 6000, 6001, 7000, 7010, 7020, 7030, 7100:
			return false
		default:
			return true
		}
	}
	var httpErr *sdHTTPError
	return errors.As(err, &httpErr) && httpErr.Code >= 400 && httpErr.Code < 500
}

func airingDate(p EPGoCache) string {
	if p.ScheduleDate != "" {
		return p.ScheduleDate
	}
	// Older caches did not record the response's startDate.
	return p.AirDateTime.UTC().Format("2006-01-02")
}

func usableProgram(p EPGoCache) bool {
	return len(p.Titles) > 0 && p.Titles[0].Title120 != ""
}

func hasUsableUpdate(days map[scheduleKey]bool, programs map[string]bool) bool {
	for station, airings := range Cache.Schedule {
		for _, airing := range airings {
			if usableProgram(Cache.Program[airing.ProgramID]) && (days[scheduleKey{station, airingDate(airing)}] || programs[airing.ProgramID]) {
				return true
			}
		}
	}
	return false
}

func overlapsAny(p EPGoCache, fresh []EPGoCache) bool {
	end := p.AirDateTime.Add(time.Duration(p.Duration) * time.Second)
	for _, other := range fresh {
		if p.AirDateTime.Before(other.AirDateTime.Add(time.Duration(other.Duration)*time.Second)) && other.AirDateTime.Before(end) {
			return true
		}
	}
	return false
}

// Validate each station/date independently. Missing, malformed or duplicate
// results invalidate that partition only. A future range-end is expected for
// broadcasters with shorter horizons; cached data may still cover that day.
func selectSchedules(body []byte, requested []channel) (map[scheduleKey]SDSchedule, map[scheduleKey]bool, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, nil, err
	}
	wanted := make(map[scheduleKey]bool)
	for _, c := range requested {
		for _, date := range c.Date {
			wanted[scheduleKey{c.ID, date}] = true
		}
	}
	good := make(map[scheduleKey]SDSchedule)
	rangeEnds := make(map[scheduleKey]bool)
	seen := make(map[scheduleKey]bool)
	var stop error
	for _, raw := range entries {
		var e struct {
			StationID     string `json:"stationID"`
			Code          int    `json:"code"`
			RequestedDate string `json:"requestedDate"`
			MaxDate       string `json:"maxDate"`
			Metadata      struct {
				StartDate string `json:"startDate"`
				Code      int    `json:"code"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		for _, code := range []int{e.Code, e.Metadata.Code} {
			if code != 0 && stopFetching(&sdAPIError{Code: code}) {
				stop = &sdAPIError{Code: code}
			}
		}
		date := e.Metadata.StartDate
		if date == "" {
			date = e.RequestedDate
		}
		key := scheduleKey{e.StationID, date}
		if !wanted[key] {
			logger.Warn("Unmatched schedule result", "station", e.StationID, "date", date, "code", e.Code)
			continue
		}
		if seen[key] {
			delete(good, key)
			delete(rangeEnds, key)
			logger.Warn("Duplicate schedule result", "station", key.station, "date", key.date)
			continue
		}
		seen[key] = true
		if e.Code == 7020 && e.RequestedDate > e.MaxDate && e.MaxDate != "" && e.RequestedDate > time.Now().UTC().Format("2006-01-02") {
			rangeEnds[key] = true
			continue
		}
		var schedule SDSchedule
		if e.Code != 0 || e.Metadata.Code != 0 || e.Metadata.StartDate == "" || json.Unmarshal(raw, &schedule) != nil || len(schedule.Programs) == 0 {
			continue
		}
		valid := true
		for _, p := range schedule.Programs {
			if p.ProgramID == "" || p.Md5 == "" || p.AirDateTime.IsZero() || p.Duration <= 0 {
				valid = false
				break
			}
		}
		if valid {
			good[key] = schedule
		}
	}
	return good, rangeEnds, stop
}

// Filter item errors before AddProgram/AddMetadata can overwrite cached data.
// Raw elements allow a malformed item's types to leave other items usable.
func selectPrograms(body []byte, requested []string, metadata bool) (map[string]json.RawMessage, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	wanted, seen := make(map[string]bool), make(map[string]bool)
	for _, id := range requested {
		wanted[id] = true
	}
	good := make(map[string]json.RawMessage)
	var stop error
	for _, raw := range entries {
		var header struct {
			ProgramID string          `json:"programID"`
			Code      int             `json:"code"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			continue
		}
		code := header.Code
		if metadata && code == 0 {
			var nested SDStatus
			if json.Unmarshal(header.Data, &nested) == nil {
				code = nested.Code
			}
		}
		if code != 0 && stopFetching(&sdAPIError{Code: code}) {
			stop = &sdAPIError{Code: code}
		}
		id := header.ProgramID
		if !wanted[id] {
			logger.Warn("Unmatched programme result", "programID", id, "metadata", metadata)
			continue
		}
		if seen[id] {
			delete(good, id)
			logger.Warn("Duplicate programme result", "programID", id, "metadata", metadata)
			continue
		}
		seen[id] = true
		if code != 0 {
			continue
		}
		if metadata {
			var entry SDMetadata
			if json.Unmarshal(raw, &entry) != nil || entry.Data == nil {
				continue
			}
		} else {
			var entry SDProgram
			if json.Unmarshal(raw, &entry) != nil || len(entry.Titles) == 0 || entry.Titles[0].Title120 == "" || entry.Md5 == "" {
				continue
			}
		}
		good[id] = raw
	}
	return good, stop
}
