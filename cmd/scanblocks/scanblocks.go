package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/btcsuite/btclog"
    apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/decred/dcrrpcclient"
)

var host = flag.String("host", "127.0.0.1:9109", "node RPC host:port")
var user = flag.String("user", "dcrd", "node RPC username")
var pass = flag.String("pass", "bananas", "node RPC password")
var notls = flag.Bool("notls", true, "Disable use of TLS for node connection")

func mainCore() int {
	defer logFILE.Close()
	flag.Parse()

	client, _, err := rpcutils.ConnectNodeRPC(*host, *user, *pass, "", *notls)
	if err != nil {
		log.Fatalf("Unable to connect to RPC server: %v", err)
		return 1
	}

    infoResult, err := client.GetInfo()
    if err != nil {
		log.Errorf("GetInfo failed: %v", err)
		return 1
	}
    log.Info("Node connection count: ", infoResult.Connections)

    _, height, err := client.GetBestBlock()
    if err != nil {
        log.Error("GetBestBlock failed: ", err)
        return 2
    }

    for i := int64(0); i<height+1; i++ {
        blockhash, err := client.GetBlockHash(i)
        if err != nil {
            log.Errorf("GetBlockHash(%d) failed: %v", i, err)
            return 3
        }

        block, err := client.GetBlock(blockhash)
        if err != nil {
            log.Errorf("GetBlock failed (%s): %v", blockhash, err)
            return 4
        }

        header := block.MsgBlock().Header

        // apitypes.BlockDataBasic{
        //     Height: block.Height,
        //     Size: block.Size,
        //     Difficulty: header.SBits,
        //     StakeDiff: header.SBits,
        //     Time: block.Height,
        // }
    }

	return 0
}

func main() {
	os.Exit(mainCore())
}

func init() {
	err := InitLogger()
	if err != nil {
		fmt.Printf("Unable to start logger: %v", err)
		os.Exit(1)
	}
    btclogger, err := btclog.NewLoggerFromWriter(log.Writer(), btclog.InfoLvl)
	if err != nil {
		log.Error("Unable to create logger for dcrrpcclient: ", err)
	}
	dcrrpcclient.UseLogger(btclogger)

	rpcutils.UseLogger(btclogger)
}
