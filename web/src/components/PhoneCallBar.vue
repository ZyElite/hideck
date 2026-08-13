<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { CallEnd24Regular, Dialpad24Regular, Mic24Regular, MicOff24Regular } from '@vicons/fluent'
import { usePhoneStore } from '../stores/phone'
import { formatCallDuration, phoneErrorMessage, phoneStatusLabel } from '../utils/phone'

const router = useRouter()
const phone = usePhoneStore()
const ending = ref(false)
const call = computed(() => phone.currentCall)
const canControl = computed(() => !!call.value
  && !call.value.read_only
  && !(call.value.direction === 'inbound' && call.value.status === 'ringing'))

async function hangup() {
  if (!call.value || ending.value) return
  ending.value = true
  try {
    await phone.hangup(call.value)
  } catch (error) {
    phone.error = phoneErrorMessage(error, '挂断失败')
    ElMessage.error(phone.error)
  } finally {
    ending.value = false
  }
}
</script>

<template>
  <aside v-if="call" class="call-bar" aria-live="polite" aria-label="当前电话">
    <button type="button" class="call-summary" @click="router.push('/phone')">
      <span class="call-pulse" aria-hidden="true" />
      <span class="call-copy">
        <strong>{{ call.peer || '未知号码' }}</strong>
        <small>{{ phoneStatusLabel(call.status) }} · {{ formatCallDuration(call, phone.now) }}</small>
      </span>
      <span v-if="call.read_only" class="read-only-tag">只读</span>
    </button>
    <div class="call-actions">
      <button
        v-if="phone.mediaReady && !call.read_only"
        type="button"
        class="call-action"
        :aria-label="phone.muted ? '取消静音' : '静音'"
        :aria-pressed="phone.muted"
        @click="phone.toggleMute"
      >
        <el-icon><MicOff24Regular v-if="phone.muted" /><Mic24Regular v-else /></el-icon>
      </button>
      <button type="button" class="call-action" aria-label="返回电话页" @click="router.push('/phone')">
        <el-icon><Dialpad24Regular /></el-icon>
      </button>
      <button
        v-if="canControl"
        type="button"
        class="call-action is-danger"
        :disabled="ending"
        aria-label="挂断电话"
        @click="hangup"
      >
        <el-icon><CallEnd24Regular /></el-icon>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.call-bar {
  min-height: 62px;
  margin: 12px 18px 0;
  padding: 8px 10px 8px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid color-mix(in srgb, var(--ui-success) 36%, var(--ui-border));
  border-radius: 14px;
  background: color-mix(in srgb, var(--ui-success) 8%, var(--ui-surface));
  box-shadow: var(--ui-shadow-sm);
}

.call-summary {
  min-width: 0;
  flex: 1;
  display: flex;
  align-items: center;
  gap: 11px;
  border: 0;
  background: transparent;
  color: var(--ui-text);
  text-align: left;
  cursor: pointer;
}

.call-pulse {
  width: 10px;
  height: 10px;
  flex: 0 0 10px;
  border-radius: 50%;
  background: var(--ui-success);
  box-shadow: 0 0 0 5px color-mix(in srgb, var(--ui-success) 14%, transparent);
}

.call-copy { min-width: 0; display: grid; }
.call-copy strong { overflow: hidden; text-overflow: ellipsis; font-family: "v-mono", monospace; font-size: 13px; white-space: nowrap; }
.call-copy small { color: var(--ui-text-muted); font-size: 11px; }
.read-only-tag { padding: 2px 7px; border-radius: 12px; background: var(--ui-surface-muted); color: var(--ui-text-muted); font-size: 10px; }
.call-actions { display: flex; gap: 7px; }
.call-action { width: 44px; height: 44px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 50%; background: var(--ui-surface); color: var(--ui-text); cursor: pointer; }
.call-action.is-danger { border-color: color-mix(in srgb, var(--ui-danger) 40%, var(--ui-border)); background: var(--ui-danger); color: #fff; }
.call-action:disabled { cursor: wait; opacity: .5; }

@media (max-width: 820px) {
  .call-bar { min-height: 58px; margin: 8px 10px 0; }
  .call-copy small { max-width: 148px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .read-only-tag { display: none; }
}
</style>
