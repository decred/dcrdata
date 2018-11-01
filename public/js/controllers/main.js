
(() => {
    function isCorrectVal(value) {
        return /^\d+$/.test(value) && value >0;
    }

    app.register("main", class extends Stimulus.Controller {
        static get targets() {
            return [ "age" ]
        }

        connect() {
            this.startAgeRefresh()
        }

        disconnect() {
            this.stopAgeRefresh()
        }

        startAgeRefresh() {
            setTimeout(() => {
                this.setAges()
            })
            this.ageRefreshTimer = setInterval(() => {
                this.setAges()
            }, 10000)
        }

        stopAgeRefresh() {
            if (this.ageRefreshTimer) {
                clearInterval(this.ageRefreshTimer)
            }
        }

        setAges() {
            if(this.data.has("lastblocktime")){
              var lbt = DCRThings.counter.data("main-lastblocktime")
              this.element.textContent = humanize.timeSince(lbt)
              if((new Date()).getTime()/1000 - lbt > 8*DCRThings.targetBlockTime){ // 8*blocktime = 40minutes = 12000 seconds
                this.element.classList.add("text-danger")
              }
              return
            }
            this.ageTargets.forEach((el,i) => {
                if (isCorrectVal(el.dataset.age)) {
                    el.textContent = humanize.timeSince(el.dataset.age)
                } else if (el.dataset.age != '') {
                    el.textContent = humanize.timeSince(Date.parse(el.dataset.age)/1000)
                }
            })
        }
    })
})()
