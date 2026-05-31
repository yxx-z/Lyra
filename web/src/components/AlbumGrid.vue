<template>
  <section class="album-grid-panel">
    <!-- 极富设计感的骨架屏或加载提示 -->
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
      <p class="muted" style="font-weight: 500;">正在加载音乐库专辑...</p>
    </div>

    <!-- 幽灵底色空状态 -->
    <div v-else-if="albums.length === 0" class="empty-state">
      <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 16px; color: var(--text-dim);">
        <circle cx="12" cy="12" r="10" />
        <path d="M8 12h8" />
      </svg>
      <p style="font-weight: 600; font-size: 15px; margin-bottom: 4px;">您的音乐库是空的</p>
      <p class="muted" style="font-size: 13px;">请前往系统扫描功能导入您的本地音乐资源。</p>
    </div>

    <!-- 专辑网格主体 -->
    <div v-else class="album-grid">
      <button
        v-for="album in albums"
        :key="album.id"
        :class="{ active: selectedAlbumId === album.id }"
        class="album-card"
        type="button"
        @click="$emit('select', album.id)"
      >
        <!-- 封面图容器，处理悬停动画与一键快捷播放 -->
        <div class="album-card-cover-wrapper">
          <img
            v-if="album.cover_url && !brokenCovers.has(album.id)"
            :src="album.cover_url"
            alt="Album cover"
            class="album-cover"
            @error="markCoverBroken(album.id)"
          />
          <div v-else class="album-cover placeholder-cover">
            {{ initials(album.title) }}
          </div>

          <!-- 一键播放蒙层 (拦截点击防止触发 select) -->
          <div class="quick-play-overlay" @click.stop="$emit('quick-play', album.id)">
            <button class="quick-play-btn" type="button" title="一键播放整张专辑">
              <svg viewBox="0 0 24 24">
                <path d="M8 5v14l11-7z" />
              </svg>
            </button>
          </div>
        </div>

        <!-- 专辑信息 -->
        <div class="album-info-container">
          <span class="album-title" :title="album.title">{{ album.title }}</span>
          <span class="album-meta" :title="album.artist">
            {{ album.artist || '未知艺术家' }} · {{ album.track_count }}首
          </span>
        </div>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { AlbumSummary } from '../api/client'

defineProps<{
  albums: AlbumSummary[]
  selectedAlbumId: string
  loading: boolean
}>()

defineEmits<{
  select: [id: string]
  'quick-play': [id: string]
}>()

function initials(value: string) {
  return value.trim().slice(0, 2).toUpperCase() || 'LY'
}

const brokenCovers = ref(new Set<string>())

function markCoverBroken(id: string) {
  brokenCovers.value = new Set([...brokenCovers.value, id])
}
</script>

<style scoped>
.animate-spin {
  animation: spin 1.2s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
