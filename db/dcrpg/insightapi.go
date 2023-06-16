// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"sort"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"

	apitypes "github.com/decred/dcrdata/v8/api/types"
	"github.com/decred/dcrdata/v8/db/cache"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// GetTransactionByHash gets a wire.MsgTx for the specified transaction hash.
func (pgb *ChainDB) GetTransactionByHash(txid string) (*wire.MsgTx, error) {
	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, err
	}
	tx, err := pgb.Client.GetRawTransaction(pgb.ctx, txhash)
	if err != nil {
		return nil, err
	}
	return tx.MsgTx(), nil
}

// GetRawTransactionVerbose gets a chainjson.TxRawResult for the specified
// transaction hash.
func (pgb *ChainDB) GetRawTransactionVerbose(txid *chainhash.Hash) (*chainjson.TxRawResult, error) {
	txraw, err := pgb.Client.GetRawTransactionVerbose(pgb.ctx, txid)
	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %s", txid)
		return nil, err
	}
	return txraw, nil
}

// SendRawTransaction attempts to decode the input serialized transaction,
// passed as hex encoded string, and broadcast it, returning the tx hash.
func (pgb *ChainDB) SendRawTransaction(txhex string) (string, error) {
	msg, err := txhelpers.MsgTxFromHex(txhex)
	if err != nil {
		log.Errorf("SendRawTransaction failed: could not decode hex")
		return "", err
	}
	ctx, cancel := context.WithTimeout(pgb.ctx, 5*time.Second)
	defer cancel()
	hash, err := pgb.Client.SendRawTransaction(ctx, msg, true)
	if err != nil {
		log.Errorf("SendRawTransaction failed: %v", err)
		return "", err
	}
	return hash.String(), err
}

type txSortable struct {
	Hash chainhash.Hash
	Time int64
}

func sortTxsByTimeAndHash(txns []txSortable) {
	sort.Slice(txns, func(i, j int) bool {
		if txns[i].Time == txns[j].Time {
			return txns[i].Hash.String() < txns[j].Hash.String()
		}
		return txns[i].Time > txns[j].Time
	})
}

// InsightAddressTransactions performs DB queries to get all transaction hashes
// for the specified addresses in descending order by time, then ascending order
// by hash. It also returns a list of recently (defined as greater than
// recentBlockHeight) confirmed transactions that can be used to validate
// mempool status.
func (pgb *ChainDB) InsightAddressTransactions(addr []string, recentBlockHeight int64) (txs, recentTxs []chainhash.Hash, err error) {
	// Time of a "recent" block
	recentBlocktime, err0 := pgb.BlockTimeByHeight(recentBlockHeight)
	if err0 != nil {
		return nil, nil, err0
	}

	// Retrieve all merged address rows for these addresses.
	var txns []chainhash.Hash // []txSortable
	var numRecent int
	for i := range addr {
		rows, err := pgb.AddressRowsMerged(addr[i])
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			//txns = append(txns, txSortable{r.TxHash, r.TxBlockTime})
			txns = append(txns, r.TxHash)
			// Count the number of "recent" txns.
			if r.TxBlockTime > recentBlocktime {
				numRecent++
			}
		}
	}

	// Sort by block time (DESC) then hash (ASC).
	//sortTxsByTimeAndHash(txns)

	txs = make([]chainhash.Hash, 0, len(txns))
	recentTxs = make([]chainhash.Hash, 0, numRecent)

	for i := range txns {
		txs = append(txs, txns[i])
		if i < numRecent {
			recentTxs = append(recentTxs, txns[i])
		}
	}

	return
}

// AddressIDsByOutpoint fetches all address row IDs for a given outpoint
// (txHash:voutIndex).
func (pgb *ChainDB) AddressIDsByOutpoint(txHash string, voutIndex uint32) ([]uint64, []string, int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	ids, addrs, val, err := RetrieveAddressIDsByOutpoint(ctx, pgb.db, txHash, voutIndex)
	return ids, addrs, val, pgb.replaceCancelError(err)
}

// GetTransactionHex returns the full serialized transaction for the specified
// transaction hash as a hex encode string.
func (pgb *ChainDB) GetTransactionHex(txid *chainhash.Hash) string {
	tx, err := pgb.Client.GetRawTransaction(pgb.ctx, txid)
	if err != nil {
		log.Errorf("GetRawTransaction(%v) failed: %v", txid, err)
		return ""
	}
	txHex, err := txhelpers.MsgTxToHex(tx.MsgTx())
	if err != nil {
		log.Errorf("MsgTxToHex for tx %v: %v", txid, err)
		return ""
	}
	return txHex
}

