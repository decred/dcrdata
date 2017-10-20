// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/db/dcrpg/internal"
	"github.com/lib/pq"
)

func RetrieveVoutIDByOutpoint(db *sql.DB, txHash string, voutIndex uint32) (id uint64, err error) {
	err = db.QueryRow(internal.SelectVoutIDByOutpoint, txHash, voutIndex).Scan(&id)
	return
}

func SetSpendingForVinDbIDs(db *sql.DB, vinDbIDs []uint64) ([]int64, int64, error) {
	// get funding details for vin and set them in the address table
	dbtx, err := db.Begin()
	if err != nil {
		return nil, 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	var vinGetStmt *sql.Stmt
	vinGetStmt, err = dbtx.Prepare(internal.SelectAllVinInfoByID)
	if err != nil {
		log.Errorf("Vin SELECT prepare failed: %v", err)
		dbtx.Rollback()
		return nil, 0, err
	}

	var addrSetStmt *sql.Stmt
	addrSetStmt, err = dbtx.Prepare(internal.SetAddressSpendingForOutpoint)
	if err != nil {
		log.Errorf("address row UPDATE prepare failed: %v", err)
		vinGetStmt.Close()
		dbtx.Rollback()
		return nil, 0, err
	}

	addressRowsUpdated := make([]int64, len(vinDbIDs))

	for iv, vinDbID := range vinDbIDs {
		// Get the funding tx outpoint (vins table) for the vin DB ID
		var prevOutHash, txHash string
		var prevOutVoutInd, txVinInd uint32
		var prevOutTree, txTree int8
		var id uint64
		err = vinGetStmt.QueryRow(vinDbID).Scan(&id,
			&txHash, &txVinInd, &txTree,
			&prevOutHash, &prevOutVoutInd, &prevOutTree)
		if err != nil {
			vinGetStmt.Close()
			addrSetStmt.Close()
			return addressRowsUpdated, 0, fmt.Errorf(`SetSpendingForVinDbIDs: `+
				`%v + %v (rollback)`, err, dbtx.Rollback())
		}

		// skip coinbase inputs
		if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
			continue
		}

		// Set the spending tx info (addresses table) for the vin DB ID
		var res sql.Result
		res, err = addrSetStmt.Exec(prevOutHash, prevOutVoutInd,
			0, txHash, txVinInd, vinDbID)
		if err != nil || res == nil {
			vinGetStmt.Close()
			addrSetStmt.Close()
			return addressRowsUpdated, 0, fmt.Errorf(`SetSpendingForVinDbIDs: `+
				`%v + %v (rollback)`, err, dbtx.Rollback())
		}

		addressRowsUpdated[iv], err = res.RowsAffected()
		if err != nil {
			vinGetStmt.Close()
			addrSetStmt.Close()
			return addressRowsUpdated, 0, fmt.Errorf(`RowsAffected: `+
				`%v + %v (rollback)`, err, dbtx.Rollback())
		}
	}

	vinGetStmt.Close()
	addrSetStmt.Close()

	var totalUpdated int64
	for _, n := range addressRowsUpdated {
		totalUpdated += n
	}

	return addressRowsUpdated, totalUpdated, dbtx.Commit()
}

func SetSpendingForVinDbID(db *sql.DB, vinDbID uint64) (int64, error) {
	// get funding details for vin and set them in the address table
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	// Get the funding tx outpoint (vins table) for the vin DB ID
	var prevOutHash, txHash string
	var prevOutVoutInd, txVinInd uint32
	var prevOutTree, txTree int8
	var id uint64
	err = dbtx.QueryRow(internal.SelectAllVinInfoByID, vinDbID).
		Scan(&id, &txHash, &txVinInd, &txTree,
			&prevOutHash, &prevOutVoutInd, &prevOutTree)
	if err != nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	// skip coinbase inputs
	if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
		return 0, dbtx.Rollback()
	}

	// Set the spending tx info (addresses table) for the vin DB ID
	var res sql.Result
	res, err = dbtx.Exec(internal.SetAddressSpendingForOutpoint,
		prevOutHash, prevOutVoutInd,
		0, txHash, txVinInd, vinDbID)
	if err != nil || res == nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}

