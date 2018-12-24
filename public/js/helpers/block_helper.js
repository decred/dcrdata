// Check for the txid in the given block
export default function txInBlock (txid, block) {
  var txTypes = [block.Tx, block.Tickets, block.Revs, block.Votes]
  for (let txIdx in txTypes) {
    let txs = txTypes[txIdx]
    for (let idx in txs) {
      if (txs[idx].TxID === txid) {
        return true
      }
    }
  }
  return false
}
