import Notify from 'notifyjs'

export function notifyNewBlock (newBlock) {
  if (Notify.needsPermission) return
  const block = newBlock.block
  const newBlockNtfn = new Notify('New Decred Block Mined', {
    body: 'Block mined at height ' + block.height,
    tag: 'blockheight',
    image: '/images/dcrdata144x128.png',
    icon: '/images/dcrdata144x128.png',
    notifyError: (e) => console.error('Error showing notification:', e),
    timeout: 7
  })
  newBlockNtfn.show()
}
