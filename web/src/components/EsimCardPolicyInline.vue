<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import type { CardPolicy } from '../types/api'
import { cardsService } from '../services/cards'
import { devicesService } from '../services/devices'
import { useCardPolicyToggles, type PolicyMirror } from '../composables/useCardPolicyToggles'

const props = defineProps<{
  deviceId: string
  iccid: string
  isActiveCard: boolean
  deviceOnline: boolean
}>()

const emit = defineEmits<{
  policyChanged: []
}>()

const policy = ref<CardPolicy | null>(null)
const loadFailed = ref(false)
const loading = ref(false)

// 激活卡 + 设备在线 → live 热切换；否则 stored 存储（激活/上线后生效）
const mode = computed<'live' | 'stored'>(() =>
  props.isActiveCard && props.deviceOnline ? 'live' : 'stored'
)

const hint = computed(() => {
  if (mode.value === 'live') return ''
  if (!props.deviceOnline) return '设备离线，改动已保存，激活/上线后生效'
  return '改动将在此卡激活后生效'
})

const mirror = computed<PolicyMirror | null>(() =>
  policy.value
    ? {
        network_enabled: policy.value.network_enabled,
        vowifi_enabled: policy.value.vowifi_enabled,
        airplane_enabled: policy.value.airplane_enabled,
        phone_mode: policy.value.phone_mode ?? 'wifi',
        data_strategy: policy.value.data_strategy ?? 'on_demand'
      }
    : null
)

async function loadPolicy() {
  loading.value = true
  loadFailed.value = false
  const r = await cardsService.getPolicy(props.iccid)
  loading.value = false
  if (r.ok) {
    policy.value = r.data
  } else {
    loadFailed.value = true
  }
}

onMounted(loadPolicy)

// stored 执行器：PUT 互斥后的完整三元组
async function putTriple(next: PolicyMirror): Promise<{ ok: boolean }> {
  const r = await cardsService.putPolicy(props.iccid, {
    network_enabled: next.network_enabled,
    vowifi_enabled: next.vowifi_enabled,
    airplane_enabled: next.airplane_enabled,
    phone_mode: next.phone_mode,
    data_strategy: next.data_strategy
  })
  return { ok: r.ok }
}

const {
  local,
  networkPending,
  networkFailed,
  vowifiPending,
  vowifiFailed,
  airplanePending,
  airplaneFailed,
  onNetworkToggle,
  onVoWiFiToggle,
  onAirplaneToggle,
  wifiCallingLocksRadio,
  radioMode,
  phoneModePending,
  phoneModeFailed,
  onPhoneModeChange,
  onDataStrategyChange
} = useCardPolicyToggles(mirror, {
  async applyNetwork(enabled, next) {
    if (mode.value === 'stored') return putTriple(next)
    const r = enabled
      ? await devicesService.startNetwork(props.deviceId, {
          ip_version: policy.value?.ip_version || 'v4',
          apn: policy.value?.apn || ''
        })
      : await devicesService.stopNetwork(props.deviceId)
    return { ok: r.ok }
  },
  async applyVoWiFi(enabled, next) {
    if (mode.value === 'stored') return putTriple(next)
    const r = enabled
      ? await devicesService.enableVoWiFi(props.deviceId, {
          mode: next.phone_mode,
          data_strategy: next.data_strategy
        })
      : await devicesService.disableVoWiFi(props.deviceId)
    return { ok: r.ok }
  },
  async applyPhoneMode(next) {
    if (mode.value === 'stored') return putTriple(next)
    const r = await devicesService.enableVoWiFi(props.deviceId, {
      mode: next.phone_mode,
      data_strategy: next.data_strategy
    })
    return { ok: r.ok }
  },
  async applyAirplane(enabled, next) {
    if (mode.value === 'stored') return putTriple(next)
    const r = await devicesService.setFlightMode(props.deviceId, enabled)
    return { ok: r.ok }
  },
  onChanged() {
    emit('policyChanged')
  }
})
</script>

