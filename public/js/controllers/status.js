(() => {
    function buildProgressBar(data){
        var progressVal = data.PercentComplete;
        var timeRemaining = humanizeTime(data.Time); // time is in secs
        var htmlString = data.BarMsg.length > 0 ? `<p style="font-size:14px;">`+data.BarMsg+`</p>`:  "";
        var subtitle = data.BarSubtitle.trim()
        var notifStr = subtitle.length > 0 ? `<span style="font-size:11px;">notification : <i>`+subtitle+`</i></span>`: "";

        var remainingStr = "pending";
        if (progressVal > 0) {
            remainingStr = data.Time > 0 ? "remaining approx.  "+timeRemaining: "0sec";
        }

        if (progressVal === 100) {
            remainingStr = "done";
        }

        if (subtitle === "sync complete") {
            notifStr = ""
        }

        return (htmlString+`<div class="progress" style="height:30px;border-radius:5px;">
                    <div class="progress-bar sync-progress-bar" role="progressbar" style="height:auto; width:`+progressVal+`%;">
                    <span class="nowrap pl-1 font-weight-bold">Progress `+progressVal+`% (`+remainingStr+`)</span>
                    </div>
                </div>`+ notifStr)
    }

    function humanizeTime(secs) {
        var years = Math.floor(secs / 29030400) % 10
        var months = Math.floor(secs / 2419200) % 12
        var weeks = Math.floor(secs / 604800) % 4
        var days  = Math.floor(secs / 86400) % 7
        var hours   = Math.floor(secs / 3600) % 24
        var minutes = Math.floor(secs / 60) % 60
        var seconds = secs % 60  
        var timeUnit =  ["yr", "mo", "wk", "d", "hr", "min", "sec"]

        return [years, months, weeks, days, hours, minutes, seconds]
            .map((v, i) => v != 0 ? v+""+timeUnit[i]: "")
            .join("  ");
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

                    $("#"+v.ProgressBarID).html(buildProgressBar(v));

                    if (v.BarSubtitle === "sync complete"){
                        $(".alert.alert-info h5").html("Blockchain sync is complete. Redirecting to home in 20 secs.");
                        setInterval(() => Turbolinks.visit("/"), 20000);
                        return
                    }
                }
            })
        }

        disconnect (){
            ws.deregisterEvtHandlers("blockchainSync")
        }
    })
})()
