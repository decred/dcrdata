// Copyright (c) 2016-2021, The Decred developers
// Copyright (c) 2017 Jonathan Chappelow
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"

	"github.com/decred/dcrdata/v6/db/dbtypes"
	"github.com/decred/dcrdata/v6/netparams"
)

const (
	defaultConfigFilename = "dcrdata.conf"
	defaultLogFilename    = "dcrdata.log"
	defaultDataDirname    = "data"
	defaultLogLevel       = "info"
	defaultLogDirname     = "logs"
)

var activeNet = &netparams.MainNetParams
var activeChain = chaincfg.MainNetParams()

var (
	defaultHomeDir           = dcrutil.AppDataDir("dcrdata", false)
	defaultConfigFile        = filepath.Join(defaultHomeDir, defaultConfigFilename)
	defaultLogDir            = filepath.Join(defaultHomeDir, defaultLogDirname)
	defaultDataDir           = filepath.Join(defaultHomeDir, defaultDataDirname)
	dcrdHomeDir              = dcrutil.AppDataDir("dcrd", false)
	defaultDaemonRPCCertFile = filepath.Join(dcrdHomeDir, "rpc.cert")
	defaultMaxLogZips        = 16

	defaultHost                = "localhost"
	defaultHTTPProfPath        = "/p"
	defaultAPIProto            = "http"
	defaultMainnetPort         = "7777"
	defaultTestnetPort         = "17778"
	defaultSimnetPort          = "17779"
	defaultIndentJSON          = "   "
	defaultCacheControlMaxAge  = 86400
	defaultInsightReqRateLimit = 20.0
	defaultMaxCSVAddrs         = 25
	defaultServerHeader        = "dcrdata"

	defaultMempoolMinInterval = 2
	defaultMempoolMaxInterval = 120
	defaultMPTriggerTickets   = 1

	defaultAgendasDBFileName = "agendas.db"
	defaultProposalsFileName = "proposals.db"
	defaultPoliteiaURL       = "https://proposals.decred.org/"
	defaultChartsCacheDump   = "chartscache.gob"

	defaultPGHost           = "127.0.0.1:5432"
	defaultPGUser           = "dcrdata"
	defaultPGPass           = ""
	defaultPGDBName         = "dcrdata"
	defaultPGQueryTimeout   = 20 * time.Minute
	defaultAddrCacheCap     = 1 << 29 // 512 MiB
	defaultAddrCacheLimit   = 4096
	defaultAddrCacheUXTOCap = 1 << 29

	defaultExchangeIndex     = "USD"
	defaultDisabledExchanges = "dragonex,poloniex"
	defaultRateCertFile      = filepath.Join(defaultHomeDir, "rpc.cert")

	defaultMainnetLink  = "https://explorer.dcrdata.org/"
	defaultTestnetLink  = "https://testnet.dcrdata.org/"
	defaultOnionAddress = ""

	maxSyncStatusLimit = 5000
)

