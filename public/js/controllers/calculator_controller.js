import {Controller} from 'stimulus'

function currencyFormat(num, decimalPlaces = 0) {
    return "$" + num.toFixed(decimalPlaces).replace(/(\d)(?=(\d{3})+(?!\d))/g, "$1,")
}

function float64format(num, decimalPlaces = 0) {
    return num.toFixed(decimalPlaces).replace(/(\d)(?=(\d{3})+(?!\d))/g, "$1,")
}
//update URL Parameter
function updateURL(key,val){
    var url = window.location.href;
    var reExp = new RegExp("[\?|\&]"+key + "=[0-9a-zA-Z\_\+\-\|\.\,\;]*");

    if(reExp.test(url)) {
        // update
        var reExp = new RegExp("[\?&]" + key + "=([^&#]*)");
        var delimiter = reExp.exec(url)[0].charAt(0);
        url = url.replace(reExp, delimiter + key + "=" + val);
    } else {
        // add
        var newParam = key + "=" + val;
        if(!url.indexOf('?')){url += '?';}

        if(url.indexOf('#') > -1){
            var urlparts = url.split('#');
            url = urlparts[0] +  "&" + newParam +  (urlparts[1] ?  "#" +urlparts[1] : '');
        } else {
            url += (url.indexOf('?')>0 ? '&' : '?') + newParam;
        }
    }
    window.history.pushState(null, document.title, url);
}
function getParameterByName(name, url) {
    if (!url) url = window.location.href;
    name = name.replace(/[\[\]]/g, '\\$&');
    var regex = new RegExp('[?&]' + name + '(=([^&#]*)|&|#|$)'),
        results = regex.exec(url);
    if (!results) return null;
    if (!results[2]) return '';
    return decodeURIComponent(results[2].replace(/\+/g, ' '));
}
const deviceList = [
    {
        hashrate: 34, //Th/s
        power: 1610, //W
        cost: 1282, //$
        name: 'DCR5s'
    },
    {
        hashrate: 44, //Th/s
        power: 2200, //W
        cost: 4199, //$
        name: 'D1s'
    }
]
export default class extends Controller {
    static get targets() {
        return [
            'hashRate', 'targetPow', 'device', 'targetPos', 'targetPosStr', 'targetHashRate',
            'targetHashRateTwo', 'timeStr', 'timeStrTwo', 'time', 'kwhRate', 'ticketPoolSize',
            'ticketAttackSize', 'price', 'ticketPrice', 'total', 'deviceName', 'deviceHashRate', 'deviceCost', 'devicePower',
            'totalKwh', 'totalKwhStr', 'totalElectricity', 'totalElectricityTwo', 'totalDeviceCost', 'totalDeviceCostTwo',
            'totalPow', 'totalPowTwo', 'totalPos', 'totalDeviceCostTwo', 'ticketSizeAttach', 'ticketSizeAttachTwo',
            'totalDCRPos', 'totalPosTwo', 'totalDCRPosTwo', 'facility', 'TotalFree', 'countDevice', 'priceTwo', 'kwhRateTwo'
        ]
    }

