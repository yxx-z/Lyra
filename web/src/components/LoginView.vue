<template>
  <main class="login-screen">
    <section class="login-panel">
      <div>
        <p class="eyebrow">Self-Hosted Music Archive</p>
        <h1>Lyra</h1>
        <p class="muted">登录即可浏览并聆听您的本地高品质无损音乐库。</p>
      </div>

      <form class="login-form" @submit.prevent="submit">
        <div class="custom-form-group">
          <label for="username">用户名</label>
          <input
            id="username"
            v-model="username"
            autocomplete="username"
            class="custom-input"
            placeholder="admin"
            required
            type="text"
          />
        </div>
        <div class="custom-form-group">
          <label for="password">密码</label>
          <input
            id="password"
            v-model="password"
            autocomplete="current-password"
            class="custom-input"
            placeholder="请输入密码"
            required
            type="password"
          />
        </div>

        <div v-if="error" class="custom-alert">
          {{ error }}
        </div>

        <button :disabled="loading" class="custom-btn-primary" type="submit">
          {{ loading ? '正在登入...' : '立即登录' }}
        </button>
      </form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const emit = defineEmits<{
  login: [payload: { username: string; password: string }]
}>()

defineProps<{
  loading: boolean
  error: string
}>()

const username = ref('admin')
const password = ref('')

function submit() {
  emit('login', { username: username.value.trim(), password: password.value })
}
</script>
