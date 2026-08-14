<script setup lang="ts">
import { computed, defineAsyncComponent, ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from './stores/auth'
import LoadingScreen from './components/LoadingScreen.vue'
import ErrorState from './components/ErrorState.vue'
import { ElMessage } from 'element-plus'
import { shouldShowDisclaimer } from './disclaimer'
import { systemService } from './services/system'
import { configureDeviceTime, deviceNow, resetDeviceTime } from './utils/deviceTime'
import { Warning24Regular } from '@vicons/fluent'

const DISCLAIMER_AGREED_AT_KEY = 'hideck_disclaimer_agreed_at'

const route = useRoute()
const auth = useAuthStore()

const isDark = ref(localStorage.getItem('theme') === 'dark')
const showDisclaimer = ref(false)
const confirmText = ref('')
const expectedConfirmText = '我同意并确认'
const canAccept = computed(() => confirmText.value === expectedConfirmText)
const deviceTimeState = ref<'idle' | 'loading' | 'ready' | 'error'>(auth.isAuthenticated ? 'loading' : 'idle')
const deviceTimeError = ref('')
let deviceTimeGeneration = 0

function toggleTheme() {
  isDark.value = !isDark.value
  const mode = isDark.value ? 'dark' : 'light'
  localStorage.setItem('theme', mode)
  updateHtmlClass(mode)
}

function updateHtmlClass(mode: 'dark' | 'light') {
  if (mode === 'dark') {
    document.documentElement.classList.add('dark')
  } else {
    document.documentElement.classList.remove('dark')
  }
}

onMounted(() => {
  updateHtmlClass(isDark.value ? 'dark' : 'light')
})

// 监听登录状态，每周首次登录弹一次（同意状态持久化在 localStorage，
// 跨会话/跨标签页生效，距上次同意满一周后再次登录才会重新弹出）
watch([() => auth.isAuthenticated, deviceTimeState], ([isAuthenticated, timeState]) => {
  if (isAuthenticated && timeState === 'ready') {
    const agreedAtRaw = localStorage.getItem(DISCLAIMER_AGREED_AT_KEY)
    const agreedAt = agreedAtRaw === null ? null : Number(agreedAtRaw)
    if (shouldShowDisclaimer(agreedAt, deviceNow())) {
      confirmText.value = ''
      showDisclaimer.value = true
    }
  } else {
    showDisclaimer.value = false
  }
}, { immediate: true })

async function syncDeviceTime() {
  const generation = ++deviceTimeGeneration
  deviceTimeState.value = 'loading'
  deviceTimeError.value = ''
  const requestStartedAt = Date.now()
  const result = await systemService.getTime()
  const responseReceivedAt = Date.now()
  if (generation !== deviceTimeGeneration || !auth.isAuthenticated) return
  if (!result.ok) {
    deviceTimeState.value = 'error'
    deviceTimeError.value = result.error.message
    return
  }
  try {
    configureDeviceTime(result.data, requestStartedAt, responseReceivedAt)
    deviceTimeState.value = 'ready'
  } catch (error) {
    deviceTimeState.value = 'error'
    deviceTimeError.value = error instanceof Error ? error.message : '设备时间同步失败'
  }
}

watch(() => auth.isAuthenticated, (isAuthenticated) => {
  if (isAuthenticated) {
    void syncDeviceTime()
    return
  }
  deviceTimeGeneration++
  resetDeviceTime()
  deviceTimeState.value = 'idle'
  deviceTimeError.value = ''
}, { immediate: true })

function acceptDisclaimer() {
  if (!canAccept.value) return
  localStorage.setItem(DISCLAIMER_AGREED_AT_KEY, String(deviceNow()))
  showDisclaimer.value = false
}

function rejectDisclaimer() {
  ElMessage.warning('正在退出并清理软件...')
  const token = localStorage.getItem('token') || ''
  fetch('/api/system/uninstall', {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : undefined
  })
    .finally(() => {
      document.body.innerHTML = '<div style="display:flex;height:100vh;background:#0a0a0a;align-items:center;justify-content:center;font-size:24px;color:#ef4444;font-weight:bold;font-family:sans-serif;flex-direction:column;gap:16px;"><div><svg style="width:64px;height:64px;" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" /></svg></div><div>软件已被卸载 / 服务已终止</div></div>'
    })
}

const AuthenticatedShell = defineAsyncComponent(() => import('./layouts/AuthenticatedShell.vue'))
const UnauthenticatedShell = defineAsyncComponent(() => import('./layouts/UnauthenticatedShell.vue'))
const shell = computed(() =>
  auth.isAuthenticated && route.name !== 'Login' ? AuthenticatedShell : UnauthenticatedShell
)
const canRenderShell = computed(() => !auth.isAuthenticated || deviceTimeState.value === 'ready')
</script>

<template>
  <div class="app-root h-screen w-screen overflow-hidden font-sans transition-colors duration-300">
    <Suspense v-if="canRenderShell">
      <template #default>
        <component :is="shell" :is-dark="isDark" @toggle-theme="toggleTheme" />
      </template>
      <template #fallback>
        <LoadingScreen />
      </template>
    </Suspense>
    <LoadingScreen v-else-if="deviceTimeState === 'loading'" />
    <div v-else class="h-full flex items-center justify-center p-6">
      <ErrorState
        class="w-full max-w-xl"
        title="设备时间同步失败"
        :message="deviceTimeError"
        retry-text="重试"
        @retry="syncDeviceTime"
      />
    </div>

    <!-- 高级感全屏免责声明弹窗 -->
    <Transition name="fade-slide">
      <div v-if="showDisclaimer" class="license-overlay fixed inset-0 z-[9999] flex items-center justify-center">
        <div class="license-dialog relative w-full max-w-lg p-7 mx-4 overflow-hidden">
          <div class="relative z-10">
            <div class="license-icon flex items-center justify-center w-12 h-12 mx-auto mb-5">
              <Warning24Regular class="w-6 h-6" />
            </div>
            
            <h2 class="mb-5 text-2xl font-extrabold text-center text-gray-900 dark:text-white tracking-tight">HiDeck 最终用户许可与免责声明</h2>
            
            <div class="space-y-4 text-[14px] text-gray-600 dark:text-gray-300 leading-relaxed font-medium">
              <div class="flex items-start">
                <div class="license-index">1</div>
                <p>本软件（HiDeck）属于个人开发者业余时间开发的工具软件，仅供技术研究、学习交流和个人内部测试使用。<strong class="license-emphasis">严禁用于任何商业用途</strong>，严禁作为生产环境的基础设施。</p>
              </div>
              <div class="flex items-start">
                <div class="license-index">2</div>
                <p>使用者承诺将严格遵守所在国家或地区的相关法律法规。<strong class="text-red-500 dark:text-red-400">严禁将本软件用于电信诈骗、垃圾短信发送、非法网络代理、渗透测试等任何非法或违规场景</strong>。</p>
              </div>
              <div class="flex items-start">
                <div class="license-index">3</div>
                <p>本软件涉及底层 Modem 通信操作，可能包含未知的缺陷。对于因使用本软件引发的硬件损坏、通信资费异常、隐私泄露等直接或间接损失，<strong>由使用者自行承担所有责任</strong>。</p>
              </div>
              <div class="flex items-start">
                <div class="license-index">4</div>
                <p>一旦点击继续即表示无条件接受本协议。如果您拒绝，本软件将立即触发自毁与环境清理机制以确保设备安全。</p>
              </div>
            </div>
            
            <div class="mt-6 pt-5 border-t border-gray-100 dark:border-gray-800">
              <p class="mb-3 text-xs font-bold text-center text-gray-500 dark:text-gray-400">
                请输入「<span class="license-emphasis select-all">{{ expectedConfirmText }}</span>」以解锁按钮
              </p>
              
              <div class="mb-5">
                <input 
                  type="text" 
                  v-model="confirmText" 
                  class="license-input w-full px-4 py-3 text-center text-sm font-semibold outline-none transition-all"
                  :placeholder="`请输入：${expectedConfirmText}`"
                  @paste.prevent
                  autocomplete="off"
                />
              </div>

              <div class="flex gap-4">
                <button @click="rejectDisclaimer" class="license-reject flex-1 px-4 py-3 text-sm font-bold transition-colors">
                  拒绝并卸载
                </button>
                <button 
                  @click="acceptDisclaimer" 
                  :disabled="!canAccept"
                  :class="[
                    'flex-[1.5] px-4 py-3 text-sm font-bold tracking-wide transition-all duration-300 rounded-md',
                    canAccept 
                      ? 'license-accept cursor-pointer'
                      : 'license-accept license-accept-disabled cursor-not-allowed'
                  ]"
                >
                  同意并继续
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style>
.app-root {
  background: var(--ui-bg);
  color: var(--ui-text);
}

