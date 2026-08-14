<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { usePhoneStore } from '../stores/phone'
import { Expand, Fold } from '@element-plus/icons-vue'
import LoadingScreen from '../components/LoadingScreen.vue'
import ErrorBoundary from '../components/ErrorBoundary.vue'
import SwitchDark from '../components/SwitchDark.vue'
import PhoneCallBar from '../components/PhoneCallBar.vue'
import { debugCollector } from '../debug/collector'
import {
  Mail24Regular,
  Settings24Regular,
  SignOut24Regular,
  Board24Regular,
  Phone24Regular,
  Globe24Regular,
  DocumentText24Regular,
  Chat24Regular,
  CalendarClock24Regular,
  Dialpad24Regular
} from '@vicons/fluent'

defineProps({
  isDark: {
    type: Boolean,
    required: true
  }
})

const emit = defineEmits(['toggle-theme'])

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const phone = usePhoneStore()
const collapsed = ref(localStorage.getItem('sidebar_collapsed') === '1')
const isMobile = ref(false)
const viewportCompact = ref(false)
const viewportNarrow = ref(false)
const drawerOpen = ref(false)
const debugOpen = ref(false)
const DebugPanel = defineAsyncComponent(() => import('../components/DebugPanel.vue'))

const menuItems = [
  { index: '/', label: '仪表盘', icon: Board24Regular },
  { index: '/devices', label: '设备管理', icon: Phone24Regular },
  { index: '/phone', label: '电话', icon: Dialpad24Regular },
  { index: '/proxy', label: '代理管理', icon: Globe24Regular },
  { index: '/sms', label: '短信中心', icon: Mail24Regular },
  { index: '/commands', label: '命令中心', icon: Chat24Regular },
  { index: '/automatic-tasks', label: '自动任务', icon: CalendarClock24Regular },
  { index: '/logs', label: '实时日志', icon: DocumentText24Regular },
  { index: '/settings', label: '系统设置', icon: Settings24Regular }
]
const mobileMenuItems = menuItems.filter((item) => ['/', '/phone', '/devices', '/sms', '/commands'].includes(item.index))
const effectiveCollapsed = computed(() => collapsed.value || viewportCompact.value)
const expandedSidebarWidth = computed(() => viewportNarrow.value ? '190px' : '218px')

async function handleLogout() {
  const { ElMessageBox } = await import('element-plus')
  const confirmed = await ElMessageBox.confirm('确认退出登录？', '提示', {
    confirmButtonText: '退出',
    cancelButtonText: '取消',
    type: 'warning'
  })
    .then(() => true)
    .catch(() => false)
  if (!confirmed) return
  auth.logout()
  router.push('/login')
}

function syncIsMobile() {
  if (typeof window === 'undefined') return
  isMobile.value = window.matchMedia('(max-width: 820px)').matches
  viewportCompact.value = !isMobile.value && window.matchMedia('(max-width: 1180px)').matches
  viewportNarrow.value = window.matchMedia('(max-width: 1480px)').matches
  if (!isMobile.value) {
    drawerOpen.value = false
  }
}

function handleNavToggle() {
  if (isMobile.value) {
    drawerOpen.value = true
    return
  }
  collapsed.value = !collapsed.value
  localStorage.setItem('sidebar_collapsed', collapsed.value ? '1' : '0')
}

function onKeydown(e: KeyboardEvent) {
  if (e.ctrlKey && e.shiftKey && String(e.key || '').toLowerCase() === 'd') {
    e.preventDefault()
    debugOpen.value = !debugOpen.value
    localStorage.setItem('debug_panel_open', debugOpen.value ? '1' : '0')
  }
}

onMounted(() => {
  syncIsMobile()
  window.addEventListener('resize', syncIsMobile, { passive: true })

  const saved = localStorage.getItem('debug_panel_open')
  debugOpen.value = saved === '1'

  window.addEventListener('keydown', onKeydown)
  void phone.initialize()
})

onUnmounted(() => {
  window.removeEventListener('resize', syncIsMobile)
  window.removeEventListener('keydown', onKeydown)
  phone.dispose()
})

