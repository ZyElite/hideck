<script setup lang="ts">
import type { ProxyDevice, ProxyInstance, ProxyMode } from '../../types/api'

defineProps<{
  devices: readonly ProxyDevice[]
  editing: boolean
  modeOptions: readonly { label: string; value: ProxyMode }[]
  saving: boolean
}>()

defineEmits<{ save: [] }>()

const open = defineModel<boolean>({ required: true })
const form = defineModel<ProxyInstance>('form', { required: true })
</script>

<template>
  <el-drawer v-model="open" :title="editing ? '编辑代理实例' : '新增代理实例'" size="min(92vw, 560px)">
    <div class="proxy-editor">
      <section>
        <header><span></span><h3>基础设置</h3></header>

        <div class="proxy-editor-grid">
          <label>
            <span>实例 ID</span>
            <el-input v-model="form.id" :disabled="editing" placeholder="唯一标识" />
          </label>
          <label>
            <span>名称</span>
            <el-input v-model="form.name" placeholder="显示名称" />
          </label>
        </div>

        <label>
          <span>绑定设备（必填）</span>
          <el-select v-model="form.device_id" placeholder="选择设备" class="w-full">
            <el-option v-for="device in devices" :key="device.id" :label="`${device.name} (${device.interface})`" :value="device.id" />
          </el-select>
        </label>

        <label>
          <span>代理模式</span>
          <el-select v-model="form.mode" placeholder="选择代理模式" class="w-full">
            <el-option v-for="option in modeOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
        </label>

        <div class="proxy-editor-grid">
          <label>
            <span>监听地址</span>
            <el-input v-model="form.listen_addr" placeholder="0.0.0.0" />
          </label>
          <label>
            <span>监听端口</span>
            <el-input-number v-model="form.listen_port" :min="1" :max="65535" class="!w-full" />
          </label>
        </div>

        <div class="proxy-editor-toggle">
          <div><strong>启用实例</strong><small>禁用后实例不会自动启动</small></div>
          <el-switch v-model="form.enabled" aria-label="启用实例" />
        </div>
      </section>

      <section>
        <header class="is-auth"><span></span><h3>认证设置</h3></header>
        <div class="proxy-editor-toggle">
          <div><strong>启用账号认证</strong><small>关闭后将允许免认证连接</small></div>
          <el-switch v-model="form.auth_enabled" aria-label="启用账号认证" />
        </div>

        <div v-if="form.auth_enabled" class="proxy-editor-grid">
          <label>
            <span>用户名</span>
            <el-input v-model="form.username" placeholder="例如 user01" />
          </label>
          <label>
            <span>密码</span>
            <el-input v-model="form.password" type="password" show-password placeholder="请输入密码" />
          </label>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="proxy-editor-footer">
        <el-button @click="open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="$emit('save')">保存</el-button>
      </div>
    </template>
  </el-drawer>
</template>

<style scoped>
.proxy-editor { display: grid; gap: 24px; padding-bottom: 24px; }
.proxy-editor section { display: grid; gap: 16px; }
.proxy-editor header { min-height: 29px; display: flex; align-items: center; gap: 8px; border-bottom: 1px solid var(--ui-border); }
.proxy-editor header > span { width: 3px; height: 16px; border-radius: 2px; background: var(--ui-primary); }
.proxy-editor header.is-auth > span { background: var(--ui-warning); }
.proxy-editor h3 { margin: 0; color: var(--ui-text); font-size: 13px; font-weight: 700; }
.proxy-editor-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.proxy-editor label { min-width: 0; display: grid; gap: 6px; }
.proxy-editor label > span { color: var(--ui-text-muted); font: 700 10px "v-mono", ui-monospace, monospace; letter-spacing: .06em; text-transform: uppercase; }
.proxy-editor-toggle { min-height: 66px; padding: 12px; display: flex; align-items: center; justify-content: space-between; gap: 16px; border: 1px solid var(--ui-border); border-radius: 7px; background: var(--ui-surface-muted); }
.proxy-editor-toggle div { min-width: 0; display: grid; gap: 3px; }
.proxy-editor-toggle strong { color: var(--ui-text); font-size: 13px; }
.proxy-editor-toggle small { color: var(--ui-text-muted); font-size: 11px; }
.proxy-editor-footer { display: flex; justify-content: flex-end; gap: 8px; }

@media (max-width: 560px) {
  .proxy-editor-grid { grid-template-columns: minmax(0, 1fr); }
  .proxy-editor-footer .el-button { min-height: 44px; flex: 1; }
}
</style>
