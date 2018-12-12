// To assist in fetching api data
import axios from 'axios'

export default function ajax (url, success, final, fail) {
  final = final || function () {}
  fail = fail || failure.bind(null, url)
  axios.get(url).then((response) => {
    success(response.data)
    final()
  }).catch((err) => {
    fail(err)
    final()
  })
}

function failure (url, error) {
  console.error('Error retrieving ' + url)
  console.error(error)
}
