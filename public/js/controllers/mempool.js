(() => {

    function incrementValue($el) {
        console.log("incrementValue", $el)
        $el.text(
            parseInt($el.text()) + 1
        )
    }

    function makeNewTable(tx) {
        var newTable = ""
        newTable += '<tr><td class="break-word"><span><a class="hash" href="/tx/' +
        tx.hash + '">' + tx.hash + '</a></span></td>' +
        '<td class="mono fs15 text-right">' + humanize.decimalParts(tx.total, false, 8, true) + '</td>' +
        '<td class="mono fs15 text-right">' + tx.size + ' B</td>' +
        '<td class="mono fs15 text-right" data-target="main.age" data-age="' + tx.time +'">' + humanize.timeSince(tx.time) + '</td></tr>'
        return newTable
    }

    function addTxRow(tx, target) {
        var rows = $(target).find('tr')
        var newRow = '<tr class="flash"><td class="break-word"><span><a class="hash" href="/tx/' +
            tx.hash + '">' + tx.hash + '</a></span></td>' +
            '<td class="mono fs15 text-right"> ' + humanize.decimalParts(tx.total, false, 8, true) +'</td>' +
            '<td class="mono fs15 text-right">' + tx.size + ' B</td>' +
            '<td class="mono fs15 text-right" data-age="' + tx.time +'">' + humanize.timeSince(tx.time) +'</td>'
        var newRowHtml = $.parseHTML(newRow)
        $(newRowHtml).insertBefore(rows.first())
    }

    app.register("mempoolMini", class extends Stimulus.Controller {
        static get targets() {
            return [ "transactions"]
        }

        connect() {
            ws.registerEvtHandler("newtx", (evt) => { 
                var txs = JSON.parse(evt)
                this.renderLatestTransactions(txs)
            })
        }

        disconnect() {
            ws.deregisterEvtHandlers("newtx")
        }

        renderLatestTransactions(txs) {
            _.each(txs, (tx, idx) => {
                var rows = $(this.transactionsTarget).find(".flex-table-row")
                rows.last().remove()
                $($.parseHTML(`<div class="d-flex flex-table-row flash">
                    <a class="hash truncate-hash" style="flex: 1 1 auto" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a>
                    <span style="flex: 0 0 60px" class="mono text-right ml-1">${tx.Type}</span>
                    <span style="flex: 0 0 105px" class="mono text-right ml-1">${humanize.decimalParts(tx.total, false, 8, true)}</span>
                    <span style="flex: 0 0 50px" class="mono text-right ml-1">${tx.size} B</span>
                    <span style="flex: 0 0 65px" class="mono text-right ml-1" data-target="main.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</span>
                </div>`)).insertBefore(rows.first())
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
            ]
        }

        connect() {
            console.log("connect mempool", this)
            ws.registerEvtHandler("newtx", (e) => {
                this.renderNewTxns(e)
            })
            ws.registerEvtHandler("mempool", (e) => {
                this.updateMempool(e)
                ws.send("getmempooltxs", "")
            });
            ws.registerEvtHandler("getmempooltxsResp", (e) => {
                this.handleTxsResp(e)                    
            })
        }

        disconnect() {
            console.log("dismempool mempool")
            ws.deregisterEvtHandlers("newtx")
            ws.deregisterEvtHandlers("mempool")
            ws.deregisterEvtHandlers("getmempooltxsResp")
        }

        updateMempool(e) {
            var m = JSON.parse(e);
            console.log("updateMempool", m, this);
            $(this.numTicketTarget).text(m.num_tickets)
            $(this.numVoteTarget).text(m.num_votes)
            $(this.numRegularTarget).text(m.num_regular)
            $(this.numRevokeTarget).text(m.num_revokes)
            $(this.bestBlockTarget).text(m.block_height)
            $(this.bestBlockTimeTarget).attr('href', '/block/' + m.block_height)
            $(this.bestBlockTimeTarget).data('age', m.block_time)
            $(this.mempoolSizeTarget).text(m.formatted_size)
        }

        handleTxsResp(event) {
            var m = JSON.parse(event)
            console.log("handleTxsResp: ", event, m, this)
            
            var ticketsTable = _.map(m.tickets, makeNewTable).join("")
            $(this.ticketTransactionsTarget).html($.parseHTML(ticketsTable))
    
            var transactionsTable = _.map(m.tx, makeNewTable).join("")
            $(this.regularTransactionsTarget).html($.parseHTML(transactionsTable))
 
            if (m.num_revokes > 0) {
                var revokesTable = _.map(m.revokes, makeNewTable).join("")
                $(this.revokeTransactionsTarget).html($.parseHTML(revokesTable))
            }
            // TODO handle no revokes scenario properly

            var votesTable = ''
            _.each(m.votes, (tx) => {
                var rows = $(this.voteTransactionsTarget).find('tr')
                votesTable += '<tr><td class="break-word"><span><a class="hash" title="(' + tx.vote_info.mempool_ticket_index +
                    ') ' + tx.vote_info.ticket_spent + '" href="/tx/' + tx.hash + '">' + tx.hash + '</a></span></td>' +
                    '<td class="mono fs15">' + tx.vote_info.vote_version + '</td>' +
                    '<td class="mono fs15">' + tx.vote_info.block_validation.validity + '</td>' +
                    '<td class="mono fs15">' + tx.vote_info.vote_choices[0].id + '</td>' +
                    '<td class="mono fs15">' + tx.vote_info.vote_choices[0].choice.Id + '</td>' +
                    '<td class="mono fs15 text-right"> ' + humanize.decimalParts(tx.total, false, 8, true) + '</td>' +
                    '<td class="mono fs15">' + tx.size + ' B</td>'  +
                    '<td class="mono fs15 text-right" data-age="' + tx.time +'">' + humanize.timeSince(tx.time) + '</td>'
            })
            $(this.voteTranscationsTarget).html($.parseHTML(votesTable))
        }

        renderNewTxns(evt) {
            console.log("mempool renderNewTx", evt, this);
            var txs = JSON.parse(evt)
            _.each(txs, (tx, idx) => {
                incrementValue($(this["num" + tx.Type + "Target"]))
                switch (tx.Type) {
                    case "Vote":
                        var rows = $(this[tx.Type.toLowerCase() + "TransactionsTarget"]).find('tr')
                        var newRow = '<tr><td class="break-word"><span><a class="hash" title="(' + tx.vote_info.mempool_ticket_index +
                            ') ' + tx.vote_info.ticket_spent + '" href="/tx/' + tx.hash + '">' + tx.hash + '</a></span></td>' +
                            '<td class="mono fs15">' + tx.vote_info.vote_version + '</td>' +
                            '<td class="mono fs15">' + tx.vote_info.block_validation.validity + '</td>' +
                            '<td class="mono fs15">' + tx.vote_info.vote_choices[0].id + '</td>' +
                            '<td class="mono fs15">' + tx.vote_info.vote_choices[0].choice.Id + '</td>' +
                            '<td class="mono fs15 text-right"> ' + humanize.decimalParts(tx.total, false, 8, true) + '</td>' +
                            '<td class="mono fs15">' + tx.size + ' B</td>' +
                            '<td class="mono fs15 text-right" data-age="' + tx.time +'">' + humanize.timeSince(tx.time) + '</td>'
                        var newRowHtml = $.parseHTML(newRow)
                        $(newRowHtml).insertBefore(rows.first())
                        break
                    default:
                        addTxRow(tx, this[tx.Type.toLowerCase() + "TransactionsTarget"])
                }
            })
        }
    })
    
})()