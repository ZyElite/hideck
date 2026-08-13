<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessage, ElMessageBox } from 'element-plus'
import WorkspaceStage from '../components/WorkspaceStage.vue'
import ErrorState from '../components/ErrorState.vue'
import ProxyCountryRuleDrawer from '../components/proxy/ProxyCountryRuleDrawer.vue'
import ProxyInstanceEditorDrawer from '../components/proxy/ProxyInstanceEditorDrawer.vue'
import ProxyOutboundInventory from '../components/proxy/ProxyOutboundInventory.vue'
import ProxyUpstreamInventory from '../components/proxy/ProxyUpstreamInventory.vue'
import { usePollingScheduler } from '../composables/usePollingScheduler'
import { useProxyStore } from '../stores/proxy'
import { useUpstreamProxyStore } from '../stores/upstream-proxy'
import type { ProxyInstance, ProxyDevice, ProxyMode, UpstreamProxy } from '../types/api'
import { toAppError } from '../services/http'
import {
  createOutboundProxyPresentation,
  createUpstreamProxyPresentation
} from '../utils/proxyPresentation'
import {
  upstreamProxyAddressWarning,
  upstreamProxyIPv6AddressHint
} from '../utils/upstreamProxyAddress'
import {
  Router24Regular,
  Earth24Regular
} from '@vicons/fluent'

// ── Tab 控制 ──
const activeTab = ref('upstream') // 默认展示前置代理

// ══════════════════════════════════════════════════════
// 出站代理（原有逻辑，不动）
// ══════════════════════════════════════════════════════
const proxyStore = useProxyStore()
const { statusMap } = storeToRefs(proxyStore)

const initialLoading = ref(true)
const refreshing = ref(false)
const loadError = ref<{ message: string; status?: number } | null>(null)
const instances = ref<ProxyInstance[]>([])
const devices = ref<ProxyDevice[]>([])
const saving = ref(false)

const drawerOpen = ref(false)
const editingInstance = ref<ProxyInstance | null>(null)
const instanceForm = ref<ProxyInstance>({
  id: '',
  name: '',
  device_id: '',
  enabled: true,
  mode: 'socks5',
  listen_addr: '0.0.0.0',
  listen_port: 1080,
  auth_enabled: false,
  username: '',
  password: ''
})

const modeOptions: Array<{ label: string; value: ProxyMode }> = [
  { label: 'SOCKS5', value: 'socks5' },
  { label: 'HTTP', value: 'http' }
]

const outboundRows = computed(() => instances.value.map(instance => (
  createOutboundProxyPresentation({
    instance,
    status: statusMap.value[instance.id],
    device: devices.value.find(device => device.id === instance.device_id)
  })
)))

watch(
  () => instanceForm.value.auth_enabled,
  (enabled) => {
    if (!enabled) {
      instanceForm.value.username = ''
      instanceForm.value.password = ''
    }
  }
)

async function fetchOverview(opts: { silent?: boolean; initial?: boolean } = {}) {
  const isInitial = opts.initial === true
  const silent = opts.silent === true
  if (isInitial) {
    initialLoading.value = true
  } else if (!silent) {
    refreshing.value = true
  }
  loadError.value = null

  try {
    const result = await proxyStore.fetchOverview()
    if (!result.ok) throw new Error(result.error.message)
    instances.value = proxyStore.instances.map((inst) => ({
      ...inst,
      mode: inst.mode || 'socks5'
    }))
    devices.value = proxyStore.devices
  } catch (e: unknown) {
    const err = toAppError(e)
    loadError.value = {
      message: err.message || '加载代理配置失败',
      status: err.status
    }
  } finally {
    if (isInitial) {
      initialLoading.value = false
    } else if (!silent) {
      refreshing.value = false
    }
  }
}

