<template>
  <aside class="detail-panel">
    <!-- 流动渐变高斯模糊背景层 (经典 Apple Music 虚化氛围) -->
    <div
      v-if="album && album.cover_url && !coverBroken"
      class="detail-backdrop"
      :style="{ backgroundImage: `url(${album.cover_url})` }"
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
          :src="album.cover_url"
          alt="Album cover artwork"
          class="detail-cover"
          @error="coverBroken = true"
        />
        <div v-else class="detail-cover placeholder-cover">
          {{ album.title.slice(0, 2).toUpperCase() }}
        </div>
        
        <div class="detail-title-info">
          <p class="eyebrow" style="color: var(--accent);">{{ album.year || '未知年份' }} · ALBUM</p>
          <h3>{{ album.title }}</h3>
          <p class="muted" style="margin-top: 4px;">
            艺术家：<span class="artist-link">{{ album.artist || '未知艺术家' }}</span>
          </p>
          <p class="muted" style="font-size: 12px; margin-top: 2px;">
            共收录 {{ album.tracks?.length || 0 }} 首高品质曲目
          </p>
        </div>
      </div>

      <!-- 精美曲目列表 -->
      <div class="track-list">
        <button
          v-for="track in album.tracks"
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

          <!-- 右侧：时长 -->
          <span class="track-duration">{{ formatDuration(track.duration) }}</span>
        </button>
      </div>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { usePlayerStore } from '../stores/player'
import type { AlbumDetail, TrackSummary } from '../api/client'

const props = defineProps<{
  album: AlbumDetail | null
}>()

defineEmits<{
  play: [track: TrackSummary]
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

watch(
  () => props.album?.id,
  () => {
    coverBroken.value = false
  },
)
</script>
