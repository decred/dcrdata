(() => {
    function txTypesFunc(d){
        var p = []
        d.time.forEach((n, i) => {
            p.push([new Date(n*1000), d.regularTx[i], d.tickets[i], d.votes[i], d.revokeTx[i]])
        });
        return p
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
   
    function plotGraph(){
        return new Dygraph(
            document.getElementById('tx-type-chart'),
            txTypesFunc(val),
            {
                digitsAfterDecimal: 8,
                showRangeSelector: true,
                drawPoints: true,
                labels: ['Date', 'RegularTx', 'Tickets', 'Votes', 'RevokeTx'],
                legend: 'follow',
                colors: ['#0066cc', '#006600', 'darkorange', '#ff0090'],
                ylabel: '# of tx types',
                xlabel: 'Date',
                title: 'Transactions Type Distribution',
                stackedGraph: true,
                labelsSeparateLines: true,
                legendFormatter: formatter,
                plotter: barchartPlotter
            });
    }

    app.register('address', class extends Stimulus.Controller {
        static get targets(){
            return ['chart', 'qrcode', 'btns']
        }
        initialize(){
            $.getScript('/js/dygraphs.min.js', () => {})
        }

        disconnect(){
            this.graph.destroy()
        }

        changeview() {
            $('body').addClass('loading');
            var _this = this
            var divHide = 'list'
            var divShow = _this.btns

            if (divShow !== 'chart') {
                divHide = 'chart'
                $('body').removeClass('loading');
            } else {
                if (_this.graph == null) {
                    _this.graph = plotGraph()
                } else {
                    _this.updatedata({'file': "data"})
                }
            }
            $('.'+divShow+'-display').removeClass('d-hide');
            $('.'+divHide+'-display').addClass('d-hide');
        }

        updategraph(){
            
        }

        get chart(){
            return this.chartTarget.name
        }

        get btns(){
            return this.btnsTarget.getElementsByClassName("btn-active")[0].name
        }

        get qrcode(){
            return this.qrcodeTarget.name
        }
    })
})()