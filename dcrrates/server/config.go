// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	flags "github.com/btcsuite/go-flags"
	"github.com/decred/dcrd/dcrutil"
)

const (
	defaultLogPath         = "logs"
	defaultExchangeRefresh = "20m"
	defaultExchangeExpiry  = "60m"
	defaultListen          = ":7778"
	defaultBtcIndex        = "USD"
)

var (
	defaultConfigPath = filepath.Join(dcrutil.AppDataDir("dcrdata", false), "dcrrates-server.conf")
	defaultCertPath   = filepath.Join(dcrutil.AppDataDir("dcrdata", false), "rpc.cert")
	defaultKeyPath    = filepath.Join(dcrutil.AppDataDir("dcrdata", false), "rpc.key")
)

type config struct {
	ConfigPath        string `short:"c" long:"config" description:"Path to a custom configuration file." env:"DCRRATES_CONFIG_PATH"`
	GRPCListen        string `short:"l" long:"listen" description:"gRPC listen address." env:"DCRRATES_LISTEN"`
	LogPath           string `long:"logpath" description:"Directory to log output." env:"DCRRATES_LOG_PATH"`
	DisabledExchanges string `long:"disable-exchange" description:"Exchanges to disable. See /exchanges/exchanges.go for available exchanges. Use a comma to separate multiple exchanges" env:"DCRRATES_DISABLE_EXCHANGES"`
	ExchangeCurrency  string `long:"exchange-currency" description:"The default bitcoin price index. A 3-letter currency code." env:"DCRRATES_EXCHANGE_INDEX"`
	ExchangeRefresh   string `long:"exchange-refresh" description:"Time between API calls for exchange data. See (ExchangeBotConfig).DataExpiry." env:"DCRRATES_EXCHANGE_REFRESH"`
	ExchangeExpiry    string `long:"exchange-expiry" description:"Maximum age before exchange data is discarded. See (ExchangeBotConfig).RequestExpiry." env:"DCRRATES_EXCHANGE_EXPIRY"`
	CertificatePath   string `long:"tlscert" description:"Path to the TLS certificate. Will be created if it doesn't already exist." env:"DCRRATES_EXCHANGE_EXPIRY"`
	KeyPath           string `long:"tlskey" description:"Path to the TLS key. Will be created if it doesn't already exist." env:"DCRRATES_EXCHANGE_EXPIRY"`
}

var defaultConfig = config{
	ConfigPath:       defaultConfigPath,
	GRPCListen:       defaultListen,
	LogPath:          defaultLogPath,
	ExchangeCurrency: defaultBtcIndex,
	ExchangeRefresh:  defaultExchangeRefresh,
	ExchangeExpiry:   defaultExchangeExpiry,
	CertificatePath:  defaultCertPath,
	KeyPath:          defaultKeyPath,
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

	_, err = os.Stat(preCfg.ConfigPath)
	if os.IsNotExist(err) {
		if preCfg.ConfigPath != defaultConfigPath {
			fmt.Fprintln(os.Stderr, "No configuration file found at "+preCfg.ConfigPath)
			os.Exit(0)
		}
	} else {
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
