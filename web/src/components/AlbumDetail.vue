<template>
  <aside class="detail-panel">
    <!-- 流动渐变高斯模糊背景层 (经典 Apple Music 虚化氛围) -->
    <div
      v-if="album && album.cover_url && !coverBroken"
      class="detail-backdrop"
      :style="{ backgroundImage: `url(${coverSrc})` }"
    ></div>

    <!-- 空数据展示 -->
    <div v-if="!album" class="empty-state">
      <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 16px; color: var(--text-dim);">
        <path d="M9 18V5l12-2v13" />
        <circle cx="6" cy="18" r="3" />
        <circle cx="18" cy="16" r="3" />
      </svg>
      <p style="font-weight: 500;">选择一张专辑查看曲目详情</p>
    </div>

    <!-- 详情内容展现 -->
    <template v-else>
      <div class="detail-header">
        <img
          v-if="album.cover_url && !coverBroken"
          :src="coverSrc"
          alt="Album cover artwork"
          class="detail-cover"
          @error="coverBroken = true"
        />
        <div v-else class="detail-cover placeholder-cover">
          {{ album.title.slice(0, 2).toUpperCase() }}
        </div>
        
        <div class="detail-title-info">
          <p class="eyebrow" style="color: var(--accent);">
            {{ album.release_date || album.year || '未知年份' }}<span v-if="album.genre"> · {{ album.genre }}</span> · ALBUM
          </p>
          <h3>{{ album.title }}</h3>
          <p class="muted" style="margin-top: 4px;">
            艺术家：<span class="artist-link">{{ album.artist || '未知艺术家' }}</span>
          </p>
          <p class="muted" style="font-size: 12px; margin-top: 2px;">
            共收录 {{ album.tracks?.length || 0 }} 首高品质曲目
          </p>
          <div style="display: flex; align-items: center; gap: 10px; margin-top: 12px;">
            <button
              class="custom-btn-primary"
              style="width: auto; padding: 8px 18px; font-size: 13px; display: inline-flex; align-items: center; gap: 8px;"
              type="button"
              :disabled="scraping"
              @click="handleScrape"
            >
              <span v-if="scraping" class="loading-spinner" aria-label="刮削中"></span>
              <span>{{ scraping ? '刮削中…' : '🔍 刮削元数据' }}</span>
            </button>
            <!-- 专辑红心收藏按钮 -->
            <button
              class="heart-btn album-heart"
              :class="{ starred: albumStarred }"
              type="button"
              :title="albumStarred ? '取消收藏专辑' : '收藏专辑'"
              @click="toggleAlbumStar"
            >{{ albumStarred ? '♥' : '♡' }}</button>
          </div>
          <p v-if="scrapeMessage" class="muted" style="font-size: 12px; margin-top: 8px;">{{ scrapeMessage }}</p>
        </div>
      </div>

      <!-- 精美曲目列表 -->
      <div class="track-list">
        <button
          v-for="track in localTracks"
          :key="track.id"
          :class="{ active: playerStore.currentTrack?.trackId === track.id }"
          class="track-row"
          type="button"
          @click="$emit('play', track)"
        >
          <!-- 左侧：序号/跳动的均衡器 -->
          <span class="track-number">
            <div
              v-if="playerStore.currentTrack?.trackId === track.id && playerStore.isPlaying"
              class="equalizer-icon"
              title="正在播放"
            >
              <span class="eq-bar"></span>
              <span class="eq-bar"></span>
              <span class="eq-bar"></span>
            </div>
            <span v-else>{{ track.track_number || '-' }}</span>
          </span>

          <!-- 中间：歌名与音质信息 -->
          <span class="track-title" :title="track.title">
            {{ track.title }}
            <span
              v-if="track.format || track.bitrate"
              class="muted"
              style="font-size: 10px; font-weight: normal; margin-left: 6px; padding: 1px 4px; border-radius: 4px; background: rgba(255,255,255,0.04); border: 1px solid rgba(255,255,255,0.03);"
            >
              {{ track.format.toUpperCase() }} {{ Math.round(track.bitrate / 1000) }}K
            </span>
          </span>

          <!-- 右侧：红心 + 时长 -->
          <button
            class="heart-btn"
            :class="{ starred: track.starred }"
            type="button"
            :title="track.starred ? '取消收藏' : '收藏'"
            @click.stop="toggleTrackStar(track)"
          >{{ track.starred ? '♥' : '♡' }}</button>
          <span class="track-duration">{{ formatDuration(track.duration) }}</span>
        </button>
      </div>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { usePlayerStore } from '../stores/player'
