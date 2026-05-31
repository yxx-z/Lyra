<template>
  <aside class="detail-panel">
    <div v-if="!album" class="empty-state">Select an album to see tracks.</div>
    <template v-else>
      <div class="detail-header">
        <img
          v-if="album.cover_url && !coverBroken"
          :src="album.cover_url"
          alt=""
          class="detail-cover"
          @error="coverBroken = true"
        />
        <div v-else class="detail-cover placeholder-cover">
          {{ album.title.slice(0, 2).toUpperCase() }}
        </div>
        <div>
          <p class="eyebrow">{{ album.year || 'Album' }}</p>
          <h3>{{ album.title }}</h3>
          <p class="muted">{{ album.artist || 'Unknown artist' }}</p>
        </div>
      </div>

      <div class="track-list">
        <button
          v-for="track in album.tracks"
          :key="track.id"
          class="track-row"
          type="button"
          @click="$emit('play', track)"
        >
          <span class="track-number">{{ track.track_number || '-' }}</span>
          <span class="track-title">{{ track.title }}</span>
          <span class="track-duration">{{ formatDuration(track.duration) }}</span>
        </button>
      </div>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { AlbumDetail, TrackSummary } from '../api/client'

const props = defineProps<{
  album: AlbumDetail | null
}>()

defineEmits<{
  play: [track: TrackSummary]
}>()

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
