import { api } from '../stores/auth'
import { callService } from './http'

export type NotificationQRChannel = 'weixin' | 'wecom-bot' | 'qq' | 'feishu'
export type NotificationQRStatus = 'wait' | 'scaned' | 'confirmed' | 'expired' | 'error'

export type NotificationQRSession = {
  session_id: string
  task_id?: string
  qr_url?: string
  open_url?: string
  expires_at?: string
  status: NotificationQRStatus
  applied?: boolean
  apply_warning?: string
  error?: string
  app_id?: string
  user_openid?: string
  bot_id?: string
  bot_account_id?: string
  bot_user_id?: string
  base_url?: string
}

const channelPaths: Record<NotificationQRChannel, string> = {
  weixin: 'weixin',
  'wecom-bot': 'wecom-bot',
  qq: 'qq',
  feishu: 'feishu'
}

function channelPath(channel: NotificationQRChannel): string {
  return `/settings/notifications/${channelPaths[channel]}/qr`
}

export const notificationOnboardingService = {
  start(channel: NotificationQRChannel, payload: { base_url?: string } = {}) {
    return callService(async () => {
      const response = await api.post<NotificationQRSession>(`${channelPath(channel)}/start`, payload)
      return response.data
    })
  },
  status(channel: NotificationQRChannel, sessionID: string) {
    return callService(async () => {
      const response = await api.get<NotificationQRSession>(`${channelPath(channel)}/status`, {
        params: { session_id: sessionID }
      })
      return response.data
    })
  },
  cancel(channel: NotificationQRChannel, sessionID: string) {
    return callService(async () => {
      await api.post(`${channelPath(channel)}/cancel`, { session_id: sessionID })
      return true
    })
  }
}
