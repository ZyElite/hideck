<script setup lang="ts">
import { computed } from 'vue'
import { Eye24Regular, EyeOff24Regular } from '@vicons/fluent'
import type { DeviceOverviewItem } from '../types/api'
import { useSensitiveVisibility } from '../composables/useSensitiveVisibility'
import { copyToClipboard } from '../utils/clipboard'
import { activeEsimProfileDisplayName } from './deviceOverviewActiveEsim'

type IdentityFact = Readonly<{
  key: string
  label: string
  value: string
  copyable?: boolean
  sensitive?: boolean
  tone?: 'default' | 'status'
}>

type IdentityFactInput = Omit<IdentityFact, 'value'> & Readonly<{
  value: unknown
}>

const props = defineProps<{
  device: DeviceOverviewItem | null
  simOperatorDisplay: string
  simOperatorCountryCode: string
  e911Starting: boolean
}>()

const emit = defineEmits<{
  'setup-e911': []
}>()

const showSensitive = useSensitiveVisibility()

const backendLabel = computed(() => {
  const backend = props.device?.backend_mode
  if (backend === 'qmi') return 'QMI'
  if (backend === 'mbim') return 'MBIM'
  if (backend === 'pcsc') return 'PC/SC'
  if (backend === 'at') return 'AT'
  return 'Auto'
})

const flightModeEnabled = computed(() => {
  if (props.device?.vowifi_active) return true
  const mode = props.device?.modem?.operating_mode
  return mode === 0 || mode === 4
})

const identityFacts = computed<readonly IdentityFact[]>(() => {
  const facts: IdentityFact[] = [
    createFact({ key: 'imei', label: 'IMEI', value: props.device?.modem?.imei, sensitive: true, copyable: true }),
    createFact({ key: 'iccid', label: 'ICCID', value: props.device?.modem?.iccid, sensitive: true, copyable: true }),
    createFact({ key: 'imsi', label: 'IMSI', value: props.device?.modem?.imsi, sensitive: true, copyable: true }),
    createFact({ key: 'phone', label: '本机号码', value: props.device?.local_phone, sensitive: true, copyable: true }),
    createFact({ key: 'operator', label: '原运营商', value: props.simOperatorDisplay, copyable: true }),
    createFact({ key: 'firmware', label: '固件版本', value: props.device?.modem?.firmware, copyable: true })
  ]
  const esimName = activeEsimProfileDisplayName(props.device)
  if (esimName) facts.push(createFact({ key: 'esim', label: '当前 eSIM', value: esimName, copyable: true }))
  facts.push(
    createFact({ key: 'camped', label: '驻网', value: flightModeEnabled.value ? '关闭' : '开启', tone: 'status' }),
    createFact({ key: 'flight', label: '飞行模式', value: flightModeEnabled.value ? '已开启' : '未开启', tone: 'status' }),
    createFact({ key: 'backend', label: '运行模式', value: backendLabel.value, tone: 'status' })
  )
  return Object.freeze(facts)
})

function createFact({ key, label, value, ...options }: IdentityFactInput): IdentityFact {
  return Object.freeze({
    key,
    label,
    value: String(value ?? '').trim() || '不可用',
    ...options
  })
}

function copyFact(fact: IdentityFact) {
  if (!fact.copyable || fact.value === '不可用') return
  void copyToClipboard(fact.value, `已复制${fact.label}`)
}
</script>

<template>
  <section class="device-overview-identity">
    <header class="identity-header">
      <div>
        <span>DEVICE IDENTITY</span>
        <h3>身份与设备</h3>
        <p>SIM 身份、设备固件与当前运行配置</p>
      </div>
      <button
        type="button"
        class="identity-visibility"
        :aria-label="showSensitive ? '隐藏敏感信息' : '显示敏感信息'"
        :aria-pressed="showSensitive"
        :title="showSensitive ? '隐藏敏感信息' : '显示敏感信息'"
        @click="showSensitive = !showSensitive"
      >
        <Eye24Regular v-if="showSensitive" aria-hidden="true" />
        <EyeOff24Regular v-else aria-hidden="true" />
      </button>
    </header>

    <dl class="identity-grid">
      <div v-for="fact in identityFacts" :key="fact.key" class="identity-fact">
        <dt>{{ fact.label }}</dt>
        <dd>
          <span
            v-if="fact.key === 'operator'"
            class="identity-operator"
            :title="fact.value"
          >
            <span
              v-if="simOperatorCountryCode"
              class="fi identity-flag"
              :class="`fi-${simOperatorCountryCode}`"
              aria-hidden="true"
            />
            <button type="button" @click="copyFact(fact)">{{ fact.value }}</button>
          </span>
          <button
            v-else-if="fact.copyable"
            type="button"
            class="identity-value is-copyable"
            :class="{ 'is-sensitive': fact.sensitive && !showSensitive }"
            :title="fact.sensitive && !showSensitive ? '' : fact.value"
            :aria-label="`复制${fact.label}`"
            @click="copyFact(fact)"
          >
            {{ fact.value }}
          </button>
          <span v-else class="identity-value" :class="{ 'is-status': fact.tone === 'status' }">
            {{ fact.value }}
          </span>
        </dd>
      </div>

      <div v-if="device?.e911_setup_available" class="identity-fact identity-e911">
        <dt>E911 地址</dt>
        <dd>
          <el-button
            size="small"
            type="primary"
            plain
            :loading="e911Starting"
            class="!border-0"
            @click="emit('setup-e911')"
          >
            设置地址
          </el-button>
        </dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.device-overview-identity {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 12px;
  background: var(--ui-surface-strong);
}

