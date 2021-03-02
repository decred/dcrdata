import { Controller } from 'stimulus'
import dompurify from 'dompurify'
import globalEventBus from '../services/event_bus_service'
import ws from '../services/messagesocket_service'

const conversionRate = 100000000

function makeNode (html) {
  const div = document.createElement('div')
  div.innerHTML = dompurify.sanitize(html, { FORBID_TAGS: ['svg', 'math'] })
  return div.firstChild
}

function makeMempoolBlock (block) {
  let fees = 0
  if (!block.Transactions) return
  for (const tx of block.Transactions) {
    fees += tx.Fees
  }

  return makeNode(`<div class="block visible">
                <div class="block-info">
                    <a class="color-code" href="/mempool">Mempool</a>
                    <div class="mono" style="line-height: 1;">${Math.floor(block.Total)} DCR</div>
                    <span class="timespan">
                        <span data-target="time.age" data-age="${block.Time}"></span>
                    </span>
                </div>
                <div class="block-rows">
                    ${makeRewardsElement(block.Subsidy, fees, block.Votes.length, '#')}
                    ${makeVoteElements(block.Votes)}
                    ${makeTicketAndRevocationElements(block.Tickets, block.Revocations, '/mempool')}
                    ${makeTransactionElements(block.Transactions, '/mempool')}
                </div>
            </div>`
  )
}

function newBlockHtmlElement (block) {
  let rewardTxId
  for (const tx of block.Transactions) {
    if (tx.Coinbase) {
      rewardTxId = tx.TxID
      break
    }
  }

  return makeNode(`<div class="block visible">
                ${makeBlockSummary(block.Height, block.Total, block.Time)}
                <div class="block-rows">
                    ${makeRewardsElement(block.Subsidy, block.MiningFee, block.Votes.length, rewardTxId)}
                    ${makeVoteElements(block.Votes)}
                    ${makeTicketAndRevocationElements(block.Tickets, block.Revocations, `/block/${block.Height}`)}
                    ${makeTransactionElements(block.Transactions, `/block/${block.Height}`)}
                </div>
            </div>`
  )
}

function makeBlockSummary (blockHeight, totalSent, time) {
  return `<div class="block-info">
                <a class="color-code" href="/block/${blockHeight}">${blockHeight}</a>
                <div class="mono" style="line-height: 1;">${Math.floor(totalSent)} DCR</div>
                <span class="timespan">
                    <span data-target="time.age" data-age="${time}"></span>&nbsp;ago
                </span>
            </div>`
}

function makeRewardsElement (subsidy, fee, voteCount, rewardTxId) {
  if (!subsidy) {
    return `<div class="block-rewards">
                    <span class="pow"><span class="paint" style="width:100%;"></span></span>
                    <span class="pos"><span class="paint" style="width:100%;"></span></span>
                    <span class="fund"><span class="paint" style="width:100%;"></span></span>
                    <span class="fees" title='{"object": "Tx Fees", "total": "${fee}"}'></span>
                </div>`
  }

  const pow = subsidy.pow / conversionRate
  const pos = subsidy.pos / conversionRate
  const fund = (subsidy.developer || subsidy.dev) / conversionRate

  const backgroundColorRelativeToVotes = `style="width: ${voteCount * 20}%"` // 5 blocks = 100% painting

  // const totalDCR = Math.round(pow + fund + fee);
  const totalDCR = 1
  return `<div class="block-rewards" style="flex-grow: ${totalDCR}">
                <span class="pow" style="flex-grow: ${pow}"
                    title='{"object": "PoW Reward", "total": "${pow}"}'>
                    <a class="block-element-link" href="/tx/${rewardTxId}">
                        <span class="paint" ${backgroundColorRelativeToVotes}></span>
                    </a>
                </span>
                <span class="pos" style="flex-grow: ${pos}"
                    title='{"object": "PoS Reward", "total": "${pos}"}'>
                    <a class="block-element-link" href="/tx/${rewardTxId}">
                        <span class="paint" ${backgroundColorRelativeToVotes}></span>
                    </a>
                </span>
                <span class="fund" style="flex-grow: ${fund}"
                    title='{"object": "Project Fund", "total": "${fund}"}'>
                    <a class="block-element-link" href="/tx/${rewardTxId}">
                        <span class="paint" ${backgroundColorRelativeToVotes}></span>
                    </a>
                </span>
                <span class="fees" style="flex-grow: ${fee}"
                    title='{"object": "Tx Fees", "total": "${fee}"}'>
                    <a class="block-element-link" href="/tx/${rewardTxId}"></a>
                </span>
            </div>`
}

