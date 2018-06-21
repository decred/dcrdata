// Common code for ploting dygraphs
function legendFormatter(data) {
    if (data.x == null) return '';
    var html = this.getLabels()[0] + ': ' + data.xHTML;
    data.series.map(function(series) {
        var labeledData = ' <span style="color: ' + series.color + ';">' +series.labelHTML + ': ' + series.yHTML;
        html += '<br>' + series.dashHTML  + labeledData + '</span>';
    });
    return html;
}

// Darken a color
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
    var yBottom = e.dygraph.toDomYCoord(0);

    ctx.fillStyle = darkenColor(e.color);

    var minSep = Infinity;
    for (var i = 1; i < points.length; i++) {
        var sep = points[i].canvasx - points[i - 1].canvasx;
        if (sep < minSep) minSep = sep;
    }
    var barWidth = Math.floor(2.0 / 3 * minSep);
    points.map((p)=> {
        var x = p.canvasx - barWidth / 2
        var height = yBottom - p.canvasy
        ctx.fillRect(x,p.canvasy,barWidth,height)
        ctx.strokeRect(x,p.canvasy,barWidth,height)
    })
}

// Plotting the actual ticketpool graphs
(() => {
    var ms = ""

    function Comparator(a, b) {
      if (a[0] < b[0]) return -1;
      if (a[0] > b[0]) return 1;
      return 0;
    }

    function purchasesGraphData(items){
      var s = [];
      var finalDate = "";

      items.time.map(function(n, i){
          s.push([new Date(n*1000), 0, items.immature[i], items.live[i], items.price[i]]);
      });

      finalDate = new Date(mp.time[0]*1000);
      s.push([finalDate , mp.mempool[0], 0, 0, mp.price[0]]); // add mempool

      origDate = s[0][0] - new Date(0)
      ms = (finalDate - new Date(0)) + 1000

      return s
    }

    function priceGraphData(items) {
      var mempl = 0
      var p = []

      items.price.map((n, i) => {
        if (n === mp.price[0]) {
            mempl = mp.mempool[0]
            p.push([n, mempl, items.immature[i], items.live[i]]);
        } else {
            p.push([n, 0, items.immature[i], items.live[i]]);
        }
      });

      if (mempl === 0){
        p.push([mp.price[0], mp.mempool[0], 0, 0]); // add mempool
        p = p.sort(Comparator)
      }
      return p
    }

    function getVal(val) { return isNaN(val) ? 0: val;}

    function outputsGraphData(items){
        return [
            getVal(items.solo),
            getVal(items.pooled),
            getVal(items.txsplit)
        ]
    }

    function getWindow(val){
      switch (val){
        case "1d": return [(ms - 8.64E+07) - 1000, ms]
        case "1wk": return [(ms - 6.048e+8) - 1000, ms]
        case "1m": return [(ms - 2.628e+9) - 1000, ms]
        default: return [origDate, ms]
      }
    }

    function purchasesGraph(){
        return new Dygraph(
            document.getElementById("tickets_by_purchase_date"),
            purchasesGraphData(graph[0]),
            {
            labels: ["Date", "Mempool Tickets", "Immature Tickets", "Live Tickets", "Ticket Value"],
            showRangeSelector: true,
            digitsAfterDecimal: 8,
            ylabel: '# of Tickets',
            y2label: 'A.v.g. Tickets Value (DCR)',
            colors: ['darkorange', '#006600', '#0066cc', '#ff0090'],
            legend: 'follow',
            fillGraph: true,
            stackedGraph: true,
            plotter: barchartPlotter,
            dateWindow: getWindow("1d"),
            legendFormatter: legendFormatter,
            title: 'Tickets Purchase Distribution',
            labelsSeparateLines: true,
            series: {
                "Ticket Value": {
                    axis: 'y2',
                    plotter: Dygraph.Plotters.linePlotter
                }
            },
            axes: {
                y2: {
                    fillGraph: true,
                    axisLabelFormatter: function(d) { return d.toFixed(1)},
                    }
                }
            }
        );
    }

    function priceGraph(){
        return new Dygraph(
            document.getElementById("tickets_by_purchase_price"),
            priceGraphData(graph[1]),
            {
                labels: ["Price", 'Mempool Tickets', 'Immature Tickets', 'Live Tickets'],
                digitsAfterDecimal: 8,
                showRangeSelector: true,
                xlabel: 'Ticket Price (DCR)',
                ylabel: '# of Tickets',
                legend: 'follow',
                labelsKMB: true,
                fillGraph: true,
                stackedGraph: true,
                plotter: barchartPlotter,
                legendFormatter: legendFormatter,
                title: 'Ticket Price Distribution',
                colors: ['darkorange', '#006600', '#0066cc'],
                labelsSeparateLines: true
            })
        ;
    }

    function outputsGraph(){
        var d = outputsGraphData(chart)
        return new Chart(
            document.getElementById("doughnutGraph"), {
            options: {
                width:200,
                height:200,
                responsive: false,
                animation: { animateScale: true },
                legend: { position: 'bottom'},
                title: {
                    display: true,
                    text: 'Tickets Grouped by # of Outputs'
                },
                tooltips: {
                callbacks: {
                    label: function(tooltipItem, data) {
                    var sum = 0
                    var dataset = data.datasets[tooltipItem.datasetIndex];
                    var currentValue = dataset.data[tooltipItem.index];
                    d.map((u) =>{sum += u})
                    var percentage = (currentValue/sum) * 100;
                    return currentValue + " Tickets ( " + percentage.toFixed(2) + "% )";
                    }
                }
                }
            },
            type: "doughnut",
            data: {
                labels: ["Solo", "Pooled", "TixSplit"],
                datasets: [{
                    data: d,
                    label: 'Solo Tickets',
                    backgroundColor: ['#2d00dd', '#FF8F00', 'limegreen'],
                    borderColor: [ 'white', 'white', 'white'],
                    borderWidth: 0.5
                }]
            }
        });
    }

    app.register("ticketpool", class extends Stimulus.Controller{
        static get targets(){
            return [ "zoom", "bars" ]
        }

        connect(){
            $.getScript('/js/dygraphs.min.js', () => {
                this.purchasesGraph = purchasesGraph()
                this.priceGraph = priceGraph()
            });

            $.getScript('/js/charts.min.js', () => {
                this.outputsGraph = outputsGraph()
            });

            ws.registerEvtHandler("newblock", (evt) => {
                ws.send("getticketpooldata", this.bars)
            });

            ws.registerEvtHandler("getticketpooldataResp", (evt) => {
                var v = JSON.parse(evt)
                console.log(' >>>>>>>>> ', v)
                this.purchasesGraph.updateOptions({'file': purchasesGraphData(v.BarGraphs[0])})
                this.priceGraph.updateOptions({'file': priceGraphData(v.BarGraphs[1])})

                console.log(' Before >>>>>>>>>>> ', this.outputsGraph.data.datasets[0].data)
                this.outputsGraph.data.datasets[0].data = outputsGraphData(v.DonutChart)
                console.log(' Before >>>>>>>>>>> ', this.outputsGraph.data.datasets[0].data)
                this.outputsGraph.update()

                console.log(' >>>>>>>>> ticketpool update complete <<<<<<<<<<<<')
            })
        }

        disconnect() {
            this.purchasesGraph.destroy()
            this.priceGraph.destroy()

            ws.deregisterEvtHandlers("ticketpool")
            ws.deregisterEvtHandlers("getticketpooldataResp")
        }

        onZoom() {
            this.purchasesGraph.updateOptions({dateWindow: getWindow(this.zoom)})
        }

        onBarsChange() {
            $("body").addClass("loading");
            var _this = this

            $.ajax({
                type: "GET",
                url: '/api/ticketpool/bydate/'+this.bars,
                beforeSend: function() {},
                error: function() {
                    $("body").removeClass("loading");
                },
                success: function(data) {
                    purchasesGraphData(data)
                    _this.purchasesGraph.updateOptions({'file': stages});
                    $("body").removeClass("loading");
                }
            });
        }

        get zoom() {
            return this.zoomTarget.getElementsByClassName("btn-active")[0].name
        }

        get bars() {
            return this.barsTarget.getElementsByClassName("btn-active")[0].name
        }
    });
})()