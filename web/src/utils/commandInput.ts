import type { CommandDefinition } from '../types/commands'

export function commandSuggestions(input: string, definitions: CommandDefinition[]) {
  const normalized = input.trimStart().toLowerCase()
  if (!normalized.startsWith('/') || normalized.includes(' ')) return []
  const prefix = normalized.slice(1)
  return definitions.filter((definition) => definition.name.startsWith(prefix)).slice(0, 8)
}

export function commandTemplate(definition: CommandDefinition, selectedDevice = '') {
  if (!definition.device_argument) return `/${definition.name}`
  const device = selectedDevice.trim()
  return device ? `/${definition.name} ${device}` : `/${definition.name} `
}

export function commandTargetDevice(input: string, definitions: CommandDefinition[]): string | null {
  const parts = input.trim().split(/\s+/)
  const name = parts[0]?.startsWith('/') ? parts[0].slice(1).toLowerCase() : ''
  const definition = definitions.find((item) => item.name.toLowerCase() === name)
  if (!definition?.device_argument) return null
  return parts[1] || ''
}

export function retargetDeviceCommand(input: string, definitions: CommandDefinition[], selectedDevice: string) {
  const device = selectedDevice.trim()
  if (!device || commandTargetDevice(input, definitions) === null) return input
  const parts = input.trim().split(/\s+/)
  if (parts.length === 1) return `${parts[0]} ${device}`
  return [parts[0], device, ...parts.slice(2)].join(' ')
}

export function carrierReplySenderError(responseMode: string, sendersText: string) {
  if (responseMode !== 'sms') return ''
  const hasSender = sendersText.split('\n').some((sender) => sender.trim() !== '')
  return hasSender ? '' : '短信回复规则必须至少设置一个预期发送者'
}

export type DangerousCommandInput = {
  name: string
  device: string
  target?: string
  phone?: string
  duration?: number
}

export function buildDangerousCommand(input: DangerousCommandInput) {
  const device = input.device.trim()
  if (!device) throw new Error('请选择设备')
  if (input.name === 'rotate') return `/rotate ${device}`
  if (input.name === 'switch') {
    const target = input.target?.trim() || ''
    if (!target) throw new Error('请填写 Profile 序号或 ICCID')
    return `/switch ${device} ${target}`
  }
  if (input.name === 'vocall') {
    const phone = input.phone?.trim() || ''
    if (!phone) throw new Error('请填写电话号码')
    return `/vocall ${device} ${phone} ${input.duration || 15}`
  }
  throw new Error(`不支持的快捷动作 /${input.name}`)
}
