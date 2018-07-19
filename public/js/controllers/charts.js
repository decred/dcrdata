(() => {
    var currentGraph = 'ticket-price';

    function ticketsFunc(gData){
        d = [];
        gData.time.forEach((n, i) => { d.push([new Date (n*1000), gData.valuef[i]]);});
        return d
    }

    function difficultyFunc(gData){
        d = [];
        gData.time.forEach((n, i) => { d.push([new Date(n*1000), gData.difficulty[i]])});
        return d
    }

    function supplyFunc (gData){
        d = [];
        gData.time.forEach((n, i) => {d.push([new Date(n*1000), gData.valuef[i]])});
        return d
    }

    function timeBtwBlocksFunc(gData){
        d = [];
        gData.value.forEach((n, i) => { if (n === 0) {return} d.push([n, gData.valuef[i]])});
        return d
    }

    function blockSizeFunc(gData){
        d = [];
        gData.time.forEach((n, i) => {d.push([new Date(n*1000), gData.size[i]])});
        return d
    }

    function blockChainSizeFunc(gData){
        d = [];
        gData.time.forEach((n, i) => {d.push([new Date(n*1000), gData.chainsize[i]])});
        return d
    }

    function txPerBlockFunc(gData){
        d = [];
        gData.value.forEach((n, i) => {d.push([n, gData.count[i]])});
        return d
    }

    function txPerDayFunc (gData){
        d = [];
        gData.timestr.forEach((n, i) => {d.push([new Date(n), gData.count[i]])});
        return d
    }

    function poolSizeFunc(gData){
        d = [];
        gData.time.forEach((n, i) =>{d.push([new Date(n*1000), gData.sizef[i]])});
        return d
    }

    function poolValueFunc(gData) {
        d = [];
        gData.time.forEach((n, i) => {d.push([new Date(n*1000), gData.valuef[i]])});
        return d
    }

    function blockFeeFunc (gData){
        d = [];
        gData.count.forEach((n,i) => {d.push([n, gData.sizef[i]]);}); 
        return d
    }

    function ticketSpendTypeFunc(gData) {
        d = [];
        gData.height.forEach((n,i) => {d.push([n, gData.unspent[i], gData.revoked[i]])}); 
        return d
    }

    function ticketByOutputCountFunc(gData) {
        d = [];
        gData.height.forEach((n,i) => {d.push([n, gData.solo[i], gData.pooled[i]]);}); 
        return d
    }

    function mapDygraphOptions(data,labelsVal, isDrawPoint, yLabel, xLabel, titleName, labelsMG, labelsMG2){
        return { 
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
            colors: ['rgb(0,128,127)']
        }
    }

    function getAPIData(gType, callback, g){
        $.ajax({
            type: 'GET',
            url: '/api/chart/'+gType,
            beforeSend: function() {},
            success: function(data) {
                callback(gType, data, g);
            }
        });
    }

    function formatter(data) {
        if (data.x == null) return '';
        var html = this.getLabels()[0] + ': ' + data.xHTML;
        data.series.forEach(function(series){
            var labeledData = `<span style="color: ` + series.color + ';">' +series.labelHTML + ': ' + series.yHTML;
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

    function plotGraph (value, data, g){
        $('.chart-btn-display').hide()
        $('.block-chart-btn-display').hide()
        var gOptions = {}
        switch(value){
            case 'ticket-price': // price graph
                d = ticketsFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Price'], true, 'Price (Decred)', 'Date', 'Ticket Price', false, false)
            break;

            case 'ticket-pool-size': // pool size graph
                d = poolSizeFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Ticket Pool Size'], false, 'Ticket Pool Size', 'Date', 
                'Ticket Pool Size', true, false)
            break;
            
            case 'ticket-pool-value': // pool value graph
                d = poolValueFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Ticket Pool Value'], true, 'Ticket Pool Value','Date', 
                'Ticket Pool Value', true, false)
            break;
            
            case 'avg-block-size': // block size graph
                d = blockSizeFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Block Size'], false, 'Block Size', 'Date','Average Block Size', true, false)
            break;

            case 'blockchain-size': // blockchain size graph
                d = blockChainSizeFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'BlockChain Size'], true, 'BlockChain Size', 'Date', 'BlockChain Size', false, true)
            break;

            case 'tx-per-block':  // tx per block graph
                d = txPerBlockFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Number of Transactions Per Block'], false, '# of Transactions', 'Date',
                'Number of Transactions Per Block', false, false)
            break;

            case 'tx-per-day': // tx per day graph
                d = txPerDayFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Number of Transactions Per Day'], true, '# of Transactions', 'Date', 
                'Number of Transactions Per Day', true, false)
            break;

            case 'pow-difficulty': // difficulty graph
                d = difficultyFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Difficulty'], true, 'Difficulty', 'Date', 'PoW Difficulty', true, false)
            break;
            
            case 'coin-supply': // supply graph
                d = supplyFunc(data)
                gOptions = mapDygraphOptions(d, ['Date', 'Coin Supply'], true, 'Coin Supply', 'Date', 'Total Coin Supply', true, false)
            break;

            case 'fee-per-block': // block fee graph
                d = blockFeeFunc(data)
                gOptions = mapDygraphOptions(d, ['Block Height', 'Total Fee'], false, 'Total Fee (DCR)', 'Block Height', 
                'Total Fee Per Block', true, false)
            break;

            case 'duration-btw-blocks': // Duration between blocks graph
                d = timeBtwBlocksFunc(data)
                gOptions = mapDygraphOptions(d, ['Block Height', 'Duration Between Block'], false, 'Duration Between Block (Seconds)', 'Block Height',
                'Duration Between Blocks', false, false)
            break;

            case 'ticket-spend-type': // Tickets spendtype per block graph
                d = ticketSpendTypeFunc(data)
                gOptions = mapDygraphOptions(d, ['Block Height', 'Unspent', 'Revoked'], false, '# of Tickets Spend Type', 'Block Height',
                'Tickets Spend Types', false, false)
                gOptions.fillGraph = true
                gOptions.stackedGraph = true
                gOptions.colors = ['orange', 'red']
                gOptions.plotter = barchartPlotter
            break;

            case 'ticket-by-outputs-windows': // Tickets by output count graph for ticket windows
                d = ticketByOutputCountFunc(data)
                gOptions = mapDygraphOptions(d, ['Window', 'Solo', 'Pooled'], false, '# of Tickets By Output Count', 'Ticket Price Window',
                'Tickets Outputs Count By Ticket Price Window', false, false)
                gOptions.fillGraph = true
                gOptions.stackedGraph = true
                gOptions.colors = ['orange', 'rgb(0,153,0)']
                gOptions.plotter = barchartPlotter
                $('.chart-btn-display').show();
                break;

            case 'ticket-by-outputs-blocks': // Tickets by output count graph for all blocks
                d = ticketByOutputCountFunc(data)
                height = data.height[data.height.length]
                gOptions = mapDygraphOptions(d, ['Block Height', 'Solo', 'Pooled'], false, '# of Tickets By Output Count', 'Block Height',
                'Tickets Outputs Count By All Blocks', false, false)
                gOptions.fillGraph = true
                gOptions.stackedGraph = true 
                gOptions.colors = ['orange', 'rgb(0,153,0)']
                gOptions.plotter = barchartPlotter
                $('.chart-btn-display').show();
                $('.block-chart-btn-display').show()
            break;
        }
        
        g.resetZoom();
        g.updateOptions(gOptions, false);
        $('body').removeClass('loading');
    }

    app.register('charts', class extends Stimulus.Controller {
        static get targets() {
            return ['chartsview', 'options', 'groupings', 'zoom']
        } 

        connect() {
            $.getScript('/js/dygraphs.min.js', () => {
                this.drawInitialGraph()
            });
        } 

        disconnect(){
            if (this.chartsview != undefined) {
                this.chartsview.destroy()
            }
        }

        drawInitialGraph(){
            this.chartsview = new Dygraph(
                document.getElementById('graphdiv'),
                ticketsFunc(ticketsPrice),
                {
                    digitsAfterDecimal: 8,
                    showRangeSelector: true,
                    drawPoints: true,
                    labels: ['Date', 'Price'],
                    legend: 'follow',
                    legendFormatter: formatter,
                    ylabel: 'Price (Decred)',
                    xlabel: 'Date',
                    title: 'Ticket Price',
                    labelsSeparateLines: true,
                    plotter: Dygraph.Plotters.linePlotter
                }); 
            }

        onChange(){
            $('body').addClass('loading');
            var selected = this.options
            if (currentGraph != selected) {
                getAPIData(selected, plotGraph, this.chartsview)
                currentGraph = selected;
            } else {
                $('body').removeClass('loading');
            }
        }

        onGrouping(){
            $('body').addClass('loading');
            var selected = this.groupings
            if (currentGraph != selected) {
                getAPIData(selected, plotGraph, this.chartsview)
                currentGraph = selected;
            } else {
                $('body').removeClass('loading');
            }
        }

        onZoom(){
            $('body').addClass('loading');
            this.chartsview.resetZoom();
            this.xVal = this.chartsview.xAxisExtremes();
            if (this.zoom > 0) {
                this.chartsview.updateOptions({
                    dateWindow:[(this.xVal[1]- this.zoom), this.xVal[1]]
                });
            }
            $('body').removeClass('loading');
        }

        get options() {
            var selectedValue = this.optionsTarget
            return selectedValue.options[selectedValue.selectedIndex].value;
          }

        get groupings(){
            return this.groupingsTarget.getElementsByClassName("btn-active")[0].name
        }

        get zoom(){
            return parseInt(this.zoomTarget.getElementsByClassName("btn-active")[0].name)
        }
     });
})()
