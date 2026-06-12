<template>
  <section class="search-panel">
    <!-- 搜索顶部栏 -->
    <div class="panel-header" style="border-bottom: 1px solid var(--border-glass); padding-bottom: 16px; margin-bottom: 24px;">
      <div>
        <p class="eyebrow" style="color: var(--accent-cyan);">SEARCH SEARCH RESULTS</p>
        <h3 style="font-size: 24px; font-weight: 800;">检索关键字：&ldquo;{{ query }}&rdquo;</h3>
      </div>
      <button class="topbar-btn secondary" type="button" @click="$emit('close')">
        关闭检索
      </button>
    </div>

    <!-- 加载中与空状态 -->
    <div v-if="loading" class="empty-state">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="animate-spin" style="margin-bottom: 12px; color: var(--accent);">
        <line x1="12" y1="2" x2="12" y2="6" />
        <line x1="12" y1="18" x2="12" y2="22" />
        <line x1="4.93" y1="4.93" x2="7.76" y2="7.76" />
        <line x1="16.24" y1="16.24" x2="19.07" y2="19.07" />
        <line x1="2" y1="12" x2="6" y2="12" />
        <line x1="18" y1="12" x2="22" y2="12" />
        <line x1="4.93" y1="19.07" x2="7.76" y2="16.24" />
        <line x1="16.24" y1="7.76" x2="19.07" y2="4.93" />
      </svg>
      <p class="muted">正在深度检索库中资源...</p>
    </div>

    <div v-else-if="isEmpty" class="empty-state">
      <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 12px; color: var(--text-dim);">
        <circle cx="11" cy="11" r="8" />
        <line x1="21" y1="21" x2="16.65" y2="16.65" />
      </svg>
      <p class="muted" style="font-weight: 500;">未检索到与 &ldquo;{{ query }}&rdquo; 相关的单曲、专辑或歌手</p>
    </div>

    <!-- 检索成果 -->
    <div v-else class="search-results">
      <!-- 1. 单曲模块 -->
      <div v-if="localTracks.length">
        <p class="eyebrow" style="margin-bottom: 12px; font-weight: 700;">TRACKS / 单曲</p>
        <!-- 使用 div 包装行，避免 button 嵌套（行内含 heart-btn 等交互按钮） -->
        <div style="display: flex; flex-direction: column; gap: 8px;">
          <div
            v-for="track in localTracks"
            :key="track.id"
            class="result-row"
          >
            <!-- 播放区域 -->
            <div style="display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1; cursor: pointer;" role="button" tabindex="0" @click="$emit('play-track', track)" @keydown.enter="$emit('play-track', track)" @keydown.space.prevent="$emit('play-track', track)">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px; color: var(--accent); flex-shrink: 0;">
                <path d="M9 18V5l12-2v13" />
                <circle cx="6" cy="18" r="3" />
                <circle cx="18" cy="16" r="3" />
              </svg>
              <span style="font-weight: 600; font-size: 14px;" class="track-title" :title="track.title">
                {{ track.title }}
              </span>
            </div>
            <span class="muted" style="font-size: 13px; margin-left: 16px; text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 280px;">
              {{ track.artist }} &middot; {{ track.album }}
            </span>
            <!-- 曲目红心 + 添加到歌单 + 下一首播放 -->
            <button
              class="heart-btn"
              :class="{ starred: track.starred }"
              type="button"
              :title="track.starred ? '取消收藏' : '收藏'"
              @click.stop="toggleTrackStar(track)"
            >{{ track.starred ? '♥' : '♡' }}</button>
            <AddToPlaylist :api="api" :track-id="track.id" />
            <PlayNextButton
              :track-id="track.id"
              :title="track.title"
              :artist="track.artist"
              :album="track.album"
              :stream-url="track.stream_url"
            />
          </div>
        </div>
      </div>

      <!-- 2. 专辑模块 -->
      <div v-if="localAlbums.length" style="margin-top: 12px;">
        <p class="eyebrow" style="margin-bottom: 12px; font-weight: 700;">ALBUMS / 专辑</p>
        <div style="display: flex; flex-direction: column; gap: 8px;">
          <button
            v-for="album in localAlbums"
            :key="album.id"
            class="result-row"
            type="button"
            @click="$emit('select-album', album.id)"
          >
            <div style="display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1;">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px; color: var(--accent-cyan); flex-shrink: 0;">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
                <circle cx="12" cy="12" r="3" />
              </svg>
              <span style="font-weight: 600; font-size: 14px;" class="track-title" :title="album.title">
                {{ album.title }}
              </span>
            </div>
            <span class="muted" style="font-size: 13px; margin-left: 16px; text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 200px;">
              {{ album.artist || '未知艺术家' }}
            </span>
            <!-- 专辑红心 -->
            <button
              class="heart-btn"
              :class="{ starred: album.starred }"
              type="button"
              :title="album.starred ? '取消收藏专辑' : '收藏专辑'"
              @click.stop="toggleAlbumStar(album)"
            >{{ album.starred ? '♥' : '♡' }}</button>
          </button>
        </div>
      </div>

      <!-- 3. 歌手模块 -->
      <div v-if="results.artists.length" style="margin-top: 12px;">
        <p class="eyebrow" style="margin-bottom: 12px; font-weight: 700;">ARTISTS / 歌手</p>
        <div style="display: flex; flex-direction: column; gap: 8px;">
          <button
            v-for="artist in results.artists"
            :key="artist.id"
            class="result-row"
            type="button"
            @click="$emit('select-artist', artist.id)"
          >
            <div style="display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1;">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px; color: var(--accent); flex-shrink: 0;">
                <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                <circle cx="12" cy="7" r="4" />
              </svg>
              <span style="font-weight: 600; font-size: 14px;">{{ artist.name || '未知歌手' }}</span>
            </div>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 14px; height: 14px; color: var(--text-dim);">
              <polyline points="9 18 15 12 9 6" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ApiClient, SearchResponse, TrackResult, AlbumResult } from '../api/client'