func SetSpendingForFundingOP(db *sql.DB,
	fundingTxHash string, fundingTxVoutIndex uint32,
	spendingTxDbID uint64, spendingTxHash string, spendingTxVinIndex uint32,
	vinDbID uint64) (int64, error) {
	res, err := db.Exec(internal.SetAddressSpendingForOutpoint,
		fundingTxHash, fundingTxVoutIndex,
		spendingTxDbID, spendingTxHash, spendingTxVinIndex, vinDbID)
	if err != nil || res == nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetSpendingByVinID is for when you got a new spending tx (vin entry) and you
// need to get the funding (previous output) tx info, and then update the
// corresponding row in the addresses table with the spending tx info.
func SetSpendingByVinID(db *sql.DB, vinDbID uint64, spendingTxDbID uint64,
	spendingTxHash string, spendingTxVinIndex uint32) (int64, error) {
	// get funding details for vin and set them in the address table
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	// Get the funding tx outpoint (vins table) for the vin DB ID
	var fundingTxHash string
	var fundingTxVoutIndex uint32
	var tree int8
	err = dbtx.QueryRow(internal.SelectFundingOutpointByVinID, vinDbID).
		Scan(&fundingTxHash, &fundingTxVoutIndex, &tree)
	if err != nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	// skip coinbase inputs
	if bytes.Equal(zeroHashStringBytes, []byte(fundingTxHash)) {
		return 0, dbtx.Rollback()
	}

	// Set the spending tx info (addresses table) for the vin DB ID
	var res sql.Result
	res, err = dbtx.Exec(internal.SetAddressSpendingForOutpoint,
		fundingTxHash, fundingTxVoutIndex,
		spendingTxDbID, spendingTxHash, spendingTxVinIndex, vinDbID)
	if err != nil || res == nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}

func SetSpendingForAddressDbID(db *sql.DB, addrDbID uint64, spendingTxDbID uint64,
	spendingTxHash string, spendingTxVinIndex uint32, vinDbID uint64) error {
	_, err := db.Exec(internal.SetAddressSpendingForID, addrDbID, spendingTxDbID,
		spendingTxHash, spendingTxVinIndex, vinDbID)
	return err
}

func RetrieveAddressIDsByOutpoint(db *sql.DB, txHash string,
	voutIndex uint32) ([]uint64, []string, error) {
	var ids []uint64
	var addresses []string
	rows, err := db.Query(internal.SelectAddressIDsByFundingOutpoint, txHash, voutIndex)
	if err != nil {
		return ids, addresses, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var addr string
		err = rows.Scan(&id, &addr)
		if err != nil {
			break
		}

		ids = append(ids, id)
		addresses = append(addresses, addr)
	}

	return ids, addresses, err
}

func RetrieveAllVinDbIDs(db *sql.DB) (vinDbIDs []uint64, err error) {
	rows, err := db.Query(internal.SelectVinIDsALL)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		err = rows.Scan(&id)
		if err != nil {
			break
		}

		vinDbIDs = append(vinDbIDs, id)
	}

	return
}

func RetrieveSpendingTxByVinID(db *sql.DB, vinDbID uint64) (tx string,
	voutIndex uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByVinID, vinDbID).Scan(&tx, &voutIndex, &tree)
	return
}

func RetrieveFundingOutpointByTxIn(db *sql.DB, txHash string,
	vinIndex uint32) (id uint64, tx string, index uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointByTxIn, txHash, vinIndex).
		Scan(&id, &tx, &index, &tree)
	return
}

func RetrieveFundingOutpointByVinID(db *sql.DB, vinDbID uint64) (tx string, index uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointByVinID, vinDbID).
		Scan(&tx, &index, &tree)
	return
}

func RetrieveVinByID(db *sql.DB, vinDbID uint64) (prevOutHash string, prevOutVoutInd uint32,
	prevOutTree int8, txHash string, txVinInd uint32, txTree int8, err error) {
	var id uint64
	err = db.QueryRow(internal.SelectAllVinInfoByID, vinDbID).
		Scan(&id, &txHash, &txVinInd, &txTree,
			&prevOutHash, &prevOutVoutInd, &prevOutTree)
	return
}

func RetrieveFundingTxByTxIn(db *sql.DB, txHash string, vinIndex uint32) (id uint64, tx string, err error) {
	err = db.QueryRow(internal.SelectFundingTxByTxIn, txHash, vinIndex).
		Scan(&id, &tx)
	return
}