async function saveConfig() {
  saving.value = true
  try {
    const result = await proxyStore.saveConfig(instances.value)
    if (!result.ok) throw new Error(result.error.message || '保存失败')
    ElMessage.success('配置已保存')
    await fetchOverview()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function startInstance(id: string) {
  try {
    const result = await proxyStore.startInstance(id)
    if (!result.ok) throw new Error(result.error.message || '启动失败')
    ElMessage.success('已启动')
    await fetchOverview()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '启动失败')
  }
}

async function stopInstance(id: string) {
  try {
    const result = await proxyStore.stopInstance(id)
    if (!result.ok) throw new Error(result.error.message || '停止失败')
    ElMessage.success('已停止')
    await fetchOverview()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '停止失败')
  }
}

async function restartInstance(id: string) {
  try {
    const result = await proxyStore.restartInstance(id)
    if (!result.ok) throw new Error(result.error.message || '重启失败')
    ElMessage.success('已重启')
    await fetchOverview()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '重启失败')
  }
}

function resetForm() {
  instanceForm.value = {
    id: '',
    name: '',
    device_id: devices.value[0]?.id || '',
    enabled: true,
    mode: 'socks5',
    listen_addr: '0.0.0.0',
    listen_port: 1080,
    auth_enabled: false,
    username: '',
    password: ''
  }
}

async function openDrawer(inst?: ProxyInstance) {
  if (!inst) {
    editingInstance.value = null
    resetForm()
    instanceForm.value.id = `proxy-${Date.now()}`
    instanceForm.value.listen_port = 10800 + instances.value.length
    drawerOpen.value = true
    return
  }

  editingInstance.value = inst
  instanceForm.value = { ...inst, mode: inst.mode || 'socks5' }
  drawerOpen.value = true

  try {
    const result = await proxyStore.fetchInstance(inst.id)
    if (result.ok) {
      instanceForm.value = { ...result.data, mode: result.data.mode || 'socks5' }
    }
  } catch {
    ElMessage.warning('读取完整实例配置失败，已使用概览数据')
  }
}

function saveForm() {
  const form = { ...instanceForm.value }

  if (!form.id) {
    ElMessage.warning('实例 ID 不能为空')
    return
  }
  if (!form.device_id) {
    ElMessage.warning('必须绑定设备')
    return
  }
  if (form.mode !== 'socks5' && form.mode !== 'http') {
    ElMessage.warning('代理模式仅支持 SOCKS5 或 HTTP')
    return
  }
  if (form.listen_port <= 0 || form.listen_port > 65535) {
    ElMessage.warning('监听端口无效')
    return
  }
  if (!form.listen_addr) {
    form.listen_addr = '0.0.0.0'
  }

  if (form.auth_enabled) {
    form.username = (form.username || '').trim()
    form.password = (form.password || '').trim()
    if (!form.username || !form.password) {
      ElMessage.warning('启用认证时必须填写用户名和密码')
      return
    }
  } else {
    form.username = ''
    form.password = ''
  }

  if (editingInstance.value) {
    const idx = instances.value.findIndex((i) => i.id === editingInstance.value!.id)
    if (idx >= 0) {
      instances.value[idx] = form
    }
  } else {
    if (instances.value.some((i) => i.id === form.id)) {
      ElMessage.warning('实例 ID 已存在')
      return
    }
    instances.value.push(form)
  }

  drawerOpen.value = false
  saveConfig()
}

