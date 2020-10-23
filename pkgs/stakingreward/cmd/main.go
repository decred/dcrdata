package main

import (
	"context"

	"github.com/decred/dcrdata/blockdata/v5"
	"github.com/decred/dcrdata/rpcutils/v3"
	"github.com/decred/dcrdata/stakingreward"
	notify "github.com/decred/dcrdata/v5/notification"
)

func main() {

	ctx := withShutdownCancel(context.Background())
	cfg := stakingreward.Config{}

	notifier := notify.NewNotifier(ctx)
	_, _, err := rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, cfg.DisableDaemonTLS, true, notifier.DcrdHandlers())
	if err != nil {
		log.Fatal(err)
	}
	// init Router, templates, etc and pass to the Init function
	calc := stakingreward.Init()
	reorgBlockDataSavers := []blockdata.BlockDataSaver{calc}
	notifier.RegisterBlockHandlerGroup(reorgBlockDataSavers)
}

// shutdownRequest is used to initiate shutdown from one of the
// subsystems using the same code paths as when an interrupt signal is received.
var shutdownRequest = make(chan struct{})

// shutdownSignal is closed whenever shutdown is invoked through an interrupt
// signal or from an JSON-RPC stop request.  Any contexts created using
// withShutdownChannel are cancelled when this is closed.
var shutdownSignal = make(chan struct{})

// withShutdownCancel creates a copy of a context that is cancelled whenever
// shutdown is invoked through an interrupt signal or from an JSON-RPC stop
// request.
func withShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignal
		cancel()
	}()
	return ctx
}

// requestShutdown signals for starting the clean shutdown of the process
// through an internal component (such as through the JSON-RPC stop request).
func requestShutdown() {
	shutdownRequest <- struct{}{}
}
