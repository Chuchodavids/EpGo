package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"log/slog"
)

// AppName : App name
const AppName = "epgo"

// Version : Version
const Version = "v3.2.1"

// Config : Config file (struct)
var Config config
var Config2 string
var logger *slog.Logger

func main() {
	var configure = flag.String("configure", "", "= Create or modify the configuration file. [filename.yaml]")
	var config = flag.String("config", "", "= Get data from Schedules Direct with configuration file. [filename.yaml]")
	var version = flag.Bool("version", false, "= Get version")
	var serve = flag.String("serve", "", "= Start a local HTTP server to serve files from the specified directory. [directory:port]")

	var h = flag.Bool("h", false, ": Show help")

	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

	flag.Parse()
	Config2 = *config
	logger.Info("EPGO starting", "version", Version)

	if *h {
		fmt.Println()
		flag.Usage()
		os.Exit(0)
	}

	if *version {
		fmt.Println(Version)
		os.Exit(0)
	}

	if len(*configure) != 0 {
		err := Configure(*configure)
		if err != nil {
			logger.Error("unable to create the configuration file", "error", err)
		}
		os.Exit(0)
	}

	if len(*config) != 0 {
		var sd SD
		err := sd.Update(*config)
		if err != nil {
			logger.Error("unable to get data from Schedules Direct", "error", err)
		}
	}

	if len(*serve) != 0 {
		parts := strings.Split(*serve, ":")
		if len(parts) != 2 {
			logger.Error("Invalid serve argument. Use format [directory:port]", "argument", *serve)
			os.Exit(1)
		}
		dir := parts[0]
		port := parts[1]
		StartServer(dir, port)
	}
}
