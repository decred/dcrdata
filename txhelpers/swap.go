// Copyright (c) 2021, The Decred developers
// See LICENSE for details.

package txhelpers

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

// AtomicSwapContractPushes models the data pushes of an atomic swap contract.
type AtomicSwapContractPushes struct {
	ContractAddress   dcrutil.Address `json:"contract_address"`
	RecipientAddress  dcrutil.Address `json:"recipient_address"`
	RefundAddress     dcrutil.Address `json:"refund_address"`
	Locktime          int64           `json:"locktime"`
	SecretHash        [32]byte        `json:"secret_hash"`
	FormattedLocktime string          `json:"formatted_locktime"`
}

// AtomicSwap models the contract and redemption details of an atomic swap.
type AtomicSwap struct {
	ContractTxRef     string  `json:"contract_txref"`
	Contract          string  `json:"contract"`
	ContractValue     float64 `json:"contract_value"`
	ContractAddress   string  `json:"contract_address"`
	RecipientAddress  string  `json:"recipient_address"`
	RefundAddress     string  `json:"refund_address"`
	Locktime          int64   `json:"locktime"`
	SecretHash        string  `json:"secret_hash"`
	Secret            string  `json:"secret"`
	FormattedLocktime string  `json:"formatted_locktime"`

	SpendTxInput string `json:"spend_tx_input"`
	IsRefund     bool   `json:"refund"`
}

// TxAtomicSwaps defines information about completed atomic swaps that are
// related to a transaction.
type TxAtomicSwaps struct {
	TxID        string                 `json:"tx_id"`
	Found       string                 `json:"found"`
	Contracts   map[uint32]*AtomicSwap `json:"contracts,omitempty"`
	Redemptions map[uint32]*AtomicSwap `json:"redemptions,omitempty"`
	Refunds     map[uint32]*AtomicSwap `json:"refunds,omitempty"`
}

// ExtractSwapDataFromInputScript checks if a tx input redeems a swap contract
// and returns details of the completed swap, the contract script and a string
// describing the identity of the redeemer.
// Returns an empty contract script and nil error if the provided script does not
// redeem a contract. Returns a non-nil error if the script could not be parsed.
func ExtractSwapDataFromInputScriptHex(inputScriptHex string, params *chaincfg.Params) (*AtomicSwapContractPushes,
	[]byte, []byte, bool, error) {
	inputScript, err := hex.DecodeString(inputScriptHex)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("error decoding txin script: %v", err)
	}
	return ExtractSwapDataFromInputScript(inputScript, params)
}

func ExtractSwapDataFromInputScript(inputScript []byte, params *chaincfg.Params) (*AtomicSwapContractPushes,
	[]byte, []byte, bool, error) {
	var contract, secret []byte
	var refund bool

	const scriptVersion = 0
	tokenizer := txscript.MakeScriptTokenizer(scriptVersion, inputScript)
	var tokenIndex = 0
	for tokenizer.Next() {
		if tokenIndex < 2 { // 0 is sig, 1 is pk
			tokenIndex++
			continue
		}
		if tokenIndex > 4 { // swap redeem sigScripts max 5 pushes, refunds are max 4
			break
		}

		data := tokenizer.Data()

		// Token at index 2 or 3 holds the branch opcode (2 for refunds where
		// there is no secret push, 3 for redeems).
		if (tokenIndex == 2 || tokenIndex == 3) && data == nil {
			switch b := tokenizer.Opcode(); b {
			case txscript.OP_TRUE: // tokenIndex == 3
			case txscript.OP_FALSE:
				refund = true // tokenIndex == 2
			default:
				// Actually, OP_IF means (not False), so this should be alright, but odd...
				fmt.Printf("opcode %d instead of OP_TRUE or OP_FALSE\n", b)
				return nil, nil, nil, false, nil /// fmt.Errorf("invalid branch OPCODE %v", b)
			}

			tokenIndex++
			continue
		}

		// Secret is token 2 for redeems.
		if tokenIndex == 2 && data != nil {
			secret = data
			tokenIndex++
			continue
		}

		// token at index 3 or 4 should hold the contract
		// if there IS data at any of those indices
		if data == nil {
			break
		}
		if tokenIndex == 4 || (tokenIndex == 3 && refund) {
			contract = data // last data in a valid contract redemption script
		}

		break // bail out below if contract == nil or there were more opcodes
	}
	if err := tokenizer.Err(); err != nil {
		return nil, nil, nil, false, fmt.Errorf("error parsing input script: %v", err)
	}

	if contract == nil || !tokenizer.Done() {
		// script should contain contract as the last data
		// if contract has been extracted, tokenizer.Done() should be true
		return nil, nil, nil, false, nil
	}

	// validate the contract script by attempting to parse it for contract info.
	contractData, err := ParseAtomicSwapContract(contract, params)
	if err != nil {
		return nil, nil, nil, false, err
	}
	if contractData == nil {
		return nil, nil, nil, false, nil // not a contract script
	}

	return contractData, contract, secret, refund, nil
}