import { ApiError, type ApiClient, type AlbumDetail, type TrackSummary } from '../api/client'

const props = defineProps<{
  album: AlbumDetail | null
  api: ApiClient
}>()

const emit = defineEmits<{
  play: [track: TrackSummary]
  refresh: []
}>()

// 引入全局播放 Store，监测当前歌曲和播放动画
const playerStore = usePlayerStore()

function formatDuration(seconds: number) {
  if (!seconds) return '--:--'
  const minutes = Math.floor(seconds / 60)
  const rest = String(seconds % 60).padStart(2, '0')
  return `${minutes}:${rest}`
}

const coverBroken = ref(false)
const scraping = ref(false)
const scrapeMessage = ref('')
const coverVersion = ref(0)

// 本地可变曲目列表（保持 starred 状态可即时翻转）
const localTracks = ref<TrackSummary[]>([])

// 专辑级别收藏状态
const albumStarred = ref(false)

// 带版本号的封面 URL：刮削后 bump 版本强制浏览器重取（同 URL 否则命中缓存）
const coverSrc = computed(() =>
  props.album ? `${props.album.cover_url}?v=${coverVersion.value}` : '',
)

async function handleScrape() {
  if (!props.album || scraping.value) return
  scraping.value = true
  scrapeMessage.value = ''
  try {
    const res = await props.api.scrapeAlbum(props.album.id)
    if (res.status === 'done') {
      emit('refresh')
      coverVersion.value++
      coverBroken.value = false
      scrapeMessage.value = '已更新'
    } else {
      scrapeMessage.value = '未匹配到专辑'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      scrapeMessage.value = '未匹配到专辑'
    } else {
      scrapeMessage.value = '刮削失败，请重试'
    }
  } finally {
    scraping.value = false
  }
}

// 专辑红心切换
async function toggleAlbumStar() {
  if (!props.album) return
  const next = !albumStarred.value
  albumStarred.value = next
  try {
    if (next) {
      await props.api.star('album', props.album.id)
    } else {
      await props.api.unstar('album', props.album.id)
    }
  } catch {
    // 失败时回滚
    albumStarred.value = !next
  }
}

// 曲目红心切换
async function toggleTrackStar(track: TrackSummary) {
  const next = !track.starred
  track.starred = next
  try {
    if (next) {
      await props.api.star('song', track.id)
    } else {
      await props.api.unstar('song', track.id)
    }
  } catch {
    // 失败时回滚
    track.starred = !next
  }
}

watch(
  () => props.album?.id,
  () => {
    coverBroken.value = false
    scrapeMessage.value = ''
    // 同步本地曲目列表与专辑收藏状态
    localTracks.value = props.album ? props.album.tracks.map(t => ({ ...t })) : []
    albumStarred.value = props.album?.starred ?? false
  },
  { immediate: true },
)
</script>

<style scoped>
/* 红心收藏按钮 */
.heart-btn {
  background: none;
  border: none;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  color: var(--text-dim, rgba(255, 255, 255, 0.3));
  padding: 2px 4px;
  border-radius: 4px;
  transition: color 0.15s, transform 0.1s;
  flex-shrink: 0;
}

.heart-btn:hover {
  color: var(--danger, #f87171);
}

.heart-btn.starred {
  color: var(--danger, #f87171);
}

.heart-btn.album-heart {
  font-size: 20px;
  padding: 4px 6px;
}
</style>
