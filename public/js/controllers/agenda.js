(() => {
    
    var chartLayout = {
        showRangeSelector: true,
        legend: 'follow',
        fillGraph: true,
        colors: ['rgb(0,153,0)', 'orange', 'red'],
        stackedGraph: true,
        legendFormatter: legendFormatter,
        labelsSeparateLines: true
    }

    function legendFormatter(data) {
        if (data.x == null) return '';
        var html = this.getLabels()[0] + ': ' + data.xHTML
        var total = data.series.reduce((total,n) => {
            return total + n.y
        }, 0)
        data.series.forEach((series) => {
            let percentage = ((series.y*100)/total).toFixed(2)
            html += `<br>${series.dashHTML}<span style="color: ${series.color};">${series.labelHTML}: ${series.yHTML} (${percentage}%)</span>`
        });
        return html
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
        var yBottom = e.dygraph.toDomYCoord(0);

        ctx.fillStyle = darkenColor(e.color);

        var minSep = Infinity;
        for (var i = 1; i < points.length; i++) {
            var sep = points[i].canvasx - points[i - 1].canvasx;
            if (sep < minSep) minSep = sep;
        }
        var barWidth = Math.floor(2.0 / 3 * minSep);
        points.forEach((p)=> {
            var x = p.canvasx - barWidth / 2
            var height = yBottom - p.canvasy
            ctx.fillRect(x,p.canvasy,barWidth,height)
            ctx.strokeRect(x,p.canvasy,barWidth,height)
        })
    }

    function cumulativeVoteChoicesData(d) {
        return d.yes.map((n,i) => {
            return [
                new Date(d.time[i]*1000),
                + d.yes[i],
                d.abstain[i], 
                d.no[i]
            ]
        })
    }

    function voteChoicesByBlockData(d) {
        return d.yes.map((n,i) => {
            return [
                d.height[i],
                + d.yes[i],
                d.abstain[i],
                d.no[i]
            ]
        });
    }

    function drawChart(el, data, options) {
        return new Dygraph(
            el,
            data,
            {
                ...chartLayout, 
                ...options
            }
        );
    }

    app.register("agenda", class extends Stimulus.Controller {
        static get targets() {
            return [
                "cumulativeVoteChoices",
                "voteChoicesByBlock"
            ]
        }

        connect() {
            var _this = this
            $.getScript("/js/dygraph.min.js", function() { 
                _this.drawCharts()
            })
        }

        disconnect(e) {
            this.cumulativeVoteChoicesChart.destroy()
            this.voteChoicesByBlockChart.destroy()   
        }

        drawCharts() {
            this.cumulativeVoteChoicesChart = drawChart(
                this.cumulativeVoteChoicesTarget,
                cumulativeVoteChoicesData(chartDataByTime),
                {
                    labels: ["Date", "Yes", "Abstain", "No"],
                    ylabel: 'Cumulative Vote Choices Cast',
                    title: 'Cumulative Vote Choices',
                    labelsKMB: true
                }
            );
            this.voteChoicesByBlockChart = drawChart(
                this.voteChoicesByBlockTarget,
                voteChoicesByBlockData(chartDataByBlock),
                {
                    labels: ["Block Height", "Yes", "Abstain", "No"],
                    ylabel: 'Vote Choices Cast',
                    title: 'Vote Choices By Block',
                    plotter: barchartPlotter
                }
            );
        }
    })

})()