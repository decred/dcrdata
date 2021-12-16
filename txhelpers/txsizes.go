// Copyright (c) 2016-2022 The Decred developers
// Copyright (c) 2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txhelpers

import (
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// DefaultRelayFeePerKb is the default minimum relay fee policy for a mempool.
// See dcrwallet/wallet/v2/txrules.DefaultRelayFeePerKb.
const DefaultRelayFeePerKb dcrutil.Amount = 1e4 // i.e. 10 atoms/B

// FeeForSerializeSize calculates the required fee for a transaction of some
// arbitrary size given a mempool's relay fee policy.
func FeeForSerializeSize(relayFeePerKb dcrutil.Amount, txSerializeSize int) dcrutil.Amount {
	fee := relayFeePerKb * dcrutil.Amount(txSerializeSize) / 1000

	if fee == 0 && relayFeePerKb > 0 {
		fee = relayFeePerKb
	}

	if fee < 0 || fee > dcrutil.MaxAmount {
		fee = dcrutil.MaxAmount
	}

	return fee
}

// Worst case script and input/output size estimates. These are valid only for
// v0 scripts.
const (
	// redeemP2PKSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PK output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	// redeemP2PKSigScriptSize = 1 + 73

	// redeemP2PKHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	redeemP2PKHSigScriptSize = 1 + 73 + 1 + 33

	// redeemP2SHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a P2SH output.
	// It is calculated as:
	//
	//  - OP_DATA_73
	//  - 73-byte signature
	//  - OP_DATA_35
	//  - OP_DATA_33
	//  - 33 bytes serialized compressed pubkey
	//  - OP_CHECKSIG
	// redeemP2SHSigScriptSize = 1 + 73 + 1 + 1 + 33 + 1

	// redeemP2PKHInputSize is the worst case (largest) serialize size of a
	// transaction input redeeming a compressed P2PKH output. It is
	// calculated as:
	//
	//   - 32 bytes previous tx
	//   - 4 bytes output index
	//   - 1 byte tree
	//   - 8 bytes amount
	//   - 4 bytes block height
	//   - 4 bytes block index
	//   - 1 byte compact int encoding value 107
	//   - 107 bytes signature script
	//   - 4 bytes sequence
	// redeemP2PKHInputSize = 32 + 4 + 1 + 8 + 4 + 4 + 1 + redeemP2PKHSigScriptSize + 4

	// p2pkhPkScriptSize is the size of a transaction output script that
	// pays to a compressed pubkey hash. It is calculated as:
	//
	//   - OP_DUP
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUALVERIFY
	//   - OP_CHECKSIG
	p2pkhPkScriptSize = 1 + 1 + 1 + 20 + 1 + 1

	// p2pkhPkTreasuryScriptSize is the size of a transaction output
	// script that pays stake change to a compressed pubkey hash  This is
	// used when a user sends coins to the treasury via OP_TADD. It is
	// calculated as:
	//
	//   - OP_SSTXCHANGE
	//   - OP_DUP
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUALVERIFY
	//   - OP_CHECKSIG
	// p2pkhPkTreasuryScriptSize = 1 + 1 + 1 + 1 + 20 + 1 + 1

	// p2shPkScriptSize is the size of a transaction output script that
	// pays to a script hash. It is calculated as:
	//
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes script hash
	//   - OP_EQUAL
	// p2shPkScriptSize = 1 + 1 + 20 + 1

	// ticketCommitmentScriptSize is the size of a ticket purchase commitment
	// script. It is calculated as:
	//
	//   - OP_RETURN
	//   - OP_DATA_30
	//   - 20 bytes P2SH/P2PKH
	//   - 8 byte amount
	//   - 2 byte fee range limits
	ticketCommitmentScriptSize = 1 + 1 + 20 + 8 + 2

	// p2pkhOutputSize is the serialize size of a transaction output with a
	// P2PKH output script. It is calculated as:
	//
	//   - 8 bytes output value
	//   - 2 bytes version
	//   - 1 byte compact int encoding value 25
	//   - 25 bytes P2PKH output script
	// p2pkhOutputSize = 8 + 2 + 1 + 25

	// tspendInputSize
	//
	//   - OP_DATA_73
	//   - 73 bytes signature
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - 1 byte OP_TSPEND
	// tspendInputSize = 1 + 73 + 1 + 33 + 1
)

// EstimateInputSize returns the worst case serialize size estimate for a tx
// input:
//   - 32 bytes previous tx
//   - 4 bytes output index
//   - 1 byte tree
//   - 8 bytes amount
//   - 4 bytes block height
//   - 4 bytes block index
//   - the compact int representation of the script size
//   - the supplied script size
//   - 4 bytes sequence
func EstimateInputSize(scriptSize int) int {
	return 32 + 4 + 1 + 8 + 4 + 4 + wire.VarIntSerializeSize(uint64(scriptSize)) + scriptSize + 4
}

// EstimateOutputSize returns the worst case serialize size estimate for a tx
// output:
//   - 8 bytes amount
//   - 2 bytes version
//   - the compact int representation of the script size
//   - the supplied script size
func EstimateOutputSize(scriptSize int) int {
	return 8 + 2 + wire.VarIntSerializeSize(uint64(scriptSize)) + scriptSize
}

// EstimateSerializeSize returns a worst case serialize size estimate for a
// signed transaction that spends a number of outputs and contains each
// transaction output from txOuts. The estimated size is incremented for an
// additional change output if changeScriptSize is greater than 0. Passing 0
// does not add a change output.
func EstimateSerializeSize(inputScriptSizes []int, txOuts []*wire.TxOut, changeScriptSize int) int {
	// Generate and sum up the estimated sizes of the inputs.
	var txInsSize int
	for _, size := range inputScriptSizes {
		txInsSize += EstimateInputSize(size)
	}

	var txOutsSize int
	for _, txOut := range txOuts {
		txOutsSize += txOut.SerializeSize()
	}

	inputCount := len(inputScriptSizes)
	outputCount := len(txOuts)
	changeSize := 0
	if changeScriptSize > 0 {
		changeSize = EstimateOutputSize(changeScriptSize)
		outputCount++
	}

	// 12 additional bytes are for version, locktime and expiry.
	return 12 + (2 * wire.VarIntSerializeSize(uint64(inputCount))) +
		wire.VarIntSerializeSize(uint64(outputCount)) +
		txInsSize + txOutsSize + changeSize
}

// EstimateSerializeSizeFromScriptSizes returns a worst case serialize size
// estimate for a signed transaction that spends len(inputSizes) previous
// outputs and pays to len(outputSizes) outputs with scripts of the provided
// worst-case sizes. The estimated size is incremented for an additional
// change output if changeScriptSize is greater than 0. Passing 0 does not
// add a change output.
func EstimateSerializeSizeFromScriptSizes(inputScriptSizes, outputScriptSizes []int, changeScriptSize int) int {
	// Generate and sum up the estimated sizes of the inputs.
	txInsSize := 0
	for _, inputSize := range inputScriptSizes {
		txInsSize += EstimateInputSize(inputSize)
	}

	// Generate and sum up the estimated sizes of the outputs.
	txOutsSize := 0
	for _, outputSize := range outputScriptSizes {
		txOutsSize += EstimateOutputSize(outputSize)
	}

	inputCount := len(inputScriptSizes)
	outputCount := len(outputScriptSizes)
	changeSize := 0
	if changeScriptSize > 0 {
		changeSize = EstimateOutputSize(changeScriptSize)
		outputCount++
	}

	// 12 additional bytes are for version, locktime and expiry.
	return 12 + (2 * wire.VarIntSerializeSize(uint64(inputCount))) +
		wire.VarIntSerializeSize(uint64(outputCount)) +
		txInsSize + txOutsSize + changeSize
}
