// Copyright (c) 2016-2018 The Decred developers
// Copyright (c) 2017 Jonathan Chappelow
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/btcsuite/btclog"
	flags "github.com/btcsuite/go-flags"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/netparams"
)

const (
	defaultConfigFilename = "dcrdata.conf"
	defaultLogFilename    = "dcrdata.log"
	defaultDataDirname    = "data"
	defaultLogLevel       = "info"
	defaultLogDirname     = "logs"
)

var activeNet = &netparams.MainNetParams
var activeChain = &chaincfg.MainNetParams

var (
	defaultHomeDir           = dcrutil.AppDataDir("dcrdata", false)
	defaultConfigFile        = filepath.Join(defaultHomeDir, defaultConfigFilename)
	defaultLogDir            = filepath.Join(defaultHomeDir, defaultLogDirname)
	defaultDataDir           = filepath.Join(defaultHomeDir, defaultDataDirname)
	dcrdHomeDir              = dcrutil.AppDataDir("dcrd", false)
	defaultDaemonRPCCertFile = filepath.Join(dcrdHomeDir, "rpc.cert")

	defaultHost               = "localhost"
	defaultHTTPProfPath       = "/p"
	defaultAPIProto           = "http"
	defaultAPIListen          = "127.0.0.1:7777"
	defaultIndentJSON         = "   "
	defaultCacheControlMaxAge = 86400

	defaultMonitorMempool     = true
	defaultMempoolMinInterval = 2
	defaultMempoolMaxInterval = 120
	defaultMPTriggerTickets   = 1

	defaultDBFileName = "dcrdata.sqlt.db"

	defaultPGHost   = "127.0.0.1:5432"
	defaultPGUser   = "dcrdata"
	defaultPGPass   = ""
	defaultPGDBName = "dcrdata"
)

