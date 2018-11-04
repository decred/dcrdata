
(() => {

    app.register("extras", class extends Stimulus.Controller {
        static get targets() {
            return [ "confirmations" ]
        }

        initialize() {
            this.startWsRetryTimer()
        }

        connect() {
            this.confirmationsTargets.forEach((el,i) => {
                this.setConfirmationText(el, el.dataset.confirmations)
            })
        }

        setConfirmationText(el, confirmations) {
            if(confirmations > 0) {
                el.textContent = "(" + confirmations + (confirmations > 1? " confirmations": " confirmation") + ")"
            }else {
                el.textContent = "(unconfirmed)"
            }
        }

        refreshConfirmations(newHeight) {
            this.confirmationsTargets.forEach((el,i) => {
                let blockHeight = el.dataset.confirmationBlockHeight
                let confirmations = newHeight - blockHeight

                this.setConfirmationText(el, confirmations)
                el.dataset.confirmations = confirmations
                el.dataset.confirmationBlockHeight = blockHeight
            })
        }

        registerWsNewBlockEnt() {
            let ctl = this
            ws.registerEvtHandler("newBlock", function (event) {
                alert('received new block in extras controller')
                var newBlock = JSON.parse(event);
                ctl.refreshConfirmations(newBlock.block.height)
            })
            alert('ws registered')
        }
/*
        disconnect() {
            this.stopWsRetryTimer()
        }*/

        startWsRetryTimer() {
            this.wsRetryTimer = setInterval(() => {
                if(ws){
                    this.registerWsNewBlockEnt()
                    this.stopWsRetryTimer()
                }
            }, 5000)
        }

        stopWsRetryTimer() {
            if (this.wsRetryTimer) {
                clearInterval(this.wsRetryTimer)
            }
        }

    })
})()