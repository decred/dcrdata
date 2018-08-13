(() => {
    function txTypesFunc(d){
        var p = []

        d.time.map((n, i) => {
            p.push([new Date(n*1000), d.regularTx[i], d.tickets[i], d.votes[i], d.revokeTx[i]])
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
        if (data.x == null) return '';
        var html = this.getLabels()[0] + ': ' + data.xHTML;
        data.series.map(function(series){
            var labeledData = `<span style="color: ` + series.color + ';">' +series.labelHTML + ': ' + series.y;
            html += '<br>' + series.dashHTML  + labeledData +'</span>';
        });
        return html;
    }

    function customizedFormatter(data) {
        if (data.x == null) return '';
        var html = this.getLabels()[0] + ': ' + data.xHTML;
        data.series.map(function(series){
            if (isNaN(series.y)) return '';
        
            var labeledData = `<span style="color: ` + series.color + ';">' +series.labelHTML + ': ' + series.y + ' DCR';
            html += '<br>' + series.dashHTML  + labeledData +'</span>';
        });
        return html;
    }

    function darkenColor(colorStr) {
        var color = Dygraph.toRGB_(colorStr);
        color.r = Math.floor((255 + color.r) / 2);
        color.g = Math.floor((255 + color.g) / 2);
        color.b = Math.floor((255 + color.b) / 2);
        return 'rgb(' + color.r + ',' + color.g + ',' + color.b + ')';
    }

    function barchartPlotter(e) {
        var ctx = e.drawingContext;
        var points = e.points;
        var y_bottom = e.dygraph.toDomYCoord(0);

        ctx.fillStyle = darkenColor(e.color);
        var min_sep = Infinity;

        for (var i = 1; i < points.length; i++) {
            var sep = points[i].canvasx - points[i - 1].canvasx;
            if (sep < min_sep) min_sep = sep;
        }

        var bar_width = Math.floor(2.0 / 3 * min_sep);
        for (var i = 0; i < points.length; i++) {
            var p = points[i];
            var center_x = p.canvasx;

            ctx.fillRect(center_x - bar_width / 2, p.canvasy,
                bar_width, y_bottom - p.canvasy);

            ctx.strokeRect(center_x - bar_width / 2, p.canvasy,
                bar_width, y_bottom - p.canvasy);
            }               
        } 
   
    function plotGraph(processedData, otherOptions){
        var commonOptions = {
            digitsAfterDecimal: 8,
            showRangeSelector: true,
            legend: 'follow',
            xlabel: 'Date',
            labelsSeparateLines: true,
        }

        return new Dygraph(
            document.getElementById('history-chart'),
            processedData,
            {...commonOptions, ...otherOptions}
        );
    }

    app.register('address', class extends Stimulus.Controller {
        static get targets(){
            return ['options', 'addr', 'btns', 'unspent', 'flow']
        }

        initialize(){
            var _this = this
            $.getScript('/js/dygraphs.min.js', () => {
                this.typesGraphOptions = {
                    labels: ['Date', 'RegularTx', 'Tickets', 'Votes', 'RevokeTx'],
                    colors: ['#0066cc', '#006600', 'darkorange', '#ff0090'],
                    ylabel: '# of Tx Types',
                    title: 'Transactions Types Distribution',
                    visibility: [true, true, true, true],
                    legendFormatter: formatter,
                    plotter: barchartPlotter,
                    stackedGraph: true,
                    fillGraph: false,
                    labelsKMB: false
                }

                this.amountFlowGraphOptions = {
                    labels: ['Date', 'Received', 'Sent', 'Net Received', 'Net Spent'],
                    colors: ['#0066cc', 'rgb(0,128,127)', 'rgb(0,153,0)', '#ff0090'],
                    ylabel: 'Total Amount (DCR)',
                    title: 'Transactions Received And Spent Amount Distribution',
                    visibility: [true, false, false, false],
                    legendFormatter: customizedFormatter,
                    plotter: barchartPlotter,
                    stackedGraph: true,
                    fillGraph: false,
                    labelsKMB: false
                }

                this.unspentGraphOptions = {
                    labels: ['Date', 'Unspent'],
                    colors: ['rgb(0,128,127)'],
                    ylabel: 'Cummulative Unspent Amount (DCR)',
                    title: 'Transactions Unspent Amount Distribution',
                    plotter: [Dygraph.Plotters.linePlotter, Dygraph.Plotters.fillPlotter],
                    legendFormatter: customizedFormatter,
                    stackedGraph: false,
                    visibility: [true],
                    fillGraph: true,
                    labelsKMB: true
                }
            })
        }

        disconnect(){
            if (this.graph != undefined) {
                this.graph.destroy()
            }
        }

        drawGraph(graphType){
            var _this = this

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
                url: '/api/address/' + _this.addr + '/' + graphType,
                beforeSend: function() {},
                success: function(data) {
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
                    }
                    $('body').removeClass('loading');
                }
            });
        }

        changeView() {
            $('body').addClass('loading');
            var _this = this
            var divHide = 'list'
            var divShow = _this.btns

            if (divShow !== 'chart') {
                divHide = 'chart'
                $('body').removeClass('loading');
            } else {
                _this.drawGraph('types')
            }

            $('.'+divShow+'-display').removeClass('d-hide');
            $('.'+divHide+'-display').addClass('d-hide');
        }

        changeGraph(){
            $('body').addClass('loading');

            this.drawGraph(this.options)
        }

        updateFlow(){
           console.log(' >>>>>> Flow >>>', this.flow)
        }

        get options(){
            var selectedValue = this.optionsTarget
            return selectedValue.options[selectedValue.selectedIndex].value;
        }
        get addr(){
            return this.addrTarget.outerText
        }

        get unspent(){
            return this.unspentTarget.id
        }

        get btns(){
            return this.btnsTarget.getElementsByClassName("btn-active")[0].name
        }

        get flow(){
            var ar = []
            var boxes = this.flowTarget.querySelectorAll('input:checked')
            console.log('mmmm <<<<<<', boxes)
            boxes.forEach((n) => ar.push[n.value])
            return ar
        }
    })
})()