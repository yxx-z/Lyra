<template>
  <main class="login-screen">
    <section class="login-panel">
      <div>
        <p class="eyebrow">Self-Hosted Music Archive</p>
        <h1>Lyra</h1>
        <p class="muted">首次启动，请创建管理员账号。</p>
      </div>

      <form class="login-form" @submit.prevent="submit">
        <div class="custom-form-group">
          <label for="setup-username">用户名</label>
          <input
            id="setup-username"
            v-model="username"
            autocomplete="username"
            class="custom-input"
            placeholder="admin"
            type="text"
          />
        </div>
        <div class="custom-form-group">
          <label for="setup-password">密码（至少 4 位）</label>
          <input
            id="setup-password"
            v-model="password"
            autocomplete="new-password"
            class="custom-input"
            placeholder="请输入密码"
            type="password"
          />
        </div>
        <div class="custom-form-group">
          <label for="setup-confirm">确认密码</label>
          <input
            id="setup-confirm"
            v-model="confirm"
            autocomplete="new-password"
            class="custom-input"
            placeholder="再次输入密码"
            type="password"
          />
        </div>

        <div v-if="displayError" class="custom-alert">
          {{ displayError }}
        </div>

        <button :disabled="loading" class="custom-btn-primary" type="submit">
          {{ loading ? '创建中…' : '创建管理员' }}
        </button>
      </form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  loading: boolean
  error: string
}>()

const emit = defineEmits<{
  (e: 'setup', payload: { username: string; password: string }): void
}>()

const username = ref('')
const password = ref('')
const confirm = ref('')
const localError = ref('')

const displayError = computed(() => localError.value || props.error)

function submit() {
  localError.value = ''
  if (!username.value.trim()) {
    localError.value = '用户名不能为空'
    return
  }
  if (password.value.length < 4) {
    localError.value = '密码至少 4 位'
    return
  }
  if (password.value !== confirm.value) {
    localError.value = '两次密码不一致'
    return
  }
  emit('setup', { username: username.value.trim(), password: password.value })
}
</script>
