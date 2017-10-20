package dbtypes

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
)

func ExtractBlockTransactions(msgBlock *wire.MsgBlock, txTree int8,
	chainParams *chaincfg.Params) ([]*Tx, [][]*Vout, []VinTxPropertyARRAY) {
	var dbTxs []*Tx
	var dbTxVouts [][]*Vout
	var dbTxVins []VinTxPropertyARRAY
	switch txTree {
	case wire.TxTreeRegular:
		dbTxs, dbTxVouts, dbTxVins = processTransactions(msgBlock.Transactions,
			msgBlock.BlockHash(), chainParams)
	case wire.TxTreeStake:
		dbTxs, dbTxVouts, dbTxVins = processTransactions(msgBlock.STransactions,
			msgBlock.BlockHash(), chainParams)
	default:
		fmt.Printf("Invalid transaction tree: %v", txTree)
	}
	return dbTxs, dbTxVouts, dbTxVins
}

func processTransactions(txs []*wire.MsgTx, blockHash chainhash.Hash,
	chainParams *chaincfg.Params) ([]*Tx, [][]*Vout, []VinTxPropertyARRAY) {
	dbTransactions := make([]*Tx, 0, len(txs))
	dbTxVouts := make([][]*Vout, len(txs))
	dbTxVins := make([]VinTxPropertyARRAY, len(txs))

	for txIndex, tx := range txs {
		var tree int8
		if txhelpers.IsStakeTx(tx) {
			tree = wire.TxTreeStake
		}
		dbTx := &Tx{
			BlockHash:  blockHash.String(),
			BlockIndex: uint32(txIndex),
			Tree:       tree,
			TxID:       tx.TxHash().String(),
			Version:    tx.Version,
			Locktime:   tx.LockTime,
			Expiry:     tx.Expiry,
			NumVin:     uint32(len(tx.TxIn)),
			NumVout:    uint32(len(tx.TxOut)),
		}

		//dbTx.Vins = make([]VinTxProperty, 0, dbTx.NumVin)
		dbTxVins[txIndex] = make(VinTxPropertyARRAY, 0, len(tx.TxIn))
		for idx, txin := range tx.TxIn {
			dbTxVins[txIndex] = append(dbTxVins[txIndex], VinTxProperty{
				PrevOut:     txin.PreviousOutPoint.String(),
				PrevTxHash:  txin.PreviousOutPoint.Hash.String(),
				PrevTxIndex: txin.PreviousOutPoint.Index,
				PrevTxTree:  uint16(txin.PreviousOutPoint.Tree),
				Sequence:    txin.Sequence,
				ValueIn:     uint64(txin.ValueIn),
				TxID:        dbTx.TxID,
				TxIndex:     uint32(idx),
				TxTree:      uint16(dbTx.Tree),
				BlockHeight: txin.BlockHeight,
				BlockIndex:  txin.BlockIndex,
				ScriptHex:   txin.SignatureScript,
			})
		}

		//dbTx.VinDbIds = make([]uint64, int(dbTx.NumVin))

		// Vouts and their db IDs
		dbTxVouts[txIndex] = make([]*Vout, 0, len(tx.TxOut))
		//dbTx.Vouts = make([]*Vout, 0, len(tx.TxOut))
		for io, txout := range tx.TxOut {
			vout := Vout{
				TxHash:       dbTx.TxID,
				TxIndex:      uint32(io),
				TxTree:       tree,
				Value:        uint64(txout.Value),
				Version:      txout.Version,
				ScriptPubKey: txout.PkScript,
			}
			scriptClass, scriptAddrs, reqSigs, err := txscript.ExtractPkScriptAddrs(
				vout.Version, vout.ScriptPubKey, chainParams)
			if err != nil && !bytes.Equal(vout.ScriptPubKey, chainParams.OrganizationPkScript) {
				fmt.Println(len(vout.ScriptPubKey), err, hex.EncodeToString(vout.ScriptPubKey))
			}
			addys := make([]string, 0, len(scriptAddrs))
			for ia := range scriptAddrs {
				addys = append(addys, scriptAddrs[ia].String())
			}
			vout.ScriptPubKeyData.ReqSigs = uint32(reqSigs)
			vout.ScriptPubKeyData.Type = scriptClass.String()
			vout.ScriptPubKeyData.Addresses = addys
			dbTxVouts[txIndex] = append(dbTxVouts[txIndex], &vout)
			//dbTx.Vouts = append(dbTx.Vouts, &vout)
		}

		//dbTx.VoutDbIds = make([]uint64, len(dbTxVouts[txIndex]))

		dbTransactions = append(dbTransactions, dbTx)
	}

	return dbTransactions, dbTxVouts, dbTxVins
}
