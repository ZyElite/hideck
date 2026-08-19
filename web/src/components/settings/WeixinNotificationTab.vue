<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useNotificationQR } from '../../composables/useNotificationQR'
import { useSettingsStore } from '../../stores/settings'
import NotificationQrConnect from './NotificationQrConnect.vue'

const settingsStore = useSettingsStore()
const { weixinForm } = storeToRefs(settingsStore)
const qr = useNotificationQR('weixin', {
  onApplied: async () => { await settingsStore.fetchNotifications() }
})

function start() {
  return qr.start({ base_url: weixinForm.value.base_url })
}
</script>

<template>
  <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(260px,340px)_minmax(0,1fr)]">
    <NotificationQrConnect
      title="个人微信扫码"
      :connected="weixinForm.enabled"
      :session="qr.session.value"
      :busy="qr.loading.value"
      :polling="qr.polling.value"
      :error="qr.error.value"
      activate-hint="请打开微信，给这个机器人发一条任意消息完成激活，之后通知才会推送给你。"
      @start="start"
      @cancel="qr.cancel()"
    />

    <section class="min-w-0" aria-labelledby="weixin-manual-title">
      <div class="mb-5 flex items-center justify-between gap-4">
        <h4 id="weixin-manual-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">个人微信 iLink</h4>
        <el-switch v-model="weixinForm.enabled" aria-label="启用个人微信" />
      </div>
      <div class="space-y-4">
        <div class="space-y-1">
          <label class="text-xs font-semibold text-gray-500">iLink 服务地址</label>
          <el-input v-model="weixinForm.base_url" placeholder="https://ilinkai.weixin.qq.com" />
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">允许私聊用户 ID</label>
            <el-input v-model="weixinForm.allowed_user_ids" placeholder="多个使用英文逗号分隔" />
          </div>
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">允许群聊 ID</label>
            <el-input v-model="weixinForm.allowed_group_ids" placeholder="多个使用英文逗号分隔" />
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
