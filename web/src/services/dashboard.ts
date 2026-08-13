import { api } from '../stores/auth'
import { callService } from './http'
import type { DashboardVM } from '../types/view-model'
import type { DeviceMgmtListItem } from '../types/api'
import { mergeDashboardDeviceOperators } from '../utils/dashboardPresentation'

export const dashboardService = {
  listDevices() {
    return callService(async () => {
      const [dashboardResponse, managedResponse] = await Promise.all([
        api.get('/dashboard/devices'),
        api.get('/devices')
      ])
      const dashboardDevices = (dashboardResponse.data || []) as DashboardVM[]
      const managedDevices = (managedResponse.data?.devices || []) as DeviceMgmtListItem[]
      return mergeDashboardDeviceOperators(dashboardDevices, managedDevices)
    })
  }
}
