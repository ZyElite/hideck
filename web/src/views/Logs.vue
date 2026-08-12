<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessage } from 'element-plus'
import PageHeader from '../components/PageHeader.vue'
import { ArrowDownload24Regular, Delete24Regular, Pause24Regular, Play24Regular } from '@vicons/fluent'
import { useLogsStore } from '../stores/logs'
import { useEventStream } from '../composables/useEventStream'
import { deviceNow, formatDeviceDate, formatDeviceDateTime } from '../utils/deviceTime'

// 日志条目类型
interface LogEntry {
  time: string
  level: string
  caller: string
  message: string
  fields?: string
}

const logsStore = useLogsStore()
const { logs } = storeToRefs(logsStore)

const connected = ref(false)
const paused = ref(false)
const autoScroll = ref(true)
const levelFilter = ref<'all' | 'debug' | 'info' | 'warn' | 'error'>('all')
const searchQuery = ref('')
const maxLogs = 1000 // 最大保留日志条数
const lastConnectError = ref<string>('')

// 日志容器引用
const logContainer = ref<HTMLElement | null>(null)

// 过滤后的日志
const filteredLogs = computed(() => {
  let result = logs.value

  // 级别过滤（精确匹配选中的级别）
  if (levelFilter.value !== 'all') {
    result = result.filter(log => 
      log.level.toLowerCase() === levelFilter.value.toLowerCase()
    )
  }

  // 搜索过滤
  if (searchQuery.value.trim()) {
    const q = searchQuery.value.toLowerCase()
    result = result.filter(log =>
      log.message.toLowerCase().includes(q) ||
      log.caller.toLowerCase().includes(q) ||
      (log.fields && log.fields.toLowerCase().includes(q))
    )
  }

  return result
})

const stream = useEventStream<LogEntry>({
  path: '/logs/stream',
  eventName: 'log',
  query: { level: '' },
  parse: (payload) => JSON.parse(payload) as LogEntry,
  onConnected: () => {
    connected.value = true
    lastConnectError.value = ''
  },
  onEvent: (entry) => {
    if (paused.value) return
    logsStore.append(entry, maxLogs)
    if (!autoScroll.value) return
    nextTick(() => {
      if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
    })
  }
})

function connect() {
  connected.value = false
  stream.setPaused(false)
}

function disconnect() {
  stream.disconnect()
  connected.value = false
}

// 暂停/继续
function togglePause() {
  paused.value = !paused.value
  stream.setPaused(paused.value)
  if (!paused.value) connect()
}

// 清空日志
function clearLogs() {
  logsStore.clear()
}

// 导出日志
function exportLogs() {
  const content = filteredLogs.value.map(log => {
    const time = formatDeviceDateTime(log.time, { fallback: log.time })
    const fields = log.fields ? ` ${log.fields}` : ''
    return `[${time}] ${log.level.toUpperCase().padEnd(5)} ${log.caller} ${log.message}${fields}`
  }).join('\n')

  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `logs-${formatDeviceDate(deviceNow())}.txt`
  a.click()
  URL.revokeObjectURL(url)
  ElMessage.success('已导出日志')
}

// 日志级别颜色
function getLevelClass(level: string): string {
  switch (level.toLowerCase()) {
    case 'debug': return 'text-gray-400'
    case 'info': return 'text-blue-500'
    case 'warn': return 'text-yellow-500'
    case 'error': return 'text-red-500'
    case 'fatal': return 'text-red-600 font-bold'
    default: return 'text-gray-500'
  }
}

// 格式化日期时间
function formatDateTime(isoTime: string): string {
  return formatDeviceDateTime(isoTime, { fallback: isoTime })
}

// 加载历史日志
async function loadHistory() {
  const result = await logsStore.fetchHistory(500)
  if (!result.ok) return
  nextTick(() => {
    if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
  })
}

onMounted(async () => {
  await loadHistory()
  connect()
})

onUnmounted(() => {
  disconnect()
})

watch(levelFilter, () => {
  stream.setQuery({ level: levelFilter.value === 'all' ? '' : levelFilter.value })
  if (!paused.value) {
    connect()
  }
})
</script>

