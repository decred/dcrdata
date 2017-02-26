package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strconv"

	"github.com/btcsuite/btclog"
	//"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	//"github.com/pkg/profile"
)

var host = flag.String("host", "127.0.0.1:9109", "node RPC host:port")
var user = flag.String("user", "dcrd", "node RPC username")
var pass = flag.String("pass", "bananas", "node RPC password")
var cert = flag.String("cert", "dcrd.cert", "node RPC TLS certificate (when notls=false)")
var notls = flag.Bool("notls", true, "Disable use of TLS for node connection")

var activeNetParams = &chaincfg.MainNetParams

func mainCore() int {
	//defer profile.Start(profile.CPUProfile).Stop()
	defer logFILE.Close()
	flag.Parse()

	client, _, err := rpcutils.ConnectNodeRPC(*host, *user, *pass, *cert, *notls)
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
	blocks := make(map[int64]*dcrutil.Block)

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

		blocks[i] = block

		// info, err := client.GetInfo()
		// if err != nil {
		// 	log.Errorf("GetInfo failed: %v", err)
		// 	return 5
		// }

		if i%500 == 0 {
			log.Infof("%d", block.Height())
		}

		header := block.MsgBlock().Header
		diffRatio := getDifficultyRatio(header.Bits)

		blockSummaries[i] = apitypes.BlockDataBasic{
			Height:     header.Height,
			Size:       header.Size,
			Hash:       blockhash.String(),
			Difficulty: diffRatio,
			StakeDiff:  dcrutil.Amount(header.SBits).ToCoin(),
			Time:       header.Timestamp.Unix(),
			PoolInfo: apitypes.TicketPoolInfo{
				Size: header.PoolSize,
			},
		}
	}

	log.Info("Building stake tree to compute pool values...")
	dbName := "ffldb_stake"
	stakeDB, poolValues, err := txhelpers.BuildStakeTree(blocks,
		activeNetParams, client, dbName)
	if err != nil {
		log.Errorf("Failed to create stake db: %v", err)
		return 8
	}
	defer os.RemoveAll(dbName)
	defer stakeDB.Close()

	log.Info("Extracting pool values...")
	for i := range blockSummaries {
		blockSummaries[i].PoolInfo.Value = dcrutil.Amount(poolValues[i]).ToCoin()
		if blockSummaries[i].PoolInfo.Size > 0 {
			blockSummaries[i].PoolInfo.ValAvg = blockSummaries[i].PoolInfo.Value / float64(blockSummaries[i].PoolInfo.Size)
		} else {
			blockSummaries[i].PoolInfo.ValAvg = 0
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

// getDifficultyRatio returns the proof-of-work difficulty as a multiple of the
// minimum difficulty using the passed bits field from the header of a block.
func getDifficultyRatio(bits uint32) float64 {
	// The minimum difficulty is the max possible proof-of-work limit bits
	// converted back to a number.  Note this is not the same as the proof of
	// work limit directly because the block difficulty is encoded in a block
	// with the compact form which loses precision.
	max := blockchain.CompactToBig(activeNetParams.PowLimitBits)
	target := blockchain.CompactToBig(bits)

	difficulty := new(big.Rat).SetFrac(max, target)
	outString := difficulty.FloatString(8)
	diff, err := strconv.ParseFloat(outString, 64)
	if err != nil {
		log.Errorf("Cannot get difficulty: %v", err)
		return 0
	}
	return diff
}
