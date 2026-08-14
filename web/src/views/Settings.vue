<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useSettingsStore } from '../stores/settings'
import { useAuthStore } from '../stores/auth'
import type { PasswordCredentialStatus } from '../types/credentials'
import WorkspaceStage from '../components/WorkspaceStage.vue'
import FieldRow from '../components/FieldRow.vue'
import QQNotificationTab from '../components/settings/QQNotificationTab.vue'
import TelegramNotificationTab from '../components/settings/TelegramNotificationTab.vue'
import WeComBotNotificationTab from '../components/settings/WeComBotNotificationTab.vue'
import WeixinNotificationTab from '../components/settings/WeixinNotificationTab.vue'
import { 
  Key24Regular, 
  Save24Regular,
  Server24Regular,
  Alert24Regular,
  Add20Regular,
  Delete20Regular,
  DocumentText24Regular
} from '@vicons/fluent'
import { formatDeviceDateTime } from '../utils/deviceTime'

const settingsStore = useSettingsStore()
const authStore = useAuthStore()
const { systemInfo, loadingNotifications, savingNotifications, testingWebhook, testingBark, testingEmail, testingWeCom, changingPassword, passwordForm, telegramForm, feishuForm, qqForm, weixinForm, weComBotForm, webhookSettings, barkSettings, emailForm, pushplusForm, weComSettings } = storeToRefs(settingsStore)
const activeNotifyTab = ref('telegram')
const openWRTDynamicInterfaces = ref(false)
const loadingSystemSettings = ref(false)
const savingSystemSettings = ref(false)
const passwordStatus = ref<PasswordCredentialStatus | null>(null)
const loadingPasswordStatus = ref(false)
const passwordManagedByEnvironment = computed(() => passwordStatus.value?.management === 'environment')

const enabledNotificationCount = computed(() => [
  telegramForm.value.enabled,
  feishuForm.value.enabled,
  qqForm.value.enabled,
  weixinForm.value.enabled,
  weComBotForm.value.enabled,
  webhookSettings.value.enabled,
  barkSettings.value.enabled,
  emailForm.value.enabled,
  pushplusForm.value.enabled,
  weComSettings.value.enabled
].filter(Boolean).length)



const hasValidWebhookURLs = computed(() => {
  if (!Array.isArray(webhookSettings.value.urls)) {
    return false
  }
  return webhookSettings.value.urls.some((u) => String(u || '').trim().length > 0)
})

const hasValidBarkURLs = computed(() => {
  if (!Array.isArray(barkSettings.value.urls)) {
    return false
  }
  return barkSettings.value.urls.some((u) => String(u || '').trim().length > 0)
})

const hasValidEmailConfig = computed(() => {
  return !!(
    emailForm.value.smtp_host &&
    emailForm.value.smtp_port &&
    emailForm.value.username &&
    emailForm.value.password &&
    emailForm.value.from_address &&
    emailForm.value.to_addresses
  )
})

const MAX_WECOM_URLS = 8
const hasValidWeComConfig = computed(() => {
  return Array.isArray(weComSettings.value.urls) &&
    weComSettings.value.urls.some(url => String(url || '').trim()) &&
    !!String(weComSettings.value.payload_template || '').trim()
})


async function changePassword() {
  if (passwordManagedByEnvironment.value) {
    ElMessage.warning(`当前密码由 ${passwordStatus.value?.environment_variable || 'PROXY_WEB_PASSWORD'} 管理，请修改部署环境并重启`)
    return
  }
  if (passwordForm.value.new_password !== passwordForm.value.confirm_password) {
    ElMessage.error('两次输入的新密码不一致')
    return
  }
  
  const result = await settingsStore.changePasswordFromForm()
  if (!result.ok) {
    ElMessage.error(result.error.message || '密码更新失败')
    return
  }
  authStore.applyToken(result.data.token)
  passwordStatus.value = result.data.credential
  ElMessage.success('密码已更新并安全写入配置文件')
  settingsStore.resetPasswordForm()
}

async function loadPasswordStatus() {
  loadingPasswordStatus.value = true
  const result = await systemService.getPasswordStatus()
  loadingPasswordStatus.value = false
  if (!result.ok) {
    passwordStatus.value = null
    ElMessage.error(result.error.message || '密码管理状态加载失败')
    return
  }
  passwordStatus.value = result.data
}

async function loadSystemInfo() {
  const result = await settingsStore.fetchSystemInfo()
  if (!result.ok) {
    console.error('系统信息读取失败', result.error)
  }
}

async function loadSystemSettings() {
  loadingSystemSettings.value = true
  const result = await systemService.getSystemSettings()
  loadingSystemSettings.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '系统设置加载失败')
    return
  }
  openWRTDynamicInterfaces.value = !!result.data.openwrt_dynamic_interfaces
}