async function deleteInstance(id: string) {
  const confirmed = await ElMessageBox.confirm(
    `确定删除实例 ${id}？`,
    '确认删除',
    { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
  ).then(() => true).catch(() => false)

  if (!confirmed) return
  instances.value = instances.value.filter((i) => i.id !== id)
  saveConfig()
}

function editOutboundInstance(id: string) {
  const instance = instances.value.find(item => item.id === id)
  if (instance) void openDrawer(instance)
}

const pollEnabled = computed(() => !initialLoading.value && instances.value.length > 0)
usePollingScheduler(() => fetchOverview({ silent: true }), 5000, {
  enabled: pollEnabled,
  maxIntervalMs: 60000,
  backgroundIntervalMs: 15000
})


// ══════════════════════════════════════════════════════
// 前置代理（新增逻辑）
// ══════════════════════════════════════════════════════
const upstreamStore = useUpstreamProxyStore()
const enabledUpstreamCount = computed(() => upstreamStore.proxies.filter((proxy) => proxy.enabled).length)
const runningOutboundCount = computed(() => outboundRows.value.filter((instance) => instance.running).length)

const upstreamLoading = ref(true)
const upstreamRefreshing = ref(false)
const upstreamError = ref<{ message: string; status?: number } | null>(null)

// ── 编辑 Drawer ──
const upstreamDrawerOpen = ref(false)
const editingUpstream = ref<UpstreamProxy | null>(null)
const upstreamForm = ref<UpstreamProxy>({
  id: '',
  name: '',
  addr: '',
  username: '',
  password: '',
  enabled: true
})

// ── 国家规则管理 Drawer ──
const countryRuleDrawerOpen = ref(false)
const countryRuleTargetProxy = ref<UpstreamProxy | null>(null)
const selectedCountryCode = ref('')

const availableCountries = computed(() => {
  if (!countryRuleTargetProxy.value) return []
  return upstreamStore.countries.filter((country) => {
    const rule = upstreamStore.getRuleForCountry(country.country_code)
    return !rule || rule.upstream_proxy_id === countryRuleTargetProxy.value!.id
  })
})

const currentProxyCountryRules = computed(() => {
  if (!countryRuleTargetProxy.value) return []
  return upstreamStore.getRulesForProxy(countryRuleTargetProxy.value.id)
})

// 前置代理列表（带国家规则数量）
const upstreamRows = computed(() => upstreamStore.proxies.map(proxy => (
  createUpstreamProxyPresentation({
    proxy,
    ruleCount: upstreamStore.getRulesForProxy(proxy.id).length
  })
)))

const countryRuleTargetPresentation = computed(() => {
  const targetId = countryRuleTargetProxy.value?.id
  if (!targetId) return null
  return upstreamRows.value.find(row => row.id === targetId) ?? null
})

async function fetchUpstream(opts: { silent?: boolean; initial?: boolean } = {}) {
  const isInitial = opts.initial === true
  const silent = opts.silent === true
  if (isInitial) {
    upstreamLoading.value = true
  } else if (!silent) {
    upstreamRefreshing.value = true
  }
  upstreamError.value = null

  try {
    const result = await upstreamStore.fetchAll()
    const error = result.ok ? upstreamStore.error : result.error
    if (error) throw error
  } catch (e: unknown) {
    const err = toAppError(e)
    upstreamError.value = {
      message: err.message || '加载前置代理失败',
      status: err.status
    }
  } finally {
    if (isInitial) {
      upstreamLoading.value = false
    } else if (!silent) {
      upstreamRefreshing.value = false
    }
  }
}

function openUpstreamDrawer(proxy?: UpstreamProxy) {
  if (!proxy) {
    editingUpstream.value = null
    upstreamForm.value = {
      id: '',
      name: '',
      addr: '',
      username: '',
      password: '',
      enabled: true
    }
  } else {
    editingUpstream.value = proxy
    upstreamForm.value = { ...proxy }
    // 密码脱敏时清空，让用户重新输入
    if (upstreamForm.value.password === '****') {
      upstreamForm.value.password = ''
    }
  }
  upstreamDrawerOpen.value = true
}

async function saveUpstreamForm() {
  const form = { ...upstreamForm.value }
  form.id = (form.id || '').trim()
  form.name = (form.name || '').trim()
  form.addr = (form.addr || '').trim()

  if (!form.id) {
    ElMessage.warning('ID 不能为空')
    return
  }
  if (!form.addr) {
    ElMessage.warning('Socks5 地址不能为空')
    return
  }
  const addrWarning = upstreamProxyAddressWarning(form.addr)
  if (addrWarning) {
    ElMessage.warning(addrWarning)
    return
  }

  try {
    if (editingUpstream.value) {
      // 更新
      const result = await upstreamStore.updateProxy(form.id, form)
      if (!result.ok) throw new Error(result.error.message || '更新失败')
      ElMessage.success('前置代理已更新，并通过连通性探测')
    } else {
      // 新增
      const result = await upstreamStore.createProxy(form)
      if (!result.ok) throw new Error(result.error.message || '创建失败')
      ElMessage.success('前置代理已创建，并通过连通性探测')
    }
    upstreamDrawerOpen.value = false
    await fetchUpstream()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '保存失败')
  }
}

