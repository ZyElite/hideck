<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowClockwise24Regular } from '@vicons/fluent'
import { automationService } from '../services/automation'
import type { AutomaticTask, AutomaticTaskRun } from '../types/automation'

const props = defineProps<{
  modelValue: boolean
  task: AutomaticTask | null
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const runs = ref<AutomaticTaskRun[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
let refreshTimer: number | null = null

watch([() => props.modelValue, () => props.task?.id], ([open]) => {
  clearRefresh()
  if (!open || !props.task) return
  page.value = 1
  void loadRuns()
  refreshTimer = window.setInterval(() => void loadRuns(true), 3000)
})

onUnmounted(clearRefresh)

async function loadRuns(silent = false) {
  if (!props.task) return
  if (!silent) loading.value = true
  const result = await automationService.runs({
    taskId: props.task.id,
    limit: pageSize,
    offset: (page.value - 1) * pageSize
  })
  loading.value = false
  if (!result.ok) {
    if (!silent) ElMessage.error(result.error.message || '运行记录加载失败')
    return
  }
  runs.value = result.data.runs
  total.value = result.data.total
}

function clearRefresh() {
  if (refreshTimer !== null) window.clearInterval(refreshTimer)
  refreshTimer = null
}

function statusType(status: AutomaticTaskRun['status']) {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

function statusText(status: AutomaticTaskRun['status']) {
  return { queued: '排队中', running: '执行中', success: '成功', failed: '失败' }[status]
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="task ? `${task.name} · 运行记录` : '运行记录'"
    width="min(920px, 96vw)"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="history-toolbar">
      <el-button circle text :loading="loading" aria-label="刷新运行记录" @click="loadRuns()">
        <el-icon v-if="!loading"><ArrowClockwise24Regular /></el-icon>
      </el-button>
    </div>
    <el-table :data="runs" v-loading="loading" stripe max-height="480">
      <el-table-column label="状态" width="92">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" effect="plain">{{ statusText(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="计划时间" min-width="170">
        <template #default="{ row }">{{ formatDate(row.scheduled_at) }}</template>
      </el-table-column>
      <el-table-column prop="attempts" label="尝试" width="72" />
      <el-table-column label="结果" min-width="280" show-overflow-tooltip>
        <template #default="{ row }">
          <span :class="row.error ? 'run-error' : ''">{{ row.error || row.output || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="完成时间" min-width="170">
        <template #default="{ row }">{{ formatDate(row.finished_at) }}</template>
      </el-table-column>
    </el-table>
    <div class="history-pagination">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next"
        @current-change="loadRuns()"
      />
    </div>
  </el-dialog>
</template>

<style scoped>
.history-toolbar { display: flex; justify-content: flex-end; min-height: 32px; }
.history-pagination { display: flex; justify-content: flex-end; padding-top: 16px; }
.run-error { color: var(--el-color-danger); }
</style>
