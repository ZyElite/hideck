import type { CommandDefinition } from '../types/commands'

export function commandSuggestions(input: string, definitions: CommandDefinition[]) {
  const normalized = input.trimStart().toLowerCase()
  if (!normalized.startsWith('/') || normalized.includes(' ')) return []
  const prefix = normalized.slice(1)
  return definitions.filter((definition) => definition.name.startsWith(prefix)).slice(0, 8)
}

export function commandTemplate(definition: CommandDefinition) {
  return definition.device_argument ? `/${definition.name} ` : `/${definition.name}`
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
