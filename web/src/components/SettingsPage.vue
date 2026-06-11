<template>
  <div class="settings-page">
    <div class="settings-tabs">
      <button
        type="button"
        class="settings-tab"
        :class="{ active: tab === 'account' }"
        @click="tab = 'account'"
      >账户设置</button>
      <button
        v-if="isAdmin"
        type="button"
        class="settings-tab"
        :class="{ active: tab === 'users' }"
        @click="tab = 'users'"
      >用户管理</button>
    </div>

    <AccountSettings v-if="tab === 'account'" :api="api" />
    <UserManagement v-else-if="tab === 'users' && isAdmin" :api="api" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ApiClient } from '../api/client'
import AccountSettings from './AccountSettings.vue'
import UserManagement from './UserManagement.vue'

defineProps<{ api: ApiClient; isAdmin: boolean }>()

const tab = ref<'account' | 'users'>('account')
</script>

<style scoped>
.settings-page {
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.settings-tabs {
  display: flex;
  gap: 8px;
  border-bottom: 1px solid var(--border, #333);
  padding-bottom: 4px;
}
.settings-tab {
  background: none;
  border: none;
  padding: 8px 14px;
  border-radius: 8px 8px 0 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted, #888);
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}
.settings-tab:hover {
  color: var(--text, #fff);
}
.settings-tab.active {
  color: var(--accent, #6ee7b7);
  background: rgba(255, 255, 255, 0.04);
}
</style>
