import type { NotificationQRSession } from '../services/notification-onboarding'

export type NotificationQRTone = 'neutral' | 'active' | 'success' | 'warning' | 'danger'

export type NotificationQRPresentation = {
  label: string
  tone: NotificationQRTone
}

export function notificationQRPresentation(
  session: NotificationQRSession | null,
  connected: boolean
): NotificationQRPresentation {
  if (!session) {
    return connected ? { label: '已配置', tone: 'success' } : { label: '未连接', tone: 'neutral' }
  }
  if (session.status === 'wait') return { label: '等待扫码', tone: 'active' }
  if (session.status === 'scaned') return { label: '已扫码，等待确认', tone: 'active' }
  if (session.status === 'expired') return { label: '二维码已过期', tone: 'warning' }
  if (session.status === 'error') return { label: '连接失败', tone: 'danger' }
  if (session.applied) return { label: '已连接', tone: 'success' }
  return { label: '凭证待应用', tone: 'warning' }
}
