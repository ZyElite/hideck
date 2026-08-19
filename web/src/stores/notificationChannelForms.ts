import type {
  WeComBotSettings,
  WeixinSettings
} from '../services/system'

export type WeixinForm = {
  enabled: boolean
  base_url: string
  allowed_user_ids: string
  allowed_group_ids: string
}

export type WeComBotForm = {
  enabled: boolean
  bot_id: string
  secret: string
  websocket_url: string
  allowed_user_ids: string
  allowed_group_ids: string
}

export const DEFAULT_WEIXIN_FORM: WeixinForm = {
  enabled: false,
  base_url: 'https://ilinkai.weixin.qq.com',
  allowed_user_ids: '',
  allowed_group_ids: ''
}

export const DEFAULT_WECOM_BOT_FORM: WeComBotForm = {
  enabled: false,
  bot_id: '',
  secret: '',
  websocket_url: 'wss://openws.work.weixin.qq.com',
  allowed_user_ids: '',
  allowed_group_ids: ''
}

export function weixinFormFromSettings(settings: Partial<WeixinSettings>): WeixinForm {
  return {
    enabled: !!settings.enabled,
    base_url: settings.base_url || DEFAULT_WEIXIN_FORM.base_url,
    allowed_user_ids: joinIDs(settings.allowed_user_ids),
    allowed_group_ids: joinIDs(settings.allowed_group_ids)
  }
}

export function weComBotFormFromSettings(settings: Partial<WeComBotSettings>): WeComBotForm {
  return {
    enabled: !!settings.enabled,
    bot_id: settings.bot_id || '',
    secret: settings.secret || '',
    websocket_url: settings.websocket_url || DEFAULT_WECOM_BOT_FORM.websocket_url,
    allowed_user_ids: joinIDs(settings.allowed_user_ids),
    allowed_group_ids: joinIDs(settings.allowed_group_ids)
  }
}

export function buildWeixinSettings(form: WeixinForm): WeixinSettings {
  return {
    enabled: !!form.enabled,
    base_url: String(form.base_url || '').trim(),
    allowed_user_ids: splitIDs(form.allowed_user_ids),
    allowed_group_ids: splitIDs(form.allowed_group_ids)
  }
}

export function buildWeComBotSettings(form: WeComBotForm): WeComBotSettings {
  return {
    enabled: !!form.enabled,
    bot_id: String(form.bot_id || '').trim(),
    secret: String(form.secret || '').trim(),
    websocket_url: String(form.websocket_url || '').trim(),
    allowed_user_ids: splitIDs(form.allowed_user_ids),
    allowed_group_ids: splitIDs(form.allowed_group_ids)
  }
}

export function splitIDs(value: string): string[] {
  return String(value || '')
    .split(',')
    .map(item => item.trim())
    .filter((item, index, values) => !!item && values.indexOf(item) === index)
}

export function mergeIDs(current: string, incoming: string[] | undefined): string {
  return splitIDs([current, ...(incoming || [])].join(',')).join(',')
}

function joinIDs(values: string[] | undefined): string {
  return Array.isArray(values) ? values.join(',') : ''
}
