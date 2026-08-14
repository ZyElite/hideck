import { defineStore } from 'pinia'
import axios, { type AxiosInstance } from 'axios'
import { debugCollector } from '../debug/collector'
import type { PasswordCredentialStatus } from '../types/credentials'

export const api: AxiosInstance = axios.create({
  baseURL: '/api'
})

try {
  const token = localStorage.getItem('token') || ''
  if (token) {
    api.defaults.headers.common.Authorization = `Bearer ${token}`
  }
} catch {
  // localStorage may be unavailable in some sandboxed contexts.
}

type AuthState = {
  token: string
  user: unknown | null
}

type LoginResult =
  | { ok: true; credential: PasswordCredentialStatus }
  | { ok: false }

type LoginResponse = {
  token?: string
  credential?: PasswordCredentialStatus
}

function isCredentialStatus(value: unknown): value is PasswordCredentialStatus {
  if (!value || typeof value !== 'object') return false
  const candidate = value as Partial<PasswordCredentialStatus>
  return typeof candidate.change_required === 'boolean' &&
    (candidate.management === 'config_file' || candidate.management === 'environment')
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    token: localStorage.getItem('token') || '',
    user: null
  }),
  getters: {
    isAuthenticated: (state: AuthState) => !!state.token
  },
  actions: {
    applyToken(token: string) {
      this.token = token
      localStorage.setItem('token', token)
      api.defaults.headers.common.Authorization = `Bearer ${token}`
    },
    async login(username: string, password: string): Promise<LoginResult> {
      try {
        const res = await api.post<LoginResponse>('/auth/login', { username, password })
        const token = String(res.data?.token || '')
        if (!token || !isCredentialStatus(res.data?.credential)) {
          throw new Error('登录响应缺少会话或凭证管理状态')
        }
        this.applyToken(token)
        return { ok: true, credential: res.data.credential }
      } catch (e) {
        console.error(e)
        return { ok: false }
      }
    },
    logout() {
      this.token = ''
      localStorage.removeItem('token')
      delete api.defaults.headers.common.Authorization
    }
  }
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    debugCollector.recordApiError(error)
    if (error?.response?.status === 401) {
      if (isPasswordValidationError(error)) {
        return Promise.reject(error)
      }
      try {
        const current = String(window.location.hash || '').replace(/^#/, '') || '/'
        if (!current.startsWith('/login')) {
          sessionStorage.setItem('post_login_redirect', current)
          debugCollector.recordAuthEvent({ ts: Date.now(), kind: '401_redirect', redirectTo: current })
          window.location.hash = `#/login?redirect=${encodeURIComponent(current)}`
          const auth = useAuthStore()
          auth.logout()
          return Promise.reject(error)
        }
      } catch {
        // Accessing sessionStorage/window hash can fail in restricted contexts.
      }
      const auth = useAuthStore()
      auth.logout()
      window.location.hash = '#/login'
    }
    return Promise.reject(error)
  }
)

function isPasswordValidationError(error: unknown): boolean {
  if (!axios.isAxiosError(error)) return false
  const data = error.response?.data as { code?: unknown } | undefined
  return error.config?.url === '/settings/password' && data?.code === 'invalid_password'
}
