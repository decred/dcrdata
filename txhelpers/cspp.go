package txhelpers

import (
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// must be sorted large to small
var splitPoints = [...]dcrutil.Amount{
	1 << 36, // 687.19476736
	1 << 34, // 171.79869184
	1 << 32, // 042.94967296
	1 << 30, // 010.73741824
	1 << 28, // 002.68435456
	1 << 26, // 000.67108864
	1 << 24, // 000.16777216
	1 << 22, // 000.04194304
	1 << 20, // 000.01048576
	1 << 18, // 000.00262144
}

var splitPointMap = map[int64]struct{}{}

func init() {
	for _, amt := range splitPoints {
		splitPointMap[int64(amt)] = struct{}{}
	}
}

// IsMixTx tests if a transaction is a CSPP-mixed transaction, which must have 3
// or more outputs of the same amount, which is one of the pre-defined mix
// denominations. mixDenom is the largest of such denominations. mixCount is the
// number of outputs of this denomination.
func IsMixTx(tx *wire.MsgTx) (isMix bool, mixDenom int64, mixCount uint32) {
	if len(tx.TxOut) < 3 || len(tx.TxIn) < 3 {
		return false, 0, 0
	}

	mixedOuts := make(map[int64]uint32)
	for _, o := range tx.TxOut {
		val := o.Value
		if _, ok := splitPointMap[val]; ok {
			mixedOuts[val]++
			continue
		}
	}

	for val, count := range mixedOuts {
		if count < 3 {
			continue
		}
		if val > mixDenom {
			mixDenom = val
			mixCount = count
		}
	}

	// TODO: revisit the input count requirements
	isMix = mixCount >= uint32(len(tx.TxOut)/2)
	return
}

// The size of a solo (non-pool) ticket purchase transaction assumes a specific
// transaction structure and worst-case signature script sizes.
func calcSoloTicketTxSize() int {
	inSizes := []int{redeemP2PKHSigScriptSize}
	outSizes := []int{p2pkhPkScriptSize + 1, ticketCommitmentScriptSize, p2pkhPkScriptSize + 1}
	return EstimateSerializeSizeFromScriptSizes(inSizes, outSizes, 0) // assume no change (with split tx)
}

var (
	soloTicketTxSize    = calcSoloTicketTxSize()
	defaultFeeForTicket = FeeForSerializeSize(DefaultRelayFeePerKb, soloTicketTxSize)
)

// IsMixedSplitTx tests if a transaction is a CSPP-mixed ticket split
// transaction (the transaction that creates appropriately-sized outputs to be
// spent by a ticket purchase). Such a transaction must have 3 or more outputs
// with an amount equal to the ticket price plus transaction fees, and at least
// as many other outputs. The expected fees to be included in the amount are
// based on the provided fee rate, relayFeeRate, and an assumed serialized size
// of a solo ticket transaction with one P2PKH input, two P2PKH outputs and one
// ticket commitment output.
func IsMixedSplitTx(tx *wire.MsgTx, relayFeeRate, ticketPrice int64) (isMix bool, ticketOutAmt int64, numTickets uint32) {
	if len(tx.TxOut) < 6 || len(tx.TxIn) < 3 {
		return false, 0, 0
	}

	ticketTxFee := defaultFeeForTicket
	if relayFeeRate != int64(DefaultRelayFeePerKb) {
		ticketTxFee = FeeForSerializeSize(dcrutil.Amount(relayFeeRate), soloTicketTxSize)
	}
	ticketOutAmt = ticketPrice + int64(ticketTxFee)

	var numOtherOut uint32
	for _, o := range tx.TxOut {
		if o.Value == ticketOutAmt {
			numTickets++
		} else {
			numOtherOut++
		}
	}

	// NOTE: The numOtherOut requirement may be too strict,
	if numTickets < 3 || numOtherOut < 3 {
		return false, 0, 0
	}

	// The input amounts do not indicate if a split tx is a mix, although it is
	// common to fund such a split transaction with mixed outputs.

	// Count the mix denomination inputs.
	// mixedIns := make(map[int64]int64)
	// for _, in := range tx.TxIn {
	// 	val := in.ValueIn
	// 	if _, ok := splitPointMap[val]; ok {
	// 		mixedIns[val]++
	// 		//numMixedIns++
	// 		continue
	// 	}
	// }

	isMix = true
	return
}
