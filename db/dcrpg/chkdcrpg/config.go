// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrdata/v8/netparams"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultConfigFilename = "chkdcrpg.conf"
	defaultLogLevel       = "info"
	defaultDataDirName    = "data"
	defaultLogDirname     = "logs"
)

var activeNet = &netparams.MainNetParams
var activeChain = chaincfg.MainNetParams()

var (
	dcrdHomeDir              = dcrutil.AppDataDir("dcrd", false)
	defaultDcrdHost          = "localhost"
	defaultDaemonRPCCertFile = filepath.Join(dcrdHomeDir, "rpc.cert")

	dcrdataHomeDir = dcrutil.AppDataDir("dcrdata", false)
	dcrdataDataDir = filepath.Join(dcrdataHomeDir, defaultDataDirName)

	defaultAppDirectory = dcrutil.AppDataDir("chkdcrpg", false)
	defaultConfigFile   = filepath.Join(defaultAppDirectory, defaultConfigFilename)
	defaultLogDir       = filepath.Join(defaultAppDirectory, defaultLogDirname)

	defaultDBHostPort = "127.0.0.1:5432"
	defaultDBUser     = "dcrdata"
	defaultDBPass     = ""
	defaultDBName     = "dcrdata"
)

type config struct {
	// General application behavior
	ConfigPath           string `short:"c" long:"config" description:"Path to a custom configuration file. (~/.chkdcrpg/rateserver.conf)" env:"CHKDCRPG_CONFIG_PATH"`
	AppDirectory         string `long:"appdir" description:"Path to application home directory. (~/.chkdcrpg)" env:"CHKDCRPG_APPDIR_PATH"`
	DcrdataDataDirectory string `long:"dcrdata-datadir" description:"Path to a dcrdata datadir" env:"DCRDATA_DATA_DIR"`
	LogPath              string `long:"logpath" description:"Directory to log output. ([appdir]/logs/)" env:"CHKDCRPG_LOG_PATH"`
	ShowVersion          bool   `short:"V" long:"version" description:"Display version information and exit"`
	TestNet              bool   `long:"testnet" description:"Use the test network (default mainnet)"`
	SimNet               bool   `long:"simnet" description:"Use the simulation test network (default mainnet)"`
	DebugLevel           string `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	Quiet                bool   `short:"q" long:"quiet" description:"Easy way to set debuglevel to error"`
	LogDir               string `long:"logdir" description:"Directory to log output"`
	HTTPProfile          bool   `long:"httpprof" short:"p" description:"Start HTTP profiler."`
	CPUProfile           string `long:"cpuprofile" description:"File for CPU profiling."`
	MemProfile           string `long:"memprofile" description:"File for memory profiling."`
	HidePGConfig         bool   `long:"hidepgconfig" description:"Blocks logging of the PostgreSQL db configuration on system start up."`

	// DB
	DBHostPort string `long:"dbhost" description:"DB host"`
	DBUser     string `long:"dbuser" description:"DB user"`
	DBPass     string `long:"dbpass" description:"DB pass"`
	DBName     string `long:"dbname" description:"DB name"`

	// RPC client options
	DcrdUser         string `long:"dcrduser" description:"Daemon RPC user name"`
	DcrdPass         string `long:"dcrdpass" description:"Daemon RPC password"`
	DcrdServ         string `long:"dcrdserv" description:"Hostname/IP and port of dcrd RPC server to connect to (default localhost:9109, testnet: localhost:19109, simnet: localhost:19556)"`
	DcrdCert         string `long:"dcrdcert" description:"File containing the dcrd certificate file"`
	DisableDaemonTLS bool   `long:"nodaemontls" description:"Disable TLS for the daemon RPC client -- NOTE: This is only allowed if the RPC client is connecting to localhost"`
}

var defaultConfig = config{
	AppDirectory: defaultAppDirectory,
	DebugLevel:   defaultLogLevel,
	ConfigPath:   defaultConfigFile,
	LogPath:      defaultLogDir,
	DBHostPort:   defaultDBHostPort,
	DBUser:       defaultDBUser,
	DBPass:       defaultDBPass,
	DBName:       defaultDBName,
	DcrdCert:     defaultDaemonRPCCertFile,
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

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s, %s-%s)\n", appName,
			ver.String(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	parser := flags.NewParser(&cfg, flags.Default)

	if preCfg.AppDirectory != defaultAppDirectory {
		preCfg.AppDirectory = cleanAndExpandPath(preCfg.AppDirectory)
	}

	defaultConfigPath := filepath.Join(preCfg.AppDirectory, defaultConfigFilename)
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
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "failed to stat configuration file at "+preCfg.ConfigPath)
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

	if cfg.LogPath == "" {
		cfg.LogPath = filepath.Join(cfg.AppDirectory, defaultLogDir)
	} else {
		cfg.LogPath = cleanAndExpandPath(cfg.LogPath)
	}

	// Choose the active network params based on the selected network. Multiple
	// networks can't be selected simultaneously.
	numNetsSet := 0
	// mainnet is set by default.
	if cfg.TestNet {
		activeNet = &netparams.TestNet3Params
		numNetsSet++
	}
	if cfg.SimNet {
		activeNet = &netparams.SimNetParams
		numNetsSet++
	}
	activeChain = activeNet.Params
	if numNetsSet > 1 {
		str := "%s: The testnet and simnet params can't be used " +
			"together -- choose one"
		err := fmt.Errorf(str, "loadConfig")
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return nil, err
	}

	if cfg.DcrdataDataDirectory == "" {
		cfg.DcrdataDataDirectory = dcrdataDataDir
	}
	cfg.DcrdataDataDirectory = filepath.Join(cfg.DcrdataDataDirectory, activeNet.Name)
	cfg.DcrdataDataDirectory = cleanAndExpandPath(cfg.DcrdataDataDirectory)

	// Set the host names and ports to the default if the user does not specify
	// them.
	if cfg.DcrdServ == "" {
		cfg.DcrdServ = defaultDcrdHost + ":" + activeNet.JSONRPCClientPort
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