type config struct {
	// General application behavior
	HomeDir      string `short:"A" long:"appdata" description:"Path to application home directory" env:"DCRDATA_APPDATA_DIR"`
	ConfigFile   string `short:"C" long:"configfile" description:"Path to configuration file" env:"DCRDATA_CONFIG_FILE"`
	DataDir      string `short:"b" long:"datadir" description:"Directory to store data" env:"DCRDATA_DATA_DIR"`
	LogDir       string `long:"logdir" description:"Directory to log output." env:"DCRDATA_LOG_DIR"`
	MaxLogZips   int    `long:"max-log-zips" description:"The number of zipped log files created by the log rotator to be retained. Setting to 0 will keep all." env:"DCRDATA_MAX_LOG_ZIPS"`
	OutFolder    string `short:"f" long:"outfolder" description:"Folder for file outputs" env:"DCRDATA_OUT_FOLDER"`
	ShowVersion  bool   `short:"V" long:"version" description:"Display version information and exit" env:"DCRDATA_SHOW_VERSION"`
	TestNet      bool   `long:"testnet" description:"Use the test network (default mainnet)" env:"DCRDATA_USE_TESTNET"`
	SimNet       bool   `long:"simnet" description:"Use the simulation test network (default mainnet)" env:"DCRDATA_USE_SIMNET"`
	DebugLevel   string `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}" env:"DCRDATA_LOG_LEVEL"`
	Quiet        bool   `short:"q" long:"quiet" description:"Easy way to set debuglevel to error" env:"DCRDATA_QUIET"`
	HTTPProfile  bool   `long:"httpprof" short:"p" description:"Start HTTP profiler." env:"DCRDATA_ENABLE_HTTP_PROFILER"`
	HTTPProfPath string `long:"httpprofprefix" description:"URL path prefix for the HTTP profiler." env:"DCRDATA_HTTP_PROFILER_PREFIX"`
	CPUProfile   string `long:"cpuprofile" description:"File for CPU profiling." env:"DCRDATA_CPU_PROFILER_FILE"`
	UseGops      bool   `short:"g" long:"gops" description:"Run with gops diagnostics agent listening. See github.com/google/gops for more information." env:"DCRDATA_USE_GOPS"`
	ReloadHTML   bool   `long:"reload-html" description:"Reload HTML templates on every request" env:"DCRDATA_RELOAD_HTML"`

	// API/server
	APIProto            string  `long:"apiproto" description:"Protocol for API (http or https)" env:"DCRDATA_ENABLE_HTTPS"`
	APIListen           string  `long:"apilisten" description:"Listen address for API. default localhost:7777, :17778 testnet, :17779 simnet" env:"DCRDATA_LISTEN_URL"`
	IndentJSON          string  `long:"indentjson" description:"String for JSON indentation (default is \"   \"), when indentation is requested via URL query." env:"DCRDATA_INDENT_JSON"`
	UseRealIP           bool    `long:"userealip" description:"Use the RealIP middleware from the pressly/chi/middleware package to get the client's real IP from the X-Forwarded-For or X-Real-IP headers, in that order." env:"DCRDATA_USE_REAL_IP"`
	CacheControlMaxAge  int     `long:"cachecontrol-maxage" description:"Set CacheControl in the HTTP response header to a value in seconds for clients to cache the response. This applies only to FileServer routes." env:"DCRDATA_MAX_CACHE_AGE"`
	InsightReqRateLimit float64 `long:"insight-limit-rps" description:"Requests/second per client IP for the Insight API's rate limiter." env:"DCRDATA_INSIGHT_RATE_LIMIT"`
	MaxCSVAddrs         int     `long:"max-api-addrs" description:"Maximum allowed comma-separated addresses for endpoints that accept multiple addresses." env:"DCRDATA_MAX_CSV_ADDRS"`
	CompressAPI         bool    `long:"compress-api" description:"Use compression for a number of endpoints with commonly large responses." env:"DCRDATA_COMPRESS_API"`
	ServerHeader        string  `long:"server-http-header" description:"Set the HTTP response header Server key value. Valid values are \"off\", \"version\", or a custom string." env:"DCRDATA_SERVER_HEADER"`

	// Mempool
	MempoolMinInterval int `long:"mp-min-interval" description:"The minimum time in seconds between mempool reports, regardless of number of new tickets seen." env:"DCRDATA_MEMPOOL_MIN_INTERVAL"`
	MempoolMaxInterval int `long:"mp-max-interval" description:"The maximum time in seconds between mempool reports (within a couple seconds), regardless of number of new tickets seen." env:"DCRDATA_MEMPOOL_MAX_INTERVAL"`
	MPTriggerTickets   int `long:"mp-ticket-trigger" description:"The minimum number of new tickets that must be seen to trigger a new mempool report." env:"DCRDATA_MP_TRIGGER_TICKETS"`

	// Consensus agendas and politeia proposals
	AgendasDBFileName string `long:"agendadbfile" description:"Agendas DB file name (default is agendas.db)." env:"DCRDATA_AGENDAS_DB_FILE_NAME"`
	ProposalsFileName string `long:"proposalsdbfile" description:"Proposals DB file name (default is proposals.db)." env:"DCRDATA_PROPOSALS_DB_FILE_NAME"`
	PoliteiaURL       string `long:"politeiaurl" description:"Defines the root API politeia URL (defaults to https://proposals.decred.org/)." env:"DCRDATA_POLITEIA_URL"`

	// Caching and optimization.
	AddrCacheCap     int    `long:"addr-cache-cap" description:"Address cache capacity in bytes." env:"DCRDATA_ADDR_CACHE_CAP"`
	AddrCacheLimit   int    `long:"addr-cache-address-limit" description:"Maximum number of addresses allowed in the address cache." env:"DCRDATA_ADDR_CACHE_LIMIT"`
	AddrCacheUXTOCap int    `long:"addr-cache-utxo-cap" description:"UTXO cache capacity in bytes." env:"DCRDATA_ADDR_CASH_UTXO_CAP"`
	NoDevPrefetch    bool   `long:"no-dev-prefetch" description:"Disable automatic dev fund balance query on new blocks. When true, the query will still be run on demand, but not automatically after new blocks are connected." env:"DCRDATA_DISABLE_DEV_PREFETCH"`
	ChartsCacheDump  string `long:"chartscache" description:"Defines the file name that holds the charts cache data on system exit." env:"DCRDATA_CHARTS_CACHE"`

	// DB backend
	PGDBName         string        `long:"pgdbname" description:"PostgreSQL DB name." env:"DCRDATA_PG_DB_NAME"`
	PGUser           string        `long:"pguser" description:"PostgreSQL DB user." env:"DCRDATA_POSTGRES_USER"`
	PGPass           string        `long:"pgpass" description:"PostgreSQL DB password." env:"DCRDATA_POSTGRES_PASS"`
	PGHost           string        `long:"pghost" description:"PostgreSQL server host:port or UNIX socket (e.g. /run/postgresql)." env:"DCRDATA_POSTGRES_HOST_URL"`
	PGQueryTimeout   time.Duration `short:"T" long:"pgtimeout" description:"Timeout (a time.Duration string) for most PostgreSQL queries used for user initiated queries." env:"DCRDATA_PG_QUERY_TIMEOUT"`
	HidePGConfig     bool          `long:"hidepgconfig" description:"Blocks logging of the PostgreSQL db configuration on system start up." env:"DCRDATA_PG_HIDE_CONFIG"`
	DropIndexes      bool          `long:"drop-inds" short:"D" description:"Drop all table indexes and exit." env:"DCRDATA_PG_DROP_INDEXES"`
	PurgeNBestBlocks int           `long:"purge-n-blocks" description:"Purge all data for the N best blocks, using the best block across all DBs if they are out of sync." env:"DCRDATA_PURGE_N_BLOCKS"`
	SyncAndQuit      bool          `long:"sync-and-quit" description:"Sync to the best block and exit. Do not start the explorer or API." env:"DCRDATA_ENABLE_SYNC_N_QUIT"`
	ImportSideChains bool          `long:"import-side-chains" description:"(experimental) Enable startup import of side chains retrieved from dcrd via getchaintips." env:"DCRDATA_IMPORT_SIDE_CHAINS"`
	SyncStatusLimit  int           `long:"sync-status-limit" description:"Sets the number of blocks behind the current best height past which only the syncing status page can be served on the running web server. Value should be greater than 2 but less than 5000." env:"DCRDATA_SYNC_STATUS_LIMIT"`

	// RPC client options
	DcrdUser         string `long:"dcrduser" description:"Daemon RPC user name" env:"DCRDATA_DCRD_USER"`
	DcrdPass         string `long:"dcrdpass" description:"Daemon RPC password" env:"DCRDATA_DCRD_PASS"`
	DcrdServ         string `long:"dcrdserv" description:"Hostname/IP and port of dcrd RPC server to connect to (default localhost:9109, testnet: localhost:19109, simnet: localhost:19556)" env:"DCRDATA_DCRD_URL"`
	DcrdCert         string `long:"dcrdcert" description:"File containing the dcrd certificate file" env:"DCRDATA_DCRD_CERT"`
	DisableDaemonTLS bool   `long:"nodaemontls" description:"Disable TLS for the daemon RPC client -- NOTE: This is only allowed if the RPC client is connecting to localhost" env:"DCRDATA_DCRD_DISABLE_TLS"`
	NoBlockPrefetch  bool   `long:"no-dcrd-block-prefetch" description:"Disable block pre-fetch from dcrd during startup sync." env:"DCRDATA_NO_BLOCK_PREFETCH"`

	// ExchangeBot settings
	EnableExchangeBot bool   `long:"exchange-monitor" description:"Enable the exchange monitor" env:"DCRDATA_MONITOR_EXCHANGES"`
	DisabledExchanges string `long:"disable-exchange" description:"Exchanges to disable. See /exchanges/exchanges.go for available exchanges. Use a comma to separate multiple exchanges" env:"DCRDATA_DISABLE_EXCHANGES"`
	ExchangeCurrency  string `long:"exchange-currency" description:"The default bitcoin price index. A 3-letter currency code" env:"DCRDATA_EXCHANGE_INDEX"`
	RateMaster        string `long:"ratemaster" description:"The address of a DCRRates instance. Exchange monitoring will get all data from a DCRRates subscription." env:"DCRDATA_RATE_MASTER"`
	RateCertificate   string `long:"ratecert" description:"File containing DCRRates TLS certificate file." env:"DCRDATA_RATE_MASTER"`

	// Links
	MainnetLink  string `long:"mainnet-link" description:"When dcrdata is on testnet, this address will be used to direct a user to a dcrdata on mainnet when appropriate." env:"DCRDATA_MAINNET_LINK"`
	TestnetLink  string `long:"testnet-link" description:"When dcrdata is on mainnet, this address will be used to direct a user to a dcrdata on testnet when appropriate." env:"DCRDATA_TESTNET_LINK"`
	OnionAddress string `long:"onion-address" description:"Hidden service address" env:"DCRDATA_ONION_ADDRESS"`
}

