<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import {
  CallInbound24Regular,
  CallMissed24Regular,
  CallOutbound24Regular,
  Play24Regular
} from '@vicons/fluent'
import type { PhoneRecord } from '../services/phone'
import { usePhoneStore } from '../stores/phone'
import { formatCallTime, formatRecordDuration, phoneStatusLabel } from '../utils/phone'

defineProps<{ records: PhoneRecord[] }>()

const phone = usePhoneStore()
const playingId = ref<number | null>(null)
const audioURLs = new Map<number, string>()
let playback: HTMLAudioElement | null = null

function statusIcon(record: PhoneRecord) {
  if (record.status === 'missed' || record.status === 'rejected' || record.status === 'busy' || record.status === 'failed') {
    return CallMissed24Regular
  }
  return record.direction === 'inbound' ? CallInbound24Regular : CallOutbound24Regular
}

async function play(record: PhoneRecord) {
  if (!record.recording_name) return
  if (playingId.value === record.id && playback) {
    stopPlayback()
    return
  }
  stopPlayback()
  playingId.value = record.id
  try {
    let url = audioURLs.get(record.id)
    if (!url) {
      url = await phone.recordingURL(record.recording_name)
      audioURLs.set(record.id, url)
    }
    playback = new Audio(url)
    playback.addEventListener('ended', stopPlayback, { once: true })
    playback.addEventListener('error', playbackError, { once: true })
    await playback.play()
  } catch (error) {
    stopPlayback()
    ElMessage.error(error instanceof Error ? error.message : '录音播放失败')
  }
}

function stopPlayback() {
  playback?.pause()
  playback = null
  playingId.value = null
}

function playbackError() {
  stopPlayback()
  ElMessage.error('录音播放失败：浏览器无法解码或读取该文件')
}

onUnmounted(() => {
  stopPlayback()
  audioURLs.forEach((url) => URL.revokeObjectURL(url))
})
</script>

<template>
  <section class="history-panel" aria-labelledby="phone-history-title">
    <header>
      <div>
        <span>CALL LOG</span>
        <h2 id="phone-history-title">最近通话</h2>
      </div>
      <strong>{{ records.length }}</strong>
    </header>
    <div v-if="records.length" class="history-list">
      <article v-for="record in records" :key="record.call_id" class="history-item">
        <div class="history-icon" :class="`is-${record.status}`">
          <el-icon><component :is="statusIcon(record)" /></el-icon>
        </div>
        <div class="history-copy">
          <div class="history-primary">
            <strong>{{ record.peer || '未知号码' }}</strong>
            <time>{{ formatCallTime(record.started_at) }}</time>
          </div>
          <div class="history-secondary">
            <span :class="`status-${record.status}`">{{ phoneStatusLabel(record.status) }}</span>
            <span>{{ formatRecordDuration(record) }}</span>
            <span>{{ record.device_id }}</span>
          </div>
          <p v-if="record.recording_error" class="recording-error">录音失败：{{ record.recording_error }}</p>
        </div>
        <el-tooltip v-if="record.recording_name" content="播放录音" placement="left">
          <button
            type="button"
            class="play-button"
            :aria-label="`${playingId === record.id ? '停止' : '播放'} ${record.peer || '未知号码'} 的通话录音`"
            @click="play(record)"
          >
            <el-icon><Play24Regular /></el-icon>
          </button>
        </el-tooltip>
      </article>
    </div>
    <div v-else class="history-empty">暂无通话记录</div>
  </section>
</template>

<style scoped>
.history-panel { min-height: 320px; overflow: hidden; background: transparent; }
.history-panel > header { min-height: 68px; padding: 14px 16px; display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid var(--ui-border); }
.history-panel header span { color: var(--ui-primary); font-family: "v-mono", monospace; font-size: 9px; font-weight: 700; letter-spacing: .12em; }
.history-panel h2 { margin: 2px 0 0; font-size: 16px; }
.history-panel header > strong { min-width: 28px; padding: 3px 8px; border-radius: 20px; background: var(--ui-surface-muted); color: var(--ui-text-muted); text-align: center; }
.history-list { max-height: 560px; overflow-y: auto; }
.history-item { min-height: 76px; padding: 12px 14px; display: flex; align-items: flex-start; gap: 11px; border-bottom: 1px solid var(--ui-border-muted); }
.history-icon { width: 34px; height: 34px; flex: 0 0 34px; display: grid; place-items: center; border-radius: 50%; background: color-mix(in srgb, var(--ui-primary) 10%, var(--ui-surface)); color: var(--ui-primary); }
.history-icon.is-missed, .history-icon.is-rejected, .history-icon.is-failed { color: var(--ui-danger); background: color-mix(in srgb, var(--ui-danger) 10%, var(--ui-surface)); }
.history-icon.is-busy { color: var(--ui-warning); }
.history-copy { min-width: 0; flex: 1; }
.history-primary { display: flex; justify-content: space-between; gap: 8px; }
.history-primary strong { overflow: hidden; text-overflow: ellipsis; font-family: "v-mono", monospace; font-size: 13px; white-space: nowrap; }
.history-primary time { color: var(--ui-text-muted); font-size: 11px; white-space: nowrap; }
.history-secondary { margin-top: 4px; display: flex; flex-wrap: wrap; gap: 5px 10px; color: var(--ui-text-muted); font-size: 11px; }
.status-missed, .status-rejected, .status-failed { color: var(--ui-danger); }
.status-busy { color: var(--ui-warning); }
.recording-error { margin: 5px 0 0; color: var(--ui-danger); font-size: 11px; line-height: 1.35; }
.play-button { width: 40px; height: 40px; flex: 0 0 40px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 50%; background: var(--ui-surface); color: var(--ui-primary); cursor: pointer; }
.play-button:disabled { cursor: wait; opacity: .45; }
.history-empty { min-height: 250px; display: grid; place-items: center; color: var(--ui-text-muted); }
</style>
