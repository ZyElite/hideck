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
    await settingsStore.refreshNotificationBinding('weixin')
  } finally {
    refreshingBinding.value = false
  }
}
const qr = useNotificationQR('weixin', {
  onApplied: async (session) => {
    await settingsStore.refreshNotificationChannel('weixin')
    const userID = String(session.bot_user_id || '').trim()
    if (userID) {
      weixinForm.value.allowed_user_ids = splitIDs(`${weixinForm.value.allowed_user_ids},${userID}`).join(',')
    }
  }
})

useNotificationBindingPoll({
  shouldPoll: waitingForBinding,
  refresh: () => settingsStore.refreshNotificationBinding('weixin')
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
      activate-hint="扫码后，记得在微信里给机器人发句话。收到你的第一条消息后，它才能把通知发给你。"
      @start="start"
      @cancel="qr.cancel()"
    />

    <section class="min-w-0" aria-labelledby="weixin-manual-title">
      <div class="mb-5 flex items-center justify-between gap-4">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h4 id="weixin-manual-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">个人微信 iLink</h4>
            <span class="rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-xs font-semibold leading-none text-amber-700 dark:text-amber-300">
              会话型通知渠道
            </span>
          </div>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-500 dark:text-gray-400" role="note">
            个人微信通知依赖最近一次聊天。太久没互动时，微信可能会暂停推送；给机器人发条消息就能恢复。
          </p>
        </div>
        <el-switch v-model="weixinForm.enabled" aria-label="启用个人微信" />
      </div>
      <div class="mb-4 flex min-h-11 flex-wrap items-center justify-between gap-3 text-sm text-gray-700 dark:text-gray-200" aria-live="polite">
        <span class="min-w-0 break-all">
          {{ boundUsers.length ? `通知会发给 ${boundUsers.join(', ')}` : '还没绑定接收人。扫码后给机器人发条消息，这里就会自动显示你的用户 ID。' }}
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
