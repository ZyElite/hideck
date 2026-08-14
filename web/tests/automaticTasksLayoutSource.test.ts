import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const view = await readFile(new URL('../src/views/AutomaticTasks.vue', import.meta.url), 'utf8')
const taskList = await readFile(new URL('../src/components/automation/AutomaticTaskList.vue', import.meta.url), 'utf8')
const taskDetail = await readFile(new URL('../src/components/automation/AutomaticTaskDetail.vue', import.meta.url), 'utf8')
const service = await readFile(new URL('../src/services/automation.ts', import.meta.url), 'utf8')

test('automatic tasks use the Studio workspace with real task selection and summary', () => {
  assert.match(view, /VOHIVE \/ AUTOMATION/)
  assert.match(view, /<AutomaticTaskList/)
  assert.match(view, /<AutomaticTaskDetail/)
  assert.match(view, /const selectedTask = computed/)
  assert.match(view, /function syncSelection/)
  assert.match(view, /detailDismissed/)
  assert.doesNotMatch(view, /<el-table/)
  assert.match(taskList, /automaticTaskSummary\(props\.tasks\)/)
  assert.match(taskList, /role="table"/)
  assert.match(taskList, /aria-selected=/)
  assert.match(taskList, /emit\('select', task\)/)
})

test('automatic task rows preserve every production action without optimistic toggle mutation', () => {
  assert.match(taskList, /emit\('toggle', task, Boolean\(\$event\)\)/)
  assert.doesNotMatch(taskList, /v-model="task\.enabled"/)
  assert.match(taskList, /emit\('run', task\)/)
  assert.match(taskList, /emit\('history', task\)/)
  assert.match(taskList, /emit\('edit', task\)/)
  assert.match(taskList, /emit\('delete', task\)/)
  assert.match(view, /automationService\.update\(task\.id, taskToInput\(task, \{ enabled \}\)\)/)
  assert.match(view, /automationService\.runNow\(task\.id\)/)
  assert.match(view, /automationService\.remove\(task\.id\)/)
  assert.match(view, /rowBusy\(task\.id\)/)
})

test('automatic task loading and failures remain explicit while polling real APIs', () => {
  assert.match(view, /TASK_REFRESH_INTERVAL_MS = 10_000/)
  assert.match(view, /window\.setInterval\(\(\) => void loadTasks\(true\)/)
  assert.match(view, /tasksError\.value = result\.error\.message/)
  assert.match(taskList, /class="task-state task-error" role="alert"/)
  assert.match(taskList, /正在读取自动任务/)
  assert.match(taskList, /暂无自动任务/)
  assert.match(service, /api\.get<TaskListResponse>\('\/automatic-tasks'\)/)
  assert.doesNotMatch(view + taskList + taskDetail, /RUN_HISTORY|automationTasks:\s*\[/)
})

test('task detail consumes selected API facts and keeps history and editing reachable', () => {
  assert.match(taskDetail, /automaticTaskTypeLabel\(task\)/)
  assert.match(taskDetail, /automaticTaskEnvironmentLabel\(task\)/)
  assert.match(taskDetail, /automaticTaskPayloadSummary\(task\)/)
  assert.match(taskDetail, /task\.profile_iccid \|\| '未绑定'/)
  assert.match(taskDetail, /emit\('history', task\)/)
  assert.match(taskDetail, /emit\('edit', task\)/)
  assert.match(taskDetail, /aria-label="关闭任务详情"/)
})

test('automatic task workspace uses touch-safe cards and reduced motion', () => {
  assert.match(taskList, /@media \(max-width: 640px\)[\s\S]*\.task-item \{[\s\S]*grid-template-columns: minmax\(0, 1fr\) auto/)
  assert.match(taskList, /\.task-actions :deep\(\.el-button\) \{ width: 44px; height: 44px; \}/)
  assert.match(taskList, /@media \(prefers-reduced-motion: reduce\)/)
  assert.match(view, /task-detail-enter-active[\s\S]*opacity 220ms[\s\S]*transform 220ms/)
  assert.match(view, /task-detail-leave-active[\s\S]*opacity 120ms[\s\S]*transform 120ms/)
  assert.match(view, /@media \(prefers-reduced-motion: reduce\)[\s\S]*transform: none/)
})