async function updateOpenWRTDynamicInterfaces(value: string | number | boolean) {
  const enabled = value === true
  const previous = !enabled
  if (enabled) {
    try {
      await ElMessageBox.confirm(
        '该设置仅适用于 OpenWrt。确认把当前拨号数据网卡交给 netifd 展示？',
        '启用 OpenWrt 接口映射',
        { confirmButtonText: '启用', cancelButtonText: '取消', type: 'warning' }
      )
    } catch {
      openWRTDynamicInterfaces.value = previous
      return
    }
  }
  savingSystemSettings.value = true
  const result = await systemService.saveSystemSettings({ openwrt_dynamic_interfaces: enabled })
  savingSystemSettings.value = false
  if (!result.ok) {
    openWRTDynamicInterfaces.value = previous
    ElMessage.error(result.error.message || 'OpenWrt 接口映射更新失败')
    return
  }
  ElMessage.success(enabled ? 'OpenWrt 接口映射已启用' : 'OpenWrt 接口映射已关闭')
}


async function loadNotifications() {
  try {
    const result = await settingsStore.fetchNotifications()
    if (!result.ok) throw new Error(result.error.message || '通知配置加载失败')
    syncWebhookHeaderRowsFromSettings()
  } catch {
    ElMessage.error('通知配置加载失败')
  }
}

function openAPIDocs() {
  const docsURL = String(systemInfo.value.docs?.swagger_ui || '').trim()
  if (!docsURL) {
    ElMessage.warning('API 文档入口暂不可用')
    return
  }
  window.open(docsURL, '_blank', 'noopener,noreferrer')
}

async function saveNotifications() {
  try {
    const result = await settingsStore.saveNotificationsFromForms()
    if (!result.ok) throw new Error(result.error.message || '通知配置保存失败')
    const applied = result.data.applied
    const warning = result.data.warning
    if (applied === false && warning) {
      ElMessage.warning(warning)
    } else {
      ElMessage.success('通知配置已保存（已写入 config.yaml）')
    }
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '通知配置保存失败')
  }
}

async function testWebhookNotification() {
  try {
    const result = await settingsStore.testWebhookFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Webhook 测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '测试通知已发送')
      return
    }
    if (Array.isArray(data.failed_urls) && data.failed_urls.length > 0) {
      ElMessage.error(`${data.message}\n失败 URL: ${data.failed_urls.join(', ')}`)
      return
    }
    ElMessage.error(data.message || 'Webhook 测试失败')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Webhook 测试失败')
  }
}

function addWebhookUrl() {
  if (!webhookSettings.value.urls) {
     webhookSettings.value.urls = []
  }
  webhookSettings.value.urls.push('')
}

function removeWebhookUrl(index: number) {
  webhookSettings.value.urls.splice(index, 1)
}

// 自定义请求头以「行」形式编辑（rows 为唯一编辑源），保存时单向回写为 map。
// 受保护的系统头由后端强制覆盖。
const PROTECTED_WEBHOOK_HEADERS = new Set(['content-type', 'x-hideck-signature'])
// 常用请求头预设，下拉可选；filterable + allow-create 也允许自行输入其它名称
const COMMON_WEBHOOK_HEADERS = [
  'Authorization',
  'X-Api-Key',
  'X-Auth-Token',
  'X-Webhook-Token',
  'X-Signature',
  'X-Request-Id',
  'Accept',
  'User-Agent'
]
// 每行带稳定 id，避免用数组下标作 v-for key 时，删除中间行后 el-select 复用实例残留选项
let webhookHeaderUid = 0
const webhookHeaderRows = ref<{ id: number; key: string; value: string }[]>([])

// 加载完成后调用，把已保存的 headers map 转换为可编辑的行
function syncWebhookHeaderRowsFromSettings() {
  const headers = webhookSettings.value.headers || {}
  webhookHeaderRows.value = Object.entries(headers).map(([key, value]) => ({
    id: webhookHeaderUid++,
    key,
    value: String(value ?? '')
  }))
}

// 行变化时单向回写为 map（丢弃空 key 与受保护头）。无反向 watch，故不会回环。
watch(
  webhookHeaderRows,
  (rows) => {
    const map: Record<string, string> = {}
    for (const row of rows) {
      const key = String(row.key || '').trim()
      if (!key || PROTECTED_WEBHOOK_HEADERS.has(key.toLowerCase())) continue
      map[key] = String(row.value ?? '')
    }
    webhookSettings.value.headers = map
  },
  { deep: true }
)

function addWebhookHeader() {
  webhookHeaderRows.value.push({ id: webhookHeaderUid++, key: '', value: '' })
}

function removeWebhookHeader(index: number) {
  webhookHeaderRows.value.splice(index, 1)
}

async function testBarkNotification() {
  try {
    const result = await settingsStore.testBarkFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Bark 测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '测试通知已发送')
      return
    }
    if (Array.isArray(data.failed_urls) && data.failed_urls.length > 0) {
      ElMessage.error(`${data.message}\n失败 URL: ${data.failed_urls.join(', ')}`)
      return
    }
    ElMessage.error(data.message || 'Bark 测试失败')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Bark 测试失败')
  }
}

async function testEmailNotification() {
  try {
    const result = await settingsStore.testEmailFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Email 测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '测试邮件已发送')
      return
    }
    ElMessage.error(data.message || 'Email 测试失败')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Email 测试失败')
  }
}