async function deleteUpstream(proxy: UpstreamProxy) {
  const confirmed = await ElMessageBox.confirm(
    `确定删除前置代理「${proxy.name || proxy.id}」？\n绑定到该代理的国家规则将自动删除，相关国家会恢复直连。`,
    '确认删除',
    { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
  ).then(() => true).catch(() => false)

  if (!confirmed) return

  try {
    const result = await upstreamStore.deleteProxy(proxy.id)
    if (!result.ok) throw new Error(result.error.message || '删除失败')
    ElMessage.success('前置代理已删除')
    await fetchUpstream()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '删除失败')
  }
}

function openCountryRuleDrawer(proxy: UpstreamProxy) {
  countryRuleTargetProxy.value = proxy
  selectedCountryCode.value = ''
  countryRuleDrawerOpen.value = true
}

function findUpstreamProxy(id: string): UpstreamProxy | undefined {
  return upstreamStore.proxies.find(proxy => proxy.id === id)
}

function editUpstreamProxy(id: string) {
  const proxy = findUpstreamProxy(id)
  if (proxy) openUpstreamDrawer(proxy)
}

function removeUpstreamProxy(id: string) {
  const proxy = findUpstreamProxy(id)
  if (proxy) void deleteUpstream(proxy)
}

function manageUpstreamRules(id: string) {
  const proxy = findUpstreamProxy(id)
  if (proxy) openCountryRuleDrawer(proxy)
}

async function doUpsertCountryRule() {
  if (!countryRuleTargetProxy.value || !selectedCountryCode.value) {
    ElMessage.warning('请选择国家')
    return
  }

  try {
    const result = await upstreamStore.upsertCountryRule(selectedCountryCode.value, {
      upstream_proxy_id: countryRuleTargetProxy.value.id,
      enabled: true
    })
    if (!result.ok) throw new Error(result.error.message || '保存规则失败')
    ElMessage.success('国家规则已保存')
    selectedCountryCode.value = ''
    await fetchUpstream()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '保存规则失败')
  }
}

async function doDeleteCountryRule(countryCode: string) {
  try {
    const result = await upstreamStore.deleteCountryRule(countryCode)
    if (!result.ok) throw new Error(result.error.message || '删除规则失败')
    ElMessage.success('国家规则已删除，该国家将默认直连')
    await fetchUpstream()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '删除规则失败')
  }
}

// ── 统一初始化 ──
onMounted(() => {
  fetchOverview({ initial: true })
  fetchUpstream({ initial: true })
})

// 前置代理轮询
const upPollEnabled = computed(() => !upstreamLoading.value && activeTab.value === 'upstream')
usePollingScheduler(() => fetchUpstream({ silent: true }), 10000, {
  enabled: upPollEnabled,
  maxIntervalMs: 60000,
  backgroundIntervalMs: 30000
})
</script>

