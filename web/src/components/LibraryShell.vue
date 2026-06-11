<template>
  <div class="app-shell">
    <!-- 侧边导航栏 -->
    <aside class="sidebar">
      <div class="sidebar-top-group" style="width: 100%; display: flex; flex-direction: column; align-items: center;">
        <div class="brand">L</div>
        
        <nav class="nav-buttons-container">
          <button
            v-for="item in navItems"
            :key="item.mode"
            :class="{ active: mode === item.mode }"
            class="nav-button"
            type="button"
            @click="$emit('change-mode', item.mode)"
          >
            <!-- 动态渲染手绘精细 SVG 图标 -->
            <component :is="item.iconComponent" />
            <span>{{ item.label }}</span>
          </button>
        </nav>
      </div>

      <!-- 登出归纳至侧边栏底部（收藏/设置已上移为垂直导航项） -->
      <div class="logout-nav-container">
        <button
          class="logout-nav-button"
          title="退出登录"
          type="button"
          @click="$emit('logout')"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
            <path d="M16 17l5-5-5-5" />
            <path d="M21 12H9" />
          </svg>
        </button>
      </div>
    </aside>

    <!-- 工作空间 -->
    <section class="workspace">
      <!-- 顶部功能状态栏 -->
      <header class="topbar">
        <div class="topbar-info">
          <p class="eyebrow">{{ title }}</p>
          <h2>{{ heading }}</h2>
        </div>

        <!-- 极简高质感搜索输入框 -->
        <div class="search-form-container">
          <div class="search-input-wrapper">
            <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              v-model="searchText"
              class="search-input"
              placeholder="搜索曲目、专辑或歌手..."
              type="text"
              @input="onSearchInput"
            />
          </div>
        </div>

        <div class="topbar-actions">
          <button class="topbar-btn secondary" type="button" @click="$emit('refresh')">
            刷新库
          </button>
        </div>
      </header>

      <!-- 主视图插槽区域 -->
      <slot />

      <!-- 底部播放条插槽区域 -->
      <slot name="player" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, h } from 'vue'
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

// 定义精美手绘 SVG 图标组件
const AlbumsIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('rect', { x: '3', y: '3', width: '7', height: '7', rx: '1.5' }),
    h('rect', { x: '14', y: '3', width: '7', height: '7', rx: '1.5' }),
    h('rect', { x: '14', y: '14', width: '7', height: '7', rx: '1.5' }),
    h('rect', { x: '3', y: '14', width: '7', height: '7', rx: '1.5' })
  ]
)

const ArtistsIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('path', { d: 'M12 12a5 5 0 1 0 0-10 5 5 0 0 0 0 10z' }),
    h('path', { d: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2' })
  ]
)

const ScanIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('path', { d: 'M23 4v6h-6' }),
    h('path', { d: 'M20.49 15a9 9 0 1 1-2.12-9.36L23 10' })
  ]
)

const FavoritesIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('path', { d: 'M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z' })
  ]
)

const PlaylistsIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('line', { x1: '8', y1: '6', x2: '21', y2: '6' }),
    h('line', { x1: '8', y1: '12', x2: '21', y2: '12' }),
    h('line', { x1: '8', y1: '18', x2: '15', y2: '18' }),
    h('circle', { cx: '4', cy: '6', r: '1' }),
    h('circle', { cx: '4', cy: '12', r: '1' }),
    h('circle', { cx: '4', cy: '18', r: '1' })
  ]
)

const SettingsIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('circle', { cx: '12', cy: '12', r: '3' }),
    h('path', { d: 'M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z' })
  ]
)

const navItems = [
  { mode: 'albums' as ViewMode, label: '专辑', iconComponent: AlbumsIcon },
  { mode: 'artists' as ViewMode, label: '歌手', iconComponent: ArtistsIcon },
  { mode: 'scan' as ViewMode, label: '系统扫描', iconComponent: ScanIcon },
  { mode: 'favorites' as ViewMode, label: '收藏', iconComponent: FavoritesIcon },
  { mode: 'playlists' as ViewMode, label: '歌单', iconComponent: PlaylistsIcon },
  { mode: 'settings' as ViewMode, label: '设置', iconComponent: SettingsIcon },
]

const title = computed(() => {
  if (props.mode === 'artists') return 'ARTISTS'
  if (props.mode === 'scan') return 'SYSTEM SCANNER'
  if (props.mode === 'favorites') return 'FAVORITES'
  if (props.mode === 'playlists') return 'PLAYLISTS'
  if (props.mode === 'settings') return 'SETTINGS'
  return 'LIBRARY'
})

const heading = computed(() => {
  if (props.mode === 'artists') return '按歌手浏览'
  if (props.mode === 'scan') return '扫描与管理状态'
  if (props.mode === 'favorites') return '我的收藏'
  if (props.mode === 'playlists') return '我的歌单'
  if (props.mode === 'settings') return '设置'
  return '我的专辑'
})

// 即输即搜实时反馈
function onSearchInput() {
  emit('search', searchText.value.trim())
}
</script>
