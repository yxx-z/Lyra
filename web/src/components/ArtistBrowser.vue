<template>
  <section class="artist-browser">
    <div class="artist-list">
      <button
        v-for="artist in artists"
        :key="artist.id"
        class="artist-row"
        :class="{ active: selectedArtist?.id === artist.id }"
        type="button"
        @click="$emit('select-artist', artist.id)"
      >
        <span>{{ artist.name }}</span>
        <span class="muted">{{ artist.album_count }}</span>
      </button>
    </div>

    <div class="artist-albums">
      <div v-if="!selectedArtist" class="empty-state">Select an artist.</div>
      <template v-else>
        <h3>{{ selectedArtist.name }}</h3>
        <div class="compact-album-grid">
          <button
            v-for="album in selectedArtist.albums"
            :key="album.id"
            class="album-card compact"
            type="button"
            @click="$emit('select-album', album.id)"
          >
            <img
              v-if="album.cover_url && !brokenCovers.has(album.id)"
              :src="album.cover_url"
              alt=""
              class="album-cover"
              @error="markCoverBroken(album.id)"
            />
            <div v-else class="album-cover placeholder-cover">
              {{ album.title.slice(0, 2).toUpperCase() }}
            </div>
            <span class="album-title">{{ album.title }}</span>
            <span class="album-meta">{{ album.track_count }} tracks</span>
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
}>()

const brokenCovers = ref(new Set<string>())

function markCoverBroken(id: string) {
  brokenCovers.value = new Set([...brokenCovers.value, id])
}
</script>
