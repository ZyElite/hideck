<script setup lang="ts">
import type { UpstreamProxy } from '../../types/api'
import { upstreamProxyIPv6AddressHint } from '../../utils/upstreamProxyAddress'

defineProps<{ editing: boolean }>()
defineEmits<{ save: [] }>()

const open = defineModel<boolean>({ required: true })
const form = defineModel<UpstreamProxy>('form', { required: true })
</script>

<template>
  <el-drawer v-model="open" :title="editing ? '编辑前置代理' : '新增前置代理'" size="min(92vw, 520px)">
    <div class="proxy-editor">
      <section>
        <header><span></span><h3>代理信息</h3></header>
        <div class="proxy-editor-grid">
          <label>
            <span>代理 ID</span>
            <el-input v-model="form.id" :disabled="editing" placeholder="唯一标识，如 jp-proxy-01" />
          </label>
          <label>
            <span>名称</span>
            <el-input v-model="form.name" placeholder="例如：日本代理" />
          </label>
        </div>
        <label>
          <span>SOCKS5 地址</span>
          <el-input v-model="form.addr" placeholder="host:port，例如 1.2.3.4:1080 或 [2001:db8::1]:1080" />
          <small>保存时探测 SOCKS5 握手与 UDP Associate。{{ upstreamProxyIPv6AddressHint }}。</small>
        </label>
        <div class="proxy-editor-toggle">
          <div><strong>启用代理</strong><small>禁用后绑定国家会回退为直连</small></div>
          <el-switch v-model="form.enabled" aria-label="启用代理" />
        </div>
      </section>

      <section>
        <header class="is-auth"><span></span><h3>鉴权设置（可选）</h3></header>
        <div class="proxy-editor-grid">
          <label>
            <span>用户名</span>
            <el-input v-model="form.username" placeholder="留空则免鉴权" />
          </label>
          <label>
            <span>密码</span>
            <el-input v-model="form.password" type="password" show-password placeholder="留空则免鉴权" />
            <small>编辑已有代理时留空会保持原密码不变。</small>
          </label>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="proxy-editor-footer">
        <el-button @click="open = false">取消</el-button>
        <el-button type="primary" @click="$emit('save')">{{ editing ? '更新' : '创建' }}</el-button>
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
.proxy-editor label > span { color: var(--ui-text-muted); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .06em; text-transform: uppercase; }
.proxy-editor label > small { color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); line-height: 1.5; }
.proxy-editor-toggle { min-height: 66px; padding: 12px; display: flex; align-items: center; justify-content: space-between; gap: 16px; border: 1px solid var(--ui-border); border-radius: 7px; background: var(--ui-surface-muted); }
.proxy-editor-toggle div { min-width: 0; display: grid; gap: 3px; }
.proxy-editor-toggle strong { color: var(--ui-text); font-size: 13px; }
.proxy-editor-toggle small { color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
.proxy-editor-footer { display: flex; justify-content: flex-end; gap: 8px; }

@media (max-width: 560px) {
  .proxy-editor-grid { grid-template-columns: minmax(0, 1fr); }
  .proxy-editor-footer .el-button { min-height: 44px; flex: 1; }
}
</style>
