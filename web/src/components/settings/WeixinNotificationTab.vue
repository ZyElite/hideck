<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useNotificationBindingPoll } from '../../composables/useNotificationBindingPoll'
import { useNotificationQR } from '../../composables/useNotificationQR'
import { splitIDs } from '../../stores/notificationChannelForms'
import { useSettingsStore } from '../../stores/settings'
import RefreshButton from '../RefreshButton.vue'
import NotificationQrConnect from './NotificationQrConnect.vue'

const settingsStore = useSettingsStore()
const { weixinForm } = storeToRefs(settingsStore)
const boundUsers = computed(() => splitIDs(weixinForm.value.allowed_user_ids))
const waitingForBinding = computed(() => weixinForm.value.enabled && boundUsers.value.length === 0)
const refreshingBinding = ref(false)

async function refreshBinding() {
  refreshingBinding.value = true
  try {
    await settingsStore.fetchNotifications({ silent: true })
  } finally {
    refreshingBinding.value = false
  }
}
const qr = useNotificationQR('weixin', {
  onApplied: async (session) => {
    await settingsStore.fetchNotifications({ silent: true })
    const userID = String(session.bot_user_id || '').trim()
    if (userID) {
      weixinForm.value.allowed_user_ids = splitIDs(`${weixinForm.value.allowed_user_ids},${userID}`).join(',')
    }
  }
})

useNotificationBindingPoll({
  shouldPoll: waitingForBinding,
  refresh: () => settingsStore.fetchNotifications({ silent: true })
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
      <div class="mb-4 flex min-h-11 flex-wrap items-center justify-between gap-3 text-sm text-gray-700 dark:text-gray-200" aria-live="polite">
        <span class="min-w-0 break-all">
          {{ boundUsers.length ? `已绑定通知目标 ${boundUsers.join(', ')}` : '尚未绑定私聊用户。扫码后请给这个机器人发一条消息，这里会自动填入用户 ID。' }}
        </span>
        <RefreshButton :loading="refreshingBinding" @click="refreshBinding" />
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