async function testWeComNotification() {
  try {
    const result = await settingsStore.testWeComFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || '企业微信测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '企业微信测试通知已发送')
      return
    }
    const failure = data.failed_count ? `（失败目标：${data.failed_count}）` : ''
    ElMessage.error(`${data.message || '企业微信测试失败'}${failure}`)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '企业微信测试失败')
  }
}

function addWeComUrl() {
  if (weComSettings.value.urls.length >= MAX_WECOM_URLS) return
  weComSettings.value.urls.push('')
}

function removeWeComUrl(index: number) {
  weComSettings.value.urls.splice(index, 1)
}

function addBarkUrl() {
  if (!barkSettings.value.urls) {
     barkSettings.value.urls = []
  }
  barkSettings.value.urls.push('')
}

function removeBarkUrl(index: number) {
  barkSettings.value.urls.splice(index, 1)
}



watch(() => emailForm.value.smtp_port, (newPort) => {
  if (Number(newPort) === 465) {
    emailForm.value.use_ssl = true
  }
})



import { systemService, type UpdateInfo } from '../services/system'

const checkingUpdate = ref(false)
const applyingUpdate = ref(false)
const updateInfo = ref<UpdateInfo | null>(null)

async function doCheckUpdate() {
  checkingUpdate.value = true
  try {
    const res = await systemService.checkUpdate()
    if (!res.ok) throw new Error(res.error.message || '检查更新失败')
    updateInfo.value = res.data
    if (!res.data.has_update) {
      ElMessage.info(res.data.release_note || '当前已是最新版本')
    }
  } catch (e: any) {
    ElMessage.error(e.message || '检查更新失败')
  } finally {
    checkingUpdate.value = false
  }
}

async function doApplyUpdate() {
  if (!updateInfo.value) return

  if (updateInfo.value.is_docker) {
    ElMessageBox.alert(
      '检测到当前系统运行在 Docker 环境下。<br><br>不建议在 Docker 容器内直接执行文件热替换。请直接拉取明确版本镜像（如 <code>docker pull yibaiba/hideck:&lt;tag&gt;</code>）并重启容器来完成升级！',
      '环境警告',
      { dangerouslyUseHTMLString: true, type: 'warning' }
    )
    return
  }

  try {
    await ElMessageBox.confirm(
      `最新版本：${updateInfo.value.latest_version}，确定要现在更新并重启服务吗？<br><br><pre style="white-space: pre-wrap; font-size: 12px; max-height: 200px; overflow-y: auto; background: var(--el-fill-color-light); padding: 8px; border-radius: 4px; margin-top: 8px;">${updateInfo.value.release_note}</pre>`,
      '应用更新',
      { dangerouslyUseHTMLString: true, confirmButtonText: '立即更新', cancelButtonText: '取消', type: 'warning' }
    )
    applyingUpdate.value = true
    const res = await systemService.applyUpdate()
    if (!res.ok) throw new Error(res.error.message || '请求应用更新失败')
    ElMessage.success(res.data?.message || '正在更新...')
    setTimeout(() => {
      window.location.reload()
    }, 5000)
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || '应用更新失败')
    }
  } finally {
    applyingUpdate.value = false
  }
}

onMounted(() => {
  loadNotifications()
  loadSystemInfo()
  loadSystemSettings()
  loadPasswordStatus()
})

</script>

