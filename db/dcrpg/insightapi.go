// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"context"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	apitypes "github.com/decred/dcrdata/v4/api/types"
	"github.com/decred/dcrdata/v4/db/cache"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/rpcutils"
	"github.com/decred/dcrdata/v4/txhelpers"
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

// InsightAddressTransactions performs a db query to pull all txids for the
// specified addresses ordered desc by time. It also returns a list of recently
// (defined as greater than recentBlockHeight) confirmed transactions that can
// be used to validate mempool status.
func (pgb *ChainDB) InsightAddressTransactions(addr []string, recentBlockHeight int64) (txs, recentTxs []chainhash.Hash, err error) {
	recentBlocktime, err0 := pgb.BlockTimeByHeight(recentBlockHeight)
	if err0 != nil {
		return nil, nil, err0
	}

	// Try cache for each address first.
	for i := range addr {
		rows, _ := pgb.AddressCache.RowsMerged(addr[i])
		if rows == nil {
			txs = nil
			recentTxs = nil
			break
		}
		rows = cache.AllCreditAddressRows(rows)
		if len(rows) == 0 {
			continue
		}
		for _, r := range rows {
			tx, err := chainhash.NewHashFromStr(r.TxHash)
			if err != nil {
				return nil, nil, err
			}
			txs = append(txs, *tx)
			if r.TxBlockTime.UNIX() > recentBlocktime {
				recentTxs = append(recentTxs, *tx)
			}
		}
	}

	if txs != nil {
		// Cache hit for all listed addresses.
		return
	}

	// Perform the DB query in the absence of all cached data.
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	txs, recentTxs, err = RetrieveAddressTxnsOrdered(ctx, pgb.db, addr, recentBlocktime)
	err = pgb.replaceCancelError(err)
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
// specfied address, max number of transactions, and offset into the transaction
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
// specified address in a []apitypes.AddressTxnOutput.
func (pgb *ChainDB) AddressUTXO(address string) ([]apitypes.AddressTxnOutput, error) {
	// Check the cache first.
	bestHash, height := pgb.BestBlock()
	utxos, validHeight := pgb.AddressCache.UTXOs(address)
	if utxos != nil && *bestHash == validHeight.Hash {
		return utxos, nil
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
	} else {
		// We will run the DB query, so block others from doing the same. When
		// query and/or cache update is completed, broadcast to any waiters that
		// the coast is clear.
		defer done()
	}

	// Cache is empty or stale, so query the DB.
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	blockHeight := pgb.Height()
	txnOutput, err := RetrieveAddressUTXOs(ctx, pgb.db, address, blockHeight)
	if err != nil {
		return nil, pgb.replaceCancelError(err)
	}

	// Update the address cache.
	pgb.AddressCache.StoreUTXOs(address, txnOutput, cache.NewBlockID(bestHash, height))
	return txnOutput, nil
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
