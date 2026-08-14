<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useNotificationQR } from '../../composables/useNotificationQR'
import { useSettingsStore } from '../../stores/settings'
import NotificationQrConnect from './NotificationQrConnect.vue'

const settingsStore = useSettingsStore()
const { qqForm } = storeToRefs(settingsStore)
const qr = useNotificationQR('qq', {
  onApplied: async () => { await settingsStore.fetchNotifications() }
})
</script>

<template>
  <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(260px,340px)_minmax(0,1fr)]">
    <NotificationQrConnect
      title="QQ 扫码注册"
      :connected="qqForm.enabled"
      :session="qr.session.value"
      :busy="qr.loading.value"
      :polling="qr.polling.value"
      :error="qr.error.value"
      @start="qr.start()"
      @cancel="qr.cancel()"
    />

    <section class="min-w-0" aria-labelledby="qq-manual-title">
      <div class="mb-5 flex items-center justify-between gap-4">
        <h4 id="qq-manual-title" class="text-base font-semibold text-gray-800 dark:text-gray-100">QQ Bot 配置</h4>
        <el-switch v-model="qqForm.enabled" aria-label="启用 QQ Bot" />
      </div>
      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">App ID</label>
            <el-input v-model="qqForm.app_id" placeholder="QQ Bot App ID" />
          </div>
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">App Secret</label>
            <el-input v-model="qqForm.app_secret" type="password" show-password placeholder="********" />
          </div>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">群聊 OpenID</label>
            <el-input v-model="qqForm.group_ids" placeholder="多个使用英文逗号分隔" />
          </div>
          <div class="space-y-1">
            <label class="text-xs font-semibold text-gray-500">私聊 OpenID</label>
            <el-input v-model="qqForm.direct_ids" placeholder="扫码用户会自动加入" />
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
