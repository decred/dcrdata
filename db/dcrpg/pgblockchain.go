// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/explorer"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	humanize "github.com/dustin/go-humanize"
)

var (
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())
)

// ChainDB provides an interface for storing and manipulating extracted
// blockchain data in a PostgreSQL database.
type ChainDB struct {
	db            *sql.DB
	chainParams   *chaincfg.Params
	devAddress    string
	dupChecks     bool
	bestBlock     int64
	lastBlock     map[chainhash.Hash]uint64
	addressCounts *addressCounter
}

type addressCounter struct {
	sync.Mutex
	validHeight int64
	balance     map[string]explorer.AddressBalance
}

func makeAddressCounter() *addressCounter {
	return &addressCounter{
		validHeight: 0,
		balance:     make(map[string]explorer.AddressBalance),
	}
}

// DBInfo holds the PostgreSQL database connection information.
type DBInfo struct {
	Host, Port, User, Pass, DBName string
}

// NewChainDB constructs a ChainDB for the given connection and Decred network
// parameters. By default, duplicate row checks on insertion are enabled.
func NewChainDB(dbi *DBInfo, params *chaincfg.Params) (*ChainDB, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB
	db, err := Connect(dbi.Host, dbi.Port, dbi.User, dbi.Pass, dbi.DBName)
	if err != nil {
		return nil, err
	}
	// Attempt to get DB best block height from tables, but if the tables are
	// empty or not yet created, it is not an error.
	bestHeight, _, _, err := RetrieveBestBlockHeight(db)
	if err != nil && !(err == sql.ErrNoRows ||
		strings.HasSuffix(err.Error(), "does not exist")) {
		return nil, err
	}
	_, devSubsidyAddress, _, err := txscript.ExtractPkScriptAddrs(
		params.OrganizationPkScriptVersion, params.OrganizationPkScript, params)
	if err != nil || len(devSubsidyAddress) != 1 {
		return nil, fmt.Errorf("Failed to decode dev subsidy address: %v", err)
	}
	return &ChainDB{
		db:            db,
		chainParams:   params,
		devAddress:    devSubsidyAddress[0].String(),
		dupChecks:     true,
		bestBlock:     int64(bestHeight),
		lastBlock:     make(map[chainhash.Hash]uint64),
		addressCounts: makeAddressCounter(),
	}, nil
}

// Close closes the underlying sql.DB connection to the database.
func (pgb *ChainDB) Close() error {
	return pgb.db.Close()
}

// EnableDuplicateCheckOnInsert specifies whether SQL insertions should check
// for row conflicts (duplicates), and avoid adding or updating.
func (pgb *ChainDB) EnableDuplicateCheckOnInsert(dupCheck bool) {
	pgb.dupChecks = dupCheck
}

// SetupTables creates the required tables and type, and prints table versions
// stored in the table comments when debug level logging is enabled.
func (pgb *ChainDB) SetupTables() error {
	if err := CreateTypes(pgb.db); err != nil {
		return err
	}

	if err := CreateTables(pgb.db); err != nil {
		return err
	}

	vers := TableVersions(pgb.db)
	for tab, ver := range vers {
		log.Debugf("Table %s: v%d", tab, ver)
	}
	return nil
}

// DropTables drops (deletes) all of the known dcrdata tables.
func (pgb *ChainDB) DropTables() {
	DropTables(pgb.db)
}

// HeightDB queries the DB for the best block height.
func (pgb *ChainDB) HeightDB() (uint64, error) {
	bestHeight, _, _, err := RetrieveBestBlockHeight(pgb.db)
	return bestHeight, err
}

// Height uses the last stored height.
func (pgb *ChainDB) Height() uint64 {
	return uint64(pgb.bestBlock)
}

// SpendingTransactions retrieves all transactions spending outpoints from the
// specified funding transaction. The spending transaction hashes, the spending
// tx input indexes, and the corresponding funding tx output indexes, and an
// error value are returned.
func (pgb *ChainDB) SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error) {
	_, spendingTxns, vinInds, voutInds, err := RetrieveSpendingTxsByFundingTx(pgb.db, fundingTxID)
	return spendingTxns, vinInds, voutInds, err
}

