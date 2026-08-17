import { ref, watch, type Ref } from 'vue'

// 卡策略三开关镜像（不含 ip/apn——那两项仅在独立"卡策略"页编辑）
export type PolicyMirror = {
  network_enabled: boolean
  vowifi_enabled: boolean
  airplane_enabled: boolean
  phone_mode?: string       // "wifi" | "cellular"
  data_strategy?: string    // "always" | "on_demand"
}

export type ToggleResult = { ok: boolean }

// 执行器接收开关目标值 enabled 与互斥后的完整目标镜像 next。
// live 消费方只用 enabled（调设备动作端点）；stored 消费方 PUT 整个 next 三元组。
export type CardPolicyExecutors = {
  applyNetwork: (enabled: boolean, next: PolicyMirror) => Promise<ToggleResult>
  applyVoWiFi: (enabled: boolean, next: PolicyMirror) => Promise<ToggleResult>
  applyAirplane: (enabled: boolean, next: PolicyMirror) => Promise<ToggleResult>
  applyPhoneMode: (next: PolicyMirror) => Promise<ToggleResult>
  onChanged?: () => void
}

// 互斥规则（照搬现有面板语义）：
// 开网络 ⇒ 关 VoWiFi、关飞行
// 开 VoWiFi ⇒ 关网络（不动飞行意图，飞行意图独立存储）
// 开飞行 ⇒ 关网络、关 VoWiFi
// 关任一项 ⇒ 不动其它项
function nextMirror(
  cur: PolicyMirror,
  field: keyof PolicyMirror,
  val: boolean
): PolicyMirror {
  if (field === 'network_enabled') {
    return val
      ? { network_enabled: true, vowifi_enabled: false, airplane_enabled: false, phone_mode: cur.phone_mode ?? 'wifi', data_strategy: cur.data_strategy ?? 'on_demand' }
      : { ...cur, network_enabled: false }
  }
  if (field === 'vowifi_enabled') {
    if (val) {
      const isCellular = (cur.phone_mode ?? 'wifi') === 'cellular'
      return {
        network_enabled: isCellular ? (cur.data_strategy === 'always') : false,
        vowifi_enabled: true,
        airplane_enabled: isCellular ? false : true,
        phone_mode: cur.phone_mode ?? 'wifi',
        data_strategy: cur.data_strategy ?? 'on_demand',
      }
    }
    return { ...cur, vowifi_enabled: false }
  }
  // airplane_enabled
  return val
    ? { network_enabled: false, vowifi_enabled: false, airplane_enabled: true, phone_mode: cur.phone_mode ?? 'wifi', data_strategy: cur.data_strategy ?? 'on_demand' }
    : { ...cur, airplane_enabled: false }
}