var (
	defaultConfig = config{
		HomeDir:             defaultHomeDir,
		DataDir:             defaultDataDir,
		LogDir:              defaultLogDir,
		MaxLogZips:          defaultMaxLogZips,
		ConfigFile:          defaultConfigFile,
		AgendasDBFileName:   defaultAgendasDBFileName,
		ProposalsFileName:   defaultProposalsFileName,
		PoliteiaURL:         defaultPoliteiaURL,
		ChartsCacheDump:     defaultChartsCacheDump,
		DebugLevel:          defaultLogLevel,
		HTTPProfPath:        defaultHTTPProfPath,
		APIProto:            defaultAPIProto,
		IndentJSON:          defaultIndentJSON,
		CacheControlMaxAge:  defaultCacheControlMaxAge,
		InsightReqRateLimit: defaultInsightReqRateLimit,
		MaxCSVAddrs:         defaultMaxCSVAddrs,
		ServerHeader:        defaultServerHeader,
		DcrdCert:            defaultDaemonRPCCertFile,
		MempoolMinInterval:  defaultMempoolMinInterval,
		MempoolMaxInterval:  defaultMempoolMaxInterval,
		MPTriggerTickets:    defaultMPTriggerTickets,
		PGDBName:            defaultPGDBName,
		PGUser:              defaultPGUser,
		PGPass:              defaultPGPass,
		PGHost:              defaultPGHost,
		PGQueryTimeout:      defaultPGQueryTimeout,
		AddrCacheCap:        defaultAddrCacheCap,
		AddrCacheLimit:      defaultAddrCacheLimit,
		AddrCacheUXTOCap:    defaultAddrCacheUXTOCap,
		ExchangeCurrency:    defaultExchangeIndex,
		DisabledExchanges:   defaultDisabledExchanges,
		RateCertificate:     defaultRateCertFile,
		MainnetLink:         defaultMainnetLink,
		TestnetLink:         defaultTestnetLink,
		OnionAddress:        defaultOnionAddress,
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

// normalizeNetworkAddress checks for a valid local network address format and
// adds default host and port if not present. Invalidates addresses that include
// a protocol identifier.
func normalizeNetworkAddress(a, defaultHost, defaultPort string) (string, error) {
	if strings.Contains(a, "://") {
		return a, fmt.Errorf("Address %s contains a protocol identifier, which is not allowed", a)
	}
	if a == "" {
		return defaultHost + ":" + defaultPort, nil
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			normalized := a + ":" + defaultPort
			host, port, err = net.SplitHostPort(normalized)
			if err != nil {
				return a, fmt.Errorf("Unable to address %s after port resolution: %v", normalized, err)
			}
		} else {
			return a, fmt.Errorf("Unable to normalize address %s: %v", a, err)
		}
	}
	if host == "" {
		host = defaultHost
	}
	if port == "" {
		port = defaultPort
	}
	return host + ":" + port, nil
}

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	_, ok := slog.LevelFromString(logLevel)
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

	// Sort the subsystems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimiters, treat it as
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
				"supported subsystems %v"
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

	// Default config
	cfg := defaultConfig
	defaultConfigNow := defaultConfig

	// Load settings from environment variables.
	err := env.Parse(&cfg)
	if err != nil {
		return loadConfigError(err)
	}

	// If appdata was specified but not the config file, change the config file
	// path, and record this as the new default config file location.
	if defaultHomeDir != cfg.HomeDir && defaultConfigNow.ConfigFile == cfg.ConfigFile {
		cfg.ConfigFile = filepath.Join(cfg.HomeDir, defaultConfigFilename)
		// Update the defaultConfig to avoid an error if the config file in this
		// "new default" location does not exist.
		defaultConfigNow.ConfigFile = cfg.ConfigFile
	}

	// Pre-parse the command line options to see if an alternative config file
	// or the version flag was specified. Override any environment variables
	// with parsed command line flags.
	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.HelpFlag|flags.PassDoubleDash)
	_, flagerr := preParser.Parse()

	if flagerr != nil {
		e, ok := flagerr.(*flags.Error)
		if !ok || e.Type != flags.ErrHelp {
			preParser.WriteHelp(os.Stderr)
		}
		if ok && e.Type == flags.ErrHelp {
			preParser.WriteHelp(os.Stdout)
			os.Exit(0)
		}
		return loadConfigError(flagerr)
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s)\n", appName,
			Version(), runtime.Version())
		os.Exit(0)
	}

	// If a non-default appdata folder is specified on the command line, it may
	// be necessary adjust the config file location. If the the config file
	// location was not specified on the command line, the default location
	// should be under the non-default appdata directory. However, if the config
	// file was specified on the command line, it should be used regardless of
	// the appdata directory.
	if defaultHomeDir != preCfg.HomeDir && defaultConfigNow.ConfigFile == preCfg.ConfigFile {
		preCfg.ConfigFile = filepath.Join(preCfg.HomeDir, defaultConfigFilename)
		// Update the defaultConfig to avoid an error if the config file in this
		// "new default" location does not exist.
		defaultConfigNow.ConfigFile = preCfg.ConfigFile
	}

	// Load additional config from file.
	var configFileError error
	// Config file name for logging.
	configFile := "NONE (defaults)"
	parser := flags.NewParser(&cfg, flags.Default)

	// Do not error default config file is missing.
	if _, err := os.Stat(preCfg.ConfigFile); os.IsNotExist(err) {
		// Non-default config file must exist
		if defaultConfigNow.ConfigFile != preCfg.ConfigFile {
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

	// If a non-default appdata folder is specified, it may be necessary to
	// adjust the DataDir and LogDir. If these other paths are their defaults,
	// they should be modifed to look under the non-default appdata directory.
	// If they are not their defaults, the user-specified values should be used.
	if defaultHomeDir != cfg.HomeDir {
		if defaultDataDir == cfg.DataDir {
			cfg.DataDir = filepath.Join(cfg.HomeDir, defaultDataDirname)
		}
		if defaultLogDir == cfg.LogDir {
			cfg.LogDir = filepath.Join(cfg.HomeDir, defaultLogDirname)
		}
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
	activeChain = chaincfg.MainNetParams()
	defaultPort := defaultMainnetPort
	if cfg.TestNet {
		activeNet = &netparams.TestNet3Params
		activeChain = chaincfg.TestNet3Params()
		defaultPort = defaultTestnetPort
		numNets++
	}
	if cfg.SimNet {
		activeNet = &netparams.SimNetParams
		activeChain = chaincfg.SimNetParams()
		defaultPort = defaultSimnetPort
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
	cfg.DataDir = filepath.Join(cfg.DataDir, activeNet.Name)
	// Create the data folder if it does not exist.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		return nil, err
	}

	logRotator = nil
	// Append the network type to the log directory so it is "namespaced"
	// per network in the same fashion as the data directory.
	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	cfg.LogDir = filepath.Join(cfg.LogDir, activeNet.Name)

	// Initialize log rotation. After log rotation has been initialized, the
	// logger variables may be used. This creates the LogDir if needed.
	if cfg.MaxLogZips < 0 {
		cfg.MaxLogZips = 0
	}
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename), cfg.MaxLogZips)

	log.Infof("Log folder:  %s", cfg.LogDir)
	log.Infof("Config file: %s", configFile)

	// Disable dev balance prefetch if network has invalid script.
	_, err = dbtypes.DevSubsidyAddress(activeChain)
	if !cfg.NoDevPrefetch && err != nil {
		cfg.NoDevPrefetch = true
		log.Warnf("%v. Disabling balance prefetch (--no-dev-prefetch).", err)
	}

	// Validate SyncStatusLimit has been set. Zero means always show sync status
	// page instead of full block explorer pages.
	if cfg.SyncStatusLimit != 0 {
		// The sync-status-limit value should not be set to a value less than 2
		// or greater than maxSyncStatusLimit.
		if cfg.SyncStatusLimit < 2 || cfg.SyncStatusLimit > maxSyncStatusLimit {
			return nil, fmt.Errorf("sync-status-limit should not be set to "+
				"a value less than 2 or more than %d", maxSyncStatusLimit)
		}
	}

	// Validate block purge options.
	if cfg.PurgeNBestBlocks < 0 {
		return nil, fmt.Errorf("purge-n-blocks must be non-negative")
	}

	// Set the host names and ports to the default if the user does not specify
	// them.
	cfg.DcrdServ, err = normalizeNetworkAddress(cfg.DcrdServ, defaultHost, activeNet.JSONRPCClientPort)
	if err != nil {
		return loadConfigError(err)
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

	// Validate DB timeout. Zero or negative should be set to the large default
	// timeout to effectively disable timeouts.
	if cfg.PGQueryTimeout <= 0 {
		cfg.PGQueryTimeout = defaultPGQueryTimeout
	}

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err = fmt.Errorf("%s: %v", funcName, err.Error())
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Check the supplied APIListen address
	if cfg.APIListen == "" {
		cfg.APIListen = defaultHost + ":" + defaultPort
	} else {
		cfg.APIListen, err = normalizeNetworkAddress(cfg.APIListen, defaultHost, defaultPort)
		if err != nil {
			return loadConfigError(err)
		}
	}

	// Checks if the expected format of the politeia URL was set. It also drops any
	// unnecessary parts of the URL.
	urlPath, err := retrieveRootPath(cfg.PoliteiaURL)
	if err != nil {
		return loadConfigError(err)
	}
	cfg.PoliteiaURL = urlPath

	switch cfg.ServerHeader {
	case "off":
		cfg.ServerHeader = ""
	case "version":
		cfg.ServerHeader = AppName + "-" + Version()
	}

	// Expand some additional paths.
	cfg.DcrdCert = cleanAndExpandPath(cfg.DcrdCert)
	cfg.AgendasDBFileName = cleanAndExpandPath(cfg.AgendasDBFileName)
	cfg.ProposalsFileName = cleanAndExpandPath(cfg.ProposalsFileName)
	cfg.RateCertificate = cleanAndExpandPath(cfg.RateCertificate)
	cfg.ChartsCacheDump = cleanAndExpandPath(cfg.ChartsCacheDump)

	// Clean up the provided mainnet and testnet links, ensuring there is a single
	// trailing slash.
	cfg.MainnetLink = strings.TrimSuffix(cfg.MainnetLink, "/") + "/"
	cfg.TestnetLink = strings.TrimSuffix(cfg.TestnetLink, "/") + "/"

	return &cfg, nil
}

// retrieveRootPath drops all extra characters that are not part of the root path.
// i.e. with input http://www.mydomain.com/xxxxx, http://www.mydomain.com should
// be returned.
func retrieveRootPath(path string) (string, error) {
	r, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid '%s' url used. error: %v", path, err)
	}

	// If the url scheme or host were not found, a regex expression can be used to
	// eliminate the unwanted part.
	if r.Scheme == "" || r.Host == "" {
		exp := regexp.MustCompile(`([\/?]\S*)`)
		return exp.ReplaceAllLiteralString(path, ""), nil
	}

	r.Path = ""     // Drop any set path and the leading slash
	r.RawQuery = "" // Drop any set Query
	return r.String(), nil
}
