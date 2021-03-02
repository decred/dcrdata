export function setCookie (cname, cvalue, exMins) {
  const d = new Date()
  d.setTime(d.getTime() + (exMins * 60 * 1000))
  const expires = 'expires=' + d.toUTCString()
  document.cookie = cname + '=' + cvalue + ';' + expires + ';path=/'
}
