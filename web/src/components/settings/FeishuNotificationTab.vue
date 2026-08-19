<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useNotificationQR } from '../../composables/useNotificationQR'
import { useSettingsStore } from '../../stores/settings'
import NotificationQrConnect from './NotificationQrConnect.vue'

const settingsStore = useSettingsStore()
const { feishuForm } = storeToRefs(settingsStore)
const qr = useNotificationQR('feishu', {
  onApplied: async () => { await settingsStore.fetchNotifications({ silent: true }) }
})
</script>

<template>
  <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(260px,340px)_minmax(0,1fr)]">
    <NotificationQrConnect
      title="飞书扫码创建应用"
      :connected="feishuForm.enabled"
      :session="qr.session.value"
      :busy="qr.loading.value"
      :polling="qr.polling.value"
      :error="qr.error.value"
      activate-hint="请打开飞书，给这个机器人发一条任意消息或把它拉进群并填写 Chat ID，之后通知才会推送给你。"
      @start="qr.start()"
      @cancel="qr.cancel()"
    />

    <section class="min-w-0" aria-labelledby="feishu-manual-title">
      <div class="mb-5 flex items-center justify-between gap-4">
        <h4 id="feishu-manual-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">飞书 Bot 配置</h4>
        <el-switch v-model="feishuForm.enabled" aria-label="启用飞书机器人" />
      </div>
      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">App ID</label>
            <el-input v-model="feishuForm.app_id" placeholder="cli_xxxx" />
          </div>
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">App Secret</label>
            <el-input v-model="feishuForm.app_secret" type="password" show-password placeholder="••••••••" />
          </div>
        </div>
        <div class="space-y-1">
          <label class="text-xs font-semibold text-gray-500">Chat IDs</label>
          <el-input v-model="feishuForm.chat_ids" placeholder="多个群组用英文逗号分隔" />
          <div class="text-xs text-gray-400">飞书群聊的 Chat ID (oc_xxxx)。扫码只创建应用，推送还要填群或先给机器人发一条消息。</div>
        </div>
      </div>
    </section>
  </div>
</template>
