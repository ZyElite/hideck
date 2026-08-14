<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useRoute, useRouter } from 'vue-router'
import { Person24Regular, LockClosed24Regular, ArrowRight24Regular } from '@vicons/fluent'
import { systemService } from '../services/system'
import type { PasswordCredentialStatus } from '../types/credentials'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const form = ref({ username: '', password: '' })
const loading = ref(false)
const passwordChangeOpen = ref(false)
const changingPassword = ref(false)
const passwordChangeError = ref('')
const newPasswordInput = ref<{ focus: () => void } | null>(null)
const passwordChangeForm = ref({ old_password: '', new_password: '', confirm_password: '' })

async function handleLogin() {
  const { ElMessage } = await import('element-plus')
  if (!form.value.username || !form.value.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  loading.value = true
  const result = await auth.login(form.value.username, form.value.password)
  loading.value = false
  if (!result.ok) {
    ElMessage.error('登录失败，请检查凭证')
    return
  }

  if (await startPasswordRemediation(result.credential)) {
    return
  }
  await completeLogin('欢迎回来')
}

async function startPasswordRemediation(status: PasswordCredentialStatus): Promise<boolean> {
  if (!status.change_required) return false
  const { ElMessageBox } = await import('element-plus')
  if (status.management === 'environment') {
    const variable = status.environment_variable || 'PROXY_WEB_PASSWORD'
    await ElMessageBox.alert(
      `当前登录密码强度不足，并由环境变量 ${variable} 管理。控制台不会覆盖环境变量，请在部署环境中修改后重启 HiDeck。`,
      '请更换弱密码',
      { confirmButtonText: '我知道了', showClose: false, closeOnClickModal: false, closeOnPressEscape: false, type: 'warning' }
    )
    return false
  }
  passwordChangeForm.value = {
    old_password: form.value.password,
    new_password: '',
    confirm_password: ''
  }
  form.value.password = ''
  passwordChangeError.value = ''
  passwordChangeOpen.value = true
  return true
}

async function submitPasswordChange() {
  passwordChangeError.value = validatePasswordChange()
  if (passwordChangeError.value) return

  changingPassword.value = true
  const result = await systemService.changePassword(passwordChangeForm.value)
  changingPassword.value = false
  if (!result.ok) {
    passwordChangeError.value = result.error.message || '密码更新失败'
    return
  }
  auth.applyToken(result.data.token)
  await completeLogin('密码已更新')
}

function validatePasswordChange(): string {
  if (!passwordChangeForm.value.old_password || !passwordChangeForm.value.new_password) {
    return '请填写当前密码和新密码'
  }
  if (passwordChangeForm.value.new_password !== passwordChangeForm.value.confirm_password) {
    return '两次输入的新密码不一致'
  }
  return ''
}

async function completeLogin(message: string) {
  const { ElMessage } = await import('element-plus')
  passwordChangeOpen.value = false
  passwordChangeForm.value = { old_password: '', new_password: '', confirm_password: '' }
  passwordChangeError.value = ''
  form.value.password = ''
  ElMessage.success(message)
  await redirectAfterLogin()
}

async function redirectAfterLogin() {
  const queryRedirect = typeof route.query.redirect === 'string' ? route.query.redirect : ''
  let redirect = queryRedirect ? decodeURIComponent(queryRedirect) : ''
  if (!redirect) {
    try {
      redirect = sessionStorage.getItem('post_login_redirect') || ''
    } catch {
      // Storage can be unavailable in hardened browser contexts.
    }
  }
  if (redirect) {
    try {
      sessionStorage.removeItem('post_login_redirect')
    } catch {
      // Storage can be unavailable in hardened browser contexts.
    }
    await router.push(redirect)
    return
  }
  await router.push('/')
}
</script>

<template>
  <main class="login-page">
    <section class="login-identity" aria-label="HiDeck 产品信息">
      <div class="network-map" aria-hidden="true">
        <span class="network-line line-a" />
        <span class="network-line line-b" />
        <span class="network-line line-c" />
        <span class="network-line line-d" />
        <span class="network-node node-a" />
        <span class="network-node node-b" />
        <span class="network-node node-c" />
        <span class="network-node node-d" />
        <span class="network-label label-a">CORE-01</span>
        <span class="network-label label-b">GW-02</span>
        <span class="network-label label-c">BTS-07</span>
      </div>

      <div class="identity-topline">
        <span class="identity-mark">H</span>
        <div>
          <strong>HiDeck</strong>
          <span>MODEM CONTROL</span>
        </div>
      </div>

      <div class="identity-copy">
        <span class="identity-kicker">TELECOM OPERATIONS</span>
        <h1>通信模组控制台</h1>
        <p>设备、网络、短信与 VoWiFi 状态集中管理。</p>
      </div>

      <div class="signal-panel" aria-label="控制台状态">
        <div class="signal-bars" aria-hidden="true">
          <i /><i /><i /><i />
        </div>
        <div>
          <strong>CONTROL PLANE READY</strong>
          <span>QMI · MBIM · AT · IMS</span>
        </div>
        <span class="ready-dot" aria-hidden="true" />
      </div>
    </section>

    <section class="login-access">
      <div class="login-form-wrap">
        <header>
          <span class="form-kicker">SECURE ACCESS</span>
          <h2>登录 HiDeck</h2>
          <p>使用管理账户进入控制台</p>
        </header>

        <form @submit.prevent="handleLogin">
          <label for="login-username">用户名</label>
          <div class="field-shell">
            <Person24Regular aria-hidden="true" />
            <input
              id="login-username"
              v-model.trim="form.username"
              type="text"
              name="username"
              autocomplete="username"
              placeholder="请输入用户名"
            />
          </div>

          <label for="login-password">密码</label>
          <div class="field-shell">
            <LockClosed24Regular aria-hidden="true" />
            <input
              id="login-password"
              v-model="form.password"
              type="password"
              name="password"
              autocomplete="current-password"
              placeholder="请输入密码"
            />
          </div>

          <button type="submit" :disabled="loading">
            <span v-if="loading" class="login-spinner" aria-hidden="true" />
            <span>{{ loading ? '正在验证' : '登录' }}</span>
            <ArrowRight24Regular v-if="!loading" aria-hidden="true" />
          </button>
        </form>

        <footer>HiDeck · 2026</footer>
      </div>
    </section>

    <el-dialog
      v-model="passwordChangeOpen"
      title="修改登录密码"
      width="min(440px, calc(100vw - 32px))"
      :show-close="false"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      @opened="newPasswordInput?.focus()"
    >
      <form class="password-change-form" @submit.prevent="submitPasswordChange">
        <p class="password-change-notice">当前密码仍是初始明文凭证或强度不足。建议立即修改。</p>

        <div class="space-y-1">
          <label for="login-current-password">当前密码</label>
          <el-input
            id="login-current-password"
            v-model="passwordChangeForm.old_password"
            type="password"
            show-password
            autocomplete="current-password"
          />
        </div>
        <div class="space-y-1">
          <label for="login-new-password">新密码</label>
          <el-input
            id="login-new-password"
            ref="newPasswordInput"
            v-model="passwordChangeForm.new_password"
            type="password"
            show-password
            autocomplete="new-password"
            placeholder="至少 8 位，建议 12 位以上"
          />
        </div>
        <div class="space-y-1">
          <label for="login-confirm-password">确认新密码</label>
          <el-input
            id="login-confirm-password"
            v-model="passwordChangeForm.confirm_password"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </div>

        <p v-if="passwordChangeError" class="password-change-error" role="alert">
          {{ passwordChangeError }}
        </p>
        <div class="password-change-actions">
          <el-button native-type="button" :disabled="changingPassword" @click="completeLogin('欢迎回来')">
            稍后处理
          </el-button>
          <el-button type="primary" native-type="submit" :loading="changingPassword">
            更新密码并进入
          </el-button>
        </div>
      </form>
    </el-dialog>
  </main>
</template>

<style scoped>
.login-page {
  width: min(1080px, calc(100% - 40px));
  min-height: 650px;
  display: grid;
  grid-template-columns: minmax(0, 1.08fr) minmax(380px, .92fr);
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 24px;
  background: var(--ui-surface);
  box-shadow: var(--ui-shadow-lg);
}

.login-identity {
  position: relative;
  padding: 38px 42px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  overflow: hidden;
  background:
    radial-gradient(circle at 76% 46%, rgba(92, 234, 177, .16), transparent 28%),
    linear-gradient(145deg, #071014, #0a1719 72%, #071012);
  color: #f2fbfb;
}

.login-identity::before,
.login-identity::after {
  position: absolute;
  content: "";
  pointer-events: none;
}

.login-identity::before {
  inset: 0;
  opacity: .16;
  background-size: 54px 54px;
  background-image:
    linear-gradient(rgba(121, 170, 174, .2) 1px, transparent 1px),
    linear-gradient(90deg, rgba(121, 170, 174, .2) 1px, transparent 1px);
}

.login-identity::after {
  inset: 0;
  background: rgba(4, 24, 27, .22);
}

.network-map {
  position: absolute;
  inset: 0;
  z-index: 1;
  pointer-events: none;
}

.network-line {
  position: absolute;
  height: 1px;
  transform-origin: left center;
  background: rgba(94, 194, 185, .42);
}

.line-a { top: 18%; left: 8%; width: 44%; transform: rotate(9deg); }
.line-b { top: 36%; left: 46%; width: 42%; transform: rotate(-16deg); }
.line-c { top: 72%; left: 5%; width: 48%; transform: rotate(-7deg); }
.line-d { top: 60%; left: 63%; width: 34%; transform: rotate(21deg); }

.network-node {
  position: absolute;
  width: 7px;
  height: 7px;
  border: 1px solid #67d2ca;
  background: #102a2e;
  box-shadow: 0 0 0 3px rgba(103, 210, 202, .09);
}

.node-a { top: 21%; left: 27%; }
.node-b { top: 30%; right: 17%; }
.node-c { bottom: 24%; left: 15%; }
.node-d { bottom: 15%; right: 9%; }

.network-label {
  position: absolute;
  color: rgba(131, 224, 216, .66);
  font: 9px/1 "v-mono", ui-monospace, monospace;
}

.label-a { top: 14%; left: 29%; }
.label-b { top: 26%; right: 9%; }
.label-c { bottom: 20%; left: 17%; }

.identity-topline,
.identity-copy,
.signal-panel {
  position: relative;
  z-index: 2;
}

.identity-topline {
  display: flex;
  align-items: center;
  gap: 12px;
}

.identity-mark {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border: 1px solid rgba(131, 224, 216, .45);
  border-radius: 13px;
  background: rgba(92, 234, 177, .12);
  color: #8ff7cb;
  font-size: 18px;
  font-weight: 700;
}

.identity-topline div {
  display: flex;
  flex-direction: column;
}

.identity-topline strong {
  font-size: 18px;
  line-height: 1.1;
}

.identity-topline div span,
.identity-kicker,
.form-kicker {
  font-family: "v-mono", ui-monospace, monospace;
  letter-spacing: 0;
}

.identity-topline div span {
  margin-top: 3px;
  color: #86a5a9;
  font-size: 9px;
}

.identity-copy {
  max-width: 430px;
}

.identity-kicker,
.form-kicker {
  color: #67d2ca;
  font-size: 11px;
  font-weight: 700;
}

.identity-copy h1 {
  margin: 13px 0 14px;
  font-size: 44px;
  font-weight: 580;
  line-height: 1.15;
  letter-spacing: 0;
}

.identity-copy p {
  margin: 0;
  color: #a9bfc2;
  font-size: 15px;
}

.signal-panel {
  min-height: 72px;
  padding: 14px 16px;
  display: grid;
  grid-template-columns: 38px 1fr auto;
  align-items: center;
  gap: 14px;
  border: 1px solid rgba(255, 255, 255, .11);
  border-radius: 16px;
  background: rgba(255, 255, 255, .04);
}

.signal-bars {
  height: 28px;
  display: flex;
  align-items: flex-end;
  gap: 3px;
}

.signal-bars i {
  width: 5px;
  border-radius: 1px;
  background: #67d2ca;
}

.signal-bars i:nth-child(1) { height: 8px; }
.signal-bars i:nth-child(2) { height: 14px; }
.signal-bars i:nth-child(3) { height: 21px; }
.signal-bars i:nth-child(4) { height: 28px; }

.signal-panel div:nth-child(2) {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.signal-panel strong {
  font-family: "v-mono", ui-monospace, monospace;
  font-size: 11px;
}

.signal-panel div span {
  color: #86a5a9;
  font-size: 11px;
}

.ready-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #4cc38a;
  box-shadow: 0 0 0 4px rgba(76, 195, 138, .12);
}

.login-access {
  padding: 52px;
  display: grid;
  place-items: center;
  background: var(--ui-surface);
}

.login-form-wrap {
  width: min(100%, 360px);
}

.login-form-wrap header {
  margin-bottom: 32px;
}

.login-form-wrap h2 {
  margin: 9px 0 7px;
  color: var(--ui-text);
  font-size: 26px;
  font-weight: 700;
  letter-spacing: 0;
}

.login-form-wrap header p {
  margin: 0;
  color: var(--ui-text-muted);
  font-size: 13px;
}

form {
  display: grid;
  gap: 9px;
}

label {
  margin-top: 7px;
  color: var(--ui-text);
  font-size: 13px;
  font-weight: 650;
}

.field-shell {
  height: 46px;
  padding: 0 13px;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 1px solid var(--ui-border);
  border-radius: 11px;
  background: var(--ui-surface-subtle);
  color: var(--ui-text-muted);
  transition: border-color 160ms ease, box-shadow 160ms ease;
}

.field-shell:focus-within {
  border-color: var(--ui-primary);
  box-shadow: var(--ui-focus);
}

.field-shell svg {
  width: 19px;
  height: 19px;
  flex: 0 0 19px;
}

input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--ui-text);
  font-size: 14px;
}

