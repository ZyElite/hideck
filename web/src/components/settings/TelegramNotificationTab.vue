<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '../../stores/settings'
import RefreshButton from '../RefreshButton.vue'

const settingsStore = useSettingsStore()
const { telegramForm, refreshingTelegramBinding } = storeToRefs(settingsStore)
const boundTarget = computed(() => Number(telegramForm.value.bound_chat_id) || 0)
const recordingModeOptions = [
  { label: '语音气泡', value: 'voice' },
  { label: '音频附件', value: 'audio' }
]
</script>

<template>
  <section class="min-w-0" aria-labelledby="telegram-settings-title">
    <div class="mb-5 flex items-center justify-between gap-4">
      <div class="min-w-0">
        <h4 id="telegram-settings-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">Telegram Bot</h4>
        <el-link href="https://t.me/BotFather" target="_blank" rel="noopener noreferrer" type="primary">
          @BotFather
        </el-link>
      </div>
      <el-switch v-model="telegramForm.enabled" aria-label="启用 Telegram Bot" />
    </div>

    <div class="mb-5 flex min-h-11 flex-wrap items-center justify-between gap-3 border-y border-gray-200 py-3 dark:border-white/10" aria-live="polite">
      <div class="flex min-w-0 items-center gap-2 text-sm">
        <span
          class="h-2.5 w-2.5 shrink-0 rounded-full"
          :class="boundTarget ? 'bg-emerald-500' : 'bg-amber-500'"
          aria-hidden="true"
        />
        <span class="break-all text-gray-700 dark:text-gray-200">
          {{ boundTarget ? `已绑定通知目标 ${boundTarget}` : '等待管理员首次私聊绑定' }}
        </span>
      </div>
      <RefreshButton :loading="refreshingTelegramBinding" @click="settingsStore.refreshTelegramBinding()" />
    </div>

    <p v-if="telegramForm.binding_error" class="mb-4 text-sm text-red-600 dark:text-red-300" role="alert">
      读取绑定状态失败：{{ telegramForm.binding_error }}
    </p>

    <div class="space-y-4">
      <div class="space-y-1">
        <label for="telegram-bot-token" class="text-xs font-semibold text-gray-600 dark:text-gray-300">Bot Token</label>
        <el-input
          id="telegram-bot-token"
          v-model="telegramForm.bot_token"
          :disabled="!telegramForm.enabled"
          type="password"
          show-password
          autocomplete="off"
          placeholder="123456789:ABC..."
        />
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div class="space-y-1">
          <label for="telegram-admin-id" class="text-xs font-semibold text-gray-600 dark:text-gray-300">管理员 ID</label>
          <el-input id="telegram-admin-id" v-model="telegramForm.admin_id" :disabled="!telegramForm.enabled" type="number" inputmode="numeric" placeholder="例如 123456789" />
        </div>
        <div class="space-y-1">
          <label for="telegram-chat-id" class="text-xs font-semibold text-gray-600 dark:text-gray-300">通知 Chat ID（可选）</label>
          <el-input id="telegram-chat-id" v-model="telegramForm.chat_id" :disabled="!telegramForm.enabled" type="number" inputmode="numeric" placeholder="自动绑定后显示" />
        </div>
      </div>

      <div class="space-y-1">
        <span id="telegram-recording-mode-label" class="text-xs font-semibold text-gray-600 dark:text-gray-300">录音发送样式</span>
        <el-segmented
          v-model="telegramForm.recording_mode"
          :options="recordingModeOptions"
          :disabled="!telegramForm.enabled"
          aria-labelledby="telegram-recording-mode-label"
          block
        />
      </div>

      <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <div class="space-y-1">
          <label for="telegram-base-url" class="text-xs font-semibold text-gray-600 dark:text-gray-300">TG API 反代（可选）</label>
          <el-input id="telegram-base-url" v-model="telegramForm.base_url" :disabled="!telegramForm.enabled" placeholder="https://example.com/bot%s/%s" />
        </div>
        <div class="space-y-1">
          <label for="telegram-proxy" class="text-xs font-semibold text-gray-600 dark:text-gray-300">HTTP 代理（可选）</label>
          <el-input id="telegram-proxy" v-model="telegramForm.proxy" :disabled="!telegramForm.enabled" placeholder="http://127.0.0.1:7890" />
        </div>
      </div>
    </div>
  </section>
</template>
