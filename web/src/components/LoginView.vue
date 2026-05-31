<template>
  <main class="login-screen">
    <section class="login-panel">
      <div>
        <p class="eyebrow">Self-hosted music</p>
        <h1>Lyra</h1>
        <p class="muted">Sign in to browse and play your local library.</p>
      </div>

      <n-form class="login-form" @submit.prevent="submit">
        <n-form-item label="Username">
          <n-input v-model:value="username" autocomplete="username" placeholder="admin" />
        </n-form-item>
        <n-form-item label="Password">
          <n-input
            v-model:value="password"
            autocomplete="current-password"
            placeholder="Password"
            type="password"
          />
        </n-form-item>
        <n-alert v-if="error" type="error" :bordered="false">
          {{ error }}
        </n-alert>
        <n-button attr-type="submit" block type="primary" :loading="loading"> Sign in </n-button>
      </n-form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { NAlert, NButton, NForm, NFormItem, NInput } from 'naive-ui'

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