watch(
  () => route.fullPath,
  () => {
    drawerOpen.value = false
  }
)

watch(
  () => debugOpen.value,
  (v) => {
    localStorage.setItem('debug_panel_open', v ? '1' : '0')
  }
)

watch(
  () => debugCollector.openPanelRequestAt.value,
  (ts) => {
    if (!ts) return
    debugOpen.value = true
  }
)

const activePath = computed(() => route.path)
const activeMenuItem = computed(() => menuItems.find((item) => item.index === route.path) || menuItems[0])
</script>

<template>
  <el-container v-if="auth.isAuthenticated && route.name !== 'Login'" class="h-full flow-shell">
    <el-aside
      v-if="!isMobile"
      :width="effectiveCollapsed ? '78px' : expandedSidebarWidth"
      class="h-full transition-[width] duration-200 relative sidebar-shell app-sidebar"
    >
      <div class="h-16 px-4 flex items-center sidebar-brand" :class="effectiveCollapsed ? 'justify-center px-0' : ''">
        <div class="sidebar-brand-icon">H</div>
        <div v-if="!effectiveCollapsed" class="ml-3 min-w-0">
          <div class="sidebar-brand-title">HiDeck</div>
          <div class="sidebar-brand-subtitle">MODEM CONTROL</div>
        </div>
      </div>

      <el-menu
        :collapse="effectiveCollapsed"
        :collapse-transition="false"
        :default-active="activePath"
        class="sidebar-menu !border-0 !border-r-0 !bg-transparent mt-2"
        router
      >
        <el-menu-item v-for="item in menuItems" :key="item.index" :index="item.index">
          <el-icon><component :is="item.icon" /></el-icon>
          <template #title><span class="sidebar-menu-label">{{ item.label }}</span></template>
        </el-menu-item>
      </el-menu>

      <div v-if="effectiveCollapsed" class="sidebar-account-compact">
        <el-tooltip content="退出登录" placement="right">
          <button type="button" aria-label="退出登录" @click="handleLogout">
            <el-icon><SignOut24Regular /></el-icon>
          </button>
        </el-tooltip>
      </div>
      <div v-else class="sidebar-account-expanded">
        <div class="sidebar-account flex items-center gap-3">
          <div class="sidebar-account-icon"><el-icon><Settings24Regular /></el-icon></div>
          <div class="flex-1 min-w-0">
            <div class="text-sm font-semibold truncate text-white">Admin</div>
            <div class="text-xs truncate sidebar-account-role">Administrator</div>
          </div>
          <el-button text type="danger" aria-label="退出登录" @click="handleLogout">
            <el-icon><SignOut24Regular /></el-icon>
          </el-button>
        </div>
      </div>
    </el-aside>

    <el-drawer v-model="drawerOpen" direction="ltr" size="256px" :with-header="false" class="mobile-drawer">
      <div class="h-full relative sidebar-shell app-sidebar">
        <div class="h-16 px-4 flex items-center">
          <div class="sidebar-brand-icon">H</div>
          <div class="ml-3 min-w-0">
            <div class="sidebar-brand-title">HiDeck</div>
            <div class="sidebar-brand-subtitle">MODEM CONTROL</div>
          </div>
        </div>

        <el-menu
          :collapse="false"
          :collapse-transition="false"
          :default-active="activePath"
          class="sidebar-menu !border-0 !border-r-0 !bg-transparent mt-2"
          router
        >
          <el-menu-item v-for="item in menuItems" :key="item.index" :index="item.index">
            <el-icon><component :is="item.icon" /></el-icon>
            <template #title><span class="sidebar-menu-label">{{ item.label }}</span></template>
          </el-menu-item>
        </el-menu>

        <div class="absolute bottom-3 w-full px-3">
          <div class="sidebar-account flex items-center gap-3">
            <div class="sidebar-account-icon">
              <el-icon><Settings24Regular /></el-icon>
            </div>
            <div class="flex-1 min-w-0">
              <div class="text-sm font-semibold truncate text-white">Admin</div>
              <div class="text-xs truncate sidebar-account-role">Administrator</div>
            </div>
            <el-button text type="danger" @click="handleLogout">
              <el-icon><SignOut24Regular /></el-icon>
            </el-button>
          </div>
        </div>
      </div>
    </el-drawer>

    <el-container class="h-full">
      <el-header class="app-topbar h-16 px-3 sm:px-5 flex items-center justify-between sticky top-0 z-10">
        <div class="topbar-side topbar-side-left">
          <el-button text :aria-label="isMobile ? '打开导航' : collapsed ? '展开侧边栏' : '收起侧边栏'" @click="handleNavToggle" class="nav-toggle !px-2">
            <el-icon>
              <Expand v-if="isMobile || collapsed" />
              <Fold v-else />
            </el-icon>
          </el-button>
          <span class="topbar-product">HIDECK</span>
        </div>

        <div class="topbar-route"><strong>{{ activeMenuItem.label }}</strong></div>

        <div class="topbar-side topbar-side-right">
          <div class="hidden sm:flex service-state" aria-label="实时连接">
            <span class="service-state-dot" />
            <span>实时连接</span>
          </div>
          <SwitchDark :is-dark="isDark" @toggle="(e) => emit('toggle-theme', e)" />
        </div>
      </el-header>

      <PhoneCallBar />

      <el-main class="app-main px-4 pb-6 sm:px-7 sm:pb-8 overflow-auto">
        <div class="main-inner mx-auto w-full">
          <router-view v-slot="{ Component, route: r }">
            <ErrorBoundary v-if="Component" title="页面渲染失败">
              <component :is="Component" :key="r.fullPath" />
            </ErrorBoundary>
            <LoadingScreen v-else title="正在加载页面…" subtitle="正在准备页面组件与资源" />
          </router-view>
        </div>
      </el-main>
    </el-container>

    <nav v-if="isMobile" class="mobile-bottom-nav" aria-label="移动导航">
      <button
        v-for="item in mobileMenuItems"
        :key="item.index"
        type="button"
        :class="{ 'is-active': activePath === item.index }"
        :aria-label="item.label"
        :aria-current="activePath === item.index ? 'page' : undefined"
        @click="router.push(item.index)"
      >
        <el-icon><component :is="item.icon" /></el-icon>
      </button>
    </nav>

    <DebugPanel v-model="debugOpen" />
  </el-container>
