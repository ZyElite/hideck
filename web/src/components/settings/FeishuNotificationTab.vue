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
const { feishuForm } = storeToRefs(settingsStore)
const boundChats = computed(() => splitIDs(feishuForm.value.chat_ids))
const waitingForBinding = computed(() => feishuForm.value.enabled && boundChats.value.length === 0)
const refreshingBinding = ref(false)

async function refreshBinding() {
  refreshingBinding.value = true
  try {
    await settingsStore.refreshNotificationBinding('feishu')
  } finally {
    refreshingBinding.value = false
  }
}

const qr = useNotificationQR('feishu', {
  onApplied: async () => { await settingsStore.refreshNotificationChannel('feishu') }
})

useNotificationBindingPoll({
  shouldPoll: waitingForBinding,
  refresh: () => settingsStore.refreshNotificationBinding('feishu')
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
      activate-hint="请打开飞书，给这个机器人发一条任意消息完成激活，之后通知才会推送给你。群聊里需要先 @机器人。"
      @start="qr.start()"
      @cancel="qr.cancel()"
    />

    <section class="min-w-0" aria-labelledby="feishu-manual-title">
      <div class="mb-5 flex items-center justify-between gap-4">
        <h4 id="feishu-manual-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">飞书 Bot 配置</h4>
        <el-switch v-model="feishuForm.enabled" aria-label="启用飞书机器人" />
      </div>
      <div class="mb-4 flex min-h-11 flex-wrap items-center justify-between gap-3 text-sm text-gray-700 dark:text-gray-200" aria-live="polite">
        <span class="min-w-0 break-all">
          {{ boundChats.length ? `已绑定通知目标 ${boundChats.join(', ')}` : '尚未绑定会话。扫码后请给这个机器人发一条消息，这里会自动填入 Chat ID。' }}
        </span>
        <RefreshButton :loading="refreshingBinding" @click="refreshBinding" />
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
          <div class="text-xs text-gray-400">飞书会话的 Chat ID (oc_xxxx)。扫码只创建应用，给机器人发一条消息后会自动填入。</div>
        </div>
      </div>
    </section>
  </div>
</template>
