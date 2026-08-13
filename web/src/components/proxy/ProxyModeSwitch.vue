<script setup lang="ts">
import { Earth24Regular, Router24Regular } from '@vicons/fluent'

type ProxyWorkspaceMode = 'outbound' | 'upstream'

defineProps<{
  outboundCount: number
  runningOutboundCount: number
  ruleCount: number
  upstreamCount: number
  enabledUpstreamCount: number
}>()

const mode = defineModel<ProxyWorkspaceMode>({ required: true })
</script>

<template>
  <nav class="proxy-mode-switch ui-card" aria-label="代理工作区">
    <div class="proxy-mode-tabs">
      <button
        type="button"
        :aria-current="mode === 'upstream' ? 'page' : undefined"
        :class="{ 'is-active': mode === 'upstream' }"
        @click="mode = 'upstream'"
      >
        <el-icon aria-hidden="true"><Earth24Regular /></el-icon>
        <span><small>ROAMING ROUTES</small><strong>漫游前置代理</strong></span>
        <b>{{ upstreamCount }}</b>
      </button>
      <button
        type="button"
        :aria-current="mode === 'outbound' ? 'page' : undefined"
        :class="{ 'is-active': mode === 'outbound' }"
        @click="mode = 'outbound'"
      >
        <el-icon aria-hidden="true"><Router24Regular /></el-icon>
        <span><small>LOCAL EGRESS</small><strong>本地出站代理</strong></span>
        <b>{{ outboundCount }}</b>
      </button>
    </div>

    <dl class="proxy-mode-stats" aria-label="代理状态摘要">
      <div><dt>前置启用</dt><dd>{{ enabledUpstreamCount }} / {{ upstreamCount }}</dd></div>
      <div><dt>国家规则</dt><dd>{{ ruleCount }}</dd></div>
      <div><dt>本地运行</dt><dd>{{ runningOutboundCount }} / {{ outboundCount }}</dd></div>
    </dl>
  </nav>
</template>

<style scoped>
.proxy-mode-switch { min-height: 62px; margin-bottom: 14px; padding: 0; display: flex; align-items: stretch; justify-content: space-between; gap: 16px; overflow: hidden; border-radius: 7px; }
.proxy-mode-tabs { min-width: 0; display: flex; }
.proxy-mode-tabs button { min-width: 220px; min-height: 62px; padding: 9px 14px; display: grid; grid-template-columns: 28px minmax(0, 1fr) auto; align-items: center; gap: 9px; border: 0; border-right: 1px solid var(--ui-border); border-bottom: 2px solid transparent; background: transparent; color: var(--ui-text-muted); text-align: left; cursor: pointer; transition: color 140ms ease, background-color 140ms ease, border-color 140ms ease; }
.proxy-mode-tabs button:focus-visible { outline: 2px solid var(--ui-primary); outline-offset: -3px; }
.proxy-mode-tabs button.is-active { border-bottom-color: var(--ui-primary); background: color-mix(in srgb, var(--ui-primary) 7%, var(--ui-surface)); color: var(--ui-primary); }
.proxy-mode-tabs .el-icon { font-size: 18px; }
.proxy-mode-tabs button > span { min-width: 0; display: grid; gap: 2px; }
.proxy-mode-tabs small { font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .1em; }
.proxy-mode-tabs strong { overflow: hidden; color: var(--ui-text); font-size: 13px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.proxy-mode-tabs b { min-width: 24px; height: 24px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 999px; color: var(--ui-text); font: 600 var(--ui-font-caption) "v-mono", ui-monospace, monospace; }
.proxy-mode-stats { margin: 0; padding: 9px 14px; display: flex; align-items: center; gap: 22px; }
.proxy-mode-stats div { display: grid; gap: 3px; }
.proxy-mode-stats dt { color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.proxy-mode-stats dd { margin: 0; color: var(--ui-text); font: 600 var(--ui-font-body-sm) "v-mono", ui-monospace, monospace; }

@media (max-width: 980px) {
  .proxy-mode-switch { display: block; }
  .proxy-mode-tabs { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .proxy-mode-tabs button { min-width: 0; }
  .proxy-mode-stats { min-height: 46px; border-top: 1px solid var(--ui-border); justify-content: space-between; }
}

@media (max-width: 460px) {
  .proxy-mode-tabs button { min-height: 58px; padding: 8px 10px; grid-template-columns: 22px minmax(0, 1fr) auto; gap: 6px; }
  .proxy-mode-tabs small { display: none; }
  .proxy-mode-tabs strong { font-size: 12px; }
  .proxy-mode-stats { gap: 10px; padding-inline: 10px; }
}
</style>
