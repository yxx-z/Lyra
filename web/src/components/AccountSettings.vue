<template>
  <div class="account-settings">
    <section class="settings-section">
      <h3>修改登录密码</h3>
      <div class="custom-form-group">
        <label>原密码</label>
        <input v-model="oldPw" class="custom-input" type="password" placeholder="请输入原密码" autocomplete="current-password" />
      </div>
      <div class="custom-form-group">
        <label>新密码（至少 4 位）</label>
        <input v-model="newPw" class="custom-input" type="password" placeholder="请输入新密码" autocomplete="new-password" />
      </div>
      <button class="custom-btn-secondary" :disabled="busy" type="button" @click="changePw">保存</button>
    </section>

    <section class="settings-section">
      <h3>Subsonic 密码</h3>
      <p class="muted-hint">用于 Symfonium 等客户端登录（与登录密码独立）。</p>
      <div class="custom-form-group">
        <label>Subsonic 密码</label>
        <input v-model="subPw" class="custom-input" type="password" placeholder="设置 Subsonic 密码" autocomplete="new-password" />
      </div>
      <button class="custom-btn-secondary" :disabled="busy" type="button" @click="saveSub">保存</button>
    </section>

    <div v-if="msg" :class="['feedback-msg', msgError ? 'feedback-error' : 'feedback-ok']">
      {{ msg }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ApiClient } from '../api/client'

const props = defineProps<{ api: ApiClient }>()

const oldPw = ref('')
const newPw = ref('')
const subPw = ref('')
const busy = ref(false)
const msg = ref('')
const msgError = ref(false)

function show(text: string, isErr = false) {
  msg.value = text
  msgError.value = isErr
}

async function changePw() {
  if (!oldPw.value || newPw.value.length < 4) {
    show('原密码不能为空，新密码至少 4 位', true)
    return
  }
  busy.value = true
  try {
    await props.api.changePassword(oldPw.value, newPw.value)
    show('登录密码已更新')
    oldPw.value = ''
    newPw.value = ''
  } catch (e) {
    show(e instanceof Error ? e.message : '更新失败', true)
  } finally {
    busy.value = false
  }
}

async function saveSub() {
  if (!subPw.value) {
    show('Subsonic 密码不能为空', true)
    return
  }
  busy.value = true
  try {
    await props.api.setSubsonicPassword(subPw.value)
    show('Subsonic 密码已更新')
    subPw.value = ''
  } catch (e) {
    show(e instanceof Error ? e.message : '更新失败', true)
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.account-settings {
  padding: 32px;
  display: flex;
  flex-direction: column;
  gap: 32px;
  max-width: 480px;
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
  gap: 14px;
}

.settings-section h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  border-bottom: 1px solid var(--border-glass);
  padding-bottom: 8px;
}

.muted-hint {
  font-size: 13px;
  color: var(--text-muted);
  line-height: 1.5;
}

/* 复用全局 custom-form-group / custom-input 类 */
.custom-btn-secondary {
  padding: 10px 20px;
  border-radius: 10px;
  border: 1px solid var(--border-glass-active);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text);
  font-size: 14px;
  font-weight: 500;
  align-self: flex-start;
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

.feedback-msg {
  padding: 10px 14px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 500;
}
.feedback-ok {
  background: rgba(16, 185, 129, 0.12);
  border: 1px solid rgba(16, 185, 129, 0.25);
  color: var(--accent);
}
.feedback-error {
  background: rgba(239, 68, 68, 0.12);
  border: 1px solid rgba(239, 68, 68, 0.2);
  color: #fca5a5;
}
</style>