button {
  height: 46px;
  margin-top: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  border: 0;
  border-radius: 11px;
  background: var(--ui-primary-solid);
  color: #fff;
  font-weight: 700;
  cursor: pointer;
  transition: background-color 160ms ease;
}

button:hover:not(:disabled) { background: var(--ui-primary-hover); }
button:disabled { opacity: .58; cursor: not-allowed; }
button svg { width: 18px; height: 18px; }

.login-spinner {
  width: 18px;
  height: 18px;
  border: 2px solid rgba(255, 255, 255, .36);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin .7s linear infinite;
}

.login-form-wrap footer {
  margin-top: 30px;
  color: var(--ui-text-muted);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: 10px;
  text-align: center;
}

.password-change-form {
  display: grid;
  gap: 14px;
}

.password-change-notice {
  margin: 0 0 2px;
  color: var(--ui-text-muted);
  font-size: 13px;
  line-height: 1.6;
}

.password-change-error {
  margin: 0;
  color: var(--ui-danger);
  font-size: 13px;
  line-height: 1.5;
}

.password-change-actions {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
  gap: 10px;
  margin-top: 4px;
}

.password-change-actions :deep(.el-button) {
  width: 100%;
  height: 44px;
  margin: 0;
  border-radius: 8px;
}

.password-change-actions :deep(.el-button:not(.el-button--primary)) {
  border: 1px solid var(--ui-border);
  background: var(--ui-surface);
  color: var(--ui-text);
}

@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 800px) {
  .login-page {
    width: min(520px, calc(100% - 24px));
    min-height: 0;
    grid-template-columns: 1fr;
  }

  .login-identity {
    min-height: 210px;
    padding: 24px;
  }

  .identity-copy h1 { font-size: 28px; }
  .identity-copy p { display: none; }
  .signal-panel { display: none; }
  .login-access { padding: 32px 24px; }
}
</style>