func RetrieveFundingTxsByTx(db *sql.DB, txHash string) ([]uint64, []*dbtypes.Tx, error) {
	var ids []uint64
	var txs []*dbtypes.Tx
	rows, err := db.Query(internal.SelectFundingTxsByTx, txHash)
	if err != nil {
		return ids, txs, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var tx dbtypes.Tx
		err = rows.Scan(&id, &tx)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, &tx)
	}

	return ids, txs, err
}

func RetrieveSpendingTxByTxOut(db *sql.DB, txHash string,
	voutIndex uint32) (id uint64, tx string, vin uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByPrevOut,
		txHash, voutIndex).Scan(&id, &tx, &vin, &tree)
	return
}

func RetrieveSpendingTxsByFundingTx(db *sql.DB, fundingTxID string) (dbIDs []uint64,
	txns []string, vinInds []uint32, voutInds []uint32, err error) {
	rows, err := db.Query(internal.SelectSpendingTxsByPrevTx, fundingTxID)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var tx string
		var vin, vout uint32
		err = rows.Scan(&id, &tx, &vin, &vout)
		if err != nil {
			break
		}

		dbIDs = append(dbIDs, id)
		txns = append(txns, tx)
		vinInds = append(vinInds, vin)
		voutInds = append(voutInds, vout)
	}

	return
}

func RetrieveTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, blockInd uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectTxByHash, txHash).Scan(&id, &blockHash, &blockInd, &tree)
	return
}

func RetrieveRegularTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, blockInd uint32, err error) {
	err = db.QueryRow(internal.SelectRegularTxByHash, txHash).Scan(&id, &blockHash, &blockInd)
	return
}

func RetrieveStakeTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, blockInd uint32, err error) {
	err = db.QueryRow(internal.SelectStakeTxByHash, txHash).Scan(&id, &blockHash, &blockInd)
	return
}

func RetrieveTxsByBlockHash(db *sql.DB, blockHash string) (ids []uint64, txs []string, blockInds []uint32, trees []int8, err error) {
	rows, err := db.Query(internal.SelectTxsByBlockHash, blockHash)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var tx string
		var bind uint32
		var tree int8
		err = rows.Scan(&id, &tx, &bind, &tree)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, tx)
		blockInds = append(blockInds, bind)
		trees = append(trees, tree)
	}

	return
}

// func RetrieveSpendingTx(db *sql.DB, outpoint string) (uint64, *dbtypes.Tx, error) {
// 	var id uint64
// 	var tx dbtypes.Tx
// 	err := db.QueryRow(internal.SelectTxByPrevOut, outpoint).Scan(&id, &tx.BlockHash,
// 		&tx.BlockIndex, &tx.TxID, &tx.Version, &tx.Locktime, &tx.Expiry,
// 		&tx.NumVin, &tx.Vins, &tx.NumVout, &tx.VoutDbIds)
// 	return id, &tx, err
// }

// func RetrieveSpendingTxs(db *sql.DB, fundingTxID string) ([]uint64, []*dbtypes.Tx, error) {
// 	var ids []uint64
// 	var txs []*dbtypes.Tx
// 	rows, err := db.Query(internal.SelectTxsByPrevOutTx, fundingTxID)
// 	if err != nil {
// 		return ids, txs, err
// 	}
// 	defer func() {
// 		if e := rows.Close(); e != nil {
// 			log.Errorf("Close of Query failed: %v", e)
// 		}
// 	}()

// 	for rows.Next() {
// 		var id uint64
// 		var tx dbtypes.Tx
// 		err = rows.Scan(&id, &tx.BlockHash,
// 			&tx.BlockIndex, &tx.TxID, &tx.Version, &tx.Locktime, &tx.Expiry,
// 			&tx.NumVin, &tx.Vins, &tx.NumVout, &tx.VoutDbIds)
// 		if err != nil {
// 			break
// 		}

// 		ids = append(ids, id)
// 		txs = append(txs, &tx)
// 	}

// 	return ids, txs, err
// }