    initialize() {

        this.initParam();
    }
    initParam(){
        var time = getParameterByName('time');
        if(time)  {
            this.timeTarget.value = parseInt(time)
        }
        var targetPow = getParameterByName('target_pow');
        if(targetPow){
            this.targetPowTarget.value =  parseFloat(targetPow)
        }
        var kwhRate = getParameterByName('kwh_rate');
        if(kwhRate){
            this.kwhRateTarget.value =  parseFloat(kwhRate)
        }
        var facility = getParameterByName('facility');
        if(facility){
            this.facilityTarget.value =  parseFloat(facility)
        }
        var targetPos = getParameterByName('target_pos');
        if(targetPos){
            this.targetPosTarget.value =  parseFloat(targetPos)
        }
        var price = getParameterByName('price');
        if(price){
            this.priceTarget.value =  parseFloat(price)
        }
        var device = getParameterByName('choose_device');
        if(device){
            device = parseInt(device);
            document.getElementsByName("exampleRadios")[device].checked=true;
            this.calculate(device)
        }
        else {
            document.getElementsByName("exampleRadios")[0].checked=true;
            this.calculate()
        }


    }
    calculate(deviceOrder) {
        var tagetHashRate = parseFloat(this.hashRateTarget.textContent) * this.targetPowTarget.value / 100;
        var deviceInfo;
        if(deviceOrder){
            deviceInfo = deviceList[deviceOrder];
        }
        else {
            deviceInfo = deviceList[this.deviceTarget.value];
        }
        var deviceCount = Math.ceil((tagetHashRate * 1000) / deviceInfo.hashrate);
        var totalDeviceCost = deviceCount * deviceInfo.cost;
        var totalKwh = deviceCount * deviceInfo.power * this.timeTarget.value / 1000;
        var totalElectricity = totalKwh * this.kwhRateTarget.value;
        var totalFree = 1+ this.facilityTarget.value/100;
        var totalPow = totalDeviceCost* totalFree + totalElectricity;
        var ticketAttackSize =  Math.ceil((parseInt(this.ticketPoolSizeTarget.textContent) * this.targetPosTarget.value) / 100);
        var totalDCRPos = ticketAttackSize * parseFloat(this.ticketPriceTarget.textContent);
        var totalPos = totalDCRPos * this.priceTarget.value;
        var total = totalPow + totalPos;
        this.targetHashRateTarget.textContent = tagetHashRate.toFixed(3);
        this.targetHashRateTwoTarget.textContent = tagetHashRate.toFixed(3);
        var timeStr = this.timeTarget.value > 1 ? this.timeTarget.value + ' hours' : this.timeTarget.value + ' hour';
        this.timeStrTarget.textContent = timeStr;
        this.timeStrTwoTarget.textContent = timeStr;
        this.countDeviceTarget.textContent = deviceCount;
        this.deviceNameTarget.textContent =  deviceInfo.name;
        this.devicePowerTarget.textContent = deviceInfo.power;
        this.TotalFreeTarget.textContent = totalFree;
        this.totalDeviceCostTarget.textContent = currencyFormat(totalDeviceCost);
        this.totalDeviceCostTwoTarget.textContent = currencyFormat(totalDeviceCost);
        this.totalKwhTarget.textContent = float64format(totalKwh, 2);
        this.totalKwhStrTarget.textContent = float64format(totalKwh, 2) + ' kWh';
        this.kwhRateTwoTarget.textContent = float64format(parseFloat(this.kwhRateTarget.value), 2);
        this.totalElectricityTarget.textContent = currencyFormat(totalElectricity, 1);
        this.totalElectricityTwoTarget.textContent = currencyFormat(totalElectricity, 1);
        this.totalPowTarget.textContent = currencyFormat(totalPow, 2);
        this.totalPowTwoTarget.textContent = currencyFormat(totalPow, 2);
        this.targetPosStrTarget.textContent = this.targetPosTarget.value + '%';
        this.ticketSizeAttachTarget.textContent = float64format(ticketAttackSize);
        this.ticketSizeAttachTwoTarget.textContent = float64format(ticketAttackSize);
        this.totalDCRPosTarget.textContent = float64format(totalDCRPos, 2);
        this.totalDCRPosTwoTarget.textContent = float64format(totalDCRPos, 2);
        this.totalPosTarget.textContent = currencyFormat(totalPos, 2);
        this.priceTwoTarget.textContent = this.priceTarget.value;
        this.totalPosTwoTarget.textContent = currencyFormat(totalPos, 2);
        //this.blockHeightTarget.textContent = deviceCount;
        this.totalTarget.textContent = currencyFormat(total, 2);
    }

    UpdateTime(){
        updateURL('time', this.timeTarget.value);
        this.calculate();
    }

    UpdateTargetPow(){
        updateURL('target_pow', this.targetPowTarget.value);
        this.calculate();
    }

    ChooseDevice(event){
        this.calculate(event.srcElement.value);
        console.log(event, event.srcElement.value)
        updateURL('choose_device', event.srcElement.value);
    }
    UpdateKwhRate(){
        updateURL('kwh_rate',  this.kwhRateTarget.value);
        this.calculate();
    }

    UpdateFacility(){
        updateURL('facility',  this.facilityTarget.value);
        this.calculate();
    }
    UpdateTargetPos(){
        updateURL('target_pos',  this.targetPosTarget.value);
        this.calculate();
    }
    UpdatePrice(){
        updateURL('price',  this.priceTarget.value);
        this.calculate();
    }
}
