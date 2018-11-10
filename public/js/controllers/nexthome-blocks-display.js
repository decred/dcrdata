const conversionRate = 100000000;

function calculateMaximumNumberOfBlocksToDisplay(blockElement) {
    const blocksSection = $('.blocks-section');
    const blocksSectionFirstChildHeight = blocksSection.children(":first").outerHeight(true);
    const blocksSectionLastChildHeight = blocksSection.children(":last").outerHeight(true);

    // make blocks section fill available window height
    const extraSpace = $(window).height() - $('#mainContainer').outerHeight(true);
    let blocksSectionHeight = blocksSection.outerHeight() + extraSpace;

    const totalAvailableWidth = blocksSection.width();
    let totalAvailableHeight = blocksSectionHeight - blocksSectionFirstChildHeight - blocksSectionLastChildHeight;

    // block section should be at least same height as netstats section
    const netstatsSectionHeight = $('.netstats-section').outerHeight();
    if (netstatsSectionHeight > totalAvailableHeight) {
        totalAvailableHeight = netstatsSectionHeight - blocksSectionFirstChildHeight - blocksSectionLastChildHeight;
    }

    const blockWidth = blockElement.width();
    const blockHeight = blockElement.height() + 20; // for spacing between rows

    const maxBlocksPerRow = Math.floor(totalAvailableWidth / blockWidth);
    let maxBlockRows = Math.floor(totalAvailableHeight / blockHeight);
    let maxBlockElements = maxBlocksPerRow * maxBlockRows;

    const totalBlocksDisplayable = $('.blocks-holder').children().length;
    while (maxBlockElements > totalBlocksDisplayable) {
        maxBlockRows--;
        maxBlockElements = maxBlocksPerRow * maxBlockRows;
    }

    return maxBlockElements;
}

function refreshBlocksDisplay() {
    const visibleBlockElements = $('.block.visible');
    const currentlyDisplayedBlockCount = visibleBlockElements.length;
    const maxBlockElements = calculateMaximumNumberOfBlocksToDisplay(visibleBlockElements);

    if (currentlyDisplayedBlockCount > maxBlockElements) {
        // remove the last x blocks
        for (let i = currentlyDisplayedBlockCount; i >= maxBlockElements; i--) {
            $(visibleBlockElements[i]).removeClass('visible');
        }
    }
    else {
        const allBlockElements = $('.block');
        // add more blocks to fill display
        for (let i = currentlyDisplayedBlockCount; i < maxBlockElements; i++) {
            $(allBlockElements[i]).addClass('visible');
        }
    }

    setupTooltips();
}

function makeMempoolBlock(block) {
    let fees = 0;
    for (const tx of block.Transactions) {
        fees += tx.Fees;
    }

    return `<div class="block visible">
                <div class="block-info">
                    <a class="color-code" href="/mempool">Mempool</a>
                    <div class="mono" style="line-height: 1;">${Math.floor(block.Total)} DCR</div>
                    <span class="timespan">
                        <span data-target="main.age" data-age="${block.Time}"></span>&nbsp;ago
                    </span>
                </div>
                <div class="block-rows">
                    ${makeRewardsElement(block.Subsidy, fees, block.Votes.length, '#')}
                    ${makeVoteElements(block.Votes)}
                    ${makeTicketAndRevocationElements(block.Tickets, block.Revocations, '/mempool')}
                    ${makeTransactionElements(block.Transactions, '/mempool')}
                </div>
            </div>`;
}

function newBlockHtmlElement(block) {
    let rewardTxId;
    for (const tx of block.Transactions) {
        if (tx.Coinbase) {
            rewardTxId = tx.TxID;
            break;
        }
    }

    return `<div class="block visible">
                ${makeBlockSummary(block.Height, block.Total, block.Time)}
                <div class="block-rows">
                    ${makeRewardsElement(block.Subsidy, block.MiningFee, block.Votes.length, rewardTxId)}
                    ${makeVoteElements(block.Votes)}
                    ${makeTicketAndRevocationElements(block.Tickets, block.Revocations, `/block/${block.Height}`)}
                    ${makeTransactionElements(block.Transactions, `/block/${block.Height}`)}
                </div>
            </div>`;
}

function makeBlockSummary(blockHeight, totalSent, time) {
    return `<div class="block-info">
                <a class="color-code" href="/block/${blockHeight}">${blockHeight}</a>
                <div class="mono" style="line-height: 1;">${Math.floor(totalSent)} DCR</div>
                <span class="timespan">
                    <span data-target="main.age" data-age="${time}"></span>&nbsp;ago
                </span>
            </div>`;
}