func InsertBlock(db *sql.DB, dbBlock *dbtypes.Block, checked bool) (uint64, error) {
	insertStatement := internal.MakeBlockInsertStatement(dbBlock, checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbBlock.Hash, dbBlock.Height, dbBlock.Size, dbBlock.Version,
		dbBlock.MerkleRoot, dbBlock.StakeRoot,
		dbBlock.NumTx, dbBlock.NumRegTx, dbBlock.NumStakeTx,
		dbBlock.Time, dbBlock.Nonce, dbBlock.VoteBits,
		dbBlock.FinalState, dbBlock.Voters, dbBlock.FreshStake,
		dbBlock.Revocations, dbBlock.PoolSize, dbBlock.Bits,
		dbBlock.SBits, dbBlock.Difficulty, dbBlock.ExtraData,
		dbBlock.StakeVersion, dbBlock.PreviousHash).Scan(&id)
	return id, err
}

func RetrieveBestBlockHeight(db *sql.DB) (height uint64, hash string, id uint64, err error) {
	err = db.QueryRow(internal.RetrieveBestBlockHeight).Scan(&id, &hash, &height)
	return
}

func RetrieveVoutValue(db *sql.DB, txHash string, voutIndex uint32) (value uint64, err error) {
	err = db.QueryRow(internal.RetrieveVoutValue, txHash, voutIndex).Scan(&value)
	return
}

func RetrieveVoutValues(db *sql.DB, txHash string) (values []uint64, txInds []uint32, txTrees []int8, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.RetrieveVoutValues, txHash)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var v uint64
		var ind uint32
		var tree int8
		err = rows.Scan(&v, &ind, &tree)
		if err != nil {
			break
		}

		values = append(values, v)
		txInds = append(txInds, ind)
		txTrees = append(txTrees, tree)
	}

	return
}

func InsertBlockPrevNext(db *sql.DB, block_db_id uint64,
	hash, prev, next string) error {
	rows, err := db.Query(internal.InsertBlockPrevNext, block_db_id, prev, hash, next)
	if err == nil {
		return rows.Close()
	}
	return err
}

func UpdateBlockNext(db *sql.DB, block_db_id uint64, next string) error {
	res, err := db.Exec(internal.UpdateBlockNext, block_db_id, next)
	if err != nil {
		return err
	}
	numRows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("UpdateBlockNext failed to update exactly 1 row (%d)", numRows)
	}
	return nil
}

func InsertVin(db *sql.DB, dbVin dbtypes.VinTxProperty) (id uint64, err error) {
	err = db.QueryRow(internal.InsertVinRow,
		dbVin.TxID, dbVin.TxIndex, dbVin.TxTree,
		dbVin.PrevTxHash, dbVin.PrevTxIndex, dbVin.PrevTxTree).Scan(&id)
	return
}

func InsertVins(db *sql.DB, dbVins dbtypes.VinTxPropertyARRAY) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.InsertVinRow)
	if err != nil {
		log.Errorf("Vin INSERT prepare: %v", err)
		dbtx.Rollback()
		return nil, err
	}

	// TODO/Question: Should we skip inserting coinbase txns, which have same PrevTxHash?

	ids := make([]uint64, 0, len(dbVins))
	for _, vin := range dbVins {
		var id uint64
		err = stmt.QueryRow(vin.TxID, vin.TxIndex, vin.TxTree,
			vin.PrevTxHash, vin.PrevTxIndex, vin.PrevTxTree).Scan(&id)
		if err != nil {
			stmt.Close()
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return ids, fmt.Errorf("InsertVins INSERT exec failed: %v", err)
		}
		// err = db.QueryRow("SELECT currval(pg_get_serial_sequence('vins', 'id'));").Scan(&id) // currval('vins_id_seq')
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			stmt.Close()
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, err
		}
		ids = append(ids, id)
	}

	stmt.Close()

	return ids, dbtx.Commit()
}

func InsertVout(db *sql.DB, dbVout *dbtypes.Vout, checked bool) (uint64, error) {
	insertStatement := internal.MakeVoutInsertStatement(checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbVout.TxHash, dbVout.TxIndex, dbVout.TxTree,
		dbVout.Value, dbVout.Version,
		dbVout.ScriptPubKey, dbVout.ScriptPubKeyData.ReqSigs,
		dbVout.ScriptPubKeyData.Type,
		pq.Array(dbVout.ScriptPubKeyData.Addresses)).Scan(&id)
	return id, err
}