export function useCardPolicyToggles(
  source: Ref<PolicyMirror | null>,
  executors: CardPolicyExecutors
) {
  const local = ref<PolicyMirror>({
    network_enabled: false,
    vowifi_enabled: false,
    airplane_enabled: false
  })

  const networkPending = ref(false)
  const networkFailed = ref(false)
  const vowifiPending = ref(false)
  const vowifiFailed = ref(false)
  const airplanePending = ref(false)
  const airplaneFailed = ref(false)

  // 上游变化原地同步各字段（不整体替换对象，避免 el-switch 在 element-plus 2.13 崩溃）
  watch(
    source,
    (p) => {
      if (!p) return
      local.value.network_enabled = p.network_enabled
      local.value.vowifi_enabled = p.vowifi_enabled
      local.value.airplane_enabled = p.airplane_enabled
      local.value.phone_mode = p.phone_mode ?? 'wifi'
      local.value.data_strategy = p.data_strategy ?? 'on_demand'
      networkFailed.value = false
      vowifiFailed.value = false
      airplaneFailed.value = false
    },
    { immediate: true }
  )

  async function onNetworkToggle(rawVal: string | number | boolean) {
    const val = rawVal as boolean
    networkPending.value = true
    networkFailed.value = false
    const next = nextMirror(local.value, 'network_enabled', val)
    const result = await executors.applyNetwork(val, next)
    networkPending.value = false
    if (!result.ok) {
      local.value.network_enabled = !val
      networkFailed.value = true
      return
    }
    local.value.network_enabled = next.network_enabled
    local.value.vowifi_enabled = next.vowifi_enabled
    local.value.airplane_enabled = next.airplane_enabled
    executors.onChanged?.()
  }

  async function onVoWiFiToggle(rawVal: string | number | boolean) {
    const val = rawVal as boolean
    vowifiPending.value = true
    vowifiFailed.value = false
    const next = nextMirror(local.value, 'vowifi_enabled', val)
    const result = await executors.applyVoWiFi(val, next)
    vowifiPending.value = false
    if (!result.ok) {
      local.value.vowifi_enabled = !val
      vowifiFailed.value = true
      return
    }
    local.value.network_enabled = next.network_enabled
    local.value.vowifi_enabled = next.vowifi_enabled
    local.value.airplane_enabled = next.airplane_enabled
    executors.onChanged?.()
  }

  async function onAirplaneToggle(rawVal: string | number | boolean) {
    const val = rawVal as boolean
    airplanePending.value = true
    airplaneFailed.value = false
    const next = nextMirror(local.value, 'airplane_enabled', val)
    const result = await executors.applyAirplane(val, next)
    airplanePending.value = false
    if (!result.ok) {
      local.value.airplane_enabled = !val
      airplaneFailed.value = true
      return
    }
    local.value.network_enabled = next.network_enabled
    local.value.vowifi_enabled = next.vowifi_enabled
    local.value.airplane_enabled = next.airplane_enabled
    executors.onChanged?.()
  }

  const phoneModePending = ref(false)
  const phoneModeFailed = ref(false)

  function withPhoneMode(cur: PolicyMirror, mode: string, strategy: string): PolicyMirror {
    const next: PolicyMirror = {
      ...cur,
      phone_mode: mode,
      data_strategy: strategy
    }
    if (!next.vowifi_enabled) return next
    if (mode === 'cellular') {
      next.airplane_enabled = false
      next.network_enabled = strategy === 'always'
    } else {
      next.airplane_enabled = true
      next.network_enabled = false
    }
    return next
  }

  async function applyPhoneSettings(next: PolicyMirror) {
    phoneModePending.value = true
    phoneModeFailed.value = false
    const prev = { ...local.value }
    local.value.phone_mode = next.phone_mode
    local.value.data_strategy = next.data_strategy
    local.value.network_enabled = next.network_enabled
    local.value.airplane_enabled = next.airplane_enabled
    const result = await executors.applyPhoneMode(next)
    phoneModePending.value = false
    if (!result.ok) {
      local.value.phone_mode = prev.phone_mode
      local.value.data_strategy = prev.data_strategy
      local.value.network_enabled = prev.network_enabled
      local.value.airplane_enabled = prev.airplane_enabled
      phoneModeFailed.value = true
      return
    }
    executors.onChanged?.()
  }

  async function onPhoneModeChange(mode: string) {
    await applyPhoneSettings(withPhoneMode(local.value, mode, local.value.data_strategy ?? 'on_demand'))
  }

  async function onDataStrategyChange(strategy: string) {
    await applyPhoneSettings(withPhoneMode(local.value, local.value.phone_mode ?? 'wifi', strategy))
  }

  return {
    local,
    networkPending,
    networkFailed,
    vowifiPending,
    vowifiFailed,
    airplanePending,
    airplaneFailed,
    phoneModePending,
    phoneModeFailed,
    onNetworkToggle,
    onVoWiFiToggle,
    onAirplaneToggle,
    onPhoneModeChange,
    onDataStrategyChange
  }
}
