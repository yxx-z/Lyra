<template>
  <div class="account-settings">
    <!-- 面板头部 -->
    <div class="account-settings-header">
      <h2>用户管理</h2>
      <button class="close-btn" type="button" @click="$emit('close')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 18px; height: 18px;">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    <!-- 操作结果提示 -->
    <div v-if="statusMessage" :class="['status-message', statusIsError ? 'status-error' : 'status-ok']">
      {{ statusMessage }}
    </div>

    <!-- 自助注册开关 -->
    <div class="settings-section">
      <h3 class="settings-section-title">注册设置</h3>
      <label class="settings-checkbox-label">
        <input
          v-model="allowRegistration"
          class="settings-checkbox"
          type="checkbox"
          @change="onToggleRegistration"
        />
        <span>允许自助注册</span>
      </label>
    </div>

    <!-- 新建用户表单 -->
    <div class="settings-section">
      <h3 class="settings-section-title">新建用户</h3>
      <form class="user-create-form" @submit.prevent="submitCreate">
        <div class="custom-form-group">
          <label>用户名</label>
          <input
            v-model="newUsername"
            class="custom-input"
            placeholder="请输入用户名"
            type="text"
          />
        </div>
        <div class="custom-form-group">
          <label>初始密码</label>
          <input
            v-model="newPassword"
            class="custom-input"
            placeholder="请输入密码"
            type="password"
          />
        </div>
        <label class="settings-checkbox-label">
          <input v-model="newIsAdmin" class="settings-checkbox" type="checkbox" />
          <span>管理员</span>
        </label>
        <button :disabled="creating" class="custom-btn-primary" style="margin-top: 8px;" type="submit">
          {{ creating ? '创建中…' : '创建' }}
        </button>
      </form>
    </div>

    <!-- 用户列表 -->
    <div class="settings-section">
      <h3 class="settings-section-title">用户列表</h3>
      <div v-if="loading" class="muted">加载中…</div>
      <div v-else-if="users.length === 0" class="muted">暂无用户。</div>
      <table v-else class="user-table">
        <thead>
          <tr>
            <th>用户名</th>
            <th>角色</th>
            <th>Subsonic 密码</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>{{ u.username }}</td>
            <td>{{ u.isAdmin ? '管理员' : '普通' }}</td>
            <td>{{ u.hasSubsonicPassword ? '已设' : '未设' }}</td>
            <td class="user-actions">
              <button class="custom-btn-secondary" type="button" @click="toggleRole(u)">
                {{ u.isAdmin ? '降为普通' : '升为管理员' }}
              </button>
              <button class="custom-btn-secondary" type="button" @click="doResetPassword(u.id)">
                重置密码
              </button>
              <button class="custom-btn-danger" type="button" @click="doDelete(u.id, u.username)">
                删除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { ApiClient, AdminUser } from '../api/client'

