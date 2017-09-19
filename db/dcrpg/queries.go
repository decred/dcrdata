package dcrpg

import (
	"database/sql"
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/db/dcrpg/internal"
)

func RetrieveFundingTxByTxIn(db *sql.DB, txHash string, vinIndex uint32) (id uint64, tx string, err error) {
	err = db.QueryRow(internal.SelectFundingTxByTxIn, txHash, vinIndex).Scan(
		&id, &tx)
	return
}

func RetrieveFundingTxsByTx(db *sql.DB, txHash string) ([]uint64, []*dbtypes.Tx, error) {
	var ids []uint64
	var txs []*dbtypes.Tx
	rows, err := db.Query(internal.SelectFundingTxsByTx, txHash)
	if err != nil {
		return ids, txs, err
	}
	defer rows.Close()

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

func RetrieveSpendingTxByTxOut(db *sql.DB, txHash string, voutIndex uint32) (id uint64, tx string, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByPrevOut, txHash, voutIndex).Scan(
		&id, &tx)
	return
}

func RetrieveSpendingTxsByFundingTx(db *sql.DB, funding_txid string) ([]uint64, []string, error) {
	var ids []uint64
	var txs []string
	rows, err := db.Query(internal.SelectSpendingTxsByPrevTx, funding_txid)
	if err != nil {
		return ids, txs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uint64
		var tx string
		err = rows.Scan(&id, &tx)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, tx)
	}

	return ids, txs, err
}

func RetrieveTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, err error) {
	err = db.QueryRow(internal.SelectTxByHash, txHash).Scan(&id, &blockHash)
	return
}

func RetrieveTxsByBlockHash(db *sql.DB, block_hash string) ([]uint64, []string, error) {
	var ids []uint64
	var txs []string
	rows, err := db.Query(internal.SelectTxsByBlockHash, block_hash)
	if err != nil {
		return ids, txs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uint64
		var tx string
		err = rows.Scan(&id, &tx)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, tx)
	}

	return ids, txs, err
}

func RetrieveSpendingTx(db *sql.DB, outpoint string) (uint64, *dbtypes.Tx, error) {
	var id uint64
	var tx dbtypes.Tx
	err := db.QueryRow(internal.SelectTxByPrevOut, outpoint).Scan(&id, &tx.BlockHash,
		&tx.BlockIndex, &tx.TxID, &tx.Version, &tx.Locktime, &tx.Expiry,
		&tx.NumVin, &tx.Vins, &tx.NumVout, &tx.VoutDbIds)
	return id, &tx, err
}

func RetrieveSpendingTxs(db *sql.DB, funding_txid string) ([]uint64, []*dbtypes.Tx, error) {
	var ids []uint64
	var txs []*dbtypes.Tx
	rows, err := db.Query(internal.SelectTxsByPrevOutTx, funding_txid)
	if err != nil {
		return ids, txs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uint64
		var tx dbtypes.Tx
		err = rows.Scan(&id, &tx.BlockHash,
			&tx.BlockIndex, &tx.TxID, &tx.Version, &tx.Locktime, &tx.Expiry,
			&tx.NumVin, &tx.Vins, &tx.NumVout, &tx.VoutDbIds)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, &tx)
	}

	return ids, txs, err
}

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

func RetrieveVoutValue(db *sql.DB, txDbID uint64, voutIndex uint32) (value uint64, err error) {
	err = db.QueryRow(internal.RetrieveVoutValue, txDbID, voutIndex+1).Scan(&value)
	return
}

func RetrieveVoutValues(db *sql.DB, txDbID uint64) (values []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.RetrieveVoutValues, txDbID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var v uint64
		err = rows.Scan(&v)
		if err != nil {
			break
		}

		values = append(values, v)
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
		dbVin.TxID, dbVin.TxIndex,
		dbVin.PrevTxHash, dbVin.PrevTxIndex).Scan(&id)
	return
}

func InsertVins(db *sql.DB, dbVins dbtypes.VinTxPropertyARRAY) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		dbtx.Rollback()
		return nil, fmt.Errorf("Unable to begin database transaction: %v", err)
	}

	ids := make([]uint64, 0, len(dbVins))
	for _, vin := range dbVins {
		var id uint64
		err := db.QueryRow(internal.InsertVinRow,
			vin.TxID, vin.TxIndex,
			vin.PrevTxHash, vin.PrevTxIndex).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			dbtx.Rollback()
			return nil, err
		}
		ids = append(ids, id)
	}

	dbtx.Commit()
	return ids, nil
}

func InsertVout(db *sql.DB, dbVout *dbtypes.Vout, checked bool) (uint64, error) {
	insertStatement := internal.MakeVoutInsertStatement(dbVout, checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbVout.TxHash, dbVout.TxIndex, dbVout.TxIndex,
		dbVout.Value, dbVout.Version,
		dbVout.ScriptPubKey, dbVout.ScriptPubKeyData.ReqSigs,
		dbVout.ScriptPubKeyData.Type).Scan(&id)
	return id, err
}

func InsertVouts(db *sql.DB, dbVouts []*dbtypes.Vout, checked bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		dbtx.Rollback()
		return nil, fmt.Errorf("Unable to begin database transaction: %v", err)
	}

	// if _, err = dbtx.Exec("SET LOCAL synchronous_commit TO OFF;"); err != nil {
	// 	dbtx.Rollback()
	// 	return nil, err
	// }

	ids := make([]uint64, 0, len(dbVouts))
	for _, vout := range dbVouts {
		insertStatement := internal.MakeVoutInsertStatement(vout, checked)
		var id uint64
		err := db.QueryRow(insertStatement,
			vout.TxHash, vout.TxIndex, vout.TxIndex,
			vout.Value, vout.Version,
			vout.ScriptPubKey, vout.ScriptPubKeyData.ReqSigs,
			vout.ScriptPubKeyData.Type).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			dbtx.Rollback()
			return nil, err
		}
		ids = append(ids, id)
	}

	dbtx.Commit()
	return ids, nil
}

func InsertTx(db *sql.DB, dbTx *dbtypes.Tx, checked bool) (uint64, error) {
	insertStatement := internal.MakeTxInsertStatement(dbTx, checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbTx.BlockHash, dbTx.BlockIndex, dbTx.Tree, dbTx.TxID,
		dbTx.Version, dbTx.Locktime, dbTx.Expiry,
		dbTx.NumVin, dbTx.Vins, dbTx.NumVout).Scan(&id)
	return id, err
}
