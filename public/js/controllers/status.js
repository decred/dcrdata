(() => {
    function buildProgressBar(data){
        var  progressVal = ((data.From/data.To)*100).toFixed(2)
        var timeRemaining = calculateTime(data.Time) // time is in secs

        return `<div class="progress" style="height:30px;">
                    <div class="progress-bar" role="progressbar" style="height:auto; width:`+progressVal+`%;">
                    <span class="nowrap pl-1 font-weight-bold">Progress `+progressVal+`% (remaining approx. `+timeRemaining+`)</span>
                    </div>
                </div>`
    }

    function calculateTime(secs){   
        var years = Math.floor(secs / 29030400)
        var months = Math.floor(secs / 2419200) % 12
        var weeks = Math.floor(secs / 604800) % 4
        var days  = Math.floor(secs / 86400) % 7
        var hours   = Math.floor(secs / 3600) % 24
        var minutes = Math.floor(secs / 60) % 60
        var seconds = secs % 60  
        var timeUnit =  ["yr", "mo", "wk", "d", "hr", "min", "sec"]

        return [years, months, weeks, days, hours, minutes, seconds]
            .map((v, i) => {
                v = v < 10 ? "0" + v : v
                return v !== "00" ? v+""+timeUnit[i]: ""
            }).join(" ");
    }

    app.register("status", class extends Stimulus.Controller {
        static get targets() {
            return [ "statusSyncing" ]
        }

        connect() {
            ws.registerEvtHandler("blockchainSync", (evt) => {
                var d = JSON.parse(evt);
                var i;

                for (i = 0; i < d.length; i ++) {
                    var v = d[i]
                    $("#"+v.UpdateType).html(buildProgressBar(v));
                }
            })
        }

        disconnect (){
            ws.deregisterEvtHandlers("blockchainSync")
        }
    })
})()