<template>
  <div class="px-4 py-3 bg-gray-50/60 dark:bg-white/5 rounded-lg space-y-3">
    <div v-if="loading" class="text-xs text-gray-400 flex items-center gap-1">
      <el-icon class="animate-spin"><Loading /></el-icon> 正在加载策略...
    </div>
    <div v-else-if="loadFailed" class="text-xs text-orange-500 flex items-center gap-2">
      策略加载失败
      <el-button size="small" text @click="loadPolicy">重试</el-button>
    </div>
    <template v-else>
      <div v-if="hint" class="text-xs text-amber-600 dark:text-amber-400">{{ hint }}</div>
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
        <div class="flex items-center justify-between rounded-lg px-3 py-2 bg-white dark:bg-white/5">
          <span class="text-sm text-gray-700 dark:text-gray-200">飞行<small class="block text-xs text-gray-400 font-normal">{{ wifiCallingLocksRadio ? 'WiFi calling 已锁定' : '关闭后注册运营商' }}</small></span>
          <div class="flex items-center gap-2">
            <span v-if="airplaneFailed" class="text-xs text-orange-500">未生效</span>
            <el-icon v-if="airplanePending" class="animate-spin text-gray-400"><Loading /></el-icon>
            <el-switch
              :model-value="radioMode === 'airplane'"
              :disabled="airplanePending || wifiCallingLocksRadio"
              @change="onAirplaneToggle"
            />
          </div>
        </div>
        <div class="flex items-center justify-between rounded-lg px-3 py-2 bg-white dark:bg-white/5">
          <span class="text-sm text-gray-700 dark:text-gray-200">网络<small class="block text-xs text-gray-400 font-normal">只开流量</small></span>
          <div class="flex items-center gap-2">
            <span v-if="networkFailed" class="text-xs text-orange-500">未生效</span>
            <el-icon v-if="networkPending" class="animate-spin text-gray-400"><Loading /></el-icon>
            <el-switch
              v-model="local.network_enabled"
              :disabled="radioMode === 'airplane' || networkPending || wifiCallingLocksRadio"
              @change="onNetworkToggle"
            />
          </div>
        </div>
        <div class="flex items-center justify-between rounded-lg px-3 py-2 bg-white dark:bg-white/5">
          <span class="text-sm text-gray-700 dark:text-gray-200">软件电话</span>
          <div class="flex items-center gap-2">
            <span v-if="vowifiFailed" class="text-xs text-orange-500">未生效</span>
            <el-icon v-if="vowifiPending" class="animate-spin text-gray-400"><Loading /></el-icon>
            <el-switch
              v-model="local.vowifi_enabled"
              :disabled="vowifiPending"
              @change="onVoWiFiToggle"
            />
          </div>
        </div>
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <div class="flex items-center justify-between rounded-lg px-3 py-2 bg-white dark:bg-white/5">
          <span class="text-sm text-gray-700 dark:text-gray-200">通话方式</span>
          <el-select
            :model-value="local.phone_mode ?? 'wifi'"
            size="small"
            style="width: 140px"
            :disabled="phoneModePending"
            @change="onPhoneModeChange"
          >
            <el-option label="WiFi calling" value="wifi" />
            <el-option label="蜂窝数据" value="cellular" />
          </el-select>
        </div>
        <div
          v-if="(local.phone_mode ?? 'wifi') === 'cellular'"
          class="flex items-center justify-between rounded-lg px-3 py-2 bg-white dark:bg-white/5"
        >
          <span class="text-sm text-gray-700 dark:text-gray-200">数据策略</span>
          <el-select
            :model-value="local.data_strategy ?? 'on_demand'"
            size="small"
            style="width: 160px"
            :disabled="phoneModePending || radioMode === 'airplane'"
            @change="onDataStrategyChange"
          >
            <el-option label="仅打电话时开启" value="on_demand" />
            <el-option label="长时间开启" value="always" />
          </el-select>
        </div>
      </div>
      <div class="text-xs leading-5 text-gray-500 dark:text-gray-400">
        <template v-if="wifiCallingLocksRadio">WiFi calling 占用射频，飞行已锁定，网络不可用。</template>
        <template v-else-if="radioMode === 'airplane'">飞行已开，射频关闭。关掉飞行后才会注册运营商。</template>
        <template v-else-if="(local.phone_mode ?? 'wifi') === 'cellular'">
          {{ local.network_enabled
            ? '已注册运营商，网络开启，会走流量。'
            : '已注册运营商，不走流量。打开网络后才按策略连数据。' }}
        </template>
        <template v-else>已注册运营商。打开网络才会用数据。</template>
      </div>
      <div v-if="phoneModeFailed" class="text-xs text-orange-500">通话方式未生效</div>
    </template>
  </div>
</template>
