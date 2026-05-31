<template>
  <section class="artist-browser">
    <!-- 左侧：歌手列表索引 -->
    <div class="artist-list">
      <button
        v-for="artist in artists"
        :key="artist.id"
        :class="{ active: selectedArtist?.id === artist.id }"
        class="artist-row"
        type="button"
        @click="$emit('select-artist', artist.id)"
      >
        <span style="font-weight: 600;">{{ artist.name || '未知歌手' }}</span>
        <span class="badge">{{ artist.album_count }} 张专辑</span>
      </button>
    </div>

    <!-- 右侧：选中歌手的专辑陈列室 -->
    <div class="artist-albums">
      <div v-if="!selectedArtist" class="empty-state" style="height: 100%;">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 16px; color: var(--text-dim);">
          <circle cx="12" cy="12" r="10" />
          <path d="M12 8v8" />
          <path d="M8 12h8" />
        </svg>
        <p style="font-weight: 500;">选择一位歌手查看其音乐专辑</p>
      </div>

      <template v-else>
        <div style="border-bottom: 1px solid var(--border-glass); padding-bottom: 16px; margin-bottom: 24px;">
          <p class="eyebrow" style="color: var(--accent);">ARTIST RECORD SHELF</p>
          <h3>{{ selectedArtist.name }}</h3>
          <p class="muted" style="font-size: 13px; margin-top: 4px;">
            共收录该歌手的 {{ selectedArtist.albums?.length || 0 }} 部高品质实体音乐专辑
          </p>
        </div>

        <div class="compact-album-grid">
          <button
            v-for="album in selectedArtist.albums"
            :key="album.id"
            class="album-card"
            type="button"
            @click="$emit('select-album', album.id)"
          >
            <!-- 缩略圆角封面，支持快捷一键切歌 -->
            <div class="album-card-cover-wrapper">
              <img
                v-if="album.cover_url && !brokenCovers.has(album.id)"
                :src="album.cover_url"
                alt="Album cover"
                class="album-cover"
                @error="markCoverBroken(album.id)"
              />
              <div v-else class="album-cover placeholder-cover">
                {{ album.title.slice(0, 2).toUpperCase() }}
              </div>

              <!-- 一键播放蒙层 (阻断冒泡) -->
              <div class="quick-play-overlay" @click.stop="$emit('quick-play-album', album.id)">
                <button class="quick-play-btn" type="button" title="播放此专辑" style="width: 38px; height: 38px;">
                  <svg viewBox="0 0 24 24" style="width: 16px; height: 16px;">
                    <path d="M8 5v14l11-7z" />
                  </svg>
                </button>
              </div>
            </div>

            <!-- 卡片信息 -->
            <div class="album-info-container">
              <span class="album-title" :title="album.title">{{ album.title }}</span>
              <span class="album-meta">{{ album.track_count }} 首曲目</span>
            </div>
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ArtistDetail, ArtistSummary } from '../api/client'

defineProps<{
  artists: ArtistSummary[]
  selectedArtist: ArtistDetail | null
}>()

defineEmits<{
  'select-artist': [id: string]
  'select-album': [id: string]
  'quick-play-album': [id: string]
}>()

const brokenCovers = ref(new Set<string>())

function markCoverBroken(id: string) {
  brokenCovers.value = new Set([...brokenCovers.value, id])
}
</script>
