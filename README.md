# EPGo

subredit: [epgo](https://www.reddit.com/r/EpGo/)

## Features

- Cache function to download only new EPG data
- No database is required
- Update EPG with CLI command for using your own scripts

## Requirements

- [Schedules Direct](https://www.schedulesdirect.org/ "Schedules Direct") Account
- Computer with 1-2 GB memory
- [Go](https://golang.org/ "Golang") 1.21+ to build the binary
- [Optional] Docker to run it in a containerized environment

## Installation

### Option 1 -- Build Binary

Run the following commands inside the source code folder:

```bash
go mod tidy
go build epgo
```

This produces a binary named `epgo` for your OS.

For a smaller production binary:

```bash
go build -ldflags="-s -w" -o epgo
```

To cross-compile (e.g., for Linux on AMD64):

```bash
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o epgo_linux_amd64
```

### Option 2 -- Docker

Clone the repo and use the provided `docker-compose.yaml`:

```bash
git clone https://github.com/Chuchodavids/EpGo.git
```

```yaml
# docker-compose.yaml
services:
  epgo:
    container_name: epgo
    build:
      context: .
      args:
        # Or amd64
        - TARGETARCH=arm64
    environment:
      - TZ=America/Chicago
    volumes:
      # Assumes config.yaml is in the same directory as this docker-compose.yaml
      - ./config.yaml:/app/config.yaml
    command: -config /app/config.yaml
    restart: never
```

The Docker image is built from the included `Dockerfile`, which downloads the latest release binary from GitHub.

### Option 3 -- Download Binary

Go to [releases](https://github.com/Chuchodavids/EpGo/releases) and download the binary for your platform. You can also use the `download.sh` script included in the repository.

## Using the APP

```epgo -h```

```bash
-config string
    = Get data from Schedules Direct with configuration file. [filename.yaml]
-configure string
    = Create or modify the configuration file. [filename.yaml]
-serve string
    = Start a local HTTP server to serve files from the specified directory. [directory:port]
-version
    = shows the current version (v3.2.1)
-h  : Show help
```

### Create a config file

**note**: You can use the sample config file that is in the /config folder inside of the docker container

```epgo -configure MY_CONFIG_FILE.yaml```  
If the configuration file does not exist, a YAML configuration file is created. 

**Configuration file from version 1.0.6 or earlier is not compatible.**  

#### Terminal Output

```txt
Configuration [MY_CONFIG_FILE.yaml]
-----------------------------
 1. Schedules Direct Account
 2. Add Lineup
 3. Remove Lineup
 4. Manage Channels
 5. Create XMLTV File [MY_CONFIG_FILE.xml]
 0. Exit
```

##### Follow the instructions in the terminal

1. Schedules Direct Account:  
Manage Schedules Direct credentials.  

2. Add Lineup:  
Add Lineup into the Schedules Direct account.  

3. Remove Lineup:  
Remove Lineup from the Schedules Direct account.  

4. Manage Channels:  
Selection of the channels to be used.
You can now choose to add all channels from a lineup at once or select them individually.
All selected channels are merged into one XML file when the XMLTV file is created.
When using all channels from all lineups it is recommended to create a separate epgo configuration file for each lineup.  
5. Create XMLTV File [MY_CONFIG_FILE.xml]:  
Creates the XMLTV file with the selected channels.  

**Example:**

Lineup 1:

```bash
epgo -configure Config_Lineup_1.yaml
```

Lineup 2:

```bash
epgo -configure Config_Lineup_2.yaml
```

## CONFIG

```yaml
Account:
    Username: YOUR_USERNAME
    Password:  YOUR_PASSWORD
Files:
    Cache: config_cache.json
    XMLTV: config.xml
    The MovieDB cache file: config_tmdb_cache.json
Server:
    Enable: false
    Address: localhost
    Port: "80"
Options:
    Live and New icons: false
    Schedule Days: 1
    Subtitle into Description: false
    Insert credits tag into XML file: false
    Images:
        Download Images from Schedules Direct: false
        Image Path: ""
        The MovieDB:
            Enable: false
            Api Key: ""
    Rating:
        Insert rating tag into XML file: false
        Maximum rating entries. 0 for all entries: 1
        Preferred countries. ISO 3166-1 alpha-3 country code. Leave empty for all systems:
            - USA
            - COL
        Use country code as rating system: false
    Show download errors from Schedules Direct in the log: false
Station:
    - Name: MTV
      ID: "12345"
      Lineup: SAMPLE
```

### Files: (Can be customized)**

```yaml
Cache: /app/file.json  
XMLTV: /app/xml  
```

### Server: (Can be customized)

```yaml
Enable: false
Address: localhost
Port: "80"
```

-   **Enable**: `true` or `false` to enable or disable the image server.
-   **Address**: The IP address or hostname for the server to listen on. Defaults to `localhost`.
-   **Port**: The port for the server to listen on. Defaults to `80`.

### Options: (Can be customized)

**Some clients only use one image, even if there are several in the XMLTV file.**  

---

```yaml
Schedule Days: 7
```

EPG data for the specified days. Schedules Direct has EPG data for the next 12-14 days  

---

```yaml
Live and New icons: false
```

**true:** Appends a unicode superscript "LIVE" or "New" badge to the programme title for live broadcasts and first-run episodes, respectively.

---

```yaml
Subtitle into Description: false
```

Some clients only display the description and ignore the subtitle tag from the XMLTV file.  

**true:** If there is a subtitle, it will be added to the description.  

```XML
<?xml version="1.0" encoding="UTF-8"?>
<programme channel="epgo.67203.schedulesdirect.org" start="20200509134500 +0000" stop="20200509141000 +0000">
   <title lang="de">Two and a Half Men</title>
   <sub-title lang="de">Ich arbeite für Caligula</sub-title>
   <desc lang="de">[Ich arbeite für Caligula]
Alan zieht aus, da seine Freundin Kandi und er in Las Vegas eine Million Dollar gewonnen haben. Charlie kehrt zu seinem ausschweifenden Lebensstil zurück und schmeißt wilde Partys, die bald ausarten. Doch dann steht Alan plötzlich wieder vor der Tür.</desc>
   <category lang="en">Sitcom</category>
   <episode-num system="xmltv_ns">3.0.</episode-num>
   <episode-num system="onscreen">S4 E1</episode-num>
   <episode-num system="original-air-date">2006-09-18</episode-num>
   ...
</programme>
```

---

### Images: (Can be customized)

```yaml
Download Images from Schedules Direct: false
Image Path: ""
```

-   **Download Images from Schedules Direct**: `true` or `false`. If `true`, images will be downloaded to the `Image Path`. If `Image Path` is not set, it will default to a folder named `images`.
-   **Image Path**: The path where the images will be downloaded.

#### The MovieDB

```yaml
The MovieDB:
    Enable: false
    Api Key: ""
```

-   **Enable**: `true` or `false` to enable or disable The MovieDB as a fallback image source.
-   **Api Key**: Your The MovieDB API key.

The TMDB cache file (default: `config_tmdb_cache.json`) persists search results to avoid redundant API calls.

---

```yaml
Insert credits tag into XML file: false
```

**true:** Adds the credits (director, actor, producer, writer) to the program information, if available.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<programme channel="epgo.67203.schedulesdirect.org" start="20200509134500 +0000" stop="20200509141000 +0000">
   <title lang="de">Two and a Half Men</title>
   <sub-title lang="de">Ich arbeite für Caligula</sub-title>
   ...
  <credits>
    <director>Jamie Widdoes</director>
    <actor role="Charlie Harper">Charlie Sheen</actor>
    <actor role="Alan Harper">Jon Cryer</actor>
    <actor role="Jake Harper">Angus T. Jones</actor>
    <actor role="Judith">Marin Hinkle</actor>
    <actor role="Evelyn Harper">Holland Taylor</actor>
    <actor role="Rose">Melanie Lynskey</actor>
    <writer>Chuck Lorre</writer>
    <writer>Lee Aronsohn</writer>
    <writer>Susan Beavers</writer>
    <writer>Don Foster</writer>
</credits>
   ...
</programme>
```

---

```yaml
Rating:
        Insert rating tag into XML file: true
        ...
```

**true:** Adds the TV parental guidelines to the program information.  

```xml
<?xml version="1.0" encoding="UTF-8"?>
<programme channel="epgo.67203.schedulesdirect.org" start="20200509134500 +0000" stop="20200509141000 +0000">
  <title lang="de">Two and a Half Men</title>
  <sub-title lang="de">Ich arbeite für Caligula</sub-title>
  <language>de</language>
  ...
  <rating system="Freiwillige Selbstkontrolle der Filmwirtschaft">
    <value>12</value>
  </rating>
   ...
</programme>
```

**false:** TV parental guidelines are not used. Further rating settings are ignored.  

```xml
<?xml version="1.0" encoding="UTF-8"?>
<programme channel="epgo.67203.schedulesdirect.org" start="20200509134500 +0000" stop="20200509141000 +0000">
  <title lang="de">Two and a Half Men</title>
  <sub-title lang="de">Ich arbeite für Caligula</sub-title>
  <language>de</language>
   ...
</programme>
```

```yaml
Rating:
        ...
        Maximum rating entries. 0 for all entries: 1
        ...
```

Specifies the number of maximum rating entries. If the value is 0, all parental guidelines available from Schedules Direct are used. Depending on the preferred countries.

```yaml
Rating:
        ...
        Preferred countries. ISO 3166-1 alpha-3 country code. Leave empty for all systems:
          - DEU
          - CHE
          - USA
        ...
```

Sets the order of the preferred countries [ISO 3166-1 alpha-3](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-3 "ISO 3166-1 alpha-3").  
Parental guidelines are not available for every country and program information. Trial and error.  
If no country is specified, all available countries are used. Many clients ignore a list with more than one entry or use the first entry.  

**If no country is specified:**  
If a rating entry exists in the same language as the Schedules Direct Lineup, it will be set to the top. In this example German (DEU).  

Lineup: **DEU**-1000097-DEFAULT  
1st rating system (Germany): Freiwillige Selbstkontrolle der Filmwirtschaft  

```xml
...
<rating system="Freiwillige Selbstkontrolle der Filmwirtschaft">
  <value>12</value>
</rating>
<rating system="USA Parental Rating">
  <value>TV14</value>
</rating>
...
```

```yaml
Rating:
        ...
        Use country code as rating system: false
```

**true:**

```xml
<rating system="DEU">
  <value>12</value>
</rating>
<rating system="USA">
  <value>TV14</value>
</rating>

```

**false:**

```xml
<rating system="Freiwillige Selbstkontrolle der Filmwirtschaft">
  <value>12</value>
</rating>
<rating system="USA Parental Rating">
  <value>TV14</value>
</rating>
```

---

```txt
Show download errors from Schedules Direct in the log: false
```

**true:** Shows incorrect downloads of Schedules Direct in the log.  

Example:

```bash
2020/07/18 19:10:53 [ERROR] Could not find requested image. Post message to http://forums.schedulesdirect.org/viewforum.php?f=6 if you are having issues. [SD API Error Code: 5000] Program ID: EP03481925
```

---

## LCN (Logical Channel Number)

Each channel in the XMLTV output includes an `lcn` attribute for subchannel auto-matching in media servers like Plex and Jellyfin. The LCN is populated from the Schedules Direct lineup map — for OTA channels it uses `atscMajor.atscMinor`, and for cable/satellite it uses the channel number string.

```xml
<channel id="epgo.42635.schedulesdirect.org">
  <display-name>WGBO-DT</display-name>
  <display-name>66.1</display-name>
  <lcn>66.1</lcn>
  <icon src="https://...png" width="360" height="270"/>
</channel>
```

---

## Image Server

When `Server.Enable` is set to `true` in the config, the HTTP image server starts automatically after XMLTV generation in `-config` mode. You can also start it independently with the `-serve` flag:

```bash
./epgo -serve /path/to/images:8080
```

---

## Cron Scheduling

To keep EPG data up to date, run EPGo on a schedule. Example crontab entries:

```
@reboot /app/epgo --config /data/livetv/config.yaml
00 * * * * /app/epgo --config /data/livetv/config.yaml
```

A `cronjob` file with these examples is included in the repository.

---

## Cache Cleanup

After each XMLTV generation, stale program and metadata entries are automatically purged from the cache file. This keeps the cache size manageable without manual intervention.

### Create the XMLTV file using the command line (CLI): 

```bash
epgo -config MY_CONFIG_FILE.yaml
```

**The configuration file must have already been created.**
