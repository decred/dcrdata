package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/btcsuite/btclog"
	//"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

var host = flag.String("host", "127.0.0.1:9109", "node RPC host:port")
var user = flag.String("user", "dcrd", "node RPC username")
var pass = flag.String("pass", "bananas", "node RPC password")
var notls = flag.Bool("notls", true, "Disable use of TLS for node connection")
var nopoolval = flag.Bool("nopoolval", true, "Do not compute pool value (a slow operation)")

var activeNetParams = &chaincfg.MainNetParams

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

	blockSummaries := make([]apitypes.BlockDataBasic, height+1)

	for i := int64(0); i < height+1; i++ {
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

		info, err := client.GetInfo()
		if err != nil {
			log.Errorf("GetInfo failed: %v", err)
			return 5
		}

		if i%500 == 0 {
			log.Infof("%d", block.Height())
			continue
		}

		header := block.MsgBlock().Header

		blockSummaries[i] = apitypes.BlockDataBasic{
			Height:     header.Height,
			Size:       header.Size,
			Difficulty: info.Difficulty,
			StakeDiff:  dcrutil.Amount(header.SBits).ToCoin(),
			Time:       header.Timestamp.Unix(),
			PoolInfo: apitypes.TicketPoolInfo{
				Size: header.PoolSize,
			},
		}
	}

	// write
	fname := "fullscan.json"
	//fullfile := filepath.Join(folder, fname)
	fp, err := os.Create(fname)
	if err != nil {
		log.Errorf("Unable to open file %v for writing: %v", fname, err)
		return 6
	}
	defer fp.Close()

	if err := json.NewEncoder(fp).Encode(blockSummaries); err != nil {
		log.Printf("JSON encode error: %v", err)
		return 7
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
