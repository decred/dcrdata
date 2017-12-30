package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	flags "github.com/btcsuite/go-flags"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/netparams"
)

const (
	defaultConfigFilename = "rebuilddb2.conf"
	defaultLogLevel       = "info"
	defaultLogDirname     = "logs"
	//defaultLogFilename    = "rebuilddb2.log"
)

var curDir, _ = os.Getwd()
var activeNet = &netparams.MainNetParams
var activeChain = &chaincfg.MainNetParams

var (
	dcrdHomeDir = dcrutil.AppDataDir("dcrd", false)
	//rebuilddbHomeDir            = dcrutil.AppDataDir("rebuilddb", false)
	defaultDaemonRPCCertFile = filepath.Join(dcrdHomeDir, "rpc.cert")
	defaultConfigFile        = filepath.Join(curDir, defaultConfigFilename)
	defaultLogDir            = filepath.Join(curDir, defaultLogDirname)
	defaultHost              = "localhost"

	defaultDBHostPort = "127.0.0.1:5432"
	defaultDBUser     = "dcrdata"
	defaultDBPass     = ""
	defaultDBName     = "dcrdata"
)

type config struct {
	// General application behavior
	ConfigFile  string `short:"C" long:"configfile" description:"Path to configuration file"`
	ShowVersion bool   `short:"V" long:"version" description:"Display version information and exit"`
	TestNet     bool   `long:"testnet" description:"Use the test network (default mainnet)"`
	SimNet      bool   `long:"simnet" description:"Use the simulation test network (default mainnet)"`
	DebugLevel  string `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	Quiet       bool   `short:"q" long:"quiet" description:"Easy way to set debuglevel to error"`
	LogDir      string `long:"logdir" description:"Directory to log output"`
	CPUProfile  string `long:"cpuprofile" description:"File for CPU profiling."`

	// DB
	DBHostPort            string `long:"dbhost" description:"DB host"`
	DBUser                string `long:"dbuser" description:"DB user"`
	DBPass                string `long:"dbpass" description:"DB pass"`
	DBName                string `long:"dbname" description:"DB name"`
	DropDBTables          bool   `short:"D" long:"droptables" description:"Drop/delete DB tables."`
	AddrSpendInfoOnline   bool   `short:"a" long:"addrspends-no-batch" description:"Continually update the address table spending transaction info during rebuild (instead of full table update at end).  SLOW if doing full rebuild!"`
	TicketSpendInfoOnline bool   `short:"t" long:"ticketspends-no-batch" description:"Continually update the tickets table spending transaction info during rebuild (instead of full table update at end).  SLOW if doing full rebuild!"`

	// RPC client options
	DcrdUser         string `long:"dcrduser" description:"Daemon RPC user name"`
	DcrdPass         string `long:"dcrdpass" description:"Daemon RPC password"`
	DcrdServ         string `long:"dcrdserv" description:"Hostname/IP and port of dcrd RPC server to connect to (default localhost:9109, testnet: localhost:19109, simnet: localhost:19556)"`
	DcrdCert         string `long:"dcrdcert" description:"File containing the dcrd certificate file"`
	DisableDaemonTLS bool   `long:"nodaemontls" description:"Disable TLS for the daemon RPC client -- NOTE: This is only allowed if the RPC client is connecting to localhost"`

	ForceReindex bool `long:"reindex" short:"R" description:"Drop indexes prior to sync and recreate after sync, with insertion conflict checks disabled in absence of constraints."`
	// TODO
	//AccountName   string `long:"accountname" description:"Account name (other than default or imported) for which balances should be listed."`
	//TicketAddress string `long:"ticketaddress" description:"Address to which you have given voting rights"`
	//PoolAddress   string `long:"pooladdress" description:"Address to which you have given rights to pool fees"`
}

var (
	defaultConfig = config{
		DebugLevel: defaultLogLevel,
		ConfigFile: defaultConfigFile,
		LogDir:     defaultLogDir,
		DBHostPort: defaultDBHostPort,
		DBUser:     defaultDBUser,
		DBPass:     defaultDBPass,
		DBName:     defaultDBName,
		DcrdCert:   defaultDaemonRPCCertFile,
	}
)

// cleanAndExpandPath expands environment variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Expand initial ~ to OS specific home directory.
	if strings.HasPrefix(path, "~") {
		homeDir := filepath.Dir(dcrdHomeDir)
		path = strings.Replace(path, "~", homeDir, 1)
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but they variables can still be expanded via POSIX-style
	// $VARIABLE.
	// So, replace any %VAR% with ${VAR}
	r := regexp.MustCompile(`%(?P<VAR>[^%/\\]*)%`)
	path = r.ReplaceAllString(path, "$${${VAR}}")
	return filepath.Clean(os.ExpandEnv(path))
}

// loadConfig initializes and parses the config using a config file and command
// line options.
func loadConfig() (*config, error) {
	loadConfigError := func(err error) (*config, error) {
		return nil, err
	}

	// Default config.
	cfg := defaultConfig

	// A config file in the current directory takes precedence.
	if _, err := os.Stat(defaultConfigFilename); !os.IsNotExist(err) {
		cfg.ConfigFile = defaultConfigFile
	}

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
	// appName := filepath.Base(os.Args[0])
	// appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	// if preCfg.ShowVersion {
	// 	fmt.Println(appName, "version", ver.String())
	// 	os.Exit(0)
	// }

	// Load additional config from file.
	var configFileError error
	parser := flags.NewParser(&cfg, flags.Default)
	err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return loadConfigError(err)
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return loadConfigError(err)
	}

	// Warn about missing config file after the final command line parse
	// succeeds.  This prevents the warning on help messages and invalid
	// options.
	if configFileError != nil {
		log.Printf("%v", configFileError)
		//fmt.Printf("%v\n",configFileError)
		return loadConfigError(configFileError)
	}

	// Choose the active network params based on the selected network.
	// Multiple networks can't be selected simultaneously.
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
		str := "%s: The testnet and simnet params can't be used " +
			"together -- choose one"
		err := fmt.Errorf(str, "loadConfig")
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Set the host names and ports to the default if the
	// user does not specify them.
	if cfg.DcrdServ == "" {
		cfg.DcrdServ = defaultHost + ":" + activeNet.JSONRPCClientPort
	}

	// Append the network type to the log directory so it is "namespaced"
	// per network.
	// cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	// cfg.LogDir = filepath.Join(cfg.LogDir, activeNet.Name)

	// Special show command to list supported subsystems and exit.
	// if cfg.DebugLevel == "show" {
	// 	fmt.Println("Supported subsystems", supportedSubsystems())
	// 	os.Exit(0)
	// }

	// Initialize logging at the default logging level.
	// initSeelogLogger(filepath.Join(cfg.LogDir, defaultLogFilename))
	// setLogLevels(defaultLogLevel)

	// Parse, validate, and set debug log level(s).
	if cfg.Quiet {
		cfg.DebugLevel = "error"
	}
	// if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
	// 	err = fmt.Errorf("%s: %v", "loadConfig", err.Error())
	// 	fmt.Fprintln(os.Stderr, err)
	// 	parser.WriteHelp(os.Stderr)
	// 	return loadConfigError(err)
	// }

	return &cfg, nil
}
