export function setCookie (cname, cvalue, exMins) {
  var d = new Date()
  d.setTime(d.getTime() + (exMins * 60 * 1000))
  var expires = 'expires=' + d.toUTCString()
  document.cookie = cname + '=' + cvalue + ';' + expires + ';path=/'
}