// ParseAtomicSwapContract checks if the provided script is an atomic swap
// contact and returns the data pushes of the contract.
func ParseAtomicSwapContract(script []byte, params *chaincfg.Params) (*AtomicSwapContractPushes, error) {
	// validate the contract by calling txscript.ExtractAtomicSwapDataPushes
	contractDataPushes, _ := txscript.ExtractAtomicSwapDataPushes(0, script)
	if contractDataPushes == nil {
		return nil, nil
	}

	contractP2SH, err := dcrutil.NewAddressScriptHash(script, params)
	if err != nil {
		return nil, fmt.Errorf("contract script to p2sh address error: %v", err)
	}

	recipientAddr, err := dcrutil.NewAddressPubKeyHash(contractDataPushes.RecipientHash160[:],
		params, dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, fmt.Errorf("error parsing swap recipient address: %v", err)
	}

	refundAddr, err := dcrutil.NewAddressPubKeyHash(contractDataPushes.RefundHash160[:],
		params, dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, fmt.Errorf("error parsing swap refund address: %v", err)
	}

	var formattedLockTime string
	if contractDataPushes.LockTime >= int64(txscript.LockTimeThreshold) {
		formattedLockTime = time.Unix(contractDataPushes.LockTime, 0).UTC().Format("2006-01-02 15:04:05 (MST)")
	} else {
		formattedLockTime = fmt.Sprintf("block %v", contractDataPushes.LockTime)
	}

	return &AtomicSwapContractPushes{
		ContractAddress:   contractP2SH,
		RecipientAddress:  recipientAddr,
		RefundAddress:     refundAddr,
		Locktime:          contractDataPushes.LockTime,
		SecretHash:        contractDataPushes.SecretHash,
		FormattedLocktime: formattedLockTime,
	}, nil
}

// CheckTxInputForSwapInfo parses the scriptsig of the provided transaction input
// for information about a completed atomic swap.
// Returns (nil, nil) if the scriptsig of the provided txin does not redeem a
// swap contract.
func CheckTxInputForSwapInfo(txraw *chainjson.TxRawResult, inputIndex uint32, params *chaincfg.Params) (*AtomicSwap, error) {
	if int(inputIndex) >= len(txraw.Vin) {
		return nil, fmt.Errorf("tx does not contain input at index %d", inputIndex)
	}
	input := txraw.Vin[inputIndex]
	if input.IsCoinBase() || input.IsStakeBase() {
		return nil, nil
	}

	contractData, contractScript, secret, isRefund, err := ExtractSwapDataFromInputScriptHex(input.ScriptSig.Hex, params)
	if contractData == nil || err != nil {
		return nil, err
	}

	return &AtomicSwap{
		ContractTxRef:     fmt.Sprintf("%s:%d", input.Txid, input.Vout),
		Contract:          fmt.Sprintf("%x", contractScript),
		ContractValue:     input.AmountIn,
		ContractAddress:   contractData.ContractAddress.String(),
		RecipientAddress:  contractData.RecipientAddress.String(),
		RefundAddress:     contractData.RefundAddress.String(),
		Locktime:          contractData.Locktime,
		SecretHash:        hex.EncodeToString(contractData.SecretHash[:]),
		Secret:            hex.EncodeToString(secret),
		FormattedLocktime: contractData.FormattedLocktime,
		SpendTxInput:      fmt.Sprintf("%s:%d", txraw.Txid, inputIndex),
		IsRefund:          isRefund,
	}, nil
}

// OutputSpender describes a transaction input that spends an output by
// specifying the spending transaction and the index of the spending input.
type OutputSpender struct {
	Tx         *chainjson.TxRawResult
	InputIndex uint32
}