<template>
  <div class="app-page logs-page">
    <PageHeader title="实时日志" subtitle="查看系统运行日志，支持过滤和搜索">
      <template #actions>
        <div class="flex items-center gap-2">
          <el-button @click="togglePause" :type="paused ? 'success' : 'warning'" class="!border-0">
            <el-icon><component :is="paused ? Play24Regular : Pause24Regular" /></el-icon>
            {{ paused ? '继续' : '暂停' }}
          </el-button>
          <el-button @click="clearLogs" class="!border-0">
            <el-icon><Delete24Regular /></el-icon>
            清空
          </el-button>
          <el-button @click="exportLogs" type="primary" class="!border-0">
            <el-icon><ArrowDownload24Regular /></el-icon>
            导出
          </el-button>
        </div>
      </template>
    </PageHeader>

    <!-- 连接状态 -->
    <div class="logs-statusbar flex items-center gap-4 mb-3">
      <div class="flex items-center gap-2">
        <span class="logs-connection-dot" :class="connected ? 'is-connected' : 'is-disconnected'" />
        <span class="text-sm font-semibold">{{ connected ? '已连接' : '未连接' }}</span>
      </div>
      <span class="text-sm text-gray-400">{{ logs.length }} 条日志</span>
      <span v-if="!connected && lastConnectError" class="text-sm text-red-500 truncate" :title="lastConnectError">{{ lastConnectError }}</span>
      <div class="flex-1" />
      <el-checkbox v-model="autoScroll" label="自动追尾" />
    </div>

    <!-- 过滤器 -->
    <div class="logs-toolbar ui-card p-3 mb-3">
      <div class="flex flex-wrap items-center gap-4">
        <el-select v-model="levelFilter" placeholder="日志级别" class="w-32">
          <el-option label="全部" value="all" />
          <el-option label="DEBUG" value="debug" />
          <el-option label="INFO" value="info" />
          <el-option label="WARN" value="warn" />
          <el-option label="ERROR" value="error" />
        </el-select>
        <el-input
          v-model="searchQuery"
          placeholder="搜索日志内容..."
          clearable
          class="w-64"
        />
        <span class="text-sm text-gray-400">显示 {{ filteredLogs.length }} / {{ logs.length }} 条</span>
      </div>
    </div>

    <!-- 日志列表 -->
    <div class="log-frame ui-card overflow-hidden">
      <div
        ref="logContainer"
        class="log-console h-[calc(100dvh-292px)] min-h-[420px] overflow-auto font-mono text-sm text-gray-100 p-4"
      >
        <div v-if="filteredLogs.length === 0" class="text-gray-500 text-center py-8">
          {{ connected ? '等待日志...' : '未连接到日志流' }}
        </div>
        <div
          v-for="(log, idx) in filteredLogs"
          :key="idx"
          class="log-row py-0.5 px-2 -mx-2 whitespace-nowrap"
        >
          <span class="text-gray-500">[{{ formatDateTime(log.time) }}]</span>
          <span class="font-bold ml-1 inline-block w-14" :class="getLevelClass(log.level)">{{ log.level.toUpperCase().padEnd(5) }}</span>
          <span class="text-cyan-400 inline-block w-48 truncate align-bottom" :title="log.caller">{{ log.caller }}</span>
          <span class="text-gray-100 ml-1">{{ log.message }}</span>
          <span v-if="log.fields" class="text-amber-300/70 ml-1">{{ log.fields }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.logs-statusbar {
  min-height: 38px;
  padding: 0 4px;
  color: var(--ui-text-muted);
}

.logs-connection-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.logs-connection-dot.is-connected {
  background: var(--ui-success);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--ui-success) 14%, transparent);
}

.logs-connection-dot.is-disconnected {
  background: var(--ui-danger);
}

.logs-toolbar {
  box-shadow: none;
  border-radius: 14px;
}

.log-frame {
  border-color: #263a40;
  border-radius: 18px;
}

.log-console {
  background:
    linear-gradient(90deg, rgba(92, 234, 177, .025) 1px, transparent 1px),
    #060b0e;
  background-size: 36px 100%;
  font-variant-numeric: tabular-nums;
}

.log-row {
  border-left: 2px solid transparent;
  border-radius: 2px;
}

.log-row:hover {
  border-left-color: #38bdb4;
  background: rgba(255, 255, 255, .045);
}

@media (max-width: 640px) {
  .log-console {
    height: calc(100dvh - 360px);
    min-height: 360px;
  }
}
</style>
