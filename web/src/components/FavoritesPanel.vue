<template>
  <div class="account-settings">
    <!-- 面板头部 -->
    <div class="account-settings-header">
      <h2>收藏夹</h2>
    </div>

    <!-- 标签页切换 -->
    <div class="fav-tabs">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="fav-tab"
        :class="{ active: activeTab === tab.key }"
        type="button"
        @click="switchTab(tab.key)"
      >{{ tab.label }}</button>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="empty-state-fav">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="spin-icon">
        <line x1="12" y1="2" x2="12" y2="6" />
        <line x1="12" y1="18" x2="12" y2="22" />
        <line x1="4.93" y1="4.93" x2="7.76" y2="7.76" />
        <line x1="16.24" y1="16.24" x2="19.07" y2="19.07" />
        <line x1="2" y1="12" x2="6" y2="12" />
        <line x1="18" y1="12" x2="22" y2="12" />
        <line x1="4.93" y1="19.07" x2="7.76" y2="16.24" />
        <line x1="16.24" y1="7.76" x2="19.07" y2="4.93" />
      </svg>
      <span class="muted" style="margin-left: 8px;">加载中…</span>
    </div>

    <!-- 错误提示 -->
    <div v-else-if="error" class="status-message status-error">{{ error }}</div>

    <!-- 我的收藏内容 -->
    <template v-else-if="activeTab === 'favorites'">
      <!-- 收藏专辑 -->
      <div v-if="favAlbums.length" class="settings-section">
        <h3 class="settings-section-title">收藏的专辑</h3>
        <div class="fav-albums-grid">
          <div
            v-for="album in favAlbums"
            :key="album.id"
            class="fav-album-card"
          >
            <img
              v-if="album.cover_url"
              :src="album.cover_url"
              :alt="album.title"
              class="fav-album-cover"
            />
            <div v-else class="fav-album-cover placeholder-cover">{{ album.title.slice(0, 2).toUpperCase() }}</div>
            <p class="fav-album-title" :title="album.title">{{ album.title }}</p>
            <p class="fav-album-artist muted">{{ album.artist || '未知艺术家' }}</p>
          </div>
        </div>
      </div>

      <!-- 收藏曲目 -->
      <div class="settings-section">
        <h3 class="settings-section-title">收藏的歌曲</h3>
        <div v-if="favTracks.length === 0" class="muted" style="font-size: 13px;">暂无收藏的歌曲</div>
        <!-- 使用 div 包装行，避免 button 嵌套（行内含 AddToPlaylist 等交互按钮） -->
        <div v-else class="fav-track-list">
          <div
            v-for="track in favTracks"
            :key="track.id"
            class="fav-track-row"
          >
            <span class="fav-track-title fav-play-area" :title="track.title" role="button" tabindex="0" @click="$emit('play-track', track)" @keydown.enter="$emit('play-track', track)" @keydown.space.prevent="$emit('play-track', track)">{{ track.title }}</span>
            <span class="muted fav-track-meta">{{ track.artist }} · {{ track.album }}</span>
            <span class="muted fav-track-duration">{{ formatDuration(track.duration) }}</span>
            <AddToPlaylist :api="api" :track-id="track.id" />
          </div>
        </div>
      </div>
    </template>

    <!-- 最近播放 -->
    <template v-else-if="activeTab === 'recent'">
      <div class="settings-section">
        <h3 class="settings-section-title">最近播放</h3>
        <div v-if="recentTracks.length === 0" class="muted" style="font-size: 13px;">暂无播放记录</div>
        <div v-else class="fav-track-list">
          <div
            v-for="track in recentTracks"
            :key="track.id"
            class="fav-track-row"
          >
            <span class="fav-track-title fav-play-area" :title="track.title" role="button" tabindex="0" @click="$emit('play-track', track)" @keydown.enter="$emit('play-track', track)" @keydown.space.prevent="$emit('play-track', track)">{{ track.title }}</span>
            <span class="muted fav-track-meta">{{ track.artist }} · {{ track.album }}</span>
            <span class="muted fav-track-duration">{{ formatDuration(track.duration) }}</span>
            <AddToPlaylist :api="api" :track-id="track.id" />
          </div>
        </div>
      </div>
    </template>

    <!-- 最常听 -->
    <template v-else-if="activeTab === 'top'">
      <div class="settings-section">
        <h3 class="settings-section-title">最常听</h3>
        <div v-if="topTracks.length === 0" class="muted" style="font-size: 13px;">暂无播放记录</div>
        <div v-else class="fav-track-list">
          <div
            v-for="(track, idx) in topTracks"
            :key="track.id"
            class="fav-track-row"
          >
            <span class="fav-track-rank muted">{{ idx + 1 }}</span>
            <span class="fav-track-title fav-play-area" :title="track.title" role="button" tabindex="0" @click="$emit('play-track', track)" @keydown.enter="$emit('play-track', track)" @keydown.space.prevent="$emit('play-track', track)">{{ track.title }}</span>
            <span class="muted fav-track-meta">{{ track.artist }} · {{ track.album }}</span>
            <span class="muted fav-track-duration">{{ formatDuration(track.duration) }}</span>
            <AddToPlaylist :api="api" :track-id="track.id" />
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import type { ApiClient, FavTrack, FavAlbum } from '../api/client'
import AddToPlaylist from './AddToPlaylist.vue'

