import { api } from '../stores/auth'
import { callService } from './http'
import type { AutomaticTask, AutomaticTaskInput, AutomaticTaskRun } from '../types/automation'

type TaskListResponse = { tasks?: AutomaticTask[] }
type RunListResponse = {
  runs?: AutomaticTaskRun[]
  total?: number
  limit?: number
  offset?: number
}

export const automationService = {
  list() {
    return callService(async () => {
      const response = await api.get<TaskListResponse>('/automatic-tasks')
      return response.data?.tasks || []
    })
  },
  create(input: AutomaticTaskInput) {
    return callService(async () => {
      const response = await api.post<AutomaticTask>('/automatic-tasks', input)
      return response.data
    })
  },
  update(id: number, input: AutomaticTaskInput) {
    return callService(async () => {
      const response = await api.put<AutomaticTask>(`/automatic-tasks/${id}`, input)
      return response.data
    })
  },
  remove(id: number) {
    return callService(async () => {
      await api.delete(`/automatic-tasks/${id}`)
      return true
    })
  },
  runNow(id: number) {
    return callService(async () => {
      const response = await api.post<AutomaticTaskRun>(`/automatic-tasks/${id}/actions/run`)
      return response.data
    })
  },
  runs(params: { taskId?: number; limit?: number; offset?: number }) {
    return callService(async () => {
      const response = await api.get<RunListResponse>('/automatic-task-runs', {
        params: {
          task_id: params.taskId,
          limit: params.limit ?? 50,
          offset: params.offset ?? 0
        }
      })
      return {
        runs: response.data?.runs || [],
        total: response.data?.total || 0,
        limit: response.data?.limit || params.limit || 50,
        offset: response.data?.offset || params.offset || 0
      }
    })
  }
}