<template>
  <div class="app-page proxy-page">
    <WorkspaceStage
      class="proxy-workspace-stage"
      kicker="ROUTE ORCHESTRATION"
      :title="activeTab === 'upstream' ? '漫游路由' : '本地出口'"
      :subtitle="activeTab === 'upstream'
        ? '通过支持 UDP Associate 的 Socks5 节点连接海外运营商 ePDG'
        : '将本地代理实例绑定到指定物理网络接口与设备出口'"
      :status="activeTab === 'upstream' ? `${enabledUpstreamCount} 个节点启用` : `${runningOutboundCount} 个实例运行`"
      :tone="(activeTab === 'upstream' ? enabledUpstreamCount : runningOutboundCount) > 0 ? 'success' : 'neutral'"
    >
      <nav class="proxy-mode-switch" aria-label="代理工作区">
        <button type="button" :class="{ 'is-active': activeTab === 'upstream' }" @click="activeTab = 'upstream'">
          <el-icon><Earth24Regular /></el-icon>
          <span><small>ROAMING ROUTES</small><strong>漫游前置代理</strong></span>
          <b>{{ upstreamStore.proxies.length }}</b>
        </button>
        <button type="button" :class="{ 'is-active': activeTab === 'outbound' }" @click="activeTab = 'outbound'">
          <el-icon><Router24Regular /></el-icon>
          <span><small>LOCAL EGRESS</small><strong>本地出站代理</strong></span>
          <b>{{ instances.length }}</b>
        </button>
      </nav>

      <template #aside>
        <dl class="workspace-stage-stats">
          <div><dt>前置节点</dt><dd>{{ enabledUpstreamCount }} / {{ upstreamStore.proxies.length }}</dd></div>
          <div><dt>国家规则</dt><dd>{{ upstreamStore.countryRules.length }}</dd></div>
          <div><dt>本地实例</dt><dd>{{ runningOutboundCount }} / {{ instances.length }}</dd></div>
        </dl>
      </template>
    </WorkspaceStage>

    <!-- ═══════════ 前置代理 Tab ═══════════ -->
    <div v-show="activeTab === 'upstream'">
      <ErrorState
        v-if="upstreamError"
        class="mb-6"
        title="加载前置代理失败"
        :message="upstreamError.message"
        :status-code="upstreamError.status"
        retry-text="重试"
        @retry="fetchUpstream"
      />

      <ProxyUpstreamInventory
        :loading="upstreamLoading"
        :refreshing="upstreamRefreshing"
        :rows="upstreamRows"
        @add="openUpstreamDrawer()"
        @delete="removeUpstreamProxy"
        @edit="editUpstreamProxy"
        @refresh="fetchUpstream"
        @rules="manageUpstreamRules"
      />
    </div>

    <!-- ═══════════ 出站代理 Tab ═══════════ -->
    <div v-show="activeTab === 'outbound'">
      <ErrorState
        v-if="loadError"
        class="mb-6"
        title="加载代理配置失败"
        :message="loadError.message"
        :status-code="loadError.status"
        retry-text="重试"
        @retry="fetchOverview"
      />

      <ProxyOutboundInventory
        :loading="initialLoading"
        :refreshing="refreshing"
        :rows="outboundRows"
        @add="openDrawer()"
        @delete="deleteInstance"
        @edit="editOutboundInstance"
        @refresh="fetchOverview"
        @restart="restartInstance"
        @start="startInstance"
        @stop="stopInstance"
      />
    </div>

    <ProxyInstanceEditorDrawer
      v-model="drawerOpen"
      v-model:form="instanceForm"
      :devices="devices"
      :editing="!!editingInstance"
      :mode-options="modeOptions"
      :saving="saving"
      @save="saveForm"
    />

    <!-- ═══════════ 前置代理编辑 Drawer ═══════════ -->
    <el-drawer v-model="upstreamDrawerOpen" :title="editingUpstream ? '编辑前置代理' : '新增前置代理'" size="520px">
      <div class="space-y-6 pb-6">
        <div class="space-y-4">
          <div class="flex items-center gap-2 pb-2 border-b border-gray-100 dark:border-gray-800">
            <div class="drawer-section-marker"></div>
            <h3 class="text-sm font-bold text-gray-900 dark:text-gray-100">代理信息</h3>
          </div>

          <div class="grid grid-cols-2 gap-4">
            <div class="space-y-1">
              <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">代理 ID</label>
              <el-input v-model="upstreamForm.id" :disabled="!!editingUpstream" placeholder="唯一标识，如 jp-proxy-01" />
            </div>
            <div class="space-y-1">
              <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">名称</label>
              <el-input v-model="upstreamForm.name" placeholder="例如：日本代理" />
            </div>
          </div>

          <div class="space-y-1">
            <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">Socks5 地址</label>
            <el-input v-model="upstreamForm.addr" placeholder="host:port，例如 1.2.3.4:1080 或 [2001:db8::1]:1080" />
            <div class="text-xs text-gray-400 mt-1">VoWiFi 通过此 Socks5 代理连接运营商，实现跨区域本地 VoWiFi。{{ upstreamProxyIPv6AddressHint }}。保存时会自动探测 Socks5 握手与 UDP Associate。</div>
          </div>

          <div class="ui-panel-muted p-3 flex items-center justify-between rounded-lg">
            <div>
              <div class="text-sm font-bold text-gray-800 dark:text-gray-100">启用代理</div>
              <div class="text-xs text-gray-500">禁用后绑定到该代理的国家规则会回退为直连</div>
            </div>
            <el-switch v-model="upstreamForm.enabled" />
          </div>
        </div>

        <div class="space-y-4">
          <div class="flex items-center gap-2 pb-2 border-b border-gray-100 dark:border-gray-800">
            <div class="w-1 h-4 bg-amber-500 rounded-full"></div>
            <h3 class="text-sm font-bold text-gray-900 dark:text-gray-100">鉴权设置（可选）</h3>
          </div>

          <div class="grid grid-cols-2 gap-4">
            <div class="space-y-1">
              <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">用户名</label>
              <el-input v-model="upstreamForm.username" placeholder="留空则免鉴权" />
            </div>
            <div class="space-y-1">
              <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">密码</label>
              <el-input v-model="upstreamForm.password" type="password" show-password placeholder="留空则免鉴权" />
              <div class="text-xs text-gray-400 mt-1">编辑已有代理时留空会保持原密码不变。</div>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <div class="flex items-center justify-end gap-2">
          <el-button @click="upstreamDrawerOpen = false">取消</el-button>
          <el-button type="primary" @click="saveUpstreamForm">
            {{ editingUpstream ? '更新' : '创建' }}
          </el-button>
        </div>
      </template>
    </el-drawer>

    <ProxyCountryRuleDrawer
      v-model="countryRuleDrawerOpen"
      v-model:selected-country-code="selectedCountryCode"
      :available-countries="availableCountries"
      :rules="currentProxyCountryRules"
      :target="countryRuleTargetPresentation"
      @delete="doDeleteCountryRule"
      @save="doUpsertCountryRule"
    />
  </div>
