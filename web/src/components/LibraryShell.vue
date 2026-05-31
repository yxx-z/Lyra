<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">Lyra</div>
      <button
        v-for="item in navItems"
        :key="item.mode"
        class="nav-button"
        :class="{ active: mode === item.mode }"
        type="button"
        @click="$emit('change-mode', item.mode)"
      >
        <span class="nav-icon">{{ item.icon }}</span>
        <span>{{ item.label }}</span>
      </button>
    </aside>

    <section class="workspace">
      <header class="topbar">
        <div>
          <p class="eyebrow">{{ title }}</p>
          <h2>{{ heading }}</h2>
        </div>
        <form class="search-form" @submit.prevent="submitSearch">
          <n-input v-model:value="searchText" clearable placeholder="Search tracks, albums, artists" />
        </form>
        <n-button secondary @click="$emit('refresh')">Refresh</n-button>
        <n-button quaternary @click="$emit('logout')">Sign out</n-button>
      </header>

      <main class="content-grid">
        <slot />
      </main>

      <slot name="player" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NInput } from 'naive-ui'
import type { ViewMode } from '../api/client'

const props = defineProps<{
  mode: ViewMode
}>()

const emit = defineEmits<{
  'change-mode': [mode: ViewMode]
  refresh: []
  logout: []
  search: [query: string]
}>()

const searchText = ref('')

const navItems: Array<{ mode: ViewMode; label: string; icon: string }> = [
  { mode: 'albums', label: 'Albums', icon: '▦' },
  { mode: 'artists', label: 'Artists', icon: '◎' },
  { mode: 'scan', label: 'Scan', icon: '↻' },
]

const title = computed(() => {
  if (props.mode === 'artists') return 'Artists'
  if (props.mode === 'scan') return 'Library management'
  return 'Library'
})

const heading = computed(() => {
  if (props.mode === 'artists') return 'Browse by artist'
  if (props.mode === 'scan') return 'Scan status'
  return 'Albums'
})

function submitSearch() {
  emit('search', searchText.value.trim())
}
</script>