<template>
  <div class="app-page settings-page max-w-[1440px] mx-auto">
    <section class="settings-workspace-shell ui-card ui-workspace-glow">
    <WorkspaceStage
      class="settings-workspace-stage"
      compact
      kicker="GATEWAY CONTROL"
      title="HiDeck Gateway"
      subtitle="管理访问安全、运行环境、系统集成与消息通知通道"
      status="系统配置已加载"
      tone="success"
    >
      <div class="workspace-stage-pills">
        <span class="workspace-stage-pill">通知通道 <strong>{{ enabledNotificationCount }} / 10</strong></span>
        <span class="workspace-stage-pill">接口映射 <strong>{{ openWRTDynamicInterfaces ? 'OPENWRT' : 'OFF' }}</strong></span>
        <span class="workspace-stage-pill">配置 <strong>{{ systemInfo.config ? 'LOADED' : 'WAITING' }}</strong></span>
      </div>

      <template #aside>
        <dl class="workspace-stage-stats">
          <div><dt>系统版本</dt><dd>{{ systemInfo.version || '--' }}</dd></div>
          <div><dt>构建时间</dt><dd>{{ formatDeviceDateTime(systemInfo.build_time, { fallback: '--' }) }}</dd></div>
          <div><dt>通知服务</dt><dd>{{ enabledNotificationCount }} ENABLED</dd></div>
        </dl>
      </template>
    </WorkspaceStage>

    <div class="settings-workspace">
      <!-- Security Card -->
      <section id="password-settings" class="settings-security-card p-5 sm:p-6 relative overflow-hidden group">
         
         <div class="flex items-center gap-3 mb-6 relative z-10">
            <div class="section-icon section-icon-primary">
               <el-icon size="24"><Key24Regular /></el-icon>
            </div>
            <div>
               <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">安全</h3>
               <p class="text-xs text-gray-500">更新访问凭证</p>
            </div>
         </div>

         <div class="space-y-4 relative z-10">
             <div v-if="loadingPasswordStatus" class="rounded-xl border border-gray-200/70 dark:border-white/10 bg-gray-50/80 dark:bg-white/[0.03] px-4 py-3 text-sm text-gray-500">
               正在读取凭证管理状态…
             </div>
             <div v-else-if="!passwordStatus" class="rounded-xl border border-red-300/60 bg-red-50/80 dark:border-red-500/30 dark:bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300">
               无法确认密码来源，修改功能已暂时禁用。
             </div>
             <div v-else-if="passwordManagedByEnvironment" class="rounded-xl border border-amber-300/60 bg-amber-50/80 dark:border-amber-500/30 dark:bg-amber-500/10 px-4 py-3 text-sm leading-6 text-amber-800 dark:text-amber-200">
               当前密码由环境变量 <code>{{ passwordStatus.environment_variable || 'PROXY_WEB_PASSWORD' }}</code> 管理。请在部署环境中修改后重启 HiDeck；控制台不会覆盖它。
             </div>
             <div v-else class="rounded-xl border border-emerald-300/60 bg-emerald-50/70 dark:border-emerald-500/25 dark:bg-emerald-500/[0.08] px-4 py-3 text-sm leading-6 text-emerald-800 dark:text-emerald-200">
               {{ passwordStatus.change_required ? '当前密码为初始明文凭证或强度不足，请尽快修改。' : '密码由配置文件管理，更新后会以 bcrypt 哈希安全写入。' }}
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">当前密码</label>
                <el-input v-model="passwordForm.old_password" :disabled="!passwordStatus || passwordManagedByEnvironment" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">新密码</label>
                <el-input v-model="passwordForm.new_password" :disabled="!passwordStatus || passwordManagedByEnvironment" type="password" show-password placeholder="至少 8 位，建议 12 位以上" size="large" />
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">确认新密码</label>
                <el-input v-model="passwordForm.confirm_password" :disabled="!passwordStatus || passwordManagedByEnvironment" type="password" show-password placeholder="再次输入新密码" size="large" />
             </div>
             
             <div class="pt-4">
                 <el-button type="primary" :loading="changingPassword" :disabled="!passwordStatus || passwordManagedByEnvironment" @click="changePassword" size="large" class="w-full !border-0">
                   <el-icon><Save24Regular /></el-icon>
                   更新凭证
                 </el-button>
             </div>
         </div>
      </section>

      <!-- System Info Card -->
      <section class="settings-system-card p-5 sm:p-6 relative overflow-hidden group">

         <div class="flex items-center gap-3 mb-6 relative z-10">
            <div class="section-icon section-icon-success">
               <el-icon size="24"><Server24Regular /></el-icon>
            </div>
            <div>
               <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">系统信息</h3>
               <p class="text-xs text-gray-500">运行环境</p>
            </div>
         </div>

         <div class="space-y-4 text-sm relative z-10">
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow label="版本" :value="systemInfo.version" monospace>
                <div class="flex items-center justify-end gap-3">
                  <el-button size="small" type="primary" class="!border-0" :loading="checkingUpdate" @click.stop="doCheckUpdate">
                    检查更新
                  </el-button>
                  <span>{{ systemInfo.version || 'Unknown' }}</span>
                </div>
              </FieldRow>
            </div>
            
            <div v-if="updateInfo?.has_update" class="p-4 bg-amber-50 dark:bg-amber-500/10 rounded-lg border border-amber-200 dark:border-amber-500/20">
               <div class="flex items-center gap-2 text-amber-800 dark:text-amber-200 mb-2 font-bold text-[13px]">
                 <el-icon><Alert24Regular /></el-icon>发现新版本: {{ updateInfo.latest_version }}
               </div>
               <div class="text-xs text-amber-700 dark:text-amber-300/80 mb-4 whitespace-pre-wrap max-h-32 overflow-y-auto pr-2 custom-scrollbar">
                 {{ updateInfo.release_note || '暂无更新说明' }}
               </div>
               <el-button type="warning" :loading="applyingUpdate" @click="doApplyUpdate" class="w-full !border-0">
                 立即更新并重启
               </el-button>
            </div>
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow
                label="构建时间"
                :value="formatDeviceDateTime(systemInfo.build_time, { fallback: systemInfo.build_time })"
                monospace
              />
            </div>
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow label="配置路径" :value="systemInfo.config" monospace copyable />
            </div>
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow label="项目主页" value="https://github.com/yibaiba/hideck" monospace copyable />
            </div>
            <div class="ui-panel-muted px-4 py-4">
              <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
                <div class="min-w-0">
                  <div class="flex items-center gap-3">
                    <div class="w-9 h-9 rounded-md bg-blue-50 dark:bg-blue-500/10 flex items-center justify-center text-blue-600 dark:text-blue-400">
                      <el-icon size="18"><DocumentText24Regular /></el-icon>
                    </div>
                    <div>
                      <div class="text-sm font-bold text-gray-800 dark:text-gray-100">API 文档</div>
                      <div class="text-xs text-gray-500">打开后端直出的 OpenAPI 页面</div>
                    </div>
                  </div>

                </div>
                <el-button
                  type="primary"
                  class="self-start sm:self-center shrink-0 !border-0"
                  :disabled="!systemInfo.docs?.swagger_ui"
                  @click="openAPIDocs"
                >
                  <el-icon><DocumentText24Regular /></el-icon>
                  打开 API 文档
                </el-button>
              </div>
            </div>
            <div class="border-t border-gray-100 dark:border-white/10 pt-4 flex items-center justify-between gap-4">
              <div class="min-w-0">
                <div class="text-sm font-bold text-gray-800 dark:text-gray-100">OpenWrt 动态接口映射</div>
                <div class="text-xs text-gray-500">netifd</div>
              </div>
              <el-switch
                v-model="openWRTDynamicInterfaces"
                :loading="loadingSystemSettings || savingSystemSettings"
                :disabled="loadingSystemSettings || savingSystemSettings"
                @change="updateOpenWRTDynamicInterfaces"
              />
            </div>
         </div>
      </section>

      <section class="notify-card p-4 sm:p-6 lg:p-8">
         <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-6">
            <div class="flex items-center gap-3">
               <div class="w-12 h-12 rounded-md bg-teal-50 dark:bg-teal-500/10 flex items-center justify-center text-teal-700 dark:text-teal-300">
                  <el-icon size="24"><Alert24Regular /></el-icon>
               </div>
               <div>
                  <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">通知</h3>
                  <p class="text-xs text-gray-500">个人微信 / 企业微信 / QQ / Webhook / 更多</p>
               </div>
            </div>
            <el-button type="primary" :loading="savingNotifications" :disabled="loadingNotifications" @click="saveNotifications" class="!border-0">
              <el-icon><Save24Regular /></el-icon>
              保存通知配置
            </el-button>
         </div>

         <div v-if="loadingNotifications" class="p-6 text-sm text-gray-500 dark:text-gray-400">正在加载通知配置…</div>

         <div v-else class="w-full overflow-hidden">
            <el-tabs v-model="activeNotifyTab" class="settings-notify-tabs">
              <!-- Telegram -->
              <el-tab-pane label="Telegram Bot" name="telegram" class="pt-2">
                <TelegramNotificationTab />
              </el-tab-pane>

              <!-- 飞书 -->
              <el-tab-pane label="飞书 Bot" name="feishu" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用飞书机器人</div>
                  </div>
                  <el-switch v-model="feishuForm.enabled" />
                </div>

                <div class="space-y-4">
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">App ID</label>
                      <el-input v-model="feishuForm.app_id" :disabled="!feishuForm.enabled" placeholder="cli_xxxx" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">App Secret</label>
                      <el-input v-model="feishuForm.app_secret" :disabled="!feishuForm.enabled" type="password" show-password placeholder="••••••••" />
                    </div>
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">Chat IDs</label>
                    <el-input v-model="feishuForm.chat_ids" :disabled="!feishuForm.enabled" placeholder="多个群组用英文逗号分隔" />
                    <div class="text-[10px] text-gray-400 mt-1">飞书群聊的 Chat ID (oc_xxxx)，可通过飞书开放平台 API 获取，支持逗号分隔多个群组。</div>
                  </div>
                  <div class="p-3 rounded-md bg-blue-50/50 dark:bg-blue-500/5 text-xs text-blue-600 dark:text-blue-400/80 leading-relaxed border border-blue-100/50 dark:border-blue-500/10">
                    <strong>配置说明：</strong>
                    <ol class="list-decimal ml-4 mt-1 space-y-1">
                      <li>在<a href="https://open.feishu.cn" target="_blank" class="underline hover:text-blue-700">飞书开放平台</a>创建自建应用，启用「机器人」能力</li>
                      <li>在「事件与回调 → 事件配置」中选择「使用长连接接收事件」</li>
                      <li>添加 <code>im:message</code> 和 <code>im:message:send_as_bot</code> 权限</li>
                    </ol>
                  </div>
                </div>
              </el-tab-pane>

              <el-tab-pane label="个人微信" name="weixin" class="pt-2">
                <WeixinNotificationTab />
              </el-tab-pane>

              <el-tab-pane label="企微机器人" name="wecom-bot" class="pt-2">
                <WeComBotNotificationTab />
              </el-tab-pane>

              <el-tab-pane label="QQ Bot" name="qq" class="pt-2">
                <QQNotificationTab />
              </el-tab-pane>

                            <!-- Bark -->
              <el-tab-pane label="Bark" name="bark" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Bark 推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingBark"
                      :disabled="!barkSettings.enabled || !hasValidBarkURLs"
                      @click="testBarkNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="barkSettings.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">目标 URLs</label>
                      <el-button size="small" type="primary" plain @click="addBarkUrl" :disabled="!barkSettings.enabled">
                         <el-icon><Add20Regular /></el-icon>
                         <span class="ml-1">添加 URL</span>
                      </el-button>
                    </div>
                    
                    <div v-if="barkSettings.urls && barkSettings.urls.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置任何 Bark URL，点击右侧添加按钮。
                    </div>

                    <div v-for="(url, index) in barkSettings.urls" :key="index" class="flex items-center gap-2">
                       <el-input v-model="barkSettings.urls[index]" :disabled="!barkSettings.enabled" placeholder="https://api.day.app/YOUR_KEY/" class="flex-1" />
                       <el-button type="danger" plain @click="removeBarkUrl(index)" :disabled="!barkSettings.enabled">
                          <el-icon><Delete20Regular /></el-icon>
                       </el-button>
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">分组 (Group)</label>
                    <el-input v-model="barkSettings.group" :disabled="!barkSettings.enabled" placeholder="例如 hideck" />
                    <div class="text-[10px] text-gray-400 mt-1">iOS 设备上的通知分组。</div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">通知级别 (Level)</label>
                    <el-select v-model="barkSettings.level" :disabled="!barkSettings.enabled" placeholder="选择通知级别" class="w-full">
                      <el-option label="时效性 (timeSensitive)" value="timeSensitive" />
                      <el-option label="积极 (active)" value="active" />
                      <el-option label="被动 (passive)" value="passive" />
                    </el-select>
                    <div class="text-[10px] text-gray-400 mt-1">iOS 的专注模式/打扰规则会根据此级别决定是否亮屏。</div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">图标 (Icon)</label>
                    <el-input v-model="barkSettings.icon" :disabled="!barkSettings.enabled" placeholder="图标 URL，可选" />
                  </div>
                </div>
              </el-tab-pane>

              <!-- Email -->
              <el-tab-pane label="Email" name="email" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Email 推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingEmail"
                      :disabled="!emailForm.enabled || !hasValidEmailConfig"
                      @click="testEmailNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="emailForm.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="grid grid-cols-1 sm:grid-cols-10 gap-4">
                    <div class="space-y-1 sm:col-span-5">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">SMTP 主机</label>
                      <el-input v-model="emailForm.smtp_host" :disabled="!emailForm.enabled" placeholder="smtp.example.com" />
                    </div>
                    <div class="space-y-1 sm:col-span-3">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">SMTP 端口</label>
                      <el-input v-model="emailForm.smtp_port" :disabled="!emailForm.enabled" type="number" inputmode="numeric" placeholder="465 / 587" />
                    </div>
                    <div class="space-y-1 sm:col-span-2">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider block">使用 SSL/TLS </label>
                      <div class="h-10 flex items-center">
                        <el-switch v-model="emailForm.use_ssl" :disabled="!emailForm.enabled" />
                      </div>
                    </div>
                  </div>
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">用户名 (Username)</label>
                      <el-input v-model="emailForm.username" :disabled="!emailForm.enabled" placeholder="邮箱账号" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">密码 (Password)</label>
                      <el-input v-model="emailForm.password" :disabled="!emailForm.enabled" type="password" show-password placeholder="邮箱密码或授权码" />
                    </div>
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">发件人地址 (From)</label>
                    <el-input v-model="emailForm.from_address" :disabled="!emailForm.enabled" placeholder="例如 noreply@example.com" />
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">收件人地址 (To)</label>
                    <el-input v-model="emailForm.to_addresses" :disabled="!emailForm.enabled" placeholder="多个收件人请用英文逗号分隔" />
                  </div>
                </div>
              </el-tab-pane>

              <!-- Pushplus -->
              <el-tab-pane label="Pushplus" name="pushplus" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Pushplus 推送</div>
                  </div>
                  <el-switch v-model="pushplusForm.enabled" />
                </div>

                <div class="space-y-4">
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">Token</label>
                    <el-input v-model="pushplusForm.token" :disabled="!pushplusForm.enabled" placeholder="Pushplus 用户的 Token" />
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">群组编码 (Topic)</label>
                    <el-input v-model="pushplusForm.topic" :disabled="!pushplusForm.enabled" placeholder="群组编码，不填则发给个人" />
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">渠道 (Channel)</label>
                    <el-select v-model="pushplusForm.channel" :disabled="!pushplusForm.enabled" placeholder="选择渠道" class="w-full">
                      <el-option label="微信 (wechat)" value="wechat" />
                      <el-option label="Webhook (webhook)" value="webhook" />
                      <el-option label="企业微信 (cp)" value="cp" />
                      <el-option label="邮件 (mail)" value="mail" />
                    </el-select>
                  </div>
                </div>
              </el-tab-pane>

              <!-- Webhook -->
              <el-tab-pane label="Webhook" name="webhook" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Webhook 推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingWebhook"
                      :disabled="!webhookSettings.enabled || !hasValidWebhookURLs"
                      @click="testWebhookNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="webhookSettings.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">目标 URLs</label>
                      <el-button size="small" type="primary" plain @click="addWebhookUrl" :disabled="!webhookSettings.enabled">
                         <el-icon><Add20Regular /></el-icon>
                         <span class="ml-1">添加 URL</span>
                      </el-button>
                    </div>
                    
                    <div v-if="webhookSettings.urls && webhookSettings.urls.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置任何 Webhook URL，点击右侧添加按钮。
                    </div>

                    <div v-for="(url, index) in webhookSettings.urls" :key="index" class="flex items-center gap-2">
                       <!-- 注意：el-input v-model="webhookSettings.urls[index]" 处理基本类型数组在 Vue3 中可能会有失去焦点问题。
                            但在这里作为简单的响应式数组依然可用，或者用更复杂的方式包裹。 -->
                       <el-input v-model="webhookSettings.urls[index]" :disabled="!webhookSettings.enabled" placeholder="https://..." class="flex-1" />
                       <el-button type="danger" plain @click="removeWebhookUrl(index)" :disabled="!webhookSettings.enabled">
                          <el-icon><Delete20Regular /></el-icon>
                       </el-button>
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">数字签名密钥 (Secret)</label>
                    <el-input v-model="webhookSettings.secret" :disabled="!webhookSettings.enabled" placeholder="用于 HMAC-SHA256 签名，选填" />
                    <div class="text-[10px] text-gray-400 mt-1">若配置，将通过请求头 X-HiDeck-Signature 提供 payload 验证。</div>
                  </div>

                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">自定义请求头 (Headers)</label>
                      <el-button size="small" type="primary" plain @click="addWebhookHeader" :disabled="!webhookSettings.enabled">
                        <el-icon><Add20Regular /></el-icon>
                        <span class="ml-1">添加 Header</span>
                      </el-button>
                    </div>

                    <div v-if="webhookHeaderRows.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置自定义请求头，例如 Authorization、X-Api-Key 等。
                    </div>

                    <div v-for="(row, index) in webhookHeaderRows" :key="row.id" class="flex items-center gap-2">
                      <el-select
                        v-model="row.key"
                        :disabled="!webhookSettings.enabled"
                        filterable
                        allow-create
                        default-first-option
                        placeholder="选择或输入 Header 名"
                        class="flex-1"
                      >
                        <el-option v-for="name in COMMON_WEBHOOK_HEADERS" :key="name" :label="name" :value="name" />
                      </el-select>
                      <el-input v-model="row.value" :disabled="!webhookSettings.enabled" placeholder="值，如 Bearer xxx" class="flex-1" />
                      <el-button type="danger" plain @click="removeWebhookHeader(index)" :disabled="!webhookSettings.enabled">
                        <el-icon><Delete20Regular /></el-icon>
                      </el-button>
                    </div>
                    <div class="text-[10px] text-gray-400 mt-1">
                      Content-Type 与 X-HiDeck-Signature 为系统保留头，自定义同名头会被忽略。
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">文本模板 (Text Template)</label>
                    <el-input
                      v-model="webhookSettings.text_template"
                      :disabled="!webhookSettings.enabled"
                      type="textarea"
                      :rows="2"
                      placeholder="{{device_label}} {{text}}"
                    />
                    <div class="text-[10px] text-gray-400 mt-1">
                      支持占位符：<code v-pre>{{text}}</code>、<code v-pre>{{event}}</code>、<code v-pre>{{timestamp}}</code>、<code v-pre>{{device_id}}</code>、<code v-pre>{{device_name}}</code>、<code v-pre>{{device_label}}</code>。留空则直接发送原始 text。
                    </div>
                  </div>
                  
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">请求超时 (ms)</label>
                      <el-input-number v-model="webhookSettings.timeout_ms" :min="1000" :max="60000" :disabled="!webhookSettings.enabled" class="w-full !w-full" controls-position="right" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">最大重试次数</label>
                      <el-input-number v-model="webhookSettings.retry_max" :min="0" :max="10" :disabled="!webhookSettings.enabled" class="w-full !w-full" controls-position="right" />
                    </div>
                  </div>
                </div>
              </el-tab-pane>

              <!-- 企业微信消息推送 -->
              <el-tab-pane label="企微 Webhook" name="wecom" class="pt-2">
                <div class="flex items-center justify-between gap-3 mb-4">
                  <div class="font-bold text-gray-800 dark:text-gray-100">启用企业微信消息推送</div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingWeCom"
                      :disabled="!weComSettings.enabled || !hasValidWeComConfig"
                      @click="testWeComNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="weComSettings.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-2">
                    <div class="flex items-center justify-between gap-3">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">Webhook URLs</label>
                      <el-button
                        size="small"
                        type="primary"
                        plain
                        :disabled="!weComSettings.enabled || weComSettings.urls.length >= MAX_WECOM_URLS"
                        @click="addWeComUrl"
                      >
                        <el-icon><Add20Regular /></el-icon>
                        <span class="ml-1">添加 URL</span>
                      </el-button>
                    </div>
                    <div v-if="weComSettings.urls.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置企业微信 Webhook URL
                    </div>
                    <div v-for="(_, index) in weComSettings.urls" :key="index" class="flex items-center gap-2">
                      <el-input
                        v-model="weComSettings.urls[index]"
                        :disabled="!weComSettings.enabled"
                        type="password"
                        show-password
                        placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."
                        class="flex-1"
                      />
                      <el-button type="danger" plain :disabled="!weComSettings.enabled" aria-label="删除 URL" @click="removeWeComUrl(index)">
                        <el-icon><Delete20Regular /></el-icon>
                      </el-button>
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">JSON 请求体模板</label>
                    <el-input
                      v-model="weComSettings.payload_template"
                      :disabled="!weComSettings.enabled"
                      type="textarea"
                      :rows="12"
                      class="wecom-template-input"
                    />
                    <div class="text-[10px] text-gray-400 mt-1">
                      变量需直接作为 JSON 值使用。支持 <code v-pre>{{event}}</code>、<code v-pre>{{title}}</code>、<code v-pre>{{message}}</code>、<code v-pre>{{timestamp}}</code>、<code v-pre>{{content}}</code>、<code v-pre>{{number}}</code>、<code v-pre>{{device_id}}</code>、<code v-pre>{{device_name}}</code>、<code v-pre>{{device_label}}</code>、<code v-pre>{{time}}</code>。
                    </div>
                  </div>
                </div>
              </el-tab-pane>
            </el-tabs>
         </div>
      </section>
    </div>
    </section>
  </div>
