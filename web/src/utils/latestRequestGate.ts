export type LatestRequestTicket = Readonly<{
  isCurrent: () => boolean
}>

export function createLatestRequestGate() {
  let activeRequest = 0

  function begin(): LatestRequestTicket {
    const requestId = ++activeRequest
    return Object.freeze({
      isCurrent: () => requestId === activeRequest
    })
  }

  return Object.freeze({ begin })
}
