// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"time"

	"github.com/decred/dcrrpcclient"

	// "goji.io"
	// "goji.io/pat"

	// Some possible URL routers
	// "github.com/julienschmidt/httprouter"
	// "github.com/labstack/echo"
	// "github.com/husobee/vestigo"
	// "github.com/go-zoo/bone"
	// "github.com/dimfeld/httptreemux"
	//"github.com/pressly/chi"
	//"github.com/pressly/chi/docgen"
	//"github.com/pressly/chi/middleware"
)

// mainCore does all the work. Deferred functions do not run after os.Exit(),
// so main wraps this function, which returns a code.
func mainCore() int {
	// Parse the configuration file, and setup logger.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return 1
	}
	defer backendLog.Flush()

	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			log.Critical(err)
			return -1
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Start with version info
	log.Infof(appName+" version %s", ver.String())

	dcrrpcclient.UseLogger(clientLog)

	log.Debugf("Output folder: %v", cfg.OutFolder)
	log.Debugf("Log folder: %v", cfg.LogDir)

	// Create data output folder if it does not already exist
	if os.MkdirAll(cfg.OutFolder, 0750) != nil {
		fmt.Printf("Failed to create data output folder %s. Error: %s\n",
			cfg.OutFolder, err.Error())
		return 2
	}

	// Connect to dcrd RPC server using websockets

	// Set up the notification handler to deliver blocks through a channel.
	makeNtfnChans(cfg)

	// Daemon client connection
	dcrdClient, nodeVer, err := connectNodeRPC(cfg)
	if err != nil || dcrdClient == nil {
		log.Infof("Connection to dcrd failed: %v", err)
		return 4
	}

	// Display connected network
	curnet, err := dcrdClient.GetCurrentNet()
	if err != nil {
		fmt.Println("Unable to get current network from dcrd:", err.Error())
		return 5
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	cerr := registerNodeNtfnHandlers(dcrdClient)
	if cerr != nil {
		log.Errorf("RPC client error: %v (%v)", cerr.Error(), cerr.Cause())
		return 6
	}

	// Ctrl-C to shut down.
	// Nothing should be sent the quit channel.  It should only be closed.
	quit := make(chan struct{})
	// Only accept a single CTRL+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Start waiting for the interrupt signal
	go func() {
		<-c
		signal.Stop(c)
		// Close the channel so multiple goroutines can get the message
		log.Infof("CTRL+C hit.  Closing goroutines.")
		close(quit)
		return
	}()

	// STUFF HERE

	// But if monitoring is disabled, simulate an OS interrupt.
	if cfg.NoMonitor {
		c <- os.Interrupt
		time.Sleep(200)
	}

	// Closing these channels should be unnecessary if quit was handled right
	closeNtfnChans()

	if dcrdClient != nil {
		log.Infof("Closing connection to dcrd.")
		dcrdClient.Shutdown()
	}

	log.Infof("Bye!")
	time.Sleep(250 * time.Millisecond)

	return 0
}

func main() {
	os.Exit(mainCore())
}
