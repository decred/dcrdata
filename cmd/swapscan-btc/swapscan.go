// Copyright (c) 2021, The Decred developers
// See LICENSE for details.

package main

import (
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/decred/dcrdata/cmd/swapscan-btc/swap"
)

var host = flag.String("host", "127.0.0.1:8332", "node RPC host:port")
var user = flag.String("user", "bitcoin", "node RPC username")
var pass = flag.String("pass", "bananas", "node RPC password")
var start = flag.Uint64("start", 590_000, "Start block height")
var end = flag.Uint64("end", 1<<32, "End block height")

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

	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         *host,
		User:         *user,
		Pass:         *pass,
	}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Unable to connect to RPC server: %v\n", err)
		return 1
	}
	defer client.Shutdown()

	infoResult, err := client.GetNetworkInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetInfo failed: %v\n", err)
		return 1
	}
	fmt.Printf("Node connection count: %d\n", infoResult.Connections)

	hash, err := client.GetBestBlockHash()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetBestBlockHash failed: %v\n", err)
		return 2
	}
	hdr, err := client.GetBlockHeaderVerbose(hash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetBestBlockHash failed: %v\n", err)
		return 2
	}
	height := hdr.Height
	if *end < uint64(height) {
		height = int32(*end)
	}

	params := &chaincfg.MainNetParams

	err = csvwriter.Write([]string{"height", "type", "spend_tx", "spend_vin", "BTC",
		"contract_tx", "contract_vout", "secret"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: csvwriter.Write failed: %v\n", err)
		return 6
	}

	var found bool
	// for i := int64(*start); i <= int64(height); i++ {
	for i := int64(height); i >= int64(*start); i-- {
		// blockhash, err := client.GetBlockHash(i)
		// if err != nil {
		// 	fmt.Fprintf(os.Stderr, "ERROR: GetBlockHash(%d) failed: %v\n", i, err)
		// 	return 3
		// }

		msgBlock, err := client.GetBlock(hash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: GetBlock failed (%s): %v\n", hash, err)
			return 4
		}
		hash = &msgBlock.Header.PrevBlock

		if i%1000 == 0 {
			fmt.Printf("%d\n", i)
		}

		// Check all regular tree txns except coinbase.
		for _, tx := range msgBlock.Transactions[1:] {
			swapRes, err := swap.MsgTxAtomicSwapsInfo(tx, nil, params)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v", err)
				return 5
			}
			if swapRes == nil || swapRes.Found == "" {
				continue
			}
			if !found {
				fmt.Printf("First swap at height %d\n", i)
				found = true
			}
			for vin, red := range swapRes.Redemptions {
				contractTx, err := client.GetRawTransaction(red.ContractTx)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: GetRawTransaction failed (%s): %v\n", red.ContractTx, err)
					return 4
				}
				red.Value = contractTx.MsgTx().TxOut[red.ContractVout].Value
				fmt.Fprintf(outw, "Redeem (%s:%d): %v from contract output %v:%d\n",
					red.SpendTx, red.SpendVin, btcutil.Amount(red.Value), red.ContractTx, red.ContractVout)

				// redeem tx, redeem vin, amt, contract tx, contract vout
				err = csvwriter.Write([]string{
					strconv.FormatInt(i, 10),
					"redeem",
					tx.TxHash().String(),
					strconv.FormatInt(int64(vin), 10),
					strconv.FormatFloat(btcutil.Amount(red.Value).ToBTC(), 'f', -1, 64),
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
				contractTx, err := client.GetRawTransaction(ref.ContractTx)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: GetRawTransaction failed (%s): %v\n", ref.ContractTx, err)
					return 4
				}
				ref.Value = contractTx.MsgTx().TxOut[ref.ContractVout].Value
				fmt.Fprintf(outw, "Refund (%s:%d): %v from contract output %v:%d\n",
					ref.SpendTx, ref.SpendVin, btcutil.Amount(ref.Value), ref.ContractTx, ref.ContractVout)

				err = csvwriter.Write([]string{
					strconv.FormatInt(i, 10),
					"refund",
					tx.TxHash().String(),
					strconv.FormatInt(int64(vin), 10),
					strconv.FormatFloat(btcutil.Amount(ref.Value).ToBTC(), 'f', -1, 64),
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
}
