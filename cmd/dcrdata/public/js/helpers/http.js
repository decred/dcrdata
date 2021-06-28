
export async function requestJSON (url) {
  const conf = {
    headers: {
      accept: 'application/json'
    },
    method: 'GET'
  }
  return await window.fetch(url, conf)
    .then(async resp => {
      if (resp.ok) {
        return await resp.json()
      }
      const msg = await resp.text()
      console.log(resp.statusText, msg)
      throw new Error(msg)
    })
}