function makeVoteElements (votes) {
  let totalDCR = 0
  const voteElements = (votes || []).map(vote => {
    totalDCR += vote.Total
    return `<span style="background-color: ${vote.VoteValid ? '#2971ff' : 'rgba(253, 113, 74, 0.8)'}"
                    title='{"object": "Vote", "total": "${vote.Total}", "voteValid": "${vote.VoteValid}"}'>
                    <a class="block-element-link" href="/tx/${vote.TxID}"></a>
                </span>`
  })

  // append empty squares to votes
  for (let i = voteElements.length; i < 5; i++) {
    voteElements.push('<span title="Empty vote slot"></span>')
  }

  // totalDCR = Math.round(totalDCR);
  totalDCR = 1
  return `<div class="block-votes" style="flex-grow: ${totalDCR}">
                ${voteElements.join('\n')}
            </div>`
}

function makeTicketAndRevocationElements (tickets, revocations, blockHref) {
  let totalDCR = 0

  const ticketElements = (tickets || []).map(ticket => {
    totalDCR += ticket.Total
    return makeTxElement(ticket, 'block-ticket', 'Ticket')
  })
  if (ticketElements.length > 50) {
    const total = ticketElements.length
    ticketElements.splice(30)
    ticketElements.push(`<span class="block-ticket" style="flex-grow: 10; flex-basis: 50px;" title="Total of ${total} tickets">
                                <a class="block-element-link" href="${blockHref}">+ ${total - 30}</a>
                            </span>`)
  }
  const revocationElements = (revocations || []).map(revocation => {
    totalDCR += revocation.Total
    return makeTxElement(revocation, 'block-rev', 'Revocation')
  })

  const ticketsAndRevocationElements = ticketElements.concat(revocationElements)

  // append empty squares to tickets+revs
  for (let i = ticketsAndRevocationElements.length; i < 20; i++) {
    ticketsAndRevocationElements.push('<span title="Empty ticket slot"></span>')
  }

  // totalDCR = Math.round(totalDCR);
  totalDCR = 1
  return `<div class="block-tickets" style="flex-grow: ${totalDCR}">
                ${ticketsAndRevocationElements.join('\n')}
            </div>`
}

function makeTransactionElements (transactions, blockHref) {
  let totalDCR = 0
  const transactionElements = (transactions || []).map(tx => {
    totalDCR += tx.Total
    return makeTxElement(tx, 'block-tx', 'Transaction', true)
  })

  if (transactionElements.length > 50) {
    const total = transactionElements.length
    transactionElements.splice(30)
    transactionElements.push(`<span class="block-tx" style="flex-grow: 10; flex-basis: 50px;" title="Total of ${total} transactions">
                                    <a class="block-element-link" href="${blockHref}">+ ${total - 30}</a>
                                </span>`)
  }

  // totalDCR = Math.round(totalDCR);
  totalDCR = 1
  return `<div class="block-transactions" style="flex-grow: ${totalDCR}">
                ${transactionElements.join('\n')}
            </div>`
}

function makeTxElement (tx, className, type, appendFlexGrow) {
  // const style = [ `opacity: ${(tx.VinCount + tx.VoutCount) / 10}` ];
  const style = []
  if (appendFlexGrow) {
    style.push(`flex-grow: ${Math.round(tx.Total)}`)
  }

  return `<span class="${className}" style="${style.join('; ')}"
                title='{"object": "${type}", "total": "${tx.Total}", "vout": "${tx.VoutCount}", "vin": "${tx.VinCount}"}'>
                <a class="block-element-link" href="/tx/${tx.TxID}"></a>
            </span>`
}

export default class extends Controller {
  static get targets () {
    return ['box', 'title', 'showmore', 'root', 'txs', 'tooltip', 'block']
  }

  connect () {
    this.handleVisualBlocksUpdate = this._handleVisualBlocksUpdate.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.handleVisualBlocksUpdate)

    ws.registerEvtHandler('getmempooltrimmedResp', (event) => {
      console.log('received mempooltx response', event)
      this.handleMempoolUpdate(event)
    })

    ws.registerEvtHandler('mempool', (event) => {
      ws.send('getmempooltrimmed', '')
    })

    this.refreshBlocksDisplay = this._refreshBlocksDisplay.bind(this)
    // on load (js file is loaded after loading html content)
    window.addEventListener('resize', this.refreshBlocksDisplay)

