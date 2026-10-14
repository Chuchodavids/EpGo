package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

var Token string

var errSDSession = errors.New("Schedules Direct session failed")

// tokensafeMargin must cover a whole run, including image downloads that can
// take tens of minutes.
const tokensafeMargin = time.Hour

// Init : Init Schedules Direct
func (sd *SD) Init() (err error) {
	sd.BaseURL = "https://json.schedulesdirect.org/20141201/"

	// Funtion to get token
	sd.Login = func() (err error) {
		if Cache.Token != "" && Cache.TokenExpires > 0 {
			expiresAt := time.Unix(Cache.TokenExpires, 0)
			safetyMargin := time.Now().Add(tokensafeMargin)
			if expiresAt.After(safetyMargin) {
				logger.Info("Using cached token", "expires", expiresAt)
				sd.Token = Cache.Token
				Token = Cache.Token
				return nil
			}
			logger.Info("Cached token expired or expiring soon, requesting new token")
		}

		sd.Req.URL = sd.BaseURL + "token"
		sd.Req.Type = "POST"
		sd.Req.Call = "login"
		sd.Req.Compression = false
		sd.Token = ""

		sd.Req.Data, err = json.MarshalIndent(Config.Account, "", "  ")
		if err != nil {
			logger.Error("could not marshall request data to get token", "error", err)
			return err
		}

		err = sd.Connect()
		if err != nil {

			if sd.Resp.Login.Code != 0 {
				return err
			}

			return
		}

		sd.Token = sd.Resp.Login.Token
		Token = sd.Token

		Cache.Token = sd.Token
		Cache.TokenExpires = sd.Resp.Login.TokenExpires

		return
	}

	// Status function to check status of schedules direct API
	sd.Status = func() (err error) {
		fmt.Println()

		sd.Req.URL = sd.BaseURL + "status"
		sd.Req.Type = "GET"
		sd.Req.Data = nil
		sd.Req.Call = "status"
		sd.Req.Compression = false

		err = sd.Connect()
		if err != nil {
			return
		}

		for _, status := range sd.Resp.Status.SystemStatus {
			if status.Status == "Offline" {
				return fmt.Errorf("Schedules Direct is offline")
			}
		}

		logger.Info("", "Expiration", sd.Resp.Status.Account.Expires)
		logger.Info("", "Lineups", len(sd.Resp.Status.Lineups), "Limit", sd.Resp.Status.Account.MaxLineups)
		logger.Info("", "Channels", len(Config.Station))

		return
	}

	sd.Countries = func() (err error) {

		sd.Req.URL = sd.BaseURL + "available/countries"
		sd.Req.Type = "GET"
		sd.Req.Data = nil
		sd.Req.Call = "countries"
		sd.Req.Compression = false

		err = sd.Connect()
		if err != nil {
			return
		}

		return
	}

	sd.Headends = func() (err error) {

		sd.Req.URL = fmt.Sprintf("%sheadends%s", sd.BaseURL, sd.Req.Parameter)
		sd.Req.Type = "GET"
		sd.Req.Data = nil
		sd.Req.Call = "headends"
		sd.Req.Compression = false

		err = sd.Connect()
		if err != nil {
			return
		}

		return
	}

	sd.Lineups = func() (err error) {

		sd.Req.URL = fmt.Sprintf("%slineups%s", sd.BaseURL, sd.Req.Parameter)
		sd.Req.Data = nil
		sd.Req.Call = "lineups"
		sd.Req.Compression = false

		err = sd.Connect()
		if err != nil {
			return
		}

		if len(sd.Resp.Lineup.Message) != 0 {
			logger.Info("", "msg", sd.Resp.Lineup.Message)
		}

		return
	}

	sd.Schedule = func() (err error) {

		sd.Req.URL = fmt.Sprintf("%sschedules", sd.BaseURL)
		sd.Req.Type = "POST"
		sd.Req.Call = "schedule"
		// Large lineups must transfer within the client timeout.
		sd.Req.Compression = true

		err = sd.Connect()
		if err != nil {
			return
		}

		return
	}

	sd.ScheduleMD5 = func() (err error) {

		sd.Req.URL = fmt.Sprintf("%sschedules/md5", sd.BaseURL)
		sd.Req.Type = "POST"
		sd.Req.Call = "schedulemd5"
		// Large lineups must transfer within the client timeout.
		sd.Req.Compression = true

		return sd.Connect()
	}

	sd.Program = func() (err error) {

		sd.Req.Type = "POST"
		sd.Req.Call = "program"
		sd.Req.Compression = true

		err = sd.Connect()
		if err != nil {
			return
		}

		return
	}

	return
}

// Connect : Connect to Schedules Direct