type config struct {
	// General application behavior
	HomeDir      string `short:"A" long:"appdata" description:"Path to application home directory"`
	ConfigFile   string `short:"C" long:"configfile" description:"Path to configuration file"`
	DataDir      string `short:"b" long:"datadir" description:"Directory to store data"`
	LogDir       string `long:"logdir" description:"Directory to log output."`
	OutFolder    string `short:"f" long:"outfolder" description:"Folder for file outputs"`
	ShowVersion  bool   `short:"V" long:"version" description:"Display version information and exit"`
	TestNet      bool   `long:"testnet" description:"Use the test network (default mainnet)"`
	SimNet       bool   `long:"simnet" description:"Use the simulation test network (default mainnet)"`
	DebugLevel   string `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	Quiet        bool   `short:"q" long:"quiet" description:"Easy way to set debuglevel to error"`
	HTTPProfile  bool   `long:"httpprof" short:"p" description:"Start HTTP profiler."`
	HTTPProfPath string `long:"httpprofprefix" description:"URL path prefix for the HTTP profiler."`
	CPUProfile   string `long:"cpuprofile" description:"File for CPU profiling."`

	// API
	APIProto           string `long:"apiproto" description:"Protocol for API (http or https)"`
	APIListen          string `long:"apilisten" description:"Listen address for API"`
	IndentJSON         string `long:"indentjson" description:"String for JSON indentation (default is \"   \"), when indentation is requested via URL query."`
	UseRealIP          bool   `long:"userealip" description:"Use the RealIP middleware from the pressly/chi/middleware package to get the client's real IP from the X-Forwarded-For or X-Real-IP headers, in that order."`
	CacheControlMaxAge int    `long:"cachecontrol-maxage" description:"Set CacheControl in the HTTP response header to a value in seconds for clients to cache the response. This applies only to FileServer routes."`

	// Data I/O
	MonitorMempool     bool   `short:"m" long:"mempool" description:"Monitor mempool for new transactions, and report ticketfee info when new tickets are added."`
	MempoolMinInterval int    `long:"mp-min-interval" description:"The minimum time in seconds between mempool reports, regarless of number of new tickets seen."`
	MempoolMaxInterval int    `long:"mp-max-interval" description:"The maximum time in seconds between mempool reports (within a couple seconds), regarless of number of new tickets seen."`
	MPTriggerTickets   int    `long:"mp-ticket-trigger" description:"The number minimum number of new tickets that must be seen to trigger a new mempool report."`
	DumpAllMPTix       bool   `long:"dumpallmptix" description:"Dump to file the fees of all the tickets in mempool."`
	DBFileName         string `long:"dbfile" description:"SQLite DB file name (default is dcrdata.sqlt.db)."`

	FullMode bool   `long:"pg" description:"Run in \"Full Mode\" mode,  enables postgresql support"`
	PGDBName string `long:"pgdbname" description:"PostgreSQL DB name."`
	PGUser   string `long:"pguser" description:"PostgreSQL DB user."`
	PGPass   string `long:"pgpass" description:"PostgreSQL DB password."`
	PGHost   string `long:"pghost" description:"PostgreSQL server host:port or UNIX socket (e.g. /run/postgresql)."`

	// WatchAddresses []string `short:"w" long:"watchaddress" description:"Watched address (receiving). One per line."`
	// SMTPUser     string `long:"smtpuser" description:"SMTP user name"`
	// SMTPPass     string `long:"smtppass" description:"SMTP password"`
	// SMTPServer   string `long:"smtpserver" description:"SMTP host name"`
	// EmailAddr    string `long:"emailaddr" description:"Destination email address for alerts"`
	// EmailSubject string `long:"emailsubj" description:"Email subject. (default \"dcrdataapi transaction notification\")"`

	// RPC client options
	DcrdUser         string `long:"dcrduser" description:"Daemon RPC user name"`
	DcrdPass         string `long:"dcrdpass" description:"Daemon RPC password"`
	DcrdServ         string `long:"dcrdserv" description:"Hostname/IP and port of dcrd RPC server to connect to (default localhost:9109, testnet: localhost:19109, simnet: localhost:19556)"`
	DcrdCert         string `long:"dcrdcert" description:"File containing the dcrd certificate file"`
	DisableDaemonTLS bool   `long:"nodaemontls" description:"Disable TLS for the daemon RPC client -- NOTE: This is only allowed if the RPC client is connecting to localhost"`
}

var (
	defaultConfig = config{
		HomeDir:            defaultHomeDir,
		DataDir:            defaultDataDir,
		LogDir:             defaultLogDir,
		ConfigFile:         defaultConfigFile,
		DBFileName:         defaultDBFileName,
		DebugLevel:         defaultLogLevel,
		HTTPProfPath:       defaultHTTPProfPath,
		APIProto:           defaultAPIProto,
		APIListen:          defaultAPIListen,
		IndentJSON:         defaultIndentJSON,
		CacheControlMaxAge: defaultCacheControlMaxAge,
		DcrdCert:           defaultDaemonRPCCertFile,
		MonitorMempool:     defaultMonitorMempool,
		MempoolMinInterval: defaultMempoolMinInterval,
		MempoolMaxInterval: defaultMempoolMaxInterval,
		MPTriggerTickets:   defaultMPTriggerTickets,
		PGDBName:           defaultPGDBName,
		PGUser:             defaultPGUser,
		PGPass:             defaultPGPass,
		PGHost:             defaultPGHost,
	}
)

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

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	_, ok := btclog.LevelFromString(logLevel)
	return ok
}

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsytems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		if !validLogLevel(debugLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, debugLevel)
		}

		// Change the logging level for all subsystems.
		setLogLevels(debugLevel)

		return nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	for _, logLevelPair := range strings.Split(debugLevel, ",") {
		if !strings.Contains(logLevelPair, "=") {
			str := "The specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "The specified subsystem [%v] is invalid -- " +
				"supported subsytems %v"
			return fmt.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, logLevel)
		}

		setLogLevel(subsysID, logLevel)
	}

	return nil
}

// loadConfig initializes and parses the config using a config file and command
// line options.
func loadConfig() (*config, error) {
	loadConfigError := func(err error) (*config, error) {
		return nil, err
	}

	// Default config.
	cfg := defaultConfig

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.
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
		return loadConfigError(err)
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s)\n", appName,
			ver.String(), runtime.Version())
		os.Exit(0)
	}

	// Load additional config from file.
	var configFileError error
	// Config file name for logging.
	configFile := "NONE (defaults)"
	parser := flags.NewParser(&cfg, flags.Default)
	if _, err := os.Stat(preCfg.ConfigFile); os.IsNotExist(err) {
		// Non-default config file must exist
		if defaultConfig.ConfigFile != preCfg.ConfigFile {
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}
		// Warn about missing default config file, but continue
		fmt.Printf("Config file (%s) does not exist. Using defaults.\n",
			preCfg.ConfigFile)
	} else {
		// The config file exists, so attempt to parse it.
		err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
		if err != nil {
			if _, ok := err.(*os.PathError); !ok {
				fmt.Fprintln(os.Stderr, err)
				parser.WriteHelp(os.Stderr)
				return loadConfigError(err)
			}
			configFileError = err
		}
		configFile = preCfg.ConfigFile
	}

	// Parse command line options again to ensure they take precedence.
	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return loadConfigError(err)
	}

	// Create the home directory if it doesn't already exist.
	funcName := "loadConfig"
	err = os.MkdirAll(cfg.HomeDir, 0700)
	if err != nil {
		// Show a nicer error message if it's because a symlink is linked to a
		// directory that does not exist (probably because it's not mounted).
		if e, ok := err.(*os.PathError); ok && os.IsExist(err) {
			if link, lerr := os.Readlink(e.Path); lerr == nil {
				str := "is symlink %s -> %s mounted?"
				err = fmt.Errorf(str, e.Path, link)
			}
		}

		str := "%s: failed to create home directory: %v"
		err := fmt.Errorf(str, funcName, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}

	// Warn about missing config file after the final command line parse
	// succeeds.  This prevents the warning on help messages and invalid
	// options.
	if configFileError != nil {
		fmt.Printf("%v\n", configFileError)
		return loadConfigError(configFileError)
	}

	// Choose the active network params based on the selected network. Multiple
	// networks can't be selected simultaneously.
	numNets := 0
	activeNet = &netparams.MainNetParams
	activeChain = &chaincfg.MainNetParams
	if cfg.TestNet {
		activeNet = &netparams.TestNet2Params
		activeChain = &chaincfg.TestNet2Params
		numNets++
	}
	if cfg.SimNet {
		activeNet = &netparams.SimNetParams
		activeChain = &chaincfg.SimNetParams
		numNets++
	}
	if numNets > 1 {
		str := "%s: the testnet and simnet params can't be " +
			"used together -- choose one of the three"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Append the network type to the data directory so it is "namespaced" per
	// network.  In addition to the block database, there are other pieces of
	// data that are saved to disk such as address manager state. All data is
	// specific to a network, so namespacing the data directory means each
	// individual piece of serialized data does not have to worry about changing
	// names per network and such.
	//
	// Make list of old versions of testnet directories here since the network
	// specific DataDir will be used after this.
	cfg.DataDir = cleanAndExpandPath(cfg.DataDir)
	cfg.DataDir = filepath.Join(cfg.DataDir, netName(activeNet))
	// Create the data folder if it does not exist.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		return nil, err
	}

	logRotator = nil
	// Append the network type to the log directory so it is "namespaced"
	// per network in the same fashion as the data directory.
	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	cfg.LogDir = filepath.Join(cfg.LogDir, netName(activeNet))

	// Initialize log rotation. After log rotation has been initialized, the
	// logger variables may be used. This creates the LogDir if needed.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

	log.Infof("Log folder:  %s", cfg.LogDir)
	log.Infof("Config file: %s", configFile)

	// Set the host names and ports to the default if the user does not specify
	// them.
	if cfg.DcrdServ == "" {
		cfg.DcrdServ = defaultHost + ":" + activeNet.JSONRPCClientPort
	}

	// Output folder
	cfg.OutFolder = cleanAndExpandPath(cfg.OutFolder)
	cfg.OutFolder = filepath.Join(cfg.OutFolder, activeNet.Name)

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Ensure HTTP profiler is mounted with a valid path prefix.
	if cfg.HTTPProfile && (cfg.HTTPProfPath == "/" || len(defaultHTTPProfPath) == 0) {
		return loadConfigError(fmt.Errorf("httpprofprefix must not be \"\" or \"/\""))
	}

	// Parse, validate, and set debug log level(s).
	if cfg.Quiet {
		cfg.DebugLevel = "error"
	}

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err = fmt.Errorf("%s: %v", funcName, err.Error())
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	return &cfg, nil
}

// netName returns the name used when referring to a decred network.  At the
// time of writing, dcrd currently places blocks for testnet version 0 in the
// data and log directory "testnet", which does not match the Name field of the
// chaincfg parameters.  This function can be used to override this directory
// name as "testnet2" when the passed active network matches wire.TestNet2.
//
// A proper upgrade to move the data and log directories for this network to
// "testnet" is planned for the future, at which point this function can be
// removed and the network parameter's name used instead.
func netName(chainParams *netparams.Params) string {
	switch chainParams.Net {
	case wire.TestNet2:
		return "testnet2"
	default:
		return chainParams.Name
	}
}
