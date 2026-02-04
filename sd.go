package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var Token string

const tokensafeMargin = 5 * time.Minute

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

		if sd.Resp.Status.Code == 3000 || sd.Resp.Status.Code == 4009 {
			logger.Error("Schedule Direct issue", "status_message", sd.Resp.Status.Message, "status_code", sd.Resp.Status.Code)
			return fmt.Errorf("schdule Direct is down: %w", err)
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
		sd.Req.Compression = false

		err = sd.Connect()
		if err != nil {
			return
		}

		return
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

func (sd *SD) Connect() (err error) {

	var sdStatus SDStatus

	req, err := http.NewRequest(sd.Req.Type, sd.Req.URL, bytes.NewBuffer(sd.Req.Data))
	if err != nil {
		logger.Warn("Could not create request for Token", "error", err)
		return
	}

	if sd.Req.Compression {
		req.Header.Set("Accept-Encoding", "deflate,gzip")
	}

	req.Header.Set("Token", sd.Token)
	req.Header.Set("User-Agent", AppName)
	req.Header.Set("X-Custom-Header", AppName)
	if sd.Req.Type == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{}
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

	sd.Resp.Body = body

	switch sd.Req.Call {

	case "login":
		err = json.Unmarshal(body, &sd.Resp.Login)
		if err != nil {
			logger.Error("could not unmarshal login response", "error", err)
			return err
		}
		if sd.Resp.Login.Code == 4009 {
			return fmt.Errorf(sd.Resp.Login.Message)
		}
		t := time.Unix(sd.Resp.Login.TokenExpires, 0)
		logger.Info("", "Token Expires", t)
		if t.Before(time.Now()) {
			logger.Error("Token has expired")
			var data map[string]interface{}
			err = json.Unmarshal(sd.Req.Data, &data)
			if err != nil {
				logger.Error("could not unmarshal request data to add newToken", "error", err)
				return err
			}
			data["newToken"] = true
			sd.Req.Data, err = json.Marshal(data)
			if err != nil {
				logger.Error("could not marshal request data with newToken", "error", err)
				return err
			}
			sd.Connect()
		}
		sdStatus.Code = sd.Resp.Login.Code
		sdStatus.Message = sd.Resp.Login.Message

	case "status":
		err = json.Unmarshal(body, &sd.Resp.Status)
		if err != nil {
			logger.Error("could not unmarshal status response", "error", err)
		}

		sdStatus.Code = sd.Resp.Status.Code
		sdStatus.Message = sd.Resp.Status.Message

	case "countries":
		err = json.Unmarshal(body, &sd.Resp.Countries)
		if err != nil {
			logger.Error("could not unmarshal countries response", "error", err)
		}

	case "headends":
		err = json.Unmarshal(body, &sd.Resp.Headend)
		if err != nil {
			logger.Error("could not unmarshal headends response", "error", err)
		}

	case "lineups":
		err = json.Unmarshal(body, &sd.Resp.Lineup)
		if err != nil {
			logger.Error("could not unmarshal lineups response", "error", err)
		}
		sd.Resp.Body = body

		sdStatus.Code = sd.Resp.Lineup.Code
		sdStatus.Message = sd.Resp.Lineup.Message

	case "schedule", "program":
		sd.Resp.Body = body

	}

	if resp.StatusCode != http.StatusOK {
		logger.Error("SchedulesDirect request returned a non-200 code", "http", resp.Status)
		return fmt.Errorf("status code non-200: %v", resp.Status)
	}

	return
}