</template>

<style scoped>
.proxy-mode-switch {
  width: min(100%, 690px);
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.proxy-workspace-stage {
  min-height: 286px;
}

.proxy-mode-switch button {
  min-height: 82px;
  padding: 15px 17px;
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  border: 1px solid var(--ui-border);
  border-radius: 15px;
  background: var(--ui-surface);
  color: var(--ui-text-muted);
  text-align: left;
  cursor: pointer;
  transition: background-color 160ms ease, border-color 160ms ease, color 160ms ease, transform 140ms var(--ui-ease-out);
}

.proxy-mode-switch button:active {
  transform: scale(.985);
}

.proxy-mode-switch button > .el-icon {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border: 1px solid var(--ui-border);
  border-radius: 10px;
  font-size: 19px;
}

.proxy-mode-switch button > span {
  display: grid;
  gap: 4px;
}

.proxy-mode-switch small {
  font: 700 9px "v-mono", monospace;
  letter-spacing: .12em;
}

.proxy-mode-switch strong {
  color: var(--ui-text);
  font-size: 15px;
}

.proxy-mode-switch b {
  color: var(--ui-text);
  font: 22px "v-mono", monospace;
}

.proxy-mode-switch button.is-active {
  border-color: color-mix(in srgb, var(--ui-primary) 48%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-primary) 8%, var(--ui-surface));
  color: var(--ui-primary);
}

.proxy-mode-switch button.is-active > .el-icon {
  border-color: color-mix(in srgb, var(--ui-primary) 45%, var(--ui-border));
  color: var(--ui-primary);
}

.proxy-page :deep(.ui-card) {
  border-radius: 18px;
}

.proxy-page :deep(.ui-panel-muted) {
  border-radius: 14px;
}

.drawer-section-marker {
  width: 3px;
  height: 16px;
  border-radius: 2px;
  background: var(--ui-primary);
}

@media (max-width: 720px) {
  .proxy-mode-switch {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