const props = defineProps<{
  api: ApiClient
}>()

const emit = defineEmits<{
  'play-track': [track: FavTrack]
}>()

type TabKey = 'favorites' | 'recent' | 'top'

const tabs = [
  { key: 'favorites' as TabKey, label: '我的收藏' },
  { key: 'recent' as TabKey, label: '最近播放' },
  { key: 'top' as TabKey, label: '最常听' },
]

const activeTab = ref<TabKey>('favorites')
const loading = ref(false)
const error = ref('')

const favTracks = ref<FavTrack[]>([])
const favAlbums = ref<FavAlbum[]>([])
const recentTracks = ref<FavTrack[]>([])
const topTracks = ref<FavTrack[]>([])

// 各 Tab 是否已加载过（避免重复请求）
const loaded = ref<Record<TabKey, boolean>>({ favorites: false, recent: false, top: false })

function formatDuration(seconds: number) {
  if (!seconds) return '--:--'
  const minutes = Math.floor(seconds / 60)
  const rest = String(seconds % 60).padStart(2, '0')
  return `${minutes}:${rest}`
}

async function loadTab(tab: TabKey) {
  if (loaded.value[tab]) return
  loading.value = true
  error.value = ''
  try {
    if (tab === 'favorites') {
      const res = await props.api.getFavorites()
      favTracks.value = res.tracks
      favAlbums.value = res.albums
    } else if (tab === 'recent') {
      const res = await props.api.getRecentlyPlayed()
      recentTracks.value = res.tracks
    } else {
      const res = await props.api.getMostPlayed()
      topTracks.value = res.tracks
    }
    loaded.value[tab] = true
  } catch (e) {
    error.value = (e as Error).message || '加载失败'
  } finally {
    loading.value = false
  }
}

function switchTab(tab: TabKey) {
  activeTab.value = tab
  void loadTab(tab)
}

onMounted(() => {
  void loadTab('favorites')
})
</script>

<style scoped>
/* ── 面板容器（与 AccountSettings/UserManagement 保持一致） ── */
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

/* ── 标签页 ── */
.fav-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--border-glass);
  padding-bottom: 0;
}

.fav-tab {
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted, rgba(255, 255, 255, 0.5));
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
  margin-bottom: -1px;
}

.fav-tab:hover {
  color: var(--text);
}

.fav-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
  font-weight: 600;
}

/* ── 加载状态 ── */
.empty-state-fav {
  display: flex;
  align-items: center;
  padding: 24px 0;
  color: var(--text-muted);
  font-size: 13px;
}

.spin-icon {
  animation: spin 1.2s linear infinite;
  color: var(--accent);
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* ── 状态消息 ── */
.status-message {
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
}

.status-error {
  background: rgba(239, 68, 68, 0.12);
  color: #f87171;
  border: 1px solid rgba(239, 68, 68, 0.25);
}

/* ── 分区 ── */
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

/* ── 专辑网格 ── */
.fav-albums-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}

.fav-album-card {
  width: 100px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.fav-album-cover {
  width: 100px;
  height: 100px;
  object-fit: cover;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
}

.placeholder-cover {
  width: 100px;
  height: 100px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.06);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  font-weight: 700;
  color: var(--text-dim);
  border: 1px solid var(--border-glass);
}

.fav-album-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin: 0;
}

.fav-album-artist {
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin: 0;
}

/* ── 曲目列表 ── */
.fav-track-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.fav-track-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 8px 10px;
  border-radius: 8px;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  color: var(--text);
  transition: background 0.15s;
}

.fav-track-row:hover {
  background: rgba(255, 255, 255, 0.05);
}

.fav-track-rank {
  font-size: 12px;
  font-weight: 600;
  width: 20px;
  text-align: right;
  flex-shrink: 0;
}

.fav-track-title {
  font-size: 14px;
  font-weight: 600;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

/* 歌名点击区域：显示手型光标 */
.fav-play-area {
  cursor: pointer;
}

.fav-play-area:focus-visible {
  outline: 2px solid var(--accent, #6ee7b7);
  outline-offset: 2px;
  border-radius: 4px;
}

.fav-track-meta {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 260px;
  flex-shrink: 0;
}

.fav-track-duration {
  font-size: 12px;
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}
</style>
