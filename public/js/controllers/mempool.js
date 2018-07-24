(() => {

    function incrementValue($el) {
        if ($el.size() > 0) {
            $el.text(
                parseInt($el.text()) + 1
            )
        }
    }

    function txTableRow(tx) {
        return `<tr class="flash">
            <td class="break-word"><span><a class="hash" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a></span></td>
            <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8, true)}</td>
            <td class="mono fs15 text-right">${tx.size} B</td>
            <td class="mono fs15 text-right" data-target="main.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
        </tr>`
    }

    function voteTxTableRow(tx) {
        return `<tr class="flash" data-height="${tx.vote_info.block_validation.height}" data-blockhash="${tx.vote_info.block_validation.hash}">
            <td class="break-word"><span><a class="hash" href="/tx/${tx.hash}">${tx.hash}</a></span></td>
            <td class="mono fs15"><span><a href="/block/${tx.vote_info.block_validation.height}">${tx.vote_info.block_validation.height}</a></span></td>
            <td class="mono fs15"><a href="/tx/${tx.vote_info.ticket_spent}">${tx.vote_info.mempool_ticket_index}<a/></td>
            <td class="mono fs15">${tx.vote_info.vote_version}</td>
            <td class="mono fs15 last_block">${tx.vote_info.last_block?'Valid':'Invalid'}</td>
            <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8, true)}</td>
            <td class="mono fs15">${tx.size} B</td>
            <td class="mono fs15 text-right" data-target="main.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
        </tr>`
    }

    function txFlexTableRow(tx) {
        return `<div class="d-flex flex-table-row flash">
            <a class="hash truncate-hash" style="flex: 1 1 auto" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a>
            <span style="flex: 0 0 60px" class="mono text-right ml-1">${tx.Type}</span>
            <span style="flex: 0 0 105px" class="mono text-right ml-1">${humanize.decimalParts(tx.total, false, 8, true)}</span>
            <span style="flex: 0 0 50px" class="mono text-right ml-1">${tx.size} B</span>
            <span style="flex: 0 0 65px" class="mono text-right ml-1" data-target="main.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</span>
        </div>`
    }

    function buildTable(target, txType, txns, rowFn) {
        if (txns && txns.length > 0) {
            var tableBody = _.map(txns, rowFn).join("")
        } else {
            var tableBody = `<tr><td colspan="${(txType === 'votes' ? 8 : 4)}">No ${txType} in mempool.</td></tr>`
        }
        $(target).html($.parseHTML(tableBody))
    }

    function addTxRow(tx, target, rowFn) {
        var rows = $(target).find('tr')
        var newRowHtml = $.parseHTML(rowFn(tx))
        $(newRowHtml).insertBefore(rows.first())
    }

    app.register("homepageMempool", class extends Stimulus.Controller {
        static get targets() {
            return [
                "transactions",
                "numVote",
                "numTicket"
            ]
        }

        connect() {
            ws.registerEvtHandler("newtx", (evt) => {
                var txs = JSON.parse(evt)
                this.renderLatestTransactions(txs, true)
                keyNav(evt, false, true)
            })
            ws.registerEvtHandler("mempool", (evt) => {
                var m = JSON.parse(evt)
                this.renderLatestTransactions(m.latest, false)
                $(this.numTicketTarget).text(m.num_tickets)
                $(this.numVoteTarget).text(m.num_votes)
                keyNav(evt, false, true)
                ws.send("getmempooltxs", "")
            });
            ws.registerEvtHandler("getmempooltxsResp", (evt) => {
                var m = JSON.parse(evt)
                this.renderLatestTransactions(m.latest, true)
                keyNav(evt, false, true)
            })
        }

        disconnect() {
            ws.deregisterEvtHandlers("newtx")
            ws.deregisterEvtHandlers("mempool")
            ws.deregisterEvtHandlers("getmempooltxsResp")
        }

        renderLatestTransactions(txs, incremental) {
            _.each(txs, (tx, idx) => {
                if (incremental) {
                    incrementValue($(this["num" + tx.Type + "Target"]))
                }
                var rows = $(this.transactionsTarget).find(".flex-table-row")
                rows.last().remove()
                $($.parseHTML(txFlexTableRow(tx))).insertBefore(rows.first())
            })
        }
    })

    app.register("mempool", class extends Stimulus.Controller {
        static get targets() {
            return [
                "bestBlock",
                "bestBlockTime",
                "mempoolSize",
                "numVote",
                "numTicket",
                "numRevoke",
                "numRegular",
                "voteTransactions",
                "ticketTransactions",
                "revokeTransactions",
                "regularTransactions",
                "totalCollected",
                "totalNeeded",
                "totalOut",
            ]
        }

        connect() {
            ws.registerEvtHandler("newtx", (evt) => {
                this.renderNewTxns(evt)
                this.filterVotesLastBlock();
                this.sortVotesTable();
                keyNav(evt, false, true)
            })
            ws.registerEvtHandler("mempool", (evt) => {
                this.updateMempool(evt)
                ws.send("getmempooltxs", "")
            });
            ws.registerEvtHandler("getmempooltxsResp", (evt) => {
                this.handleTxsResp(evt)
                this.filterVotesLastBlock();
                this.sortVotesTable();
                keyNav(evt, false, true)
            })
        }

        disconnect() {
            ws.deregisterEvtHandlers("newtx")
            ws.deregisterEvtHandlers("mempool")
            ws.deregisterEvtHandlers("getmempooltxsResp")
        }

        updateMempool(e) {
            var m = JSON.parse(e);
            $(this.numTicketTarget).text(m.num_tickets)
            $(this.numVoteTarget).text(m.num_votes)
            $(this.numRegularTarget).text(m.num_regular)
            $(this.numRevokeTarget).text(m.num_revokes)
            $(this.bestBlockTarget).text(m.block_height)
            $(this.bestBlockTarget).data("hash",m.block_hash)
            $(this.bestBlockTarget).attr("data-hash",m.block_hash)
            $(this.bestBlockTarget).attr('href', '/block/' + m.block_height)
            $(this.bestBlockTimeTarget).data('age', m.block_time)
            $(this.bestBlockTimeTarget).attr('data-age', m.block_time)
            $(this.mempoolSizeTarget).text(m.formatted_size)
            $(this.totalCollectedTarget).text(m.voting_info.total_votes_collected)
            $(this.totalNeededTarget).text(m.voting_info.total_votes_required)
            $(this.totalOutTarget).html(`${humanize.decimalParts(m.total, false, 8, true)}`);
            $(this.mempoolSizeTarget).text(m.formatted_size);
            this.filterVotesLastBlock();
        }

        handleTxsResp(event) {
            var m = JSON.parse(event)
            buildTable(this.regularTransactionsTarget, 'regular transactions', m.tx, txTableRow)
            buildTable(this.revokeTransactionsTarget, 'revocations', m.revokes, txTableRow)
            buildTable(this.voteTransactionsTarget, 'votes', m.votes, voteTxTableRow)
            buildTable(this.ticketTransactionsTarget, 'tickets', m.tickets, txTableRow)
        }

        renderNewTxns(evt) {
            var txs = JSON.parse(evt)
            _.each(txs, (tx, idx) => {
                incrementValue($(this["num" + tx.Type + "Target"]))
                var rowFn = tx.Type === "Vote" ? voteTxTableRow : txTableRow
                addTxRow(tx, this[tx.Type.toLowerCase() + "TransactionsTarget"], rowFn)
            })
        }

        filterVotesLastBlock() {
            var bestBlockHash=$(this.bestBlockTarget).data("hash");
            var bestBlockHeight=$(this.bestBlockTarget).text();
            $(this.voteTransactionsTarget).children("tr").each(function(i,el) {
                var voteValidationHash=$(this).data("blockhash");
                var voteBlockHeight=$(this).data("height");
                if(voteBlockHeight>bestBlockHeight) {
                    $(this).closest("tr").addClass("upcoming-vote");
                    $(this).closest("tr").removeClass("old-vote");
                }else if(voteValidationHash!=bestBlockHash){
                    $(this).closest("tr").addClass("old-vote");
                    $(this).closest("tr").removeClass("upcoming-vote");
                    $(this).find("td.last_block").text("Invalid");
                }else{
                    $(this).closest("tr").removeClass("old-vote");
                    $(this).closest("tr").removeClass("upcoming-vote");
                    $(this).find("td.last_block").text("Valid");
                }
            })
        };

        sortVotesTable() {
            var $rows = $(this.voteTransactionsTarget).children("tr");
            $rows.sort(function(a, b){
                var heightA = parseInt($('td:nth-child(2)',a).text());
                var heightB = parseInt($('td:nth-child(2)',b).text());
                if(heightA == heightB) {
                     var indexA = parseInt($('td:nth-child(3)',a).text());
                     var indexB = parseInt($('td:nth-child(3)',b).text());
                     return (indexA - indexB);
                }else{
                    return (heightB - heightA);
                }
            });
            $(this.voteTransactionsTarget).html($rows);
        }
    })
})()