// SpendingTransaction returns the transaction that spends the specified
// transaction outpoint, if it is spent. The spending transaction hash, input
// index, tx tree, and an error value are returned.
func (pgb *ChainDB) SpendingTransaction(fundingTxID string,
	fundingTxVout uint32) (string, uint32, int8, error) {
	_, spendingTx, vinInd, tree, err := RetrieveSpendingTxByTxOut(pgb.db, fundingTxID, fundingTxVout)
	return spendingTx, vinInd, tree, err
}

// BlockTransactions retrieves all transactions in the specified block, their
// indexes in the block, their tree, and an error value.
func (pgb *ChainDB) BlockTransactions(blockHash string) ([]string, []uint32, []int8, error) {
	_, blockTransactions, blockInds, trees, err := RetrieveTxsByBlockHash(pgb.db, blockHash)
	return blockTransactions, blockInds, trees, err
}

// VoutValue retrieves the value of the specified transaction outpoint in atoms.
func (pgb *ChainDB) VoutValue(txID string, vout uint32) (uint64, error) {
	// txDbID, _, _, err := RetrieveTxByHash(pgb.db, txID)
	// if err != nil {
	// 	return 0, fmt.Errorf("RetrieveTxByHash: %v", err)
	// }
	voutValue, err := RetrieveVoutValue(pgb.db, txID, vout)
	if err != nil {
		return 0, fmt.Errorf("RetrieveVoutValue: %v", err)
	}
	return voutValue, nil
}

// VoutValues retrieves the values of each outpoint of the specified
// transaction. The corresponding indexes in the block and tx trees of the
// outpoints, and an error value are also returned.
func (pgb *ChainDB) VoutValues(txID string) ([]uint64, []uint32, []int8, error) {
	// txDbID, _, _, err := RetrieveTxByHash(pgb.db, txID)
	// if err != nil {
	// 	return nil, fmt.Errorf("RetrieveTxByHash: %v", err)
	// }
	voutValues, txInds, txTrees, err := RetrieveVoutValues(pgb.db, txID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("RetrieveVoutValues: %v", err)
	}
	return voutValues, txInds, txTrees, nil
}

// TransactionBlock retrieves the hash of the block containing the specified
// transaction. The index of the transaction within the block, the transaction
// index, and an error value are also returned.
func (pgb *ChainDB) TransactionBlock(txID string) (string, uint32, int8, error) {
	_, blockHash, blockInd, tree, err := RetrieveTxByHash(pgb.db, txID)
	return blockHash, blockInd, tree, err
}

// AddressHistory queries the database for all rows of the addresses table for
// the given address.
func (pgb *ChainDB) AddressHistory(address string, N, offset int64) ([]*dbtypes.AddressRow, *explorer.AddressBalance, error) {

	bb, err := pgb.HeightDB()
	if err != nil {
		return nil, nil, err
	}
	bestBlock := int64(bb)

	pgb.addressCounts.Lock()
	defer pgb.addressCounts.Unlock()

	// See if address count cache includes a fresh count for this address.
	var balanceInfo explorer.AddressBalance
	var fresh bool
	if pgb.addressCounts.validHeight == bestBlock {
		balanceInfo, fresh = pgb.addressCounts.balance[address]
	} else {
		// StoreBlock should do this, but the idea is to clear the old cached
		// results when a new block is encountered.
		log.Warnf("Address receive counter stale, at block %d when best is %d.",
			pgb.addressCounts.validHeight, bestBlock)
		pgb.addressCounts.balance = make(map[string]explorer.AddressBalance)
		pgb.addressCounts.validHeight = bestBlock
	}

	// The organization address occurs very frequently, so use the regular (non
	// sub-query) select as it is much more efficient.
	var addressRows []*dbtypes.AddressRow
	if address == pgb.devAddress {
		_, addressRows, err = RetrieveAddressTxnsAlt(pgb.db, address, N, offset)
	} else {
		_, addressRows, err = RetrieveAddressTxns(pgb.db, address, N, offset)
	}
	if err != nil {
		return nil, &balanceInfo, err
	}

	// If the address receive count was not cached, store it in the cache if it
	// is worth storing (when the length of the short list returned above is no
	// less than the query limit).
	if !fresh {
		addrInfo := explorer.ReduceAddressHistory(addressRows)
		if addrInfo == nil {
			return addressRows, nil, fmt.Errorf("ReduceAddressHistory failed")
		}

		if addrInfo.NumFundingTxns < N {
			balanceInfo = explorer.AddressBalance{
				Address:      address,
				NumSpent:     addrInfo.NumSpendingTxns,
				NumUnspent:   addrInfo.NumFundingTxns - addrInfo.NumSpendingTxns,
				TotalSpent:   int64(addrInfo.TotalSent),
				TotalUnspent: int64(addrInfo.Unspent),
			}
		} else {
			var numSpent, numUnspent, totalSpent, totalUnspent int64
			numSpent, numUnspent, totalSpent, totalUnspent, err =
				RetrieveAddressSpentUnspent(pgb.db, address)
			if err != nil {
				return nil, nil, err
			}
			balanceInfo = explorer.AddressBalance{
				Address:      address,
				NumSpent:     numSpent,
				NumUnspent:   numUnspent,
				TotalSpent:   totalSpent,
				TotalUnspent: totalUnspent,
			}
		}

		log.Infof("%s: %d spent totalling %f DCR, %d unspent totalling %f DCR",
			address, balanceInfo.NumSpent, dcrutil.Amount(balanceInfo.TotalSpent).ToCoin(),
			balanceInfo.NumUnspent, dcrutil.Amount(balanceInfo.TotalUnspent).ToCoin())
		log.Infof("Caching address receive count for address %s: "+
			"count = %d at block %d.", address,
			balanceInfo.NumSpent+balanceInfo.NumUnspent, bestBlock)
		pgb.addressCounts.balance[address] = balanceInfo
	}

	return addressRows, &balanceInfo, err
}

