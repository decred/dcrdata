// helpers for dynamic module imports

export async function getDefault (dynamicImportPromise) {
  let module = await dynamicImportPromise
  return new Promise(resolve => {
    resolve(module.default)
  })
}