function makeRewardsElement(subsidy, fee, voteCount, rewardTxId) {
    if (!subsidy) {
        return `<div class="block-rewards">
                    <span class="pow"><span class="paint" style="width:100%;"></span></span>
                    <span class="pos"><span class="paint" style="width:100%;"></span></span>
                    <span class="fund"><span class="paint" style="width:100%;"></span></span>
                    <span class="fees" title='{"object": "Tx Fees", "total": "${fee}"}'></span>
                </div>`;
    }

    const pow = subsidy.pow / conversionRate;
    const pos = subsidy.pos / conversionRate;
    const fund = (subsidy.developer || subsidy.dev) / conversionRate;
    
    const backgroundColorRelativeToVotes = `style="width: ${voteCount * 20}%"`; // 5 blocks = 100% painting

    // const totalDCR = Math.round(pow + fund + fee);
    const totalDCR = 1;
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
            </div>`;
}

function makeVoteElements(votes) {
    let totalDCR = 0;
    const voteElements = (votes || []).map(vote => {
        totalDCR += vote.Total;
        return `<span style="background-color: ${vote.VoteValid ? '#2971ff' : 'rgba(253, 113, 74, 0.8)' }"
                    title='{"object": "Vote", "total": "${vote.Total}", "voteValid": "${vote.VoteValid}"}'>
                    <a class="block-element-link" href="/tx/${vote.TxID}"></a>
                </span>`;
    });

    // append empty squares to votes
    for (var i = voteElements.length; i < 5; i++) {
        voteElements.push('<span title="Empty vote slot"></span>');
    }

    // totalDCR = Math.round(totalDCR);
    totalDCR = 1;
    return `<div class="block-votes" style="flex-grow: ${totalDCR}">
                ${voteElements.join("\n")}
            </div>`;
}

function makeTicketAndRevocationElements(tickets, revocations, blockHref) {
    let totalDCR = 0;

    const ticketElements = (tickets || []).map(ticket => {
        totalDCR += ticket.Total;
        return makeTxElement(ticket, "block-ticket", "Ticket");
    });
    if (ticketElements.length > 50) {
        const total = ticketElements.length;
        ticketElements.splice(30);
        ticketElements.push(`<span class="block-ticket" style="flex-grow: 10; flex-basis: 50px;" title="Total of ${total} tickets">
                                <a class="block-element-link" href="${blockHref}">+ ${total - 30}</a>
                            </span>`);
    }
    const revocationElements = (revocations || []).map(revocation => {
        totalDCR += revocation.Total;
        return makeTxElement(revocation, "block-rev", "Revocation");
    });

    const ticketsAndRevocationElements = ticketElements.concat(revocationElements);

    // append empty squares to tickets+revs
    for (var i = ticketsAndRevocationElements.length; i < 20; i++) {
        ticketsAndRevocationElements.push('<span title="Empty ticket slot"></span>');
    }

    // totalDCR = Math.round(totalDCR);
    totalDCR = 1;
    return `<div class="block-tickets" style="flex-grow: ${totalDCR}">
                ${ticketsAndRevocationElements.join("\n")}
            </div>`;
}

function makeTransactionElements(transactions, blockHref) {
    let totalDCR = 0;
    const transactionElements = (transactions || []).map(tx => {
        totalDCR += tx.Total;
        return makeTxElement(tx, "block-tx", "Transaction", true);
    });

    if (transactionElements.length > 50) {
        const total = transactionElements.length;
        transactionElements.splice(30);
        transactionElements.push(`<span class="block-tx" style="flex-grow: 10; flex-basis: 50px;" title="Total of ${total} transactions">
                                    <a class="block-element-link" href="${blockHref}">+ ${total - 30}</a>
                                </span>`);
    }

    // totalDCR = Math.round(totalDCR);
    totalDCR = 1;
    return `<div class="block-transactions" style="flex-grow: ${totalDCR}">
                ${transactionElements.join("\n")}
            </div>`;
}

function makeTxElement(tx, className, type, appendFlexGrow) {
    // const style = [ `opacity: ${(tx.VinCount + tx.VoutCount) / 10}` ];
    const style = [];
    if (appendFlexGrow) {
        style.push(`flex-grow: ${Math.round(tx.Total)}`);
    }

    return `<span class="${className}" style="${style.join("; ")}"
                title='{"object": "${type}", "total": "${tx.Total}", "vout": "${tx.VoutCount}", "vin": "${tx.VinCount}"}'>
                <a class="block-element-link" href="/tx/${tx.TxID}"></a>
            </span>`;
}

function setupTooltips() {
    // check for emtpy tx rows and set custom tooltip
    $('.block-transactions').each(function() {
        var blockTx = $(this);
        if (blockTx.children().length === 0) {
            blockTx.attr('title', 'No regular transaction in block');
        }
    });

    $('.block-rows [title]').each(function() {
        var tooltipElement = $(this);
        try {
            // parse the content
            var data = JSON.parse(tooltipElement.attr('title'));
            var newContent;
            if (data.object === "Vote") {
                newContent = `<b>${data.object} (${data.voteValid ? "Yes" : "No"})</b>`;
            }
            else {
                newContent = `<b>${data.object}</b><br>${data.total} DCR`;
            }

            if (data.vin && data.vout) {
                newContent += `<br>${data.vin} Inputs, ${data.vout} Outputs`
            }
            
            tooltipElement.attr('title', newContent);
        }
        catch (error) {}
    });

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
}

// on load (js file is loaded after loading html content)
window.addEventListener('resize', refreshBlocksDisplay);