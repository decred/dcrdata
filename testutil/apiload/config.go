package main

import (
	"fmt"
	"os"

	flags "github.com/jessevdk/go-flags"
)

const (
	DefaultResultDirectory = "results"
	DefaultProfilesPath    = "profiles.json"
	DefaultServer          = "http://localhost:7777"
)

type config struct {
	ConfigPath      string `short:"c" long:"config" description:"Path to a custom configuration file."`
	ResultDirectory string `short:"d" long:"directory" description:"Directory for the result files."`
	ProfilesPath    string `short:"p" long:"profiles" description:"Path to custom attack profiles."`
	Server          string `short:"s" long:"server" description:"Target base URL, with protocol."`
	Attack          string `short:"a" long:"attack" description:"The attack to perform. The attack must match an entry in the profile definitions."`
	ListAttacks     bool   `short:"l" long:"list" description:"List available attacks from attack profiles."`
	FormatResults   bool   `short:"f" long:"format" description:"Pretty print the JSON result file."`
	CPUs            int    `long:"cpus" description:"Maximum number of processors to use. Defaults to all available."`
	Duration        int    `long:"duration" description:"Overrides the duration of the chosen attack. Units are seconds."`
	Frequency       int    `long:"frequency" description:"Overrides the attack frequency for all attackers. Units are requests/second."`
}

var defaultConfig = config{
	ConfigPath:      "",
	ResultDirectory: DefaultResultDirectory,
	ProfilesPath:    DefaultProfilesPath,
	Server:          DefaultServer,
	Attack:          "",
}

func loadConfig() (*config, error) {
	cfg := defaultConfig

	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.HelpFlag|flags.PassDoubleDash)
	_, err := preParser.Parse()

	if err != nil {
		e, ok := err.(*flags.Error)
		if !ok || e.Type != flags.ErrHelp {
			preParser.WriteHelp(os.Stderr)
		}
		if ok && e.Type == flags.ErrHelp {
			preParser.WriteHelp(os.Stdout)
			os.Exit(0)
		}
		return nil, err
	}

	parser := flags.NewParser(&cfg, flags.Default)

	if preCfg.ConfigPath != "" {
		if _, err := os.Stat(preCfg.ConfigPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("No configuration file found at %s", preCfg.ConfigPath)
		}
		err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return nil, fmt.Errorf("Unable to parse configuration file.")
		}
	}

	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return nil, fmt.Errorf("Error parsing command line arguments: %v", err)
	}

	return &cfg, nil
}
