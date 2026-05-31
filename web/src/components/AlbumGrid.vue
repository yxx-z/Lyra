<template>
  <section class="album-grid-panel">
    <div v-if="loading" class="empty-state">Loading albums...</div>
    <div v-else-if="albums.length === 0" class="empty-state">
      No albums yet. Scan your music library first.
    </div>
    <div v-else class="album-grid">
      <button
        v-for="album in albums"
        :key="album.id"
        class="album-card"
        :class="{ active: selectedAlbumId === album.id }"
        type="button"
        @click="$emit('select', album.id)"
      >
        <img
          v-if="album.cover_url"
          :src="album.cover_url"
          alt=""
          class="album-cover"
          @error="hideBrokenImage"
        />
        <div v-else class="album-cover placeholder-cover">{{ initials(album.title) }}</div>
        <span class="album-title">{{ album.title }}</span>
        <span class="album-meta">{{ album.artist || 'Unknown artist' }} · {{ album.track_count }} tracks</span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { AlbumSummary } from '../api/client'

defineProps<{
  albums: AlbumSummary[]
  selectedAlbumId: string
  loading: boolean
}>()

defineEmits<{
  select: [id: string]
}>()

function initials(value: string) {
  return value.trim().slice(0, 2).toUpperCase() || 'LY'
}

function hideBrokenImage(event: Event) {
  const image = event.target as HTMLImageElement
  image.style.display = 'none'
}
</script>
