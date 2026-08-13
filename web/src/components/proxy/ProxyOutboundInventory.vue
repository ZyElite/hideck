<script setup lang="ts">
import {
  ArrowSync24Regular,
  Delete24Regular,
  Edit24Regular,
  Play24Regular,
  Router24Regular,
  Stop24Regular
} from '@vicons/fluent'
import type { OutboundProxyPresentation } from '../../utils/proxyPresentation'
import ProxyInventoryShell from './ProxyInventoryShell.vue'
import ProxyStatusBadge from './ProxyStatusBadge.vue'

defineProps<{
  loading: boolean
  refreshing: boolean
  rows: readonly OutboundProxyPresentation[]
}>()

defineEmits<{
  add: []
  delete: [id: string]
  edit: [id: string]
  refresh: []
  restart: [id: string]
  start: [id: string]
  stop: [id: string]
}>()
</script>

<template>
  <ProxyInventoryShell
    add-label="新增实例"
    :empty="rows.length === 0"
    empty-subtitle="创建实例并绑定设备后，运行状态和出口信息会显示在这里。"
    empty-title="暂无本地出站实例"
    kicker="LOCAL EGRESS INVENTORY"
    :loading="loading"
    :refreshing="refreshing"
    subtitle="每个实例绑定一个真实设备网络接口，提供 SOCKS5 或 HTTP 出口。"
    title="本地出站实例"
    title-id="outbound-inventory-title"
    tone="primary"
    @add="$emit('add')"
    @refresh="$emit('refresh')"
  >
    <template #icon><el-icon><Router24Regular /></el-icon></template>

    <div class="proxy-table-wrap">
      <table class="proxy-inventory-table">
        <thead>
          <tr>
            <th scope="col">实例名称</th>
            <th scope="col">监听地址</th>
            <th scope="col">运行状态</th>
            <th scope="col">启用状态</th>
            <th scope="col">模式 / 认证</th>
            <th scope="col">设备绑定</th>
            <th scope="col"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <td data-label="实例名称">
              <strong>{{ row.name }}</strong>
              <small class="proxy-row-id">{{ row.id || '无 ID' }}</small>
            </td>
            <td data-label="监听地址"><code>{{ row.endpoint }}</code></td>
            <td data-label="运行状态">
              <ProxyStatusBadge :label="row.runningLabel" :tone="row.runningTone" />
              <small v-if="row.lastError" class="proxy-runtime-error">{{ row.lastError }}</small>
            </td>
            <td data-label="启用状态">
              <ProxyStatusBadge :label="row.enabledLabel" :tone="row.enabledTone" />
            </td>
            <td data-label="模式 / 认证">
              <strong>{{ row.modeLabel }}</strong>
              <small>{{ row.authenticationLabel }}</small>
            </td>
            <td data-label="设备绑定">{{ row.deviceLabel }}</td>
            <td data-label="操作">
              <span class="proxy-row-actions">
                <button
                  v-if="!row.running"
                  type="button"
                  :disabled="!row.enabled"
                  :aria-label="`启动 ${row.name}`"
                  :title="`启动 ${row.name}`"
                  @click="$emit('start', row.id)"
                ><Play24Regular aria-hidden="true" /></button>
                <button
                  v-else
                  type="button"
                  :aria-label="`停止 ${row.name}`"
                  :title="`停止 ${row.name}`"
                  @click="$emit('stop', row.id)"
                ><Stop24Regular aria-hidden="true" /></button>
                <button
                  type="button"
                  :disabled="!row.enabled"
                  :aria-label="`重启 ${row.name}`"
                  :title="`重启 ${row.name}`"
                  @click="$emit('restart', row.id)"
                ><ArrowSync24Regular aria-hidden="true" /></button>
                <button type="button" :aria-label="`编辑 ${row.name}`" :title="`编辑 ${row.name}`" @click="$emit('edit', row.id)">
                  <Edit24Regular aria-hidden="true" />
                </button>
                <button type="button" class="is-danger" :aria-label="`删除 ${row.name}`" :title="`删除 ${row.name}`" @click="$emit('delete', row.id)">
                  <Delete24Regular aria-hidden="true" />
                </button>
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </ProxyInventoryShell>
</template>