// FillAddressTransactions is used to fill out the transaction details in an
// explorer.AddressInfo generated by explorer.ReduceAddressHistory, usually from
// the output of AddressHistory. This function also sets the number of
// unconfirmed transactions for the current best block in the database.
func (pgb *ChainDB) FillAddressTransactions(addrInfo *explorer.AddressInfo) error {
	if addrInfo == nil {
		return nil
	}

	var numUnconfirmed int64

	for _, txn := range addrInfo.Transactions {
		_, dbTx, err := RetrieveDbTxByHash(pgb.db, txn.TxID)
		if err != nil {
			return err
		}
		txn.TxID = dbTx.TxID
		txn.FormattedSize = humanize.Bytes(uint64(dbTx.Size))
		txn.Total = dcrutil.Amount(dbTx.Sent).ToCoin()
		txn.Time = dbTx.BlockTime
		if dbTx.BlockTime > 0 {
			txn.Confirmations = pgb.Height() - uint64(dbTx.BlockHeight) + 1
		} else {
			numUnconfirmed++
			txn.Confirmations = 0
		}
		txn.FormattedTime = time.Unix(dbTx.BlockTime, 0).Format("1/2/06 15:04:05")
	}

	addrInfo.NumUnconfirmed = numUnconfirmed

	return nil
}

// Store satisfies BlockDataSaver
func (pgb *ChainDB) Store(_ *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	if pgb == nil {
		return nil
	}
	_, _, err := pgb.StoreBlock(msgBlock, true, true)
	return err
}

