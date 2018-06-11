
(() => {

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
            this.ageTargets.forEach((el,i) => {
                if (el.dataset.age > 0) {
                    el.textContent = humanize.timeSince(el.dataset.age)
                }
            })
        }
    })
})()