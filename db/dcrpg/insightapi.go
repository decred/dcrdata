// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"sort"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	apitypes "github.com/decred/dcrdata/api/types/v3"
	"github.com/decred/dcrdata/db/cache/v2"
	"github.com/decred/dcrdata/db/dbtypes/v2"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/txhelpers/v2"
)

// GetRawTransaction gets a dcrjson.TxRawResult for the specified transaction
// hash.
func (pgb *ChainDBRPC) GetRawTransaction(txid *chainhash.Hash) (*dcrjson.TxRawResult, error) {
	txraw, err := rpcutils.GetTransactionVerboseByID(pgb.Client, txid)
	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %s", txid)
		return nil, err
	}
	return txraw, nil
}

// GetBlockHeight returns the height of the block with the specified hash.
func (pgb *ChainDB) GetBlockHeight(hash string) (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	height, err := RetrieveBlockHeight(ctx, pgb.db, hash)
	if err != nil {
		log.Errorf("Unable to get block height for hash %s: %v", hash, err)
		return -1, pgb.replaceCancelError(err)
	}
	return height, nil
}

// SendRawTransaction attempts to decode the input serialized transaction,
// passed as hex encoded string, and broadcast it, returning the tx hash.
func (pgb *ChainDBRPC) SendRawTransaction(txhex string) (string, error) {
	msg, err := txhelpers.MsgTxFromHex(txhex)
	if err != nil {
		log.Errorf("SendRawTransaction failed: could not decode hex")
		return "", err
	}
	hash, err := pgb.Client.SendRawTransaction(msg, true)
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
// for the specified addresses in descending order by time. It also returns a
// list of recently (defined as greater than recentBlockHeight) confirmed
// transactions that can be used to validate mempool status.
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
// (txHash:voutIndex). TODO: Update the vin due to the issue with amountin
// invalid for unconfirmed txns.
func (pgb *ChainDB) AddressIDsByOutpoint(txHash string, voutIndex uint32) ([]uint64, []string, int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	ids, addrs, val, err := RetrieveAddressIDsByOutpoint(ctx, pgb.db, txHash, voutIndex)
	return ids, addrs, val, pgb.replaceCancelError(err)
} // Update Vin due to DCRD AMOUNTIN - END

// InsightSearchRPCAddressTransactions performs a searchrawtransactions for the
// specified address, max number of transactions, and offset into the transaction
// list. The search results are in reverse temporal order.
// TODO: Does this really need all the prev vout extra data?
func (pgb *ChainDBRPC) InsightSearchRPCAddressTransactions(addr string, count,
	skip int) []*dcrjson.SearchRawTransactionsResult {
	address, err := dcrutil.DecodeAddress(addr)
	if err != nil {
		log.Infof("Invalid address %s: %v", addr, err)
		return nil
	}
	prevVoutExtraData := true
	txs, err := pgb.Client.SearchRawTransactionsVerbose(
		address, skip, count, prevVoutExtraData, true, nil)

	if err != nil {
		log.Warnf("GetAddressTransactions failed for address %s: %v", addr, err)
		return nil
	}
	return txs
}

// GetTransactionHex returns the full serialized transaction for the specified
// transaction hash as a hex encode string.
func (pgb *ChainDBRPC) GetTransactionHex(txid *chainhash.Hash) string {
	txraw, err := rpcutils.GetTransactionVerboseByID(pgb.Client, txid)

	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %v", err)
		return ""
	}

	return txraw.Hex
}

// GetBlockVerboseByHash returns a *dcrjson.GetBlockVerboseResult for the
// specified block hash, optionally with transaction details.
func (pgb *ChainDBRPC) GetBlockVerboseByHash(hash string, verboseTx bool) *dcrjson.GetBlockVerboseResult {
	return rpcutils.GetBlockVerboseByHash(pgb.Client, hash, verboseTx)
}

// GetTransactionsForBlockByHash returns a *apitypes.BlockTransactions for the
// block with the specified hash.
func (pgb *ChainDBRPC) GetTransactionsForBlockByHash(hash string) *apitypes.BlockTransactions {
	blockVerbose := rpcutils.GetBlockVerboseByHash(pgb.Client, hash, false)

	return makeBlockTransactions(blockVerbose)
}

func makeBlockTransactions(blockVerbose *dcrjson.GetBlockVerboseResult) *apitypes.BlockTransactions {
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
	// Check the cache first.
	bestHash, height := pgb.BestBlock()
	utxos, validHeight := pgb.AddressCache.UTXOs(address)
	if utxos != nil && *bestHash == validHeight.Hash {
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
