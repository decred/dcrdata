package main

import (
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/btcsuite/btclog"
	"github.com/dcrdata/dcrdata/dcrsqlite"
	"github.com/decred/dcrrpcclient"
	//"github.com/btcsuite/seelog"
)

func init() {
	err := InitLogger()
	if err != nil {
		fmt.Printf("Unable to start logger: %v", err)
		os.Exit(1)
	}
}

// var routes = flag.Bool("dbuser", "dcrdata", "DB user")
// var proto = flag.String("dbpass", "bananas", "DB pass")

func mainCore() int {
	// defer logFile.Close()
	// flag.Parse()

	// Parse the configuration file, and setup logger.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return 1
	}

	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			log.Fatal(err)
			return -1
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	btclogger, err := btclog.NewLoggerFromWriter(log.Writer(), btclog.InfoLvl)
	if err != nil {
		log.Error("Unable to create logger for dcrrpcclient: ", err)
	}
	dcrrpcclient.UseLogger(btclogger)

	db, err := dcrsqlite.InitDB(&dcrsqlite.DBInfo{cfg.DBFileName})
	if err != nil {
		log.Fatalf("InitDB failed: %v", err)
		return 1
	}
	
	log.Infof("sqlite db successfully opened: %s", cfg.DBFileName)
	defer db.Close()

	return 0
}

func main() {
	os.Exit(mainCore())
}
