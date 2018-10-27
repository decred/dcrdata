function handleMempoolUpdate(evt) {
    const mempool = JSON.parse(evt);
    mempool.Time = Date.now() / 1000;
    const mempoolElement = makeMempoolBlock(mempool);
    const currentMempoolElement = $('.blocks-holder > *:first-child');
    $(mempoolElement).insertAfter(currentMempoolElement);
    currentMempoolElement.remove();
    setupTooltips();
}

function handleBlockUpdate(block) {
    // show only regular tx in block.Transactions, exclude coinbase (reward) transactions
    const transactions = block.Tx.filter(tx => !tx.Coinbase);

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
    };

    $(newBlockHtmlElement(trimmedBlockInfo)).insertAfter($('.blocks-holder > *:first-child'));
    // hide last visible block as 1 more block is now visible
    $('.blocks-holder > .block.visible').last().removeClass('visible');
    // remove last block from dom to maintain max of 30 blocks (hidden or visible) in dom at any time
    $('.blocks-holder > .block').last().remove();
    setupTooltips();
}