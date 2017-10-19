// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
package dcrpg

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

type ChainDB struct {
	db          *sql.DB
	chainParams *chaincfg.Params
	dupChecks   bool
	lastBlock   map[chainhash.Hash]uint64
}

type DBInfo struct {
	Host, Port, User, Pass, DBName string
}

func NewChainDB(dbi *DBInfo, params *chaincfg.Params) (*ChainDB, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB
	db, err := Connect(dbi.Host, dbi.Port, dbi.User, dbi.Pass, dbi.DBName)
	if err != nil {
		return nil, err
	}
	return &ChainDB{
		db:          db,
		chainParams: params,
		dupChecks:   true,
		lastBlock:   make(map[chainhash.Hash]uint64),
	}, nil
}

func (pgb *ChainDB) Close() error {
	return pgb.db.Close()
}

func (pdb *ChainDB) EnableDuplicateCheckOnInsert(dupCheck bool) {
	pdb.dupChecks = dupCheck
}

func (pdb *ChainDB) SetupTables() error {
	if err := CreateTypes(pdb.db); err != nil {
		return err
	}

	if err := CreateTables(pdb.db); err != nil {
		return err
	}

	vers := TableVersions(pdb.db)
	for tab, ver := range vers {
		log.Debugf("Table %s: v%d", tab, ver)
	}
	return nil
}

func (pgb *ChainDB) DropTables() {
	DropTables(pgb.db)
}

func (pgb *ChainDB) Height() (uint64, error) {
	bestHeight, _, _, err := RetrieveBestBlockHeight(pgb.db)
	return bestHeight, err
}

func (pgb *ChainDB) SpendingTransactions(fundingTxID string) ([]string, error) {
	_, spendingTxns, err := RetrieveSpendingTxsByFundingTx(pgb.db, fundingTxID)
	return spendingTxns, err
}

func (pgb *ChainDB) SpendingTransaction(fundingTxID string, fundingTxVout uint32) (string, error) {
	_, spendingTx, err := RetrieveSpendingTxByTxOut(pgb.db, fundingTxID, fundingTxVout)
	return spendingTx, err
}

func (pgb *ChainDB) BlockTransactions(blockHash string) ([]string, []uint32, []int8, error) {
	_, blockTransactions, blockInds, trees, err := RetrieveTxsByBlockHash(pgb.db, blockHash)
	return blockTransactions, blockInds, trees, err
}

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

func (pgb *ChainDB) TransactionBlock(txID string) (string, uint32, int8, error) {
	_, blockHash, blockInd, tree, err := RetrieveTxByHash(pgb.db, txID)
	return blockHash, blockInd, tree, err
}

func (pgb *ChainDB) Store(_ *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	_, _, err := pgb.StoreBlock(msgBlock)
	return err
}

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
	return errAny
}

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
	return IndexVoutTableOnTxHash(pgb.db)
}

func (pgb *ChainDB) StoreBlock(msgBlock *wire.MsgBlock) (numVins int64, numVouts int64, err error) {
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
			pgb.chainParams, &dbBlock.TxDbIDs)
	}()

	// stake transactions
	resChanStake := make(chan storeTxnsResult)
	go func() {
		resChanStake <- pgb.storeTxns(msgBlock, wire.TxTreeStake,
			pgb.chainParams, &dbBlock.STxDbIDs)
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
	blockDbID, err = InsertBlock(pgb.db, dbBlock, pgb.dupChecks)
	if err != nil {
		log.Error("InsertBlock:", err)
		return
	}
	pgb.lastBlock[msgBlock.BlockHash()] = blockDbID

	err = InsertBlockPrevNext(pgb.db, blockDbID, dbBlock.Hash,
		dbBlock.PreviousHash, "")
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertBlockPrevNext:", err)
		return
	}

	// Update last block in db with this block's hash as it's next
	lastBlockDbID, ok := pgb.lastBlock[msgBlock.Header.PrevBlock]
	if ok {
		err = UpdateBlockNext(pgb.db, lastBlockDbID, dbBlock.Hash)
		if err != nil {
			log.Error("UpdateBlockNext:", err)
			return
		}
	}

	return
}

type storeTxnsResult struct {
	numVins, numVouts int64
	err               error
}

func (r *storeTxnsResult) Error() string {
	return r.err.Error()
}

func (pgb *ChainDB) storeTxns(msgBlock *wire.MsgBlock, txTree int8,
	chainParams *chaincfg.Params, TxDbIDs *[]uint64) storeTxnsResult {
	dbTransactions, dbTxVouts, dbTxVins := dbtypes.ExtractBlockTransactions(msgBlock,
		txTree, chainParams)

	var txRes storeTxnsResult

	var err error
	for it, dbtx := range dbTransactions {
		dbtx.VoutDbIds, err = InsertVouts(pgb.db, dbTxVouts[it], pgb.dupChecks)
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVouts:", err)
			txRes.err = err
			return txRes
		}
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

	// Get the tx PK IDs for the block
	*TxDbIDs, err = InsertTxns(pgb.db, dbTransactions, pgb.dupChecks)
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertTxns:", err)
		txRes.err = err
		return txRes
	}

	return txRes
}
