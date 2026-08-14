<script setup lang="ts">
import { computed } from 'vue'
import QrcodeVue from 'qrcode.vue'
import {
  Dismiss20Regular,
  Open20Regular,
  QrCode24Regular
} from '@vicons/fluent'
import type { NotificationQRSession } from '../../services/notification-onboarding'
import { notificationQRPresentation } from '../../utils/notificationQrPresentation'

const props = defineProps<{
  title: string
  connected: boolean
  session: NotificationQRSession | null
  busy: boolean
  polling: boolean
  error: string
}>()

defineEmits<{
  start: []
  cancel: []
}>()

const presentation = computed(() => notificationQRPresentation(props.session, props.connected))
const openURL = computed(() => {
  const explicit = String(props.session?.open_url || '').trim()
  if (explicit) return explicit
  const qrURL = String(props.session?.qr_url || '').trim()
  return /^https:\/\//i.test(qrURL) ? qrURL : ''
})
</script>

<template>
  <section class="qr-connect" :aria-label="title">
    <div class="qr-connect__header">
      <div class="min-w-0">
        <h4>{{ title }}</h4>
        <div class="qr-status" :class="`qr-status--${presentation.tone}`" aria-live="polite">
          <span class="qr-status__dot" aria-hidden="true"></span>
          <span>{{ presentation.label }}</span>
          <span
            class="qr-status__polling"
            :class="{ 'is-visible': polling }"
            :aria-hidden="!polling"
          >正在查询</span>
        </div>
      </div>
      <div class="qr-connect__actions">
        <el-button
          v-if="session && (session.status === 'wait' || session.status === 'scaned')"
          plain
          :disabled="busy"
          aria-label="取消扫码"
          @click="$emit('cancel')"
        >
          <el-icon><Dismiss20Regular /></el-icon>
          取消
        </el-button>
        <el-button type="primary" :loading="busy" @click="$emit('start')">
          <el-icon><QrCode24Regular /></el-icon>
          {{ session ? '重新扫码' : '扫码连接' }}
        </el-button>
      </div>
    </div>

    <div class="qr-connect__stage">
      <div v-if="session?.qr_url" class="qr-connect__code">
        <QrcodeVue :value="session.qr_url" :size="184" level="M" render-as="svg" />
      </div>
      <div v-else class="qr-connect__empty">
        <el-icon size="34"><QrCode24Regular /></el-icon>
        <span>尚未创建扫码会话</span>
      </div>
    </div>

    <div v-if="openURL" class="qr-connect__open">
      <el-button tag="a" :href="openURL" target="_blank" rel="noopener noreferrer" plain>
        <el-icon><Open20Regular /></el-icon>
        在新窗口打开
      </el-button>
    </div>

    <p v-if="error" class="qr-connect__error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.qr-connect {
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--ui-border);
  border-radius: 6px;
  background: var(--ui-surface-muted);
}

.qr-connect__header,
.qr-connect__actions,
.qr-status,
.qr-connect__open {
  display: flex;
  align-items: center;
}

.qr-connect__header {
  justify-content: space-between;
  gap: 16px;
}

.qr-connect h4 {
  margin: 0 0 4px;
  color: var(--ui-text);
  font-size: var(--ui-font-title);
  font-weight: 650;
}

.qr-connect__actions {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.qr-status {
  min-height: 20px;
  gap: 6px;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-caption);
}

.qr-status__dot {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--ui-text-muted);
}

.qr-status--active .qr-status__dot { background: var(--ui-communication); }
.qr-status--success .qr-status__dot { background: var(--ui-success); }
.qr-status--warning .qr-status__dot { background: var(--ui-warning); }
.qr-status--danger .qr-status__dot { background: var(--ui-danger); }
.qr-status__polling {
  min-width: 4em;
  color: var(--ui-text-muted);
  visibility: hidden;
}

.qr-status__polling.is-visible { visibility: visible; }

.qr-connect__stage {
  display: grid;
  min-height: 216px;
  margin-top: 16px;
  place-items: center;
  border: 1px dashed var(--ui-border);
  border-radius: 4px;
  background: var(--ui-surface);
}

.qr-connect__code {
  display: grid;
  width: 200px;
  height: 200px;
  padding: 8px;
  place-items: center;
  background: #fff;
}

.qr-connect__empty {
  display: grid;
  gap: 8px;
  place-items: center;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
}

.qr-connect__open {
  justify-content: center;
  margin-top: 12px;
}

.qr-connect__error {
  margin: 12px 0 0;
  color: var(--ui-danger);
  font-size: var(--ui-font-body-sm);
  line-height: 1.5;
  overflow-wrap: anywhere;
}

@media (max-width: 640px) {
  .qr-connect__header {
    align-items: stretch;
    flex-direction: column;
  }

  .qr-connect__actions {
    justify-content: stretch;
  }

  .qr-connect__actions :deep(.el-button) {
    min-height: 44px;
    flex: 1 1 0;
  }
}
</style>