const props = defineProps<{
  api: ApiClient
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const loading = ref(false)
const users = ref<AdminUser[]>([])
const allowRegistration = ref(false)

const newUsername = ref('')
const newPassword = ref('')
const newIsAdmin = ref(false)
const creating = ref(false)

const statusMessage = ref('')
const statusIsError = ref(false)

function showStatus(msg: string, isError = false) {
  statusMessage.value = msg
  statusIsError.value = isError
  setTimeout(() => {
    statusMessage.value = ''
  }, 4000)
}

async function loadData() {
  loading.value = true
  try {
    const [usersResp, settingsResp] = await Promise.all([
      props.api.listUsers(),
      props.api.getAdminSettings(),
    ])
    users.value = usersResp.users
    allowRegistration.value = settingsResp.allowRegistration
  } catch (e) {
    showStatus((e as Error).message, true)
  } finally {
    loading.value = false
  }
}

async function onToggleRegistration() {
  try {
    await props.api.setAdminSettings(allowRegistration.value)
    showStatus('注册设置已保存')
  } catch (e) {
    showStatus((e as Error).message, true)
    // 回滚本地 checkbox 状态
    allowRegistration.value = !allowRegistration.value
  }
}

async function submitCreate() {
  if (!newUsername.value.trim()) {
    showStatus('用户名不能为空', true)
    return
  }
  if (!newPassword.value) {
    showStatus('密码不能为空', true)
    return
  }
  creating.value = true
  try {
    await props.api.createUser(newUsername.value.trim(), newPassword.value, newIsAdmin.value)
    newUsername.value = ''
    newPassword.value = ''
    newIsAdmin.value = false
    showStatus('用户创建成功')
    await loadData()
  } catch (e) {
    showStatus((e as Error).message, true)
  } finally {
    creating.value = false
  }
}

async function toggleRole(u: AdminUser) {
  try {
    await props.api.setUserRole(u.id, !u.isAdmin)
    showStatus(`已将 ${u.username} 设为${u.isAdmin ? '普通用户' : '管理员'}`)
    await loadData()
  } catch (e) {
    showStatus((e as Error).message, true)
  }
}

async function doResetPassword(id: string) {
  const newPwd = window.prompt('请输入新密码（至少 4 位）：')
  if (newPwd === null) return
  if (newPwd.length < 4) {
    showStatus('密码至少 4 位', true)
    return
  }
  try {
    await props.api.resetUserPassword(id, newPwd)
    showStatus('密码已重置')
  } catch (e) {
    showStatus((e as Error).message, true)
  }
}

async function doDelete(id: string, username: string) {
  if (!window.confirm(`确定要删除用户 "${username}" 吗？此操作不可撤销。`)) return
  try {
    await props.api.deleteUser(id)
    showStatus(`用户 ${username} 已删除`)
    await loadData()
  } catch (e) {
    showStatus((e as Error).message, true)
  }
}

onMounted(() => {
  void loadData()
})
</script>

<style scoped>
/* ── 面板容器（与 AccountSettings 保持一致） ── */
.account-settings {
  padding: 32px;
  display: flex;
  flex-direction: column;
  gap: 24px;
  max-width: 720px;
}

.account-settings-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.account-settings-header h2 {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--text);
}

.close-btn {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  transition: color 0.2s, background 0.2s;
}

.close-btn:hover {
  color: var(--text);
  background: rgba(255, 255, 255, 0.05);
}

.close-btn svg {
  width: 16px;
  height: 16px;
}

.settings-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.settings-section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  border-bottom: 1px solid var(--border-glass);
  padding-bottom: 8px;
  margin: 0;
}

.settings-checkbox-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--text);
  cursor: pointer;
}

.settings-checkbox {
  width: 16px;
  height: 16px;
  cursor: pointer;
  accent-color: var(--accent);
}

/* ── 用户表格 ── */
.user-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.user-table th,
.user-table td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border, rgba(255, 255, 255, 0.08));
}

.user-table th {
  font-weight: 600;
  color: var(--muted, rgba(255, 255, 255, 0.5));
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.user-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.user-create-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.status-message {
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
  margin-bottom: 12px;
}

.status-ok {
  background: rgba(34, 197, 94, 0.12);
  color: #4ade80;
  border: 1px solid rgba(34, 197, 94, 0.25);
}

.status-error {
  background: rgba(239, 68, 68, 0.12);
  color: #f87171;
  border: 1px solid rgba(239, 68, 68, 0.25);
}

.custom-btn-secondary {
  padding: 6px 14px;
  border-radius: 8px;
  border: 1px solid var(--border-glass-active);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.custom-btn-secondary:hover {
  background: rgba(255, 255, 255, 0.08);
  border-color: var(--accent);
  color: var(--accent);
}

.custom-btn-secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.custom-btn-danger {
  padding: 6px 14px;
  border-radius: 8px;
  border: 1px solid rgba(239, 68, 68, 0.35);
  background: rgba(239, 68, 68, 0.08);
  color: #f87171;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.custom-btn-danger:hover {
  background: rgba(239, 68, 68, 0.15);
  border-color: #f87171;
}
</style>
