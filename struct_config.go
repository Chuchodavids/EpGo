package main

type config struct {
	File       string   `yaml:"-"`
	ChannelIDs []string `yaml:"-"`

	Account struct {
		Username string `yaml:"Username" json:"username"`
		Password string `yaml:"Password" json:"password"`
	} `yaml:"Account"`

	Files struct {
		Cache         string `yaml:"Cache"`
		XMLTV         string `yaml:"XMLTV"`
		TmdbCacheFile string `yaml:"The MovieDB cache file"`
	} `yaml:"Files"`

	Server struct {
		Enable  bool   `yaml:"Enable"`
		Address string `yaml:"Address"`
		Port    string `yaml:"Port"`
	} `yaml:"Server"`

	Options struct {
		LiveIcons               bool `yaml:"Live and New icons"`
		Schedule                int  `yaml:"Schedule Days"`
		SubtitleIntoDescription bool `yaml:"Subtitle into Description"`
		Credits                 bool `yaml:"Insert credits tag into XML file"`
		Images                  struct {
			Download bool   `yaml:"Download Images from Schedules Direct"`
			Path     string `yaml:"Image Path"`
			PosterAspect string `yaml:"Poster Aspect"` // all | 2x3 | 4x3 | 16x9
			Tmdb     struct {
				Enable bool   `yaml:"Enable"`
				ApiKey string `yaml:"Api Key"`
			} `yaml:"The MovieDB"`
		} `yaml:"Images"`
		Rating struct {
			Guidelines          bool     `yaml:"Insert rating tag into XML file"`
			MaxEntries          int      `yaml:"Maximum rating entries. 0 for all entries"`
			Countries           []string `yaml:"Preferred countries. ISO 3166-1 alpha-3 country code. Leave empty for all systems"`
			CountryCodeAsSystem bool     `yaml:"Use country code as rating system"`
		} `yaml:"Rating"`

		SDDownloadErrors bool `yaml:"Show download errors from Schedules Direct in the log"`
	} `yaml:"Options"`

	Station []channel `yaml:"Station"`
}

type channel struct {
	Name        string        `yaml:"Name" json:"-" xml:"-"`
	DisplayName []DisplayName `yaml:"-" json:"-" xml:"display-name"`
	ID          string        `yaml:"ID" json:"stationID" xml:"id,attr"`
	Lineup      string        `yaml:"Lineup" json:"-" xml:"-"`
	Date        []string      `yaml:"-" json:"date"`
	Icon        Icon          `yaml:"-" json:"-" xml:"icon"`
}