</template>

<style scoped>
.sidebar-shell {
  font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
  --sidebar-menu-text: #9bb2b6;
  --sidebar-menu-hover-bg: rgba(255, 255, 255, 0.06);
  --sidebar-menu-active-bg: rgba(56, 189, 180, 0.14);
  --sidebar-menu-active-color: #d8fffa;
  --sidebar-menu-active-ring: rgba(56, 189, 180, 0.24);
}

:deep(.sidebar-menu) {
  margin-top: 0 !important;
  padding: 18px 12px;
  border-right: 0 !important;
  --el-menu-hover-bg-color: var(--sidebar-menu-hover-bg);
  --el-menu-active-color: var(--sidebar-menu-active-color);
  --el-menu-text-color: var(--sidebar-menu-text);
}

:deep(.sidebar-menu .el-menu-item) {
  height: 46px;
  min-height: 46px;
  line-height: 46px;
  margin: 0 0 5px;
  border-radius: 8px;
  padding-left: 14px !important;
  padding-right: 14px !important;
  font-size: 14px;
  font-weight: 400;
  letter-spacing: 0;
  color: var(--sidebar-menu-text);
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

:deep(.sidebar-menu .el-menu-item .el-icon) {
  margin-right: 14px !important;
  font-size: 20px;
}

:deep(.sidebar-menu .el-menu-item .el-icon svg) {
  width: 1.18rem;
  height: 1.18rem;
}

:deep(.sidebar-menu .el-menu-item:hover) {
  background: var(--sidebar-menu-hover-bg);
}

:deep(.sidebar-menu .el-menu-item.is-active) {
  position: relative;
  background: var(--sidebar-menu-active-bg);
  color: var(--sidebar-menu-active-color);
  box-shadow: none;
}

:deep(.sidebar-menu .el-menu-item.is-active::before) {
  position: absolute;
  left: 0;
  width: 2px;
  height: 30px;
  border-radius: 2px;
  background: var(--ui-primary);
  box-shadow: none;
  content: "";
}

:deep(.sidebar-menu .el-menu-item.is-active .el-icon),
:deep(.sidebar-menu .el-menu-item.is-active .sidebar-menu-label) {
  color: inherit;
}

:deep(.sidebar-menu .el-menu-item::after) {
  display: none !important;
}

:deep(.sidebar-menu.el-menu--collapse) {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item) {
  width: 46px;
  height: 46px;
  min-height: 46px;
  line-height: 46px;
  margin: 0 auto 5px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  padding: 0 !important;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .el-icon) {
  width: 1.18rem;
  height: 1.18rem;
  margin: 0 !important;
  font-size: 1.18rem;
  line-height: 1;
  display: grid;
  place-items: center;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .el-icon svg) {
  width: 1.18rem;
  height: 1.18rem;
  display: block;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .el-menu-tooltip__trigger) {
  position: static;
  inset: auto;
  width: 100%;
  height: 100%;
  padding: 0 !important;
  display: grid;
  place-items: center;
}

:deep(.sidebar-menu.el-menu--collapse > .el-menu-item [class^=el-icon]) {
  width: 1.18rem !important;
}

:deep(.sidebar-menu.el-menu--collapse .el-tooltip) {
  width: 36px;
  display: grid;
  place-items: center;
}

:deep(.sidebar-menu.el-menu--collapse .el-tooltip__trigger) {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
}

.main-inner {
  max-width: 100%;
}

@media (min-width: 768px) {
  .main-inner {
    max-width: clamp(0px, calc(100vw - 240px - 48px), 80rem);
  }
}

:deep(.mobile-drawer .el-drawer__body) {
  padding: 0 !important;
}

.app-sidebar {
  border: 0;
  border-right: 1px solid var(--ui-border);
  border-radius: 0;
  background: linear-gradient(180deg, color-mix(in srgb, var(--ui-nav) 99%, transparent), color-mix(in srgb, #050a0c 98%, transparent));
  box-shadow: none;
  color: #fff;
}

.sidebar-brand {
  height: 78px !important;
  flex: 0 0 78px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.sidebar-brand-title {
  min-height: auto;
  padding: 0;
  background: none;
  color: #f5fbfb;
  filter: none;
  -webkit-text-fill-color: currentColor;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0;
  line-height: 1.1;
}

.sidebar-brand-subtitle {
  margin-top: 3px;
  color: #7fa4a8;
  font-family: "v-mono", ui-monospace, monospace;
  font-size: var(--ui-font-caption);
  font-weight: 600;
  letter-spacing: 0;
}

.sidebar-brand-icon {
  width: 44px;
  height: 44px;
  display: grid;
  place-items: center;
  border: 1px solid rgba(130, 232, 223, 0.34);
  border-radius: 14px;
  background: linear-gradient(145deg, rgba(92, 234, 177, 0.24), rgba(91, 225, 222, 0.08));
  color: #8ff7cb;
  box-shadow: inset 0 0 20px rgba(92, 234, 177, 0.08);
  font-size: 20px;
  font-weight: 800;
}

:global(html.dark) .sidebar-shell {
  --sidebar-menu-text: #93a8ad;
  --sidebar-menu-hover-bg: rgba(255, 255, 255, 0.06);
  --sidebar-menu-active-bg: rgba(56, 189, 180, 0.14);
  --sidebar-menu-active-color: #d8fffa;
  --sidebar-menu-active-ring: rgba(56, 189, 180, 0.24);
}

.sidebar-menu-label {
  font-weight: 500;
  letter-spacing: 0;
}

.sidebar-account {
  min-height: 52px;
  padding: 8px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.04);
}

.sidebar-account-compact {
  position: absolute;
  bottom: 20px;
  left: 0;
  width: 100%;
  display: grid;
  place-items: center;
}

.sidebar-account-expanded {
  position: absolute;
  right: 12px;
  bottom: 16px;
  left: 12px;
}

.sidebar-account-compact button {
  width: 44px;
  height: 44px;
  display: grid;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.035);
  color: var(--sidebar-menu-text);
  cursor: pointer;
}

.sidebar-account-icon {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  place-items: center;
  border-radius: 4px;
  background: rgba(56, 189, 180, 0.12);
  color: #83e0d8;
}

.sidebar-account-role {
  color: #789398;
}

.app-topbar {
  width: calc(100% - 36px);
  margin: 18px 18px 0 !important;
  padding: 0 22px !important;
  flex: 0 0 64px;
  border: 1px solid var(--ui-border);
  border-radius: 20px;
  background: color-mix(in srgb, var(--ui-surface-subtle) 92%, transparent);
  box-shadow: var(--ui-shadow-sm);
  backdrop-filter: blur(18px);
}

.nav-toggle {
  width: 40px;
  color: var(--ui-text-muted);
}

.topbar-route {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
}

.topbar-route strong {
  color: var(--ui-text);
  font-size: 15px;
  font-weight: 600;
}

.topbar-side {
  min-width: 180px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.topbar-side-right { justify-content: flex-end; }

.topbar-product {
  color: var(--ui-text-muted);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: var(--ui-font-caption);
  letter-spacing: 0.16em;
}

.service-state {
  min-height: 32px;
  align-items: center;
  gap: 7px;
  padding: 0 10px;
  border: 0;
  color: var(--ui-success);
  font-size: 12px;
  font-weight: 600;
}

.service-state-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--ui-success);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--ui-success) 16%, transparent);
}

