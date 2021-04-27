// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	flags "github.com/jessevdk/go-flags"
)

const (
	defaultExchangeRefresh = "5m"
	defaultExchangeExpiry  = "60m"
	defaultListen          = ":7778"
	defaultBtcIndex        = "USD"
)

var (
	defaultConfigName  = "rateserver.conf"
	defaultLogDirName  = "logs"
	defaultAltDNSNames = []string(nil)
)

type config struct {
	ConfigPath        string   `short:"c" long:"config" description:"Path to a custom configuration file. (~/.dcrrates/rateserver.conf)" env:"DCRRATES_CONFIG_PATH"`
	AppDirectory      string   `long:"appdir" description:"Path to application home directory. (~/.dcrrates)" env:"DCRRATES_APPDIR_PATH"`
	GRPCListen        string   `short:"l" long:"listen" description:"gRPC listen address." env:"DCRRATES_LISTEN"`
	LogPath           string   `long:"logpath" description:"Directory to log output. ([appdir]/logs/)" env:"DCRRATES_LOG_PATH"`
	LogLevel          string   `long:"loglevel" description:"Logging level {trace, debug, info, warn, error, critical}" env:"DCRRATES_LOG_LEVEL"`
	DisabledExchanges string   `long:"disable-exchange" description:"Exchanges to disable. See /exchanges/exchanges.go for available exchanges. Use a comma to separate multiple exchanges" env:"DCRRATES_DISABLE_EXCHANGES"`
	ExchangeCurrency  string   `long:"exchange-currency" description:"The default bitcoin price index. A 3-letter currency code." env:"DCRRATES_EXCHANGE_INDEX"`
	ExchangeRefresh   string   `long:"exchange-refresh" description:"Time between API calls for exchange data. See (ExchangeBotConfig).DataExpiry." env:"DCRRATES_EXCHANGE_REFRESH"`
	ExchangeExpiry    string   `long:"exchange-expiry" description:"Maximum age before exchange data is discarded. See (ExchangeBotConfig).RequestExpiry." env:"DCRRATES_EXCHANGE_EXPIRY"`
	CertificatePath   string   `long:"tlscert" description:"Path to the TLS certificate. Will be created if it doesn't already exist. ([appdir]/rpc.cert)" env:"DCRRATES_EXCHANGE_EXPIRY"`
	KeyPath           string   `long:"tlskey" description:"Path to the TLS key. Will be created if it doesn't already exist. ([appdir]/rpc.key)" env:"DCRRATES_EXCHANGE_EXPIRY"`
	AltDNSNames       []string `long:"altdnsnames" description:"Specify additional dns names to use when generating the rpc server certificate" env:"DCRRATES_ALT_DNSNAMES" env-delim:","`
}

var defaultConfig = config{
	AppDirectory:     DefaultAppDirectory,
	GRPCListen:       defaultListen,
	ExchangeCurrency: defaultBtcIndex,
	ExchangeRefresh:  defaultExchangeRefresh,
	ExchangeExpiry:   defaultExchangeExpiry,
	AltDNSNames:      defaultAltDNSNames,
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

	if preCfg.AppDirectory != DefaultAppDirectory {
		preCfg.AppDirectory = cleanAndExpandPath(preCfg.AppDirectory)
	}

	defaultConfigPath := filepath.Join(preCfg.AppDirectory, defaultConfigName)
	if preCfg.ConfigPath == "" {
		preCfg.ConfigPath = defaultConfigPath
	} else {
		preCfg.ConfigPath = cleanAndExpandPath(preCfg.ConfigPath)
	}

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

	if cfg.AppDirectory != preCfg.AppDirectory {
		cfg.AppDirectory = cleanAndExpandPath(cfg.AppDirectory)
		err = os.MkdirAll(cfg.AppDirectory, 0700)
		if err != nil {
			return nil, fmt.Errorf("Application directory error: %v", err)
		}
	}

	if cfg.CertificatePath == "" {
		cfg.CertificatePath = filepath.Join(cfg.AppDirectory, DefaultCertName)
	} else {
		cfg.CertificatePath = cleanAndExpandPath(cfg.CertificatePath)
	}
	if cfg.KeyPath == "" {
		cfg.KeyPath = filepath.Join(cfg.AppDirectory, DefaultKeyName)
	} else {
		cfg.KeyPath = cleanAndExpandPath(cfg.KeyPath)
	}
	if cfg.LogPath == "" {
		cfg.LogPath = filepath.Join(cfg.AppDirectory, defaultLogDirName)
	} else {
		cfg.LogPath = cleanAndExpandPath(cfg.LogPath)
	}

	return &cfg, nil
}

// cleanAndExpandPath expands environment variables and leading ~ in the passed
// path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but the variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser to
	// otheruser's home directory.  On Windows, both forward and backward
	// slashes can be used.
	path = path[1:]

	var pathSeparators string
	if runtime.GOOS == "windows" {
		pathSeparators = string(os.PathSeparator) + "/"
	} else {
		pathSeparators = string(os.PathSeparator)
	}

	userName := ""
	if i := strings.IndexAny(path, pathSeparators); i != -1 {
		userName = path[:i]
		path = path[i:]
	}

	homeDir := ""
	var u *user.User
	var err error
	if userName == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(userName)
	}
	if err == nil {
		homeDir = u.HomeDir
	}
	// Fallback to CWD if user lookup fails or user has no home directory.
	if homeDir == "" {
		homeDir = "."
	}

	return filepath.Join(homeDir, path)
}