// TxAtomicSwapsInfo checks the outputs of the specified transaction for possible
// atomic swap contracts and the inputs for possible swap redemptions or refunds.
// Returns all contracts, redemptions and refunds that were found.
func TxAtomicSwapsInfo(tx *chainjson.TxRawResult, outputSpenders map[uint32]*OutputSpender, params *chaincfg.Params) (*TxAtomicSwaps, error) {
	txSwaps := &TxAtomicSwaps{
		TxID:        tx.Txid,
		Contracts:   make(map[uint32]*AtomicSwap),
		Redemptions: make(map[uint32]*AtomicSwap),
		Refunds:     make(map[uint32]*AtomicSwap),
	}

	// Check if tx is a stake tree tx or coinbase tx and return empty swap info.
	for _, input := range tx.Vin {
		if input.IsCoinBase() || input.IsStakeBase() || input.IsTreasurySpend() ||
			input.Treasurybase {
			return txSwaps, nil
		}
	}

	appendFound := func(found string) {
		if strings.Contains(txSwaps.Found, found) {
			return
		}
		if txSwaps.Found == "" {
			txSwaps.Found = found
		} else {
			txSwaps.Found = fmt.Sprintf("%s, %s", txSwaps.Found, found)
		}
	}

	// Check if any of this tx's outputs are contracts. Requires the output to
	// be spent AND the spending input to have the correct sigscript type.
	for _, vout := range tx.Vout {
		if vout.ScriptPubKey.Type != txscript.ScriptHashTy.String() {
			continue // non-p2sh outputs cannot currently be contracts
		}
		spender, spent := outputSpenders[vout.N]
		if !spent {
			continue // output must be spent to determine if it is a contract
		}
		// Sanity check that the provided `spender` actually spends this output.
		spendingVin := spender.Tx.Vin[spender.InputIndex]
		if spendingVin.Txid != tx.Txid || spendingVin.Vout != vout.N {
			return nil, fmt.Errorf("invalid tx spending data, %s:%d not spent by %s:%d", tx.Txid, vout.N, spendingVin.Txid, spendingVin.Vout)
		}
		// Use the spending tx input script to retrieve swap details.
		swapInfo, err := CheckTxInputForSwapInfo(spender.Tx, spender.InputIndex, params)
		if err != nil {
			return nil, fmt.Errorf("error checking if tx output is a contract: %v", err)
		}
		if swapInfo != nil {
			appendFound("Contract")
			txSwaps.Contracts[vout.N] = swapInfo
		}
	}

	// Check if any of this tx's inputs are redeems or refunds, i.e. inputs that
	// spend the output of an atomic swap contract.
	for i := range tx.Vin {
		inputIndex := uint32(i)
		swapInfo, err := CheckTxInputForSwapInfo(tx, inputIndex, params)
		if err != nil {
			return nil, fmt.Errorf("error checking if input redeems a contract: %v", err)
		}
		if swapInfo == nil {
			continue
		}
		if swapInfo.IsRefund {
			txSwaps.Refunds[inputIndex] = swapInfo
			appendFound("Refund")
		} else {
			txSwaps.Redemptions[inputIndex] = swapInfo
			appendFound("Redemption")
		}
	}

	return txSwaps, nil
}

type OutputSpenderTxOut struct {
	Tx  *wire.MsgTx
	Vin uint32
}

type AtomicSwapData struct {
	ContractTx       *chainhash.Hash
	ContractVout     uint32
	SpendTx          *chainhash.Hash
	SpendVin         uint32
	Value            int64
	ContractAddress  string
	RecipientAddress string
	RefundAddress    string
	Locktime         int64
	SecretHash       [32]byte
	Secret           []byte
	Contract         []byte
	IsRefund         bool
}

type TxSwapResults struct {
	TxID        chainhash.Hash
	Found       string
	Contracts   map[uint32]*AtomicSwapData
	Redemptions map[uint32]*AtomicSwapData
	Refunds     map[uint32]*AtomicSwapData
}