// DeindexAll drops all of the indexes in all tables
func (pgb *ChainDB) DeindexAll() error {
	var err, errAny error
	if err = DeindexBlockTableOnHash(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexTransactionTableOnHashes(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexTransactionTableOnBlockIn(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexVinTableOnVins(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexVinTableOnPrevOuts(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexVoutTableOnTxHashIdx(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexVoutTableOnTxHash(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexAddressTableOnAddress(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexAddressTableOnVoutID(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err = DeindexAddressTableOnTxHash(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	return errAny
}

// IndexAll creates all of the indexes in all tables
func (pgb *ChainDB) IndexAll() error {
	log.Infof("Indexing blocks table...")
	if err := IndexBlockTableOnHash(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing transactions table on tx/block hashes...")
	if err := IndexTransactionTableOnHashes(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing transactions table on block id/indx...")
	if err := IndexTransactionTableOnBlockIn(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vins table on txin...")
	if err := IndexVinTableOnVins(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vins table on prevouts...")
	if err := IndexVinTableOnPrevOuts(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vouts table on tx hash and index...")
	if err := IndexVoutTableOnTxHashIdx(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vouts table on tx hash...")
	if err := IndexVoutTableOnTxHash(pgb.db); err != nil {
		return err
	}
	// Not indexing the address table on vout ID or address here. See
	// IndexAddressTable to create those indexes.
	log.Infof("Indexing addresses table on funding tx hash...")
	return IndexAddressTableOnTxHash(pgb.db)
}

// IndexAddressTable creates the indexes on the address table on the vout ID and
// address columns, separately.
func (pgb *ChainDB) IndexAddressTable() error {
	log.Infof("Indexing addresses table on address...")
	if err := IndexAddressTableOnAddress(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing addresses table on vout Db ID...")
	return IndexAddressTableOnVoutID(pgb.db)
}

// DeindexAddressTable drops the vin ID and address column indexes for the
// address table.
func (pgb *ChainDB) DeindexAddressTable() error {
	var errAny error
	if err := DeindexAddressTableOnAddress(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	if err := DeindexAddressTableOnVoutID(pgb.db); err != nil {
		log.Warn(err)
		errAny = err
	}
	return errAny
}

// StoreBlock processes the input wire.MsgBlock, and saves to the data tables.
// The number of vins, and vouts stored are also returned.
func (pgb *ChainDB) StoreBlock(msgBlock *wire.MsgBlock, isValid,
	updateAddressesSpendingInfo bool) (numVins int64, numVouts int64, err error) {
	// Convert the wire.MsgBlock to a dbtypes.Block
	dbBlock := dbtypes.MsgBlockToDBBlock(msgBlock, pgb.chainParams)

	// Extract transactions and their vouts, and insert vouts into their pg table,
	// returning their DB PKs, which are stored in the corresponding transaction
	// data struct. Insert each transaction once they are updated with their
	// vouts' IDs, returning the transaction PK ID, which are stored in the
	// containing block data struct.

	// regular transactions
	resChanReg := make(chan storeTxnsResult)
	go func() {
		resChanReg <- pgb.storeTxns(msgBlock, wire.TxTreeRegular,
			pgb.chainParams, &dbBlock.TxDbIDs, updateAddressesSpendingInfo)
	}()

	// stake transactions
	resChanStake := make(chan storeTxnsResult)
	go func() {
		resChanStake <- pgb.storeTxns(msgBlock, wire.TxTreeStake,
			pgb.chainParams, &dbBlock.STxDbIDs, updateAddressesSpendingInfo)
	}()

	errReg := <-resChanReg
	errStk := <-resChanStake
	if errStk.err != nil {
		if errReg.err == nil {
			err = errStk.err
			numVins = errReg.numVins
			numVouts = errReg.numVouts
			return
		}
		err = errors.New(errReg.Error() + ", " + errStk.Error())
		return
	} else if errReg.err != nil {
		err = errReg.err
		numVins = errStk.numVins
		numVouts = errStk.numVouts
		return
	}

	numVins = errStk.numVins + errReg.numVins
	numVouts = errStk.numVouts + errReg.numVouts

	// Store the block now that it has all it's transaction PK IDs
	var blockDbID uint64
	blockDbID, err = InsertBlock(pgb.db, dbBlock, isValid, pgb.dupChecks)
	if err != nil {
		log.Error("InsertBlock:", err)
		return
	}
	pgb.lastBlock[msgBlock.BlockHash()] = blockDbID

	pgb.bestBlock = int64(dbBlock.Height)

	err = InsertBlockPrevNext(pgb.db, blockDbID, dbBlock.Hash,
		dbBlock.PreviousHash, "")
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertBlockPrevNext:", err)
		return
	}

	// Update last block in db with this block's hash as it's next. Also update
	// isValid flag in last block if votes in this block invalidated it.
	lastBlockHash := msgBlock.Header.PrevBlock
	lastBlockDbID, ok := pgb.lastBlock[lastBlockHash]
	if ok {
		lastIsValid := dbBlock.VoteBits&1 != 0
		if !lastIsValid {
			log.Infof("Setting last block %s as INVALID", lastBlockHash)
			err = UpdateLastBlock(pgb.db, lastBlockDbID, lastIsValid)
			if err != nil {
				log.Error("UpdateLastBlock:", err)
				return
			}
		}
		err = UpdateBlockNext(pgb.db, lastBlockDbID, dbBlock.Hash)
		if err != nil {
			log.Error("UpdateBlockNext:", err)
			return
		}
	}

	pgb.addressCounts.Lock()
	pgb.addressCounts.validHeight = int64(msgBlock.Header.Height)
	pgb.addressCounts.balance = map[string]explorer.AddressBalance{}
	pgb.addressCounts.Unlock()

	return
}

// storeTxnsResult is the type of object sent back from the goroutines wrapping
// storeTxns in StoreBlock.
type storeTxnsResult struct {
	numVins, numVouts, numAddresses int64
	err                             error
}

func (r *storeTxnsResult) Error() string {
	return r.err.Error()
}

func (pgb *ChainDB) storeTxns(msgBlock *wire.MsgBlock, txTree int8,
	chainParams *chaincfg.Params, TxDbIDs *[]uint64,
	updateAddressesSpendingInfo bool) storeTxnsResult {
	dbTransactions, dbTxVouts, dbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock, txTree, chainParams)

	var txRes storeTxnsResult
	dbAddressRows := make([][]dbtypes.AddressRow, len(dbTransactions))
	var totalAddressRows int

	var err error
	for it, dbtx := range dbTransactions {
		dbtx.VoutDbIds, dbAddressRows[it], err = InsertVouts(pgb.db, dbTxVouts[it], pgb.dupChecks)
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVouts:", err)
			txRes.err = err
			return txRes
		}
		totalAddressRows += len(dbAddressRows[it])
		txRes.numVouts += int64(len(dbtx.VoutDbIds))
		if err == sql.ErrNoRows || len(dbTxVouts[it]) != len(dbtx.VoutDbIds) {
			log.Warnf("Incomplete Vout insert.")
		}

		dbtx.VinDbIds, err = InsertVins(pgb.db, dbTxVins[it])
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVins:", err)
			txRes.err = err
			return txRes
		}
		txRes.numVins += int64(len(dbtx.VinDbIds))
	}

	// Get the tx PK IDs for storage in the blocks table
	*TxDbIDs, err = InsertTxns(pgb.db, dbTransactions, pgb.dupChecks)
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertTxns:", err)
		txRes.err = err
		return txRes
	}

	// Store tx Db IDs as funding tx in AddressRows and rearrange
	dbAddressRowsFlat := make([]*dbtypes.AddressRow, 0, totalAddressRows)
	for it, txDbID := range *TxDbIDs {
		// Set the tx ID of the funding transactions
		for iv := range dbAddressRows[it] {
			// Transaction that pays to the address
			dba := &dbAddressRows[it][iv]
			dba.FundingTxDbID = txDbID
			// Funding tx hash, vout id, value, and address are already assigned
			// by InsertVouts. Only the funding tx DB ID was needed.
			dbAddressRowsFlat = append(dbAddressRowsFlat, dba)
		}
	}

	// Insert each new AddressRow, absent spending fields
	_, err = InsertAddressOuts(pgb.db, dbAddressRowsFlat)
	if err != nil {
		log.Error("InsertAddressOuts:", err)
		txRes.err = err
		return txRes
	}

	if !updateAddressesSpendingInfo {
		return txRes
	}

	// Check the new vins and update spending tx data in Addresses table
	for it, txDbID := range *TxDbIDs {
		for iv := range dbTxVins[it] {
			// Transaction that spends an outpoint paying to >=0 addresses
			vin := &dbTxVins[it][iv]
			// Get the tx hash and vout index (previous output) from vins table
			// vinDbID, txHash, txIndex, _, err := RetrieveFundingOutpointByTxIn(
			// 	pgb.db, vin.TxID, vin.TxIndex)
			vinDbID := dbTransactions[it].VinDbIds[iv]

			// Single transaction to get funding tx info for the vin, get
			// address row index for the funding tx, and set spending info.
			// var numAddressRowsSet int64
			// numAddressRowsSet, err = SetSpendingByVinID(pgb.db, vinDbID, txDbID, vin.TxID, vin.TxIndex)
			// if err != nil {
			// 	log.Errorf("SetSpendingByVinID: %v", err)
			// }
			// txRes.numAddresses += numAddressRowsSet

			// prevout, ok := pgb.vinPrevOutpoints[vinDbID]
			// if !ok {
			// 	log.Errorf("No funding tx info found for vin %s:%d (prev %s)",
			// 		vin.TxID, vin.TxIndex, vin.PrevOut)
			// 	continue
			// }
			// delete(pgb.vinPrevOutpoints, vinDbID)

			// skip coinbase inputs
			if bytes.Equal(zeroHashStringBytes, []byte(vin.PrevTxHash)) {
				continue
			}

			var numAddressRowsSet int64
			numAddressRowsSet, err = SetSpendingForFundingOP(pgb.db,
				vin.PrevTxHash, vin.PrevTxIndex, // funding
				txDbID, vin.TxID, vin.TxIndex, vinDbID) // spending
			if err != nil {
				log.Errorf("SetSpendingForFundingOP: %v", err)
			}
			txRes.numAddresses += numAddressRowsSet

			/* separate transactions
			txHash, txIndex, _, err := RetrieveFundingOutpointByVinID(pgb.db, vinDbID)
			if err != nil && err != sql.ErrNoRows {
				if err == sql.ErrNoRows {
					log.Warnf("No funding transaction found for input %s:%d", vin.TxID, vin.TxIndex)
					continue
				}
				log.Error("RetrieveFundingOutpointByTxIn:", err)
				continue
			}

			// skip coinbase inputs
			if bytes.Equal(zeroHashStringBytes, []byte(txHash)) {
				continue
			}

			var numAddressRowsSet int64
			numAddressRowsSet, err = SetSpendingForFundingOP(pgb.db, txHash, txIndex, // funding
				txDbID, vin.TxID, vin.TxIndex, vinDbID) // spending
			if err != nil {
				log.Errorf("SetSpendingForFundingOP: %v", err)
			}
			txRes.numAddresses += numAddressRowsSet
			*/
		}
	}

	return txRes
}

// UpdateSpendingInfoInAllAddresses completely rebuilds the spending transaction
// info columns of the address table. This is intended to be use after syncing
// all other tables and creating their indexes, particularly the indexes on the
// vins table, and the addresses table index on the funding tx columns. This can
// be used instead of using updateAddressesSpendingInfo=true with storeTxns,
// which will update these addresses table columns too, but much more slowly for
// a number of reasons (that are well worth investigating BTW!).
func (pgb *ChainDB) UpdateSpendingInfoInAllAddresses() (int64, error) {
	// Get the full list of vinDbIDs
	allVinDbIDs, err := RetrieveAllVinDbIDs(pgb.db)
	if err != nil {
		log.Errorf("RetrieveAllVinDbIDs: %v", err)
		return 0, err
	}

	log.Infof("Updating spending tx info for %d addresses...", len(allVinDbIDs))
	var numAddresses int64
	for i := 0; i < len(allVinDbIDs); i += 1000 {
		//for i, vinDbID := range allVinDbIDs {
		if i%250000 == 0 {
			endRange := i + 250000 - 1
			if endRange > len(allVinDbIDs) {
				endRange = len(allVinDbIDs)
			}
			log.Infof("Updating from vins %d to %d...", i, endRange)
		}

		/*var numAddressRowsSet int64
		numAddressRowsSet, err = SetSpendingForVinDbID(pgb.db, vinDbID)
		if err != nil {
			log.Errorf("SetSpendingForFundingOP: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet*/
		var numAddressRowsSet int64
		endChunk := i + 1000
		if endChunk > len(allVinDbIDs) {
			endChunk = len(allVinDbIDs)
		}
		_, numAddressRowsSet, err = SetSpendingForVinDbIDs(pgb.db,
			allVinDbIDs[i:endChunk])
		if err != nil {
			log.Errorf("SetSpendingForFundingOP: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet

		/* // Get the funding and spending tx info for the vin
		prevoutHash, prevoutVoutInd, _, txHash, txIndex, _, erri :=
			RetrieveVinByID(pgb.db, vinDbID)
		if erri != nil {
			if erri == sql.ErrNoRows {
				log.Warnf("No funding transaction found for vin DB ID %d", vinDbID)
				continue
			}
			log.Error("RetrieveFundingOutpointByTxIn:", erri)
			continue
		}

		// skip coinbase inputs
		if bytes.Equal(zeroHashStringBytes, []byte(txHash)) {
			continue
		}

		// Set the spending tx for the address rows with a matching funding tx
		var numAddressRowsSet int64
		numAddressRowsSet, err = SetSpendingForFundingOP(pgb.db,
			prevoutHash, prevoutVoutInd, // funding
			0, txHash, txIndex, vinDbID) // spending
		// DANGER!!!! missing txDbID above
		if err != nil {
			log.Errorf("SetSpendingForFundingOP: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet */
	}

	return numAddresses, err
}
