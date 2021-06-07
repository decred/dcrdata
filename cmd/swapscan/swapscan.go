// Copyright (c) 2021, The Decred developers
// See LICENSE for details.

package main

import (
	"context"
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/rpcclient/v6"

	"github.com/decred/dcrdata/v6/rpcutils"
	"github.com/decred/dcrdata/v6/txhelpers"
	"github.com/decred/slog"
)

var host = flag.String("host", "127.0.0.1:9109", "node RPC host:port")
var user = flag.String("user", "dcrd", "node RPC username")
var pass = flag.String("pass", "bananas", "node RPC password")
var cert = flag.String("cert", "dcrd.cert", "node RPC TLS certificate (when notls=false)")
var notls = flag.Bool("notls", false, "Disable use of TLS for node connection")
var start = flag.Uint64("start", 491_705, "Start block height")
var end = flag.Uint64("end", 1<<32, "End block height")

var (
	activeNetParams = chaincfg.MainNetParams()

	backendLog      *slog.Backend
	rpcclientLogger slog.Logger
)

func mainCore() int {
	//defer profile.Start(profile.CPUProfile).Stop()
	defer func() {
		logFILE.Close()
		os.Stdout.Sync()
	}()
	flag.Parse()

	csvfile, err := os.Create("swapscan.csv")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating utxos file: %v\n", err)
		return 1
	}
	defer csvfile.Close()

	csvwriter := csv.NewWriter(csvfile)
	defer csvwriter.Flush()

	client, _, err := rpcutils.ConnectNodeRPC(*host, *user, *pass, *cert, *notls, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Unable to connect to RPC server: %v\n", err)
		return 1
	}

	infoResult, err := client.GetInfo(context.TODO())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetInfo failed: %v\n", err)
		return 1
	}
	fmt.Printf("Node connection count: %d\n", infoResult.Connections)

	_, height, err := client.GetBestBlock(context.TODO())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetBestBlock failed: %v\n", err)
		return 2
	}
	if *end != 1<<32 {
		height = int64(*end)
	}

	params := chaincfg.MainNetParams()

	err = csvwriter.Write([]string{"height", "type", "spend_tx", "spend_vin", "DCR",
		"contract_tx", "contract_vout", "secret"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "csvwriter.Write: %s\n", err)
		return 6
	}

	var found bool
	for i := int64(*start); i <= height; i++ {
		blockhash, err := client.GetBlockHash(context.TODO(), i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: GetBlockHash(%d) failed: %v\n", i, err)
			return 3
		}

		msgBlock, err := client.GetBlock(context.TODO(), blockhash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: GetBlock failed (%s): %v\n", blockhash, err)
			return 4
		}

		if i%1000 == 0 {
			fmt.Printf("%d\n", msgBlock.Header.Height)
		}

		// Check all regular tree txns except coinbase.
		for _, tx := range msgBlock.Transactions[1:] {
			swapRes, err := txhelpers.MsgTxAtomicSwapsInfo(tx, nil, params, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v", err)
				return 5
			}
			if swapRes.Found == "" {
				continue
			}
			if !found {
				fmt.Printf("First swap at height %d\n", i)
				found = true
			}
			for vin, red := range swapRes.Redemptions {
				fmt.Fprintf(outw, "Redeem (%s:%d): %v from contract output %v:%d\n",
					red.SpendTx, red.SpendVin, dcrutil.Amount(red.Value), red.ContractTx, red.ContractVout)

				// redeem tx, redeem vin, amt, contract tx, contract vout
				err = csvwriter.Write([]string{
					strconv.FormatInt(i, 10),
					"redeem",
					tx.TxHash().String(),
					strconv.FormatInt(int64(vin), 10),
					strconv.FormatFloat(dcrutil.Amount(red.Value).ToCoin(), 'f', -1, 64),
					red.ContractTx.String(),
					strconv.FormatInt(int64(red.ContractVout), 10),
					hex.EncodeToString(red.Secret),
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "csvwriter.Write: %s\n", err)
					return 6
				}
			}
			for vin, ref := range swapRes.Refunds {
				fmt.Fprintf(outw, "Refund (%s:%d): %v from contract output %v:%d\n",
					ref.SpendTx, ref.SpendVin, dcrutil.Amount(ref.Value), ref.ContractTx, ref.ContractVout)

				err = csvwriter.Write([]string{
					strconv.FormatInt(i, 10),
					"refund",
					tx.TxHash().String(),
					strconv.FormatInt(int64(vin), 10),
					strconv.FormatFloat(dcrutil.Amount(ref.Value).ToCoin(), 'f', -1, 64),
					ref.ContractTx.String(),
					strconv.FormatInt(int64(ref.ContractVout), 10),
					"", // no secret with a refund
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "csvwriter.Write: %s\n", err)
					return 6
				}
			}
		}
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

	backendLog = slog.NewBackend(os.Stderr)
	rpcclientLogger = backendLog.Logger("RPC")
	rpcclient.UseLogger(rpcclientLogger)
	rpcutils.UseLogger(rpcclientLogger)
}
