<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useNotificationQR } from '../../composables/useNotificationQR'
import { useSettingsStore } from '../../stores/settings'
import NotificationQrConnect from './NotificationQrConnect.vue'

const settingsStore = useSettingsStore()
const { weComBotForm } = storeToRefs(settingsStore)
const qr = useNotificationQR('wecom-bot', {
  onApplied: async () => { await settingsStore.fetchNotifications() }
})
</script>

<template>
  <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(260px,340px)_minmax(0,1fr)]">
    <NotificationQrConnect
      title="企微机器人扫码"
      :connected="weComBotForm.enabled"
      :session="qr.session.value"
      :busy="qr.loading.value"
      :polling="qr.polling.value"
      :error="qr.error.value"
      activate-hint="机器人已接入。请打开企业微信，给这个机器人发一条任意消息完成激活，之后通知才会推送给你。"
      @start="qr.start()"
      @cancel="qr.cancel()"
    />

    <section class="min-w-0" aria-labelledby="wecom-bot-manual-title">
      <div class="mb-5 flex items-center justify-between gap-4">
        <h4 id="wecom-bot-manual-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">企微长连接机器人</h4>
        <el-switch v-model="weComBotForm.enabled" aria-label="启用企业微信长连接机器人" />
      </div>
      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">Bot ID</label>
            <el-input v-model="weComBotForm.bot_id" placeholder="企业微信 Bot ID" />
          </div>
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">Secret</label>
            <el-input v-model="weComBotForm.secret" type="password" show-password placeholder="********" />
          </div>
        </div>
        <div class="space-y-1">
          <label class="text-xs font-semibold text-gray-500">WebSocket 地址</label>
          <el-input v-model="weComBotForm.websocket_url" placeholder="wss://openws.work.weixin.qq.com" />
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">允许私聊用户 ID</label>
            <el-input v-model="weComBotForm.allowed_user_ids" placeholder="首个私聊用户会自动绑定" />
          </div>
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">允许群聊 ID</label>
            <el-input v-model="weComBotForm.allowed_group_ids" placeholder="多个使用英文逗号分隔" />
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