func MsgTxAtomicSwapsInfo(msgTx *wire.MsgTx, outputSpenders map[uint32]*OutputSpenderTxOut,
	params *chaincfg.Params, treasuryEnabled bool) (*TxSwapResults, error) {

	// Skip if the tx is generating coins (coinbase, treasurybase, stakebase).
	for _, input := range msgTx.TxIn {
		if input.PreviousOutPoint.Hash == zeroHash {
			return nil, nil
		}
	}

	// Only compute hash and allocate a TxSwapResults if something is found.
	var hash *chainhash.Hash
	var txSwaps *TxSwapResults

	appendFound := func(found string) {
		if txSwaps == nil { // first one
			txSwaps = &TxSwapResults{
				TxID:  *hash, // assign hash before doing appendFound!
				Found: found,
			}
			return
		}
		if txSwaps.Found == "" {
			txSwaps.Found = found
			return
		}
		if strings.Contains(txSwaps.Found, found) {
			return
		}
		txSwaps.Found = fmt.Sprintf("%s, %s", txSwaps.Found, found)
	}

	// Check if any of this tx's inputs are redeems or refunds, i.e. inputs that
	// spend the output of an atomic swap contract.
	for i, vin := range msgTx.TxIn {
		contractData, contractScript, secret, isRefund, err :=
			ExtractSwapDataFromInputScript(vin.SignatureScript, params)
		if err != nil {
			return nil, fmt.Errorf("error checking if input redeems a contract: %w", err)
		}
		if contractData == nil {
			continue
		}
		if hash == nil {
			hash = msgTx.CachedTxHash()
		}
		swapInfo := &AtomicSwapData{
			ContractTx:       &vin.PreviousOutPoint.Hash,
			ContractVout:     vin.PreviousOutPoint.Index,
			SpendTx:          hash,
			SpendVin:         uint32(i),
			Value:            vin.ValueIn,
			ContractAddress:  contractData.ContractAddress.String(),
			RecipientAddress: contractData.RecipientAddress.String(),
			RefundAddress:    contractData.RefundAddress.String(),
			Locktime:         contractData.Locktime,
			SecretHash:       contractData.SecretHash,
			Secret:           secret, // should be empty for refund
			Contract:         contractScript,
			IsRefund:         isRefund,
		}
		if isRefund {
			appendFound("Refund")
			if txSwaps.Refunds == nil {
				txSwaps.Refunds = make(map[uint32]*AtomicSwapData)
			}
			txSwaps.Refunds[uint32(i)] = swapInfo
		} else {
			appendFound("Redemption")
			if txSwaps.Redemptions == nil {
				txSwaps.Redemptions = make(map[uint32]*AtomicSwapData)
			}
			txSwaps.Redemptions[uint32(i)] = swapInfo
		}
	}

	if len(outputSpenders) == 0 {
		return txSwaps, nil
	}

	// Check if any of this tx's outputs are contracts. Requires the output to
	// be spent AND the spending input to have the correct sigscript type.
	for i, vout := range msgTx.TxOut {
		spender, spent := outputSpenders[uint32(i)]
		if !spent {
			continue // output must be spent to determine if it is a contract
		}

		scriptClass := txscript.GetScriptClass(vout.Version, vout.PkScript, treasuryEnabled)
		if scriptClass != txscript.ScriptHashTy {
			continue // non-p2sh outputs cannot currently be contracts
		}

		spendHash := spender.Tx.TxHash()

		// Sanity check that the provided `spender` actually spends this output.
		if len(spender.Tx.TxIn) <= int(spender.Vin) {
			fmt.Println("invalid:", spender.Vin)
			continue
		}
		if hash == nil {
			hash = msgTx.CachedTxHash()
		}
		spendingVin := spender.Tx.TxIn[spender.Vin]
		if spendingVin.PreviousOutPoint.Hash != *hash {
			return nil, fmt.Errorf("invalid tx spending data, %s:%d not spent by %s",
				hash, i, spendHash)
		}
		// Use the spending tx input script to retrieve swap details.
		contractData, contractScript, secret, isRefund, err :=
			ExtractSwapDataFromInputScript(spendingVin.SignatureScript, params)
		if err != nil {
			return nil, fmt.Errorf("error checking if tx output is a contract: %w", err)
		}
		if contractData != nil {
			appendFound("Contract")
			if txSwaps.Contracts == nil {
				txSwaps.Contracts = make(map[uint32]*AtomicSwapData)
			}
			txSwaps.Contracts[uint32(i)] = &AtomicSwapData{
				ContractTx:       hash,
				ContractVout:     uint32(i),
				SpendTx:          &spendHash,
				SpendVin:         spender.Vin,
				Value:            vout.Value,
				ContractAddress:  contractData.ContractAddress.String(),
				RecipientAddress: contractData.RecipientAddress.String(),
				RefundAddress:    contractData.RefundAddress.String(),
				Locktime:         contractData.Locktime,
				SecretHash:       contractData.SecretHash,
				Secret:           secret,
				Contract:         contractScript,
				IsRefund:         isRefund,
			}
		}
	}

	return txSwaps, nil
}