func InsertVouts(db *sql.DB, dbVouts []*dbtypes.Vout, checked bool) ([]uint64, []dbtypes.AddressRow, error) {
	addressRows := make([]dbtypes.AddressRow, 0, len(dbVouts)*2)
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeVoutInsertStatement(checked))
	if err != nil {
		log.Errorf("Vout INSERT prepare: %v", err)
		dbtx.Rollback()
		return nil, nil, err
	}

	ids := make([]uint64, 0, len(dbVouts))
	for _, vout := range dbVouts {
		var id uint64
		err := stmt.QueryRow(
			vout.TxHash, vout.TxIndex, vout.TxTree, vout.Value, vout.Version,
			vout.ScriptPubKey, vout.ScriptPubKeyData.ReqSigs,
			vout.ScriptPubKeyData.Type,
			pq.Array(vout.ScriptPubKeyData.Addresses)).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			stmt.Close()
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, nil, err
		}
		for _, addr := range vout.ScriptPubKeyData.Addresses {
			addressRows = append(addressRows, dbtypes.AddressRow{
				Address:            addr,
				FundingTxHash:      vout.TxHash,
				FundingTxVoutIndex: vout.TxIndex,
				VoutDbID:           id,
				Value:              vout.Value,
			})
		}
		ids = append(ids, id)
	}

	stmt.Close()

	return ids, addressRows, dbtx.Commit()
}

func InsertAddressOut(db *sql.DB, dbA *dbtypes.AddressRow) (uint64, error) {
	var id uint64
	err := db.QueryRow(internal.InsertAddressRow, dbA.Address, dbA.FundingTxDbID,
		dbA.FundingTxHash, dbA.FundingTxVoutIndex, dbA.VoutDbID, dbA.Value).Scan(&id)
	return id, err
}

func InsertAddressOuts(db *sql.DB, dbAs []*dbtypes.AddressRow) ([]uint64, error) {
	// Create the address table if it does not exist
	tableName := "addresses"
	if haveTable, _ := TableExists(db, tableName); !haveTable {
		if err := CreateTable(db, tableName); err != nil {
			log.Errorf("Failed to create table %s: %v", tableName, err)
		}
	}

	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.InsertAddressRow)
	if err != nil {
		log.Errorf("AddressRow INSERT prepare: %v", err)
		dbtx.Rollback()
		return nil, err
	}

	ids := make([]uint64, 0, len(dbAs))
	for _, dbA := range dbAs {
		var id uint64
		err := stmt.QueryRow(dbA.Address, dbA.FundingTxDbID, dbA.FundingTxHash,
			dbA.FundingTxVoutIndex, dbA.VoutDbID, dbA.Value).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			stmt.Close()
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, err
		}
		ids = append(ids, id)
	}

	stmt.Close()

	return ids, dbtx.Commit()
}

func InsertTx(db *sql.DB, dbTx *dbtypes.Tx, checked bool) (uint64, error) {
	insertStatement := internal.MakeTxInsertStatement(checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbTx.BlockHash, dbTx.BlockIndex, dbTx.Tree, dbTx.TxID,
		dbTx.Version, dbTx.Locktime, dbTx.Expiry,
		dbTx.NumVin, dbTx.Vins, dbtypes.UInt64Array(dbTx.VinDbIds),
		dbTx.NumVout, pq.Array(dbTx.Vouts), dbtypes.UInt64Array(dbTx.VoutDbIds)).Scan(&id)
	return id, err
}

func InsertTxns(db *sql.DB, dbTxns []*dbtypes.Tx, checked bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeTxInsertStatement(checked))
	if err != nil {
		log.Errorf("Vout INSERT prepare: %v", err)
		dbtx.Rollback()
		return nil, err
	}

	ids := make([]uint64, 0, len(dbTxns))
	for _, tx := range dbTxns {
		var id uint64
		err := stmt.QueryRow(
			tx.BlockHash, tx.BlockIndex, tx.Tree, tx.TxID,
			tx.Version, tx.Locktime, tx.Expiry,
			tx.NumVin, tx.Vins, dbtypes.UInt64Array(tx.VinDbIds),
			tx.NumVout, pq.Array(tx.Vouts), dbtypes.UInt64Array(tx.VoutDbIds)).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			stmt.Close()
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, err
		}
		ids = append(ids, id)
	}

	stmt.Close()

	return ids, dbtx.Commit()
}
