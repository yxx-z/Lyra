<template>
  <main class="login-screen">
    <section class="login-panel">
      <div>
        <p class="eyebrow">Self-Hosted Music Archive</p>
        <h1>Lyra</h1>
        <p class="muted">创建一个普通用户账号。</p>
      </div>

      <form class="login-form" @submit.prevent="submit">
        <div class="custom-form-group">
          <label for="reg-username">用户名</label>
          <input
            id="reg-username"
            v-model="username"
            autocomplete="username"
            class="custom-input"
            placeholder="请输入用户名"
            type="text"
          />
        </div>
        <div class="custom-form-group">
          <label for="reg-password">密码（至少 4 位）</label>
          <input
            id="reg-password"
            v-model="password"
            autocomplete="new-password"
            class="custom-input"
            placeholder="请输入密码"
            type="password"
          />
        </div>
        <div class="custom-form-group">
          <label for="reg-confirm">确认密码</label>
          <input
            id="reg-confirm"
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
          {{ loading ? '注册中…' : '注册账号' }}
        </button>

        <button class="custom-btn-secondary" type="button" @click="$emit('back')">
          返回登录
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
  (e: 'register', payload: { username: string; password: string }): void
  (e: 'back'): void
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
  emit('register', { username: username.value.trim(), password: password.value })
}
</script>
