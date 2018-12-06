(() => {
    function txTypesFunc(d){
        var p = []

        d.time.map((n, i) => {
            p.push([new Date(n*1000), d.sentRtx[i], d.receivedRtx[i], d.tickets[i], d.votes[i], d.revokeTx[i]])
        });
        return p
    }

    function amountFlowFunc(d){
        var p = []

        d.time.map((n, i) => {
            var v = d.net[i]
            var netReceived = 0
            var netSent = 0

            v > 0 ? (netReceived = v) : (netSent = (v* -1))
            p.push([new Date(n*1000), d.received[i], d.sent[i], netReceived, netSent])
        });
        return p
    }

    function unspentAmountFunc(d){
        var p = []
        // start plotting 6 days before the actual day
        if (d.length > 0) {
            p.push([new Date((d.time[0] - 10000) * 1000), 0])
        }

        d.time.map((n, i) => p.push([new Date(n*1000), d.amount[i]]));
        return p
    }

    function formatter(data) {
        var html = this.getLabels()[0] + ': ' + ((data.xHTML==undefined) ? '': data.xHTML);
        data.series.map(function(series){
            if (series.color==undefined) return '';
            var l = `<span style="color: ` + series.color + ';"> ' + series.labelHTML;
            html = `<span style="color:#2d2d2d;">`+html+`</span>`
            html += '<br>' + series.dashHTML  + l + ': ' + (isNaN(series.y) ? '': series.y) + '</span>';
        });
        return html;
    }

    function customizedFormatter(data) {
        var html = this.getLabels()[0] + ': ' + ((data.xHTML==undefined) ? '': data.xHTML);
        data.series.map(function(series){
            if (series.color==undefined) return '';
            if (series.y === 0 && series.labelHTML.includes('Net')) return '';
            var l = `<span style="color: ` + series.color + ';"> ' + series.labelHTML;
            html = `<span style="color:#2d2d2d;">`+html+`</span>`
            html += '<br>' + series.dashHTML  + l + ': ' + (isNaN(series.y) ? '': series.y + ' DCR') + '</span> ';
        });
        return html;
    }

    function plotGraph(processedData, otherOptions){
        var commonOptions = {
            digitsAfterDecimal: 8,
            showRangeSelector: true,
            legend: 'follow',
            xlabel: 'Date',
            fillAlpha: 0.9,
            labelsKMB: true
        }

        return new Dygraph(
            document.getElementById('history-chart'),
            processedData,
            {...commonOptions, ...otherOptions}
        );
    }

    app.register('address', class extends Stimulus.Controller {
        static get targets(){
            return ['options', 'addr', 'btns', 'unspent',
                    'flow', 'zoom', 'interval']
        }

        initialize(){
            var _this = this
            $.getScript('/js/dygraphs.min.js', () => {
                _this.typesGraphOptions = {
                    labels: ['Date', 'Sending (regular)', 'Receiving (regular)', 'Tickets', 'Votes', 'Revocations'],
                    colors: ['#69D3F5', '#2971FF', '#41BF53', 'darkorange', '#FF0090'],
                    ylabel: 'Number of Transactions by Type',
                    title: 'Transactions Types',
                    visibility: [true, true, true, true, true],
                    legendFormatter: formatter,
                    plotter: barchartPlotter,
                    stackedGraph: true,
                    fillGraph: false
                }

                _this.amountFlowGraphOptions = {
                    labels: ['Date', 'Received', 'Spent', 'Net Received', 'Net Spent'],
                    colors: ['#2971FF', '#2ED6A1', '#41BF53', '#FF0090'],
                    ylabel: 'Total Amount (DCR)',
                    title: 'Sent And Received',
                    visibility:[true, false, false, false],
                    legendFormatter: customizedFormatter,
                    plotter: barchartPlotter,
                    stackedGraph: true,
                    fillGraph: false
                }

                _this.unspentGraphOptions = {
                    labels: ['Date', 'Unspent'],
                    colors: ['#41BF53'],
                    ylabel: 'Cummulative Unspent Amount (DCR)',
                    title: 'Total Unspent',
                    plotter: [Dygraph.Plotters.linePlotter, Dygraph.Plotters.fillPlotter],
                    legendFormatter: customizedFormatter,
                    stackedGraph: false,
                    visibility: [true],
                    fillGraph: true
                }
            })
        }

        disconnect(){
            if (this.graph != undefined) {
                this.graph.destroy()
            }
        }

        drawGraph(){
            var _this = this
            var graphType = _this.options
            var interval = _this.interval

            $('#no-bal').addClass('d-hide');
            $('#history-chart').removeClass('d-hide');
            $('#toggle-charts').addClass('d-hide');

            if (_this.unspent == "0" && graphType === 'unspent') {
                $('#no-bal').removeClass('d-hide');
                $('#history-chart').addClass('d-hide');
                $('body').removeClass('loading');
                return
            }

            $.ajax({
                type: 'GET',
                url: '/api/address/' + _this.addr + '/' + graphType +'/'+ interval,
                beforeSend: function() {},
                success: function(data) {
                    if(!_.isEmpty(data)) {
                        var newData = []
                        var options = {}

                        switch(graphType){
                            case 'types':
                                newData = txTypesFunc(data)
                                options = _this.typesGraphOptions
                                break

                            case 'amountflow':
                                newData = amountFlowFunc(data)
                                options = _this.amountFlowGraphOptions
                                $('#toggle-charts').removeClass('d-hide');
                                break

                            case 'unspent':
                                newData = unspentAmountFunc(data)
                                options = _this.unspentGraphOptions
                                break
                        }

                        if (_this.graph == undefined) {
                            _this.graph = plotGraph(newData, options)
                        } else {
                            _this.graph.updateOptions({
                                ...{'file': newData},
                                ...options})
                            _this.graph.resetZoom()
                        }
                        _this.updateFlow()
                        _this.xVal = _this.graph.xAxisExtremes()
                    }else{
                        $('#no-bal').removeClass('d-hide');
                        $('#history-chart').addClass('d-hide');
                        $('#toggle-charts').removeClass('d-hide');
                    }

                    $('body').removeClass('loading');
                }
            });
        }

        changeView() {
            var _this = this
            _this.disableBtnsIfNotApplicable()

            $('body').addClass('loading');

            var divHide = 'list'
            var divShow = _this.btns

            if (divShow !== 'chart') {
                divHide = 'chart'
                $('body').removeClass('loading');
            } else {
                _this.drawGraph()
            }

            $('.'+divShow+'-display').removeClass('d-hide');
            $('.'+divHide+'-display').addClass('d-hide');
        }

        changeGraph(){
            $('body').addClass('loading');
            this.drawGraph()
        }

        updateFlow(){
            if (this.options != 'amountflow') return ''
            for (var i = 0; i < this.flow.length; i++){
               var d = this.flow[i]
               this.graph.setVisibility(d[0], d[1])
           }
        }

        onZoom(){
            if (this.graph == undefined) {
                return
            }
            $('body').addClass('loading');
            this.graph.resetZoom();
            if (this.zoom > 0 && this.zoom < this.xVal[1]) {
                this.graph.updateOptions({
                    dateWindow:[(this.xVal[1] - this.zoom), this.xVal[1]]
                });
            }
            $('body').removeClass('loading');
        }

        disableBtnsIfNotApplicable(){
            var val = parseInt(this.addrTarget.id)*1000
            var d = new Date()

            var pastYear = d.getFullYear() - 1;
            var pastMonth = d.getMonth() - 1;
            var pastWeek = d.getDate() - 7
            var pastDay = d.getDate() - 1

            this.enabledButtons = []
            var setApplicableBtns = (className, ts) => {
                var isDisabled = (val > Number(new Date(ts))) ||
                    (this.options === 'unspent' && this.unspent == "0")

                if (isDisabled) {
                    this.zoomTarget.getElementsByClassName(className)[0].setAttribute("disabled", isDisabled)
                    this.intervalTarget.getElementsByClassName(className)[0].setAttribute("disabled", isDisabled)
                }

                if (className !== "year" && !isDisabled){
                    this.enabledButtons.push(className)
                }
            }

            setApplicableBtns('year', new Date().setFullYear(pastYear))
            setApplicableBtns('month', new Date().setMonth(pastMonth))
            setApplicableBtns('week', new Date().setDate(pastWeek))
            setApplicableBtns('day', new Date().setDate(pastDay))

            if (parseInt(this.intervalTarget.dataset.txcount) < 20 || this.enabledButtons.length === 0) {
                this.enabledButtons[0] = "all"
            }

            $("input#chart-size").removeClass("btn-active")
            $("input#chart-size." + this.enabledButtons[0]).addClass("btn-active");
        }

        get options(){
            var selectedValue = this.optionsTarget
            return selectedValue.options[selectedValue.selectedIndex].value;
        }

        get addr(){
            return this.addrTarget.dataset.address
        }

        get unspent(){
            return this.unspentTarget.id
        }

        get btns(){
            return this.btnsTarget.getElementsByClassName("btn-active")[0].name
        }

        get zoom(){
            var v = this.zoomTarget.getElementsByClassName("btn-active")[0].name
            return parseFloat(v)
        }

        get interval(){
            return this.intervalTarget.getElementsByClassName("btn-active")[0].name
        }

        get flow(){
            var ar = []
            var boxes = this.flowTarget.querySelectorAll('input[type=checkbox]')
            boxes.forEach((n) => {
                var intVal = parseFloat(n.value)
                ar.push([isNaN(intVal) ? 0 : intVal, n.checked])
                if (intVal == 2){
                    ar.push([3, n.checked])
                }
            })
            return ar
        }
    })
})()