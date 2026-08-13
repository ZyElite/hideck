export type DeviceRequestToken = Readonly<{
  deviceId: string
  generation: number
}>

export function createDeviceRequestScope(initialDeviceId: string) {
  let currentDeviceId = initialDeviceId
  let generation = 0

  return {
    begin(deviceId: string): DeviceRequestToken {
      if (deviceId !== currentDeviceId) {
        currentDeviceId = deviceId
      }
      generation += 1
      return Object.freeze({ deviceId, generation })
    },
    invalidate(deviceId: string) {
      currentDeviceId = deviceId
      generation += 1
    },
    isCurrent(token: DeviceRequestToken, deviceId: string) {
      return token.deviceId === deviceId
        && token.deviceId === currentDeviceId
        && token.generation === generation
    }
  }
}