.device-overview-identity.is-wide { grid-column: 1 / -1; }
.identity-header { min-height: 78px; padding: 15px 18px; display: flex; align-items: center; justify-content: space-between; gap: 18px; border-bottom: 1px solid var(--ui-border); background: color-mix(in srgb, var(--ui-primary) 4%, var(--ui-surface)); }
.identity-header span { color: var(--ui-primary); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .14em; }
.identity-header h3 { margin: 4px 0 0; color: var(--ui-text); font-size: 17px; font-weight: 650; }
.identity-header p { margin: 3px 0 0; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
.identity-visibility { width: 38px; height: 38px; flex: 0 0 38px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 8px; background: var(--ui-surface-strong); color: var(--ui-text-muted); cursor: pointer; transition: border-color 160ms var(--ui-ease-out), color 160ms var(--ui-ease-out), background-color 160ms var(--ui-ease-out); }
.identity-visibility:hover,
.identity-visibility:focus-visible { border-color: var(--ui-primary); color: var(--ui-primary); }
.identity-visibility:focus-visible { outline: 2px solid color-mix(in srgb, var(--ui-primary) 35%, transparent); outline-offset: 2px; }
.identity-visibility svg { width: 18px; height: 18px; }

.identity-grid { margin: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.identity-fact { min-width: 0; min-height: 66px; padding: 12px 16px; display: grid; align-content: center; gap: 6px; border-bottom: 1px solid var(--ui-border-muted); }
.identity-fact:nth-child(odd) { border-right: 1px solid var(--ui-border-muted); }
.identity-fact dt { color: var(--ui-text-muted); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .07em; }
.identity-fact dd { min-width: 0; margin: 0; color: var(--ui-text); }
.identity-value,
.identity-operator { min-width: 0; max-width: 100%; color: var(--ui-text); font: var(--ui-font-body-sm)/1.45 "v-mono", ui-monospace, monospace; overflow-wrap: anywhere; }
.identity-value.is-copyable,
.identity-operator button { padding: 0; border: 0; background: transparent; color: inherit; text-align: left; cursor: copy; }
.identity-value.is-copyable:hover,
.identity-operator button:hover { color: var(--ui-primary); text-decoration: underline; text-underline-offset: 3px; }
.identity-value.is-sensitive { filter: blur(5px); user-select: none; }
.identity-value.is-status { width: fit-content; padding: 3px 8px; border: 1px solid color-mix(in srgb, var(--ui-primary) 28%, var(--ui-border)); border-radius: 999px; background: color-mix(in srgb, var(--ui-primary) 7%, transparent); color: var(--ui-primary); font-family: "v-sans", system-ui, sans-serif; font-size: var(--ui-font-caption); }
.identity-operator { display: inline-flex; align-items: center; gap: 8px; }
.identity-flag { width: 20px; height: 14px; flex: 0 0 20px; overflow: hidden; border-radius: 2px; box-shadow: 0 0 0 1px color-mix(in srgb, var(--ui-text) 12%, transparent); }
.identity-e911 dd { display: flex; justify-content: flex-start; }

@media (max-width: 520px) {
  .identity-header { align-items: flex-start; padding: 14px; }
  .identity-header p { max-width: 220px; }
  .identity-visibility { width: 44px; height: 44px; flex-basis: 44px; }
  .identity-grid { grid-template-columns: minmax(0, 1fr); }
  .identity-fact { min-height: 62px; padding: 11px 14px; border-right: 0 !important; }
  .identity-value.is-copyable,
  .identity-operator button { min-height: 44px; }
}
</style>
