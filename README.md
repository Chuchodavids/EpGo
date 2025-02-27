# This is a Fork of the abandoned (?) Guide2GO project

I implemented changes that were missing in the current epgo version.

**Recent changes:**

1. For players like tiviMate I added the "live" and "new" icon in the tittle of the program. This is usually auto-added in Emby or other IPTV players, but TiviMate does not do this by reading XML tags.
1. Searching for channel to add is no longer a painful task. It can be done easily from the terminal
1. Add images to shows from The Movie Database

## epgo

### Features

- Cache function to download only new EPG data
- No database is required
- Update EPG with CLI command for using your own scripts

### Requirements

- [Schedules Direct](https://www.schedulesdirect.org/ "Schedules Direct") Account
- Computer with 1-2 GB memory
- [Go](https://golang.org/ "Golang") to build the binary
- [Optional] Docker to run it in a dockerize environment

## Installation

### Option 1 -- Build Binary

The following command must be executed with the terminal / command prompt inside the source code folder.  

```bash
go mod tidy
go build
```

This will spit out a binary for your OS named `epgo`

### Docker

I need to set up the docker image. However, you can build your own by running:

**docker-compose**
```
version: "3.4"
services:
    epgo:
      container_name: epgo
      image: chuchodavids/epgo:stable
      ports:
        - 8080:8080
      environment:
        - TZ: America/Chicago
      volumes:
        - /YOU_APP_PATH/epgo:/app
        - /YOUR_IMAGE_PATH:/app/images
      restart: always
```

## Using the APP

```epgo -h```

```bash
-config string
    = Get data from Schedules Direct with configuration file. [filename.yaml]
-configure string
    = Create or modify the configuration file. [filename.yaml]
-h  : Show help
```

### Create a config file

**note: You can use the sample config file that is in the /config folder inside of the docker container**

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


#### The YAML configuration file can be customize with an editor.:

```yaml
Account:
    Username: SCHEDULES_DIRECT_USERNAME
    Password: SCHEDULES_DIRECT_HASHED_PASSWORD
Files:
    Cache: MY_CONFIG_FILE.json
    XMLTV: MY_CONFIG_FILE.xml
Options:
    Poster Aspect: all
    Schedule Days: 7
    Subtitle into Description: false
    Insert credits tag into XML file: true
    Rating:
        Insert rating tag into XML file: true
        Maximum rating entries. 0 for all entries: 1
        Preferred countries. ISO 3166-1 alpha-3 country code. Leave empty for all systems:
          - DEU
          - CHE
          - USA
        Use country code as rating system: false
    Show download errors from Schedules Direct in the log: false
Station:
  - Name: Fox Sports 1 HD
    ID: "82547"
    Lineup: USA-DITV-DEFAULT
  - Name: Fox Sports 2 HD
    ID: "59305"
    Lineup: USA-DITV-DEFAULT
```

##### Account: (Don't change)

Schedules Direct Account data, do not change them in the configuration file.  

##### Files: (Can be customized)**

```yaml
Cache: /app/file.json  
XMLTV: /app/file.xml  
```

##### Options: (Can be customized)

```yaml
Poster Aspect: all
```

**Some clients only use one image, even if there are several in the XMLTV file.**  

---

```yaml
Schedule Days: 7
```

EPG data for the specified days. Schedules Direct has EPG data for the next 12-14 days  

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

```yaml
Use SchedulesDirect Links for images: false
```

Change to true to use schedule direct as show images as fallback from tmdb images.

###### WARNINGS

1. This will append the token to the images link. Not my fault. that is how schedules direct works
1. Because tokens are valid for 24 hours only, you need to re-download the xmltv file everyday. This will be better paired with downloading only one day of EPG and using maybe a cron job to keep it updated
1. If you download more than 500 images, schdulesdirect will rate block you. So, not great for big EPG (more than 100 channels ish)

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

### Create the XMLTV file using the command line (CLI): 

```bash
epgo -config MY_CONFIG_FILE.yaml
```

**The configuration file must have already been created.**