import AddToPlaylist from './AddToPlaylist.vue'
import PlayNextButton from './PlayNextButton.vue'

const props = defineProps<{
  query: string
  results: SearchResponse
  loading: boolean
  api: ApiClient
}>()

defineEmits<{
  close: []
  'play-track': [track: TrackResult]
  'select-album': [id: string]
  'select-artist': [id: string]
}>()

// 本地可变列表，用于即时翻转 starred 状态
const localTracks = ref<TrackResult[]>([])
const localAlbums = ref<AlbumResult[]>([])

watch(
  () => props.results,
  (r) => {
    localTracks.value = r.tracks.map(t => ({ ...t }))
    localAlbums.value = r.albums.map(a => ({ ...a }))
  },
  { immediate: true, deep: true },
)

// 曲目红心切换
async function toggleTrackStar(track: TrackResult) {
  const next = !track.starred
  track.starred = next
  try {
    if (next) {
      await props.api.star('song', track.id)
    } else {
      await props.api.unstar('song', track.id)
    }
  } catch {
    track.starred = !next
  }
}

// 专辑红心切换
async function toggleAlbumStar(album: AlbumResult) {
  const next = !album.starred
  album.starred = next
  try {
    if (next) {
      await props.api.star('album', album.id)
    } else {
      await props.api.unstar('album', album.id)
    }
  } catch {
    album.starred = !next
  }
}

const isEmpty = computed(() => {
  return (
    props.results.tracks.length === 0 &&
    props.results.albums.length === 0 &&
    props.results.artists.length === 0
  )
})
</script>

<style scoped>
.animate-spin {
  animation: spin 1.2s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* 红心收藏按钮 */
.heart-btn {
  background: none;
  border: none;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  color: var(--text-dim, rgba(255, 255, 255, 0.3));
  padding: 2px 6px;
  border-radius: 4px;
  transition: color 0.15s;
  flex-shrink: 0;
  margin-left: 8px;
}

.heart-btn:hover {
  color: var(--danger, #f87171);
}

.heart-btn.starred {
  color: var(--danger, #f87171);
}
</style>
