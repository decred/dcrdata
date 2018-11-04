(() => {
    app.register("newBlock", class extends Stimulus.Controller {
        static get targets() {
            return [ "confirmations" ]
        }
        initialize() {
            let that = this
            globalEventBus.on("BLOCK_RECEIVED", function (newBlock) {
                that.refreshConfirmations(newBlock.block.height)
            })
        }
        connect() {
            this.confirmationsTargets.forEach((el,i) => {
                this.setConfirmationText(el, el.dataset.confirmations)
            })
        }
        setConfirmationText(el, confirmations) {
            if(!el.dataset.formatted){
                el.textContent = confirmations
                return
            }
            if(confirmations > 0) {
                el.textContent = "(" + confirmations + (confirmations > 1? " confirmations": " confirmation") + ")"
            }else {
                el.textContent = "(unconfirmed)"
            }
        }
        refreshConfirmations(expHeight) {
            this.confirmationsTargets.forEach((el,i) => {
                let confirmHeight = el.dataset.confirmationBlockHeight
                let confirmations = expHeight - (confirmHeight + 1)
                this.setConfirmationText(el, confirmations)
                el.dataset.confirmations = confirmations
            })
        }
    })
})()