// GetBlockVerboseByHash returns a *chainjson.GetBlockVerboseResult for the
// specified block hash, optionally with transaction details.
func (pgb *ChainDB) GetBlockVerboseByHash(hash string, verboseTx bool) *chainjson.GetBlockVerboseResult {
	blockhash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		log.Errorf("Invalid block hash %s", hash)
		return nil
	}

	blockVerbose, err := pgb.Client.GetBlockVerbose(context.TODO(), blockhash, verboseTx)
	if err != nil {
		log.Errorf("GetBlockVerbose(%v) failed: %v", hash, err)
		return nil
	}
	return blockVerbose
}

// GetTransactionsForBlockByHash returns a *apitypes.BlockTransactions for the
// block with the specified hash.
func (pgb *ChainDB) GetTransactionsForBlockByHash(hash string) *apitypes.BlockTransactions {
	blockVerbose := pgb.GetBlockVerboseByHash(hash, false)
	return makeBlockTransactions(blockVerbose)
}

func makeBlockTransactions(blockVerbose *chainjson.GetBlockVerboseResult) *apitypes.BlockTransactions {
	blockTransactions := new(apitypes.BlockTransactions)

	blockTransactions.Tx = make([]string, len(blockVerbose.Tx))
	copy(blockTransactions.Tx, blockVerbose.Tx)

	blockTransactions.STx = make([]string, len(blockVerbose.STx))
	copy(blockTransactions.STx, blockVerbose.STx)

	return blockTransactions
}

// GetBlockHash returns the hash of the block at the specified height. TODO:
// create GetBlockHashes to return all blocks at a given height.
func (pgb *ChainDB) GetBlockHash(idx int64) (string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	hash, err := RetrieveBlockHash(ctx, pgb.db, idx)
	if err != nil {
		log.Errorf("Unable to get block hash for block number %d: %v", idx, err)
		return "", pgb.replaceCancelError(err)
	}
	return hash, nil
}

// BlockSummaryTimeRange returns the blocks created within a specified time
// range (min, max time), up to limit transactions.
func (pgb *ChainDB) BlockSummaryTimeRange(min, max int64, limit int) ([]dbtypes.BlockDataBasic, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	blockSummary, err := RetrieveBlockSummaryByTimeRange(ctx, pgb.db, min, max, limit)
	return blockSummary, pgb.replaceCancelError(err)
}

// AddressUTXO returns the unspent transaction outputs (UTXOs) paying to the
// specified address in a []*dbtypes.AddressTxnOutput.
func (pgb *ChainDB) AddressUTXO(address string) ([]*dbtypes.AddressTxnOutput, bool, error) {
	_, err := stdaddr.DecodeAddress(address, pgb.chainParams)
	if err != nil {
		return nil, false, err
	}

	// Check the cache first.
	bestHash, height := pgb.BestBlock()
	utxos, validHeight := pgb.AddressCache.UTXOs(address)
	if utxos != nil && validHeight != nil /* validHeight.Hash == *bestHash */ {
		return utxos, false, nil
	}

	// Cache is empty or stale.

	// Make the pointed to AddressTxnOutput structs eligible for garbage
	// collection. pgb.AddressCache.StoreUTXOs sets a new slice retrieved from
	// the database, so we do not want to hang on to a copy of the old slice.
	var oldUTXOs bool
	if utxos != nil {
		//nolint:ineffassign
		utxos = nil
		oldUTXOs = true // flag to clear them from cache if we update them
	}

	busy, wait, done := pgb.CacheLocks.utxo.TryLock(address)
	if busy {
		// Let others get the wait channel while we wait.
		// To return stale cache data if it is available:
		// utxos, _ := pgb.AddressCache.UTXOs(address)
		// if utxos != nil {
		// 	return utxos, nil
		// }
		<-wait

		// Try again, starting with the cache.
		return pgb.AddressUTXO(address)
	}

	// We will run the DB query, so block others from doing the same. When query
	// and/or cache update is completed, broadcast to any waiters that the coast
	// is clear.
	defer done()

	// Prior to performing the query, clear the old UTXOs to save memory.
	if oldUTXOs {
		pgb.AddressCache.ClearUTXOs(address)
	}

	// Query the DB for the current UTXO set for this address.
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	txnOutputs, err := RetrieveAddressDbUTXOs(ctx, pgb.db, address)
	if err != nil {
		return nil, false, pgb.replaceCancelError(err)
	}

	// Update the address cache.
	cacheUpdated := pgb.AddressCache.StoreUTXOs(address, txnOutputs,
		cache.NewBlockID(bestHash, height))
	return txnOutputs, cacheUpdated, nil
}

// SpendDetailsForFundingTx will return the details of any spending transactions
// (tx, index, block height) for a given funding transaction.
func (pgb *ChainDB) SpendDetailsForFundingTx(fundHash string) ([]*apitypes.SpendByFundingHash, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	addrRow, err := RetrieveSpendingTxsByFundingTxWithBlockHeight(ctx, pgb.db, fundHash)
	if err != nil {
		return nil, pgb.replaceCancelError(err)
	}
	return addrRow, nil
}
