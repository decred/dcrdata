package dbtypes

import (
	"fmt"

	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
)

func ExtractBlockTransactions(msgBlock *wire.MsgBlock, txTree int8,
	chainParams *chaincfg.Params) ([]*Tx, [][]*Vout) {
	var dbTxs []*Tx
	var dbTxVouts [][]*Vout
	switch txTree {
	case wire.TxTreeRegular:
		dbTxs, dbTxVouts = processTransactions(msgBlock.Transactions,
			msgBlock.BlockHash(), chainParams)
	case wire.TxTreeStake:
		dbTxs, dbTxVouts = processTransactions(msgBlock.STransactions,
			msgBlock.BlockHash(), chainParams)
	default:
		fmt.Printf("Invalid transaction tree: %v", txTree)
	}
	return dbTxs, dbTxVouts
}

func processTransactions(txs []*wire.MsgTx, blockHash chainhash.Hash,
	chainParams *chaincfg.Params) ([]*Tx, [][]*Vout) {
	dbTransactions := make([]*Tx, 0, len(txs))
	dbTxVouts := make([][]*Vout, len(txs))

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

		dbTx.Vins = make([]VinTxProperty, 0, dbTx.NumVin)
		for idx, txin := range tx.TxIn {
			dbTx.Vins = append(dbTx.Vins, VinTxProperty{
				PrevOut:     txin.PreviousOutPoint.String(),
				PrevTxHash:  txin.PreviousOutPoint.Hash.String(),
				PrevTxIndex: txin.PreviousOutPoint.Index,
				PrevTxTree:  uint16(txin.PreviousOutPoint.Tree),
				Sequence:    txin.Sequence,
				ValueIn:     uint64(txin.ValueIn),
				TxID:        dbTx.TxID,
				TxIndex:     uint32(idx),
				BlockHeight: txin.BlockHeight,
				BlockIndex:  txin.BlockIndex,
				ScriptHex:   txin.SignatureScript,
			})
		}

		//dbTx.VinDbIds = make([]uint64, int(dbTx.NumVin))

		// Vouts and their db IDs
		dbTxVouts[txIndex] = make([]*Vout, 0, len(tx.TxOut))
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
			if err != nil {
				fmt.Println(len(vout.ScriptPubKey), err)
			}
			addys := make([]string, 0, len(scriptAddrs))
			for ia := range scriptAddrs {
				addys = append(addys, scriptAddrs[ia].String())
			}
			vout.ScriptPubKeyData.ReqSigs = uint32(reqSigs)
			vout.ScriptPubKeyData.Type = scriptClass.String()
			vout.ScriptPubKeyData.Addresses = addys
			dbTxVouts[txIndex] = append(dbTxVouts[txIndex], &vout)
		}

		//dbTx.VoutDbIds = make([]uint64, len(dbTxVouts[txIndex]))

		dbTransactions = append(dbTransactions, dbTx)
	}

	return dbTransactions, dbTxVouts
}