.license-overlay {
  padding: 16px;
  background: rgba(4, 17, 20, 0.72);
}

.license-dialog {
  border: 1px solid var(--ui-border);
  border-radius: 8px;
  background: var(--ui-surface-strong);
  box-shadow: var(--ui-shadow-lg);
}

.license-dialog h2 {
  color: var(--ui-text);
  background: none;
  -webkit-text-fill-color: currentColor;
  letter-spacing: 0;
}

.license-icon {
  border: 1px solid color-mix(in srgb, var(--ui-warning) 34%, var(--ui-border));
  border-radius: 6px;
  background: color-mix(in srgb, var(--ui-warning) 12%, var(--ui-surface));
  color: var(--ui-warning);
}

.license-index {
  width: 24px;
  height: 24px;
  margin: 2px 12px 0 0;
  flex: 0 0 24px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  background: color-mix(in srgb, var(--ui-primary) 12%, var(--ui-surface));
  color: var(--ui-primary);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: var(--ui-font-caption);
  font-weight: 700;
}

.license-emphasis {
  color: var(--ui-primary);
}

.license-input {
  border: 1px solid var(--ui-border);
  border-radius: 4px;
  background: var(--ui-surface-subtle);
  color: var(--ui-text);
}

.license-input:focus {
  border-color: var(--ui-primary);
  box-shadow: var(--ui-focus);
}

.license-reject,
.license-accept {
  border: 1px solid var(--ui-border);
  border-radius: 4px;
}

.license-reject {
  background: var(--ui-surface-muted);
  color: var(--ui-text-muted);
}

.license-reject:hover {
  border-color: var(--ui-danger);
  color: var(--ui-danger);
}

.license-accept {
  border-color: var(--ui-primary);
  background: var(--ui-primary-solid);
  color: #fff;
}

.license-accept:hover {
  background: var(--ui-primary-hover);
}

.license-accept-disabled {
  border-color: var(--ui-border);
  background: var(--ui-surface-muted);
  color: var(--ui-text-muted);
  opacity: .58;
}

.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: opacity 180ms ease, transform 180ms ease;
}

.fade-slide-enter-from {
  opacity: 0;
  transform: translateY(20px);
}

.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-20px);
}

/* Custom Scrollbar */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}
::-webkit-scrollbar-track {
  background: transparent;
}
::-webkit-scrollbar-thumb {
  background: #cbd5e1;
  border-radius: 4px;
}
.dark ::-webkit-scrollbar-thumb {
  background: #334155;
}
::-webkit-scrollbar-thumb:hover {
  background: #94a3b8;
}
.dark ::-webkit-scrollbar-thumb:hover {
  background: #475569;
}
</style>