<style scoped>
.proxy-table-wrap { min-width: 0; }
.proxy-inventory-table { width: 100%; border-collapse: collapse; table-layout: fixed; }
.proxy-inventory-table th { height: 40px; padding: 0 10px; border-bottom: 1px solid var(--ui-border); color: var(--ui-text-muted); font-size: 10px; font-weight: 600; text-align: left; }
.proxy-inventory-table td { min-width: 0; min-height: 58px; padding: 11px 10px; border-bottom: 1px solid var(--ui-border-muted); color: var(--ui-text); font-size: 12px; vertical-align: middle; overflow-wrap: anywhere; }
.proxy-inventory-table tr:last-child td { border-bottom: 0; }
.proxy-inventory-table th:nth-child(1) { width: 14%; }
.proxy-inventory-table th:nth-child(2) { width: 17%; }
.proxy-inventory-table th:nth-child(3) { width: 14%; }
.proxy-inventory-table th:nth-child(4) { width: 11%; }
.proxy-inventory-table th:nth-child(5) { width: 13%; }
.proxy-inventory-table th:nth-child(6) { width: 14%; }
.proxy-inventory-table th:nth-child(7) { width: 172px; }
.proxy-inventory-table strong { display: block; font-weight: 650; }
.proxy-inventory-table small { display: block; margin-top: 3px; color: var(--ui-text-muted); font-size: 10px; }
.proxy-row-id,
.proxy-inventory-table code { color: var(--ui-text-muted); font: 10px/1.5 "v-mono", ui-monospace, monospace; }
.proxy-inventory-table code { color: var(--ui-text); font-size: 11px; }
.proxy-runtime-error { color: var(--ui-danger) !important; overflow-wrap: anywhere; }
.proxy-row-actions { display: flex; justify-content: flex-end; gap: 3px; }
.proxy-row-actions button { width: 36px; height: 36px; display: grid; place-items: center; border: 1px solid transparent; border-radius: 5px; background: transparent; color: var(--ui-text-muted); cursor: pointer; }
.proxy-row-actions button:hover,
.proxy-row-actions button:focus-visible { border-color: var(--ui-border); background: var(--ui-surface-muted); color: var(--ui-text); }
.proxy-row-actions button:disabled { opacity: .35; cursor: not-allowed; }
.proxy-row-actions button.is-danger:hover,
.proxy-row-actions button.is-danger:focus-visible { color: var(--ui-danger); }
.proxy-row-actions svg { width: 16px; height: 16px; }

@media (max-width: 900px) {
  .proxy-inventory-table th:nth-child(6),
  .proxy-inventory-table td:nth-child(6) { display: none; }
  .proxy-inventory-table th:nth-child(7) { width: 172px; }
}

@media (max-width: 900px) {
  .proxy-inventory-table thead { display: none; }
  .proxy-inventory-table,
  .proxy-inventory-table tbody { display: grid; gap: 10px; }
  .proxy-inventory-table tr { margin: 0 12px; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); border: 1px solid var(--ui-border); border-radius: 7px; overflow: hidden; background: var(--ui-surface-strong); }
  .proxy-inventory-table tr:first-child { margin-top: 12px; }
  .proxy-inventory-table tr:last-child { margin-bottom: 12px; }
  .proxy-inventory-table td,
  .proxy-inventory-table td:nth-child(6) { min-height: 66px; padding: 11px 12px; display: grid; align-content: center; gap: 6px; border-bottom: 1px solid var(--ui-border-muted) !important; }
  .proxy-inventory-table td:nth-child(odd) { border-right: 1px solid var(--ui-border-muted); }
  .proxy-inventory-table td::before { content: attr(data-label); color: var(--ui-text-muted); font: 700 9px "v-mono", ui-monospace, monospace; letter-spacing: .06em; }
  .proxy-inventory-table td:nth-last-child(-n+2) { border-bottom: 0 !important; }
  .proxy-row-actions { justify-content: flex-start; }
  .proxy-row-actions button { width: 44px; height: 44px; }
}

@media (max-width: 460px) {
  .proxy-inventory-table tr { grid-template-columns: minmax(0, 1fr); }
  .proxy-inventory-table td:nth-child(odd) { border-right: 0; }
  .proxy-inventory-table td:nth-last-child(2) { border-bottom: 1px solid var(--ui-border-muted) !important; }
}
</style>
