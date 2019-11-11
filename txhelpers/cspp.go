package txhelpers

import (
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
)

// 42.94967296: https://dcrdata.decred.org/tx/30d861942410e042cec6a88dcc3065ebab4d9f5e23fa2f7e2c7f64cdc28153d4/out/3
// 10.73741824: https://dcrdata.decred.org/tx/4e3cdbc6ff756a0fc7eba8626713a677cc70517c731c3485494b3fdb8746c7ae/out/10
// 2.68435456: https://dcrdata.decred.org/tx/ab70b9b3fc88feb7be0c1a1b9ba47cac9dea10f158911fd9cfaf3af3f80878f3/out/2
// 0.67108864: https://dcrdata.decred.org/tx/86986953f5534fb6942e38ccde8c8f4335828613abfe49a5d01c074b3bb2105b/out/10
// 0.16777216: https://dcrdata.decred.org/tx/9ba917606149761e32b03e0f65c4f45fa3aa6b550f422d2089f89b763fab6104/out/8
// 0.04194304: https://dcrdata.decred.org/tx/fe01cf6fde802d2c1241312e7d3edcba857c7462eb584f4830dee7de8f287a95/out/0
// 0.01048576: https://dcrdata.decred.org/tx/88690788e1559a99d79a95dfb427228fe3dae2229fff4dc6382af3eb3ff993e8/out/1
// 0.00262144: https://dcrdata.decred.org/tx/c1586230bacfea4384488a5c29f8bf90bb93fd1e46f657dde79cb09054d860cc/out/1

// all feeding a mix split tx: https://dcrdata.decred.org/tx/9575c5eb83ed4ac9eece7213ffa28cf5abab39fd202d0c89cfaa8416a178031b/in/49

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

func IsMixTx(tx *wire.MsgTx) (isMix bool, mixDenom int64, mixCount uint32) {
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

	isMix = mixCount > uint32(len(tx.TxOut)/2)
	return
}

func IsMixedSplitTx(tx *wire.MsgTx, ticketPrice int64) (bool, uint32) {
	var numTickets uint32
	for _, o := range tx.TxOut {
		if o.Value == ticketPrice {
			numTickets++
		}
	}

	if numTickets < 3 {
		return false, 0
	}

	// Check that there are mix denomination inputs.
	var numMixedIns uint32
	mixedIns := make(map[int64]int64)
	for _, in := range tx.TxIn {
		val := in.ValueIn
		if _, ok := splitPointMap[val]; ok {
			mixedIns[val]++
			numMixedIns++
			continue
		}
	}

	// What about the mixed Inputs?
	// either a mixed amount or a vote output?

	if numMixedIns < numTickets {
		return false, 0
	}

	return true, numTickets
}
