<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { MusicNote224Regular } from '@vicons/fluent'
import { commandService } from '../../services/commands'
import type { CommandAttachment } from '../../types/commands'

const props = defineProps<{ attachment: CommandAttachment }>()
const source = ref('')
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null

watch(() => props.attachment.recording, loadRecording, { immediate: true })
onUnmounted(releaseRecording)

async function loadRecording(recording: string) {
  releaseRecording()
  const requestController = new AbortController()
  controller = requestController
  loading.value = true
  error.value = ''
  const result = await commandService.recording(recording, requestController.signal)
  if (requestController.signal.aborted || controller !== requestController) return
  controller = null
  loading.value = false
  if (!result.ok) {
    error.value = result.error.message || '录音加载失败'
    return
  }
  source.value = URL.createObjectURL(result.data)
}

function releaseRecording() {
  controller?.abort()
  controller = null
  if (source.value) URL.revokeObjectURL(source.value)
  source.value = ''
}
</script>

<template>
  <section class="audio-recording" aria-label="通话录音">
    <header>
      <el-icon><MusicNote224Regular /></el-icon>
      <span>{{ attachment.recording }}</span>
    </header>
    <div v-if="loading" class="audio-state">录音载入中</div>
    <div v-else-if="error" class="audio-state error" role="alert">{{ error }}</div>
    <audio v-else-if="source" :src="source" controls preload="metadata" />
  </section>
</template>

<style scoped>
.audio-recording { margin-top: 7px; padding: 10px 12px; border: 1px solid var(--ui-border); border-radius: 8px; background: var(--ui-surface-strong); display: grid; gap: 8px; }
.audio-recording header { min-width: 0; display: flex; align-items: center; gap: 7px; color: #475569; font-size: 12px; }
.audio-recording header span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.audio-recording .el-icon { flex: 0 0 auto; color: var(--ui-primary); font-size: 17px; }
.audio-recording audio { width: min(100%, 420px); height: 40px; display: block; }
.audio-state { min-height: 40px; display: flex; align-items: center; color: #64748b; font-size: 12px; }
.audio-state.error { color: #dc2626; }
:global(html.dark) .audio-recording header { color: #cbd5e1; }
</style>
