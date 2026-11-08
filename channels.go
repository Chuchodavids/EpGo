package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
)

func (e *Entry) manageChannels(sd *SD) (err error) {

	defer func() {
		Config.Save()
		Cache.Save()
	}()

	var menu Menu
	var entry Entry

	err = Cache.Open()
	if err != nil {
		logger.Error("unable to open the cache", "error", err)
		return
	}

	Cache.Init()

	menu.Entry = make(map[int]Entry)

	menu.Select = getMsg(0204)
	menu.Headline = e.Value

	// Cancel
	entry.Key = 0
	entry.Value = getMsg(0200)
	menu.Entry[0] = entry

	index := 1 // Start index at 1 to avoid conflict with the "Cancel" option
	for _, lineup := range sd.Resp.Status.Lineups {
		entry.Key = index
		entry.Value = fmt.Sprintf("%s [%s]", lineup.Name, lineup.Lineup)
		entry.Lineup = lineup.Lineup

		menu.Entry[index] = entry
		index++
	}

	selection := menu.Show()

	switch selection {
	case 0:
		return // Cancel selected
	default:
		entry = menu.Entry[selection]
	}

	sd.Req.Parameter = fmt.Sprintf("/%s", entry.Lineup)
	sd.Req.Type = "GET"

	err = sd.Lineups()
	if err != nil {
		logger.Error("unable to get lineups", "error", err)
		return
	}

	entry.headline()

	Config.GetChannels()

	existing := make(map[string]bool)
	for _, id := range Config.ChannelIDs {
		existing[id] = true
	}

	// One row per station, sorted by name. Stations that share a name are
	// ordered by ID so each one keeps its own row.
	stations := make([]Station, len(sd.Resp.Lineup.Stations))
	copy(stations, sd.Resp.Lineup.Stations)
	sort.SliceStable(stations, func(i, j int) bool {
		if stations[i].Name != stations[j].Name {
			return stations[i].Name < stations[j].Name
		}
		return stations[i].StationID < stations[j].StationID
	})

	// The select all and deselect all rows come first, then the channels.
	promptItems := []*channelItem{
		{Action: channelActionSelectAll, Display: fmt.Sprintf("[+] Select all channels (%d)", len(stations))},
		{Action: channelActionDeselectAll, Display: "[-] Deselect all channels"},
	}
	firstChannel := len(promptItems)

	for _, station := range stations {
		item := &channelItem{
			Name:     station.Name,
			ID:       station.StationID,
			Lineup:   entry.Lineup,
			Language: station.BroadcastLanguage,
			Selected: existing[station.StationID],
		}
		item.updateDisplay()
		promptItems = append(promptItems, item)
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> {{ .Display | green }}",
		Inactive: "  {{ .Display }}",
		Selected: "{{ .Display | yellow }}",
		Details: `
--------- Channel Details ----------
{{ if .Action }}{{ "Applies to every channel in this lineup" | faint }}
{{ else }}{{ "Name:" | faint }}	{{ .Name }}
{{ "ID:" | faint }}	{{ .ID }}
{{ end }}`,
	}

	prompt := promptui.Select{
		Label:     "Available Channels (Enter To Select, Control + C to save.)",
		Items:     promptItems,
		Size:      20,
		Templates: templates,
		CursorPos: firstChannel,
	}

	// Search by channel name. The select all and deselect all rows act on the
	// whole lineup, not on the search results, so they are hidden while a
	// search term is entered.
	prompt.Searcher = func(input string, index int) bool {
		item := promptItems[index]
		if item.Action != "" {
			return input == ""
		}

		return caseInsensitiveContains(item.Name, input)
	}

	for {
		index, _, err = prompt.Run()

		if err != nil {
			if err == promptui.ErrInterrupt {
				break // User pressed Ctrl+C, exit loop
			}
			if err == promptui.ErrEOF { // Check for Ctrl+D (EOF)
				break
			}
			logger.Error("prompt failed", "error", err)
			return err
		}

		item := promptItems[index]

		switch item.Action {
		case channelActionSelectAll:
			setAllChannels(promptItems, true)
		case channelActionDeselectAll:
			setAllChannels(promptItems, false)
		default:
			item.Selected = !item.Selected
			item.updateDisplay()
		}

		prompt.CursorPos = index
	}

	// Collect selected channels and add/remove accordingly.
	for _, item := range promptItems {
		if item.Action != "" {
			continue
		}

		ch := channel{Name: item.Name, ID: item.ID, Lineup: item.Lineup}
		if item.Selected {
			Config.AddChannel(&ch)
		} else {
			Config.RemoveChannel(&ch)
		}
	}

	return
}

// Picker rows that act on every channel of the lineup instead of one.
const (
	channelActionSelectAll   = "select-all"
	channelActionDeselectAll = "deselect-all"
)

// channelItem is one row of the channel picker: a station of the lineup,
// or a select all / deselect all row when Action is set.
type channelItem struct {
	Name     string
	ID       string
	Lineup   string
	Language []string
	Selected bool
	Action   string
	Display  string
}

func (c *channelItem) updateDisplay() {
	status := "-"
	if c.Selected {
		status = "+"
	}
	c.Display = fmt.Sprintf("[%s] %s [%s] %v", status, c.Name, c.ID, c.Language)
}

// setAllChannels selects or deselects every channel row of the picker.
func setAllChannels(items []*channelItem, selected bool) {
	for _, item := range items {
		if item.Action != "" {
			continue
		}
		item.Selected = selected
		item.updateDisplay()
	}
}

// Helper function for case-insensitive contains
func caseInsensitiveContains(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}

func (c *config) AddChannel(ch *channel) {
	for _, existingCh := range c.Station {
		if existingCh.ID == ch.ID {
			return // Channel already exists; don't add it again
		}
	}
	c.Station = append(c.Station, *ch)
}

func (c *config) RemoveChannel(ch *channel) {
	var tmp []channel
	for _, old := range c.Station {
		if old.ID != ch.ID {
			tmp = append(tmp, old)
		}
	}
	c.Station = tmp
}

func (c *config) GetChannels() {
	c.ChannelIDs = []string{}
	for _, channel := range c.Station {
		c.ChannelIDs = append(c.ChannelIDs, channel.ID)
	}
}