</template>

<style scoped>
:deep(.notify-card .el-input-number) {
  width: 100%;
}
:deep(.wecom-template-input textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
:deep(.settings-notify-tabs) {
  border: none;
  background: transparent;
}
:deep(.settings-notify-tabs .el-tabs__header) {
  margin-bottom: 24px;
  background-color: var(--el-fill-color-light);
  border-radius: 6px;
  border-bottom: none;
  display: block;
  width: 100%;
  padding: 4px;
}
:deep(.settings-notify-tabs .el-tabs__nav-wrap::after) {
  display: none;
}
:deep(.settings-notify-tabs .el-tabs__active-bar) {
  display: none;
}
:deep(.settings-notify-tabs .el-tabs__item) {
  height: 38px;
  line-height: 38px;
  padding: 0 14px !important;
  border-radius: 4px;
  margin-right: 4px;
  color: var(--el-text-color-regular);
  transition: background-color 160ms ease, color 160ms ease;
  font-weight: 500;
}
:deep(.settings-notify-tabs .el-tabs__item:last-child) {
  margin-right: 0;
}
:deep(.settings-notify-tabs .el-tabs__item:hover) {
  color: var(--el-color-primary);
}
:deep(.settings-notify-tabs .el-tabs__item.is-active) {
  background-color: var(--el-bg-color);
  color: var(--el-color-primary);
  font-weight: 600;
  box-shadow: inset 0 0 0 1px var(--ui-border);
}

.settings-page :deep(.el-form-item__label),
.settings-page label {
  color: var(--ui-text-muted);
  letter-spacing: 0;
}

.settings-workspace-shell {
  min-width: 0;
  overflow: hidden;
  container-type: inline-size;
  animation: settings-panel-enter 240ms var(--ui-ease-out) both;
}

.settings-workspace-stage {
  margin-bottom: 0;
  border: 0;
  border-bottom: 1px solid var(--ui-border);
  border-radius: 0;
  animation: none;
}

.settings-workspace {
  display: grid;
  grid-template-columns: minmax(300px, 360px) minmax(0, 1fr);
  align-items: start;
}

.settings-workspace > section {
  min-width: 0;
  background: transparent;
  animation: settings-panel-enter 240ms var(--ui-ease-out) both;
}

.settings-security-card {
  border-right: 1px solid var(--ui-border);
}

.settings-system-card {
  animation-delay: 40ms !important;
}

.settings-page .notify-card {
  grid-column: 1 / -1;
  border-top: 1px solid var(--ui-border);
  background: transparent;
  animation-delay: 80ms;
}

@media (max-width: 640px) {
  :deep(.settings-notify-tabs .el-tabs__item) {
    height: 44px;
    line-height: 44px;
    padding: 0 12px !important;
  }
}

@keyframes settings-panel-enter {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

@media (max-width: 960px) {
  .settings-workspace {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-page .notify-card {
    grid-column: auto;
  }

  .settings-security-card {
    border-right: 0;
    border-bottom: 1px solid var(--ui-border);
  }
}

@container (max-width: 980px) {
  .settings-workspace-stage {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-workspace-stage :deep(.workspace-stage-aside) {
    padding: 12px 24px 16px;
    border-top: 1px solid var(--ui-border);
    border-left: 0;
  }

  .settings-workspace-stage :deep(.workspace-stage-stats) {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (prefers-reduced-motion: reduce) {
  .settings-workspace-shell,
  .settings-workspace > section {
    animation-name: settings-panel-fade;
  }

  @keyframes settings-panel-fade {
    from { opacity: 0; }
    to { opacity: 1; }
  }
}
</style>
