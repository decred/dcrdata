package main

import (
	"fmt"
	"os"

	"github.com/decred/dcrd/chaincfg/v2"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultURL = "ws://localhost:7777/ps"
)

var activeChain = chaincfg.MainNetParams()

type config struct {
	ConfigPath string `short:"c" long:"config" description:"Path to a custom configuration file."`
	TestNet    bool   `long:"testnet" description:"Use the test network (default mainnet)."`
	SimNet     bool   `long:"simnet" description:"Use the simulation test network (default mainnet)."`
	URL        string `short:"u" long:"url" description:"Target URL, with protocol and path."`
}

var defaultConfig = config{
	URL: defaultURL,
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

	// Choose the active network params based on the selected network. Multiple
	// networks can't be selected simultaneously.
	var numNets int
	activeChain = chaincfg.MainNetParams()
	if cfg.TestNet {
		activeChain = chaincfg.TestNet3Params()
		numNets++
	}
	if cfg.SimNet {
		activeChain = chaincfg.SimNetParams()
		numNets++
	}
	if numNets > 1 {
		str := "the testnet and simnet options can't be used together"
		fmt.Fprintln(os.Stderr, str)
		parser.WriteHelp(os.Stderr)
		return nil, fmt.Errorf(str)
	}

	return &cfg, nil
}