.app-main {
  padding-top: 34px !important;
  background: var(--ui-bg);
}

.mobile-bottom-nav { display: none; }

.main-inner {
  max-width: 1600px;
}

@media (min-width: 768px) {
  .main-inner {
    max-width: 1680px;
  }
}

@media (max-width: 1480px) and (min-width: 821px) {
  .app-main { padding-right: 14px !important; padding-left: 22px !important; }
}

@media (max-width: 1180px) and (min-width: 821px) {
  .app-main { padding: 24px 8px 48px 16px !important; }
  .app-topbar {
    width: calc(100% - 24px);
    margin-right: 12px !important;
    margin-left: 12px !important;
    padding-right: 14px !important;
    padding-left: 14px !important;
  }
}

@media (max-width: 820px) {
  .app-topbar {
    width: 100%;
    height: 56px !important;
    margin: 0 !important;
    padding: 0 14px !important;
    flex-basis: 56px;
    border-width: 0 0 1px;
    border-radius: 0;
    background: color-mix(in srgb, var(--ui-surface-subtle) 88%, transparent);
  }

  .topbar-side { min-width: auto; }
  .topbar-product { display: none; }

  .app-main {
    padding: 20px 12px calc(86px + env(safe-area-inset-bottom)) !important;
  }

  .mobile-bottom-nav {
    position: fixed;
    z-index: 40;
    right: 12px;
    bottom: calc(8px + env(safe-area-inset-bottom));
    left: 12px;
    height: 58px;
    display: grid;
    grid-template-columns: repeat(5, minmax(44px, 1fr));
    border: 1px solid color-mix(in srgb, var(--ui-text) 12%, transparent);
    border-radius: 18px;
    background: color-mix(in srgb, var(--ui-surface) 88%, transparent);
    box-shadow: 0 1px 0 color-mix(in srgb, var(--ui-text) 7%, transparent) inset, 0 22px 64px rgba(0, 0, 0, .38);
    backdrop-filter: blur(22px) saturate(1.2);
    overflow: hidden;
  }

  .mobile-bottom-nav button {
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--ui-text-muted);
    cursor: pointer;
  }

  .mobile-bottom-nav button.is-active {
    background: color-mix(in srgb, var(--ui-primary) 12%, transparent);
    color: var(--ui-primary);
    box-shadow: 2px 0 0 var(--ui-primary) inset;
  }

  .mobile-bottom-nav .el-icon { font-size: 19px; }
}
</style>
