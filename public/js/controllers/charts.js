(() => {

    var selectedChart;

    function legendFormatter(data) {
        if (data.x == null) {
            return `<div class="d-flex flex-wrap justify-content-center align-items-center">
                <div class="pr-3">${this.getLabels()[0]}: N/A</div>
                <div class="d-flex flex-wrap">
                ${_.map(data.series,(series) => {
                    return `<div class="pr-2">${series.dashHTML} ${series.labelHTML}</div>`
                }).join("")}
                </div>
            </div>
            `
        }

        if (selectedChart == 'ticket-by-outputs-windows') {
            let start = data.x * 144
            let end = start + 143
            var xAxisAdditionalInfo = ` (Blocks ${start} &mdash; ${end})`
        } else {
            var xAxisAdditionalInfo = ""
        }

        return `<div class="d-flex flex-wrap justify-content-center align-items-center">
            <div class="pr-3">${this.getLabels()[0]}: ${data.xHTML}${xAxisAdditionalInfo}</div>
            <div class="d-flex flex-wrap">
            ${_.map(data.series,(series) => {
                if (!series.isVisible) return;
                return `<div class="pr-2">${series.dashHTML} ${series.labelHTML}: ${series.yHTML}</div>`
            }).join("")}
            </div>
        </div>
        `
    }

    function nightModeOptions(nightModeOn) {
        if (nightModeOn) {
            return {
                rangeSelectorAlpha: .3,
                gridLineColor: "#596D81",
                colors: ['#2DD8A3', '#2970FF']
            }
        }
        return {
            rangeSelectorAlpha: .4,
            gridLineColor: "#C4CBD2",
            colors: ['#2970FF','#2DD8A3']
        }
    }

    function ticketsFunc(gData){
        return _.map(gData.time, (n,i) => {
            return [
                new Date (n*1000),
                gData.valuef[i]
            ]
        })
    }

    function difficultyFunc(gData){
        return _.map(gData.time, (n, i) => { return [new Date(n*1000), gData.difficulty[i]] })
    }

    function supplyFunc (gData){
       return _.map(gData.time, (n, i) => { return [new Date(n*1000), gData.valuef[i]] });
    }

    function timeBtwBlocksFunc(gData){
        var d = [];
        gData.value.forEach((n, i) => { if (n === 0) {return} d.push([n, gData.valuef[i]])});
        return d
    }

    function blockSizeFunc(gData){
        return _.map(gData.time,(n,i) => {
            return [new Date(n*1000), gData.size[i]]
        })
    }

    function blockChainSizeFunc(gData){
        return _.map(gData.time,(n,i) => { return [new Date(n*1000), gData.chainsize[i]] })
    }

    function txPerBlockFunc(gData){
        return _.map(gData.value,(n,i) => { return [n, gData.count[i]] })
    }

    function txPerDayFunc (gData){
        return _.map(gData.timestr,(n,i) => { return [new Date(n), gData.count[i]] })
    }

    function poolSizeFunc(gData){
        return _.map(gData.time,(n,i) => { return [new Date(n*1000), gData.sizef[i]] })
    }

    function poolValueFunc(gData) {
        return _.map(gData.time,(n,i) => { return [new Date(n*1000), gData.valuef[i]] })
    }

    function blockFeeFunc (gData){
        return _.map(gData.count, (n,i) => { return [n, gData.sizef[i]] })
    }

    function ticketSpendTypeFunc(gData) {
        return _.map(gData.height, (n,i) => { return [n, gData.unspent[i], gData.revoked[i]] })
    }

    function ticketByOutputCountFunc(gData) {
        return _.map(gData.height, (n,i) => { return [n, gData.solo[i], gData.pooled[i]] })
    }

    function mapDygraphOptions(data,labelsVal, isDrawPoint, yLabel, xLabel, titleName, labelsMG, labelsMG2){
        return _.merge({
            'file': data,
            digitsAfterDecimal: 8,
            labels: labelsVal,
            drawPoints: isDrawPoint,
            ylabel: yLabel,
            xlabel: xLabel,
            labelsKMB: labelsMG,
            labelsKMG2: labelsMG2,
            title: titleName,
            fillGraph: false,
            stackedGraph: false,
            plotter: Dygraph.Plotters.linePlotter,
        }, nightModeOptions(darkEnabled()))
    }

    function getAPIData(chartType, controllerContext){
        return $.ajax({
            type: 'GET',
            url: '/api/chart/' + chartType,
            context: controllerContext
        });
    }

    app.register('charts', class extends Stimulus.Controller {
        static get targets() {
            return [
                'chartWrapper',
                'labels',
                'chartsView',
                'chartSelect',
                'zoomSelector',
                'zoomOption',
                'rollPeriodInput',
            ]
        }

        connect() {
            $.getScript('/js/dygraphs.min.js', () => {
                this.drawInitialGraph()
                $(document).on("nightMode", this, function(event,params) {
                    event.data.chartsView.updateOptions(
                        nightModeOptions(params.nightMode)
                    );
                })

            });

        }

        disconnect(){
            if (this.chartsView != undefined) {
                this.chartsView.destroy()
            }
            selectedChart = null
        }

        drawInitialGraph(){
            var options = {
                digitsAfterDecimal: 8,
                showRangeSelector: true,
                rangeSelectorPlotFillColor: '#8997A5',
                rangeSelectorPlotFillGradientColor: "",
                rangeSelectorPlotStrokeColor: "",
                rangeSelectorAlpha: .4,
                rangeSelectorHeight: 40,
                drawPoints: true,
                pointSize: .25,
                labelsSeparateLines: true,
                plotter: Dygraph.Plotters.linePlotter,
                labelsDiv: this.labelsTarget,
                legend: 'always',
                legendFormatter: legendFormatter,
                highlightCircleSize: 4,
            }

            this.chartsView = new Dygraph(
                this.chartsViewTarget,
                [[1,1]],
                options
            );
            $(this.chartSelectTarget).val(window.location.hash.replace("#","") || 'ticket-price')
            this.selectChart()
        }

        plotGraph(chartName, data) {
            var d = []
            Turbolinks.controller.replaceHistoryWithLocationAndRestorationIdentifier(Turbolinks.Location.wrap(`#${chartName}`), Turbolinks.uuid())
            var gOptions = {
                rollPeriod: 1
            }
            switch(chartName){
                case 'ticket-price': // price graph
                    d = ticketsFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Price'], true, 'Price (Decred)', 'Date', undefined, false, false))
                break;

                case 'ticket-pool-size': // pool size graph
                    d = poolSizeFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Pool Size'], false, 'Ticket Pool Size', 'Date',
                    undefined, true, false))
                break;

                case 'ticket-pool-value': // pool value graph
                    d = poolValueFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Pool Value'], true, 'Ticket Pool Value','Date',
                    undefined, true, false))
                break;

                case 'avg-block-size': // block size graph
                    d = blockSizeFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Block Size'], false, 'Block Size', 'Date', undefined, true, false))
                break;

                case 'blockchain-size': // blockchain size graph
                    d = blockChainSizeFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'BlockChain Size'], true, 'BlockChain Size', 'Date', undefined, false, true))
                break;

                case 'tx-per-block':  // tx per block graph
                    d = txPerBlockFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Number of Transactions Per Block'], false, '# of Transactions', 'Date',
                    undefined, false, false))
                break;

                case 'tx-per-day': // tx per day graph
                    d = txPerDayFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Number of Transactions Per Day'], true, '# of Transactions', 'Date',
                    undefined, true, false))
                break;

                case 'pow-difficulty': // difficulty graph
                    d = difficultyFunc(data)
                    _.assign(gOptions,  mapDygraphOptions(d, ['Date', 'Difficulty'], true, 'Difficulty', 'Date', undefined, true, false))
                break;

                case 'coin-supply': // supply graph
                    d = supplyFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Date', 'Coin Supply'], true, 'Coin Supply', 'Date', undefined, true, false))
                break;

                case 'fee-per-block': // block fee graph
                    d = blockFeeFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Total Fee'], false, 'Total Fee (DCR)', 'Block Height',
                    undefined, true, false))
                break;

                case 'duration-btw-blocks': // Duration between blocks graph
                    d = timeBtwBlocksFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Duration Between Block'], false, 'Duration Between Block (Seconds)', 'Block Height',
                    undefined, false, false))
                break;

                case 'ticket-spend-type': // Tickets spendtype per block graph
                    d = ticketSpendTypeFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Unspent', 'Revoked'], false, '# of Tickets Spend Type', 'Block Height',
                    undefined, false, false),{
                        fillGraph: true,
                        stackedGraph: true,
                        plotter: barchartPlotter
                    })
                break;

                case 'ticket-by-outputs-windows': // Tickets by output count graph for ticket windows
                    d = ticketByOutputCountFunc(data)
                    _.assign(gOptions, mapDygraphOptions(d, ['Window', '3 Outputs (likely Solo)', '5 Outputs (likely Pooled)'], false, '# of Tickets By Output Count', 'Ticket Price Window',
                    undefined, false, false), {
                        fillGraph: true,
                        stackedGraph: true,
                        plotter: barchartPlotter,
                    })
                    break;
                case 'ticket-by-outputs-blocks': // Tickets by output count graph for all blocks
                    d = ticketByOutputCountFunc(data)
                    _.assign(gOptions, mapDygraphOptions(
                        d,
                        ['Block Height', 'Solo', 'Pooled'],
                        false,
                        '# of Tickets By Output Count',
                        'Block Height',
                        undefined,
                        false,
                        false
                    ), {
                        fillGraph: true,
                        stackedGraph: true,
                        plotter: barchartPlotter
                    })
                break;
            }
            this.chartsView.updateOptions(gOptions, false);
            this.chartsView.resetZoom();
            $(this.chartWrapperTarget).removeClass('loading');
        }

        selectChart(e){
            var selection = this.chartSelectTarget.value
            $(this.rollPeriodInputTarget).val(undefined)
            $(this.chartWrapperTarget).addClass('loading');
            if (selectedChart != selection) {
                getAPIData(selection, this).success((data) => {
                    console.log("got api data", data, this, selection)
                    this.plotGraph(selection, data)
                })
                selectedChart = selection;
            } else {
                $(this.chartWrapperTarget).removeClass('loading');
            }
            this.zoomOptionTargets.forEach((el) => {
                el.classList.toggle("active", $(el).data('zoom') == 0)
            })
        }

        async onZoom(event){
            var selectedZoom = parseInt($(event.srcElement).data('zoom'))
            await animationFrame()
            $(this.chartWrapperTarget).addClass('loading');
            await animationFrame()
            this.xVal = this.chartsView.xAxisExtremes();
            if (selectedZoom > 0) {
                if (this.xVal[0] >= 1400000000000) {
                    var normalizedZoom = selectedZoom * 1000 * 300
                } else if (selectedChart === 'ticket-by-outputs-windows') {
                    var normalizedZoom = selectedZoom / 144
                } else {
                    var normalizedZoom = selectedZoom
                }
                this.chartsView.updateOptions({
                    dateWindow: [(this.xVal[1] - normalizedZoom), this.xVal[1]]
                });
            } else {
                this.chartsView.resetZoom();
            }
            this.zoomOptionTargets.forEach((el, i) => {
                el.classList.toggle("active", $(el).data('zoom') == selectedZoom)
            })
            await animationFrame()
            $(this.chartWrapperTarget).removeClass('loading');
        }

        async onRollPeriodChange(event){
            var rollPeriod = parseInt(event.target.value) || 1
            await animationFrame()
            $(this.chartWrapperTarget).addClass('loading');
            await animationFrame()
            this.chartsView.updateOptions({
                rollPeriod: rollPeriod
            });
            await animationFrame()
            $(this.chartWrapperTarget).removeClass('loading');
        }

     });
})()
