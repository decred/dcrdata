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
    const trimmedBlockInfo = trimBlockInfo(block);
    $(newBlockHtmlElement(trimmedBlockInfo)).insertAfter($('.blocks-holder > *:first-child'));
    // hide last visible block as 1 more block is now visible
    $('.blocks-holder > .block.visible').last().removeClass('visible');
    // remove last block from dom to maintain max of 30 blocks (hidden or visible) in dom at any time
    $('.blocks-holder > .block').last().remove();
    setupTooltips();
}

function trimTxInfo(txs) {
    return txs.map(tx => {
        const voteValid = !tx.VoteInfo ? false : tx.VoteInfo.block_validation.validity;
        return {
            VinCount: tx.Vin.length,
            VoutCount: tx.Vout.length,
            VoteValid: voteValid,
            TxID: tx.TxID,
            Total: tx.Total,
        };
    });
}

function trimBlockInfo(block) {
    return {
        Time: block.time,
        Height: block.height,
        Total: block.TotalSent,
        MiningFee: block.MiningFee,
        Subsidy: block.Subsidy,
        Votes: trimTxInfo(block.Votes),
        Tickets: trimTxInfo(block.Tickets),
        Revocations: trimTxInfo(block.Revs),
        Transactions: trimTxInfo(block.Tx),
    }
}