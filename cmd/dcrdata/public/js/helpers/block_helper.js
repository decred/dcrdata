// Check for the txid in the given block
export default function txInBlock (txid, block) {
  const txTypes = [block.Tx, block.Tickets, block.Revs, block.Votes]
  for (const txIdx in txTypes) {
    const txs = txTypes[txIdx]
    for (const idx in txs) {
      if (txs[idx].TxID === txid) {
        return true
      }
    }
  }
  return false
}