// Connect retries only transient read operations, never account mutations.
// One expired-token recovery is allowed per request; result errors fail closed.
func (sd *SD) Connect() error {
	original := sd.Req
	refreshed := false
	for attempt := 0; attempt < 3; attempt++ {
		err := sd.connectOnce()
		if err == nil {
			return nil
		}
		var apiErr *sdAPIError
		if errors.As(err, &apiErr) && apiErr.Code == 4006 && original.Call != "login" && !refreshed {
			refreshed = true
			Cache.Token = ""
			Cache.TokenExpires = 0
			if loginErr := sd.Login(); loginErr != nil {
				sd.Req = original
				return fmt.Errorf("%w: token renewal: %w", errSDSession, loginErr)
			}
			sd.Req = original
			if Config.Files.Cache != "" {
				if saveErr := Cache.saveToken(); saveErr != nil {
					return fmt.Errorf("%w: save renewed token: %w", errSDSession, saveErr)
				}
			}
			// Renewal has its own bounded budget, not a transient attempt.
			attempt--
			continue
		}
		var httpErr *sdHTTPError
		var netErr net.Error
		transient := errors.As(err, &httpErr) && (httpErr.Code == 502 || httpErr.Code == 503 || httpErr.Code == 504)
		transient = transient || errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF)
		if !transient || attempt == 2 || (original.Type != "GET" && original.Type != "POST") {
			return err
		}
		wait := sd.Sleep
		if wait == nil {
			wait = time.Sleep
		}
		wait(time.Second * time.Duration(1<<attempt))
	}
	return fmt.Errorf("Schedules Direct retries exhausted")
}

type sdAPIError struct{ Code int }

func (e *sdAPIError) Error() string { return fmt.Sprintf("Schedules Direct API error %d", e.Code) }

type sdHTTPError struct{ Code int }

func (e *sdHTTPError) Error() string { return fmt.Sprintf("Schedules Direct HTTP status %d", e.Code) }

func (sd *SD) connectOnce() (err error) {

	req, err := http.NewRequest(sd.Req.Type, sd.Req.URL, bytes.NewBuffer(sd.Req.Data))
	if err != nil {
		logger.Warn("Could not create request for Token", "error", err)
		return
	}

	if sd.Req.Compression {
		req.Header.Set("Accept-Encoding", "gzip")
	}

	req.Header.Set("Token", sd.Token)
	req.Header.Set("User-Agent", AppName)
	req.Header.Set("X-Custom-Header", AppName)
	if sd.Req.Type == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := sd.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("failed communicate with Schedules Direct API", "error", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("Could not read response body from Schedules direct token retrieval method", "error", err)
		return
	}

	// Explicit gzip requests disable net/http's automatic decompression. SD can
	// also return plain JSON (especially errors), regardless of this request.
	body, err = decodeSDPayload(body)
	if err != nil {
		return err
	}
	var result SDStatus
	if json.Unmarshal(body, &result) == nil && result.Code != 0 {
		return &sdAPIError{Code: result.Code}
	}
	if resp.StatusCode != http.StatusOK {
		return &sdHTTPError{Code: resp.StatusCode}
	}
	sd.Resp.Body = body
	var decoded SD // absent fields must not inherit a previous response

	switch sd.Req.Call {

	case "login":
		err = json.Unmarshal(body, &decoded.Resp.Login)
		if err != nil {
			logger.Error("could not unmarshal login response", "error", err)
			return err
		}
		sd.Resp.Login = decoded.Resp.Login
		if sd.Resp.Login.Token == "" {
			return fmt.Errorf("missing token in login response")
		}
		t := time.Unix(sd.Resp.Login.TokenExpires, 0)
		logger.Info("", "Token Expires", t)
		// Without newToken the API returns the existing token, even if it
		// expires a few seconds later.
		if t.Before(time.Now().Add(tokensafeMargin)) {
			var data map[string]interface{}
			err = json.Unmarshal(sd.Req.Data, &data)
			if err != nil {
				logger.Error("could not unmarshal request data to add newToken", "error", err)
				return err
			}
			if data["newToken"] != true {
				logger.Info("Token expires too soon, requesting a new token")
				data["newToken"] = true
				sd.Req.Data, err = json.Marshal(data)
				if err != nil {
					logger.Error("could not marshal request data with newToken", "error", err)
					return err
				}
				return sd.Connect()
			}
			return fmt.Errorf("renewed token expires too soon")
		}

	case "status":
		err = json.Unmarshal(body, &decoded.Resp.Status)
		if err != nil {
			logger.Error("could not unmarshal status response", "error", err)
		}

		sd.Resp.Status = decoded.Resp.Status

	case "countries":
		err = json.Unmarshal(body, &decoded.Resp.Countries)
		sd.Resp.Countries = decoded.Resp.Countries
		if err != nil {
			logger.Error("could not unmarshal countries response", "error", err)
		}

	case "headends":
		err = json.Unmarshal(body, &decoded.Resp.Headend)
		sd.Resp.Headend = decoded.Resp.Headend
		if err != nil {
			logger.Error("could not unmarshal headends response", "error", err)
		}

	case "lineups":
		err = json.Unmarshal(body, &decoded.Resp.Lineup)
		sd.Resp.Lineup = decoded.Resp.Lineup
		if err != nil {
			logger.Error("could not unmarshal lineups response", "error", err)
		}
		sd.Resp.Body = body

	case "schedule", "schedulemd5", "program":
		sd.Resp.Body = body

	}

	return
}
