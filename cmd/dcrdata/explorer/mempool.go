// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import "github.com/decred/dcrdata/v8/explorer/types"

// matchMempoolVins filters relevant mempool transaction inputs whose previous
// outpoints match the specified transaction id.
func matchMempoolVins(txid string, txsList []types.MempoolTx) (vins []types.MempoolVin) {
	for idx := range txsList {
		tx := &txsList[idx]
		var inputs []types.MempoolInput
		for vindex := range tx.Vin {
			input := &tx.Vin[vindex]
			if input.TxId != txid {
				continue
			}
			inputs = append(inputs, *input)
		}
		if len(inputs) == 0 {
			continue
		}
		vins = append(vins, types.MempoolVin{
			TxId:   tx.TxID,
			Inputs: inputs,
		})
	}
	return
}

// GetTxMempoolInputs grabs very simple information about mempool transaction
// inputs that spend a particular previous transaction's outputs. The returned
// slice has just enough information to match an unspent transaction output.
func (exp *explorerUI) GetTxMempoolInputs(txid string, txType string) (vins []types.MempoolVin) {
	// Lock the pointer from changing, and the contents of the shared struct.
	inv := exp.MempoolInventory()
	inv.RLock()
	defer inv.RUnlock()
	vins = append(vins, matchMempoolVins(txid, inv.Transactions)...)
	vins = append(vins, matchMempoolVins(txid, inv.Tickets)...)
	vins = append(vins, matchMempoolVins(txid, inv.Revocations)...)
	vins = append(vins, matchMempoolVins(txid, inv.Votes)...)
	return
}