    // allow some ms for page to properly render blocks before refreshing display
    setTimeout(this.refreshBlocksDisplay, 500)
  }

  disconnect () {
    ws.deregisterEvtHandlers('getmempooltrimmedResp')
    ws.deregisterEvtHandlers('mempool')
    globalEventBus.off('BLOCK_RECEIVED', this.handleVisualBlocksUpdate)
    window.removeEventListener('resize', this.refreshBlocksDisplay)
  }

  _handleVisualBlocksUpdate (newBlock) {
    const block = newBlock.block
    // show only regular tx in block.Transactions, exclude coinbase (reward) transactions
    const transactions = block.Tx.filter(tx => !tx.Coinbase)
    // trim unwanted data in this block
    const trimmedBlockInfo = {
      Time: block.time,
      Height: block.height,
      Total: block.TotalSent,
      MiningFee: block.MiningFee,
      Subsidy: block.Subsidy,
      Votes: block.Votes,
      Tickets: block.Tickets,
      Revocations: block.Revs,
      Transactions: transactions
    }

    const box = this.boxTarget
    box.insertBefore(newBlockHtmlElement(trimmedBlockInfo), box.firstChild.nextSibling)
    // hide last visible block as 1 more block is now visible
    const vis = this.visibleBlocks()
    vis[vis.length - 1].classList.remove('visible')
    // remove last block from dom to maintain max of 30 blocks (hidden or visible) in dom at any time
    box.removeChild(box.lastChild)
    this.setupTooltips()
  }

  handleMempoolUpdate (evt) {
    const mempool = JSON.parse(evt)
    mempool.Time = Math.round((new Date()).getTime() / 1000)
    this.boxTarget.replaceChild(makeMempoolBlock(mempool), this.boxTarget.firstChild)
    this.setupTooltips()
  }

  _refreshBlocksDisplay () {
    const visibleBlockElements = this.visibleBlocks()
    const currentlyDisplayedBlockCount = visibleBlockElements.length
    const maxBlockElements = this.calculateMaximumNumberOfBlocksToDisplay(visibleBlockElements[0])
    if (currentlyDisplayedBlockCount > maxBlockElements) {
      // remove the last x blocks
      for (let i = currentlyDisplayedBlockCount; i >= maxBlockElements; i--) {
        visibleBlockElements[i - 1].classList.remove('visible')
      }
    } else {
      const allBlockElements = this.blockTargets
      // add more blocks to fill display
      for (let i = currentlyDisplayedBlockCount; i < maxBlockElements; i++) {
        allBlockElements[i].classList.add('visible')
      }
    }
    this.setupTooltips()
  }

  calculateMaximumNumberOfBlocksToDisplay (blockElement) {
    const blocksSection = this.rootTarget.getBoundingClientRect()
    const margin = 20
    const blocksSectionFirstChildHeight = this.titleTarget.offsetHeight + margin
    const blocksSectionLastChildHeight = this.showmoreTarget.offsetHeight + margin

    // make blocks section fill available window height
    const extraSpace = window.innerHeight - document.getElementById('mainContainer').offsetHeight
    const blocksSectionHeight = blocksSection.height + extraSpace

    const totalAvailableWidth = blocksSection.width
    const totalAvailableHeight = blocksSectionHeight - blocksSectionFirstChildHeight - blocksSectionLastChildHeight

    const rect = blockElement.getBoundingClientRect()
    const blockWidth = rect.width
    const blockHeight = rect.height + margin // for spacing between rows

    const maxBlocksPerRow = Math.floor(totalAvailableWidth / blockWidth)
    let maxBlockRows = Math.floor(totalAvailableHeight / blockHeight)
    let maxBlockElements = maxBlocksPerRow * maxBlockRows

    const totalBlocksDisplayable = this.boxTarget.childElementCount
    while (maxBlockElements > totalBlocksDisplayable) {
      maxBlockRows--
      maxBlockElements = maxBlocksPerRow * maxBlockRows
    }

    return maxBlockElements
  }

  setupTooltips () {
    // check for empty tx rows and set custom tooltip
    this.txsTargets.forEach((div) => {
      if (div.childeElementCount === 0) {
        div.title = 'No regular transaction in block'
      }
    })

    this.tooltipTargets.forEach((tooltipElement) => {
      try {
        // parse the content
        const data = JSON.parse(tooltipElement.title)
        let newContent
        if (data.object === 'Vote') {
          newContent = `<b>${data.object} (${data.voteValid ? 'Yes' : 'No'})</b>`
        } else {
          newContent = `<b>${data.object}</b><br>${data.total} DCR`
        }

        if (data.vin && data.vout) {
          newContent += `<br>${data.vin} Inputs, ${data.vout} Outputs`
        }

        tooltipElement.title = newContent
      } catch (error) {}
    })

    import(/* webpackChunkName: "tippy" */ '../vendor/tippy.all').then(module => {
      const tippy = module.default
      tippy('.block-rows [title]', {
        allowTitleHTML: true,
        animation: 'shift-away',
        arrow: true,
        createPopperInstanceOnInit: true,
        dynamicTitle: true,
        performance: true,
        placement: 'top',
        size: 'small',
        sticky: true,
        theme: 'light'
      })
    })
  }

  visibleBlocks () {
    return this.boxTarget.querySelectorAll('.visible')
  }
}
