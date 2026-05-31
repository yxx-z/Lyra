<template>
  <section class="search-panel">
    <div class="panel-header">
      <div>
        <p class="eyebrow">Search</p>
        <h3>{{ query }}</h3>
      </div>
      <n-button quaternary @click="$emit('close')">Close</n-button>
    </div>

    <div v-if="loading" class="empty-state">Searching...</div>
    <div v-else-if="isEmpty" class="empty-state">No matching results.</div>
    <div v-else class="search-results">
      <div v-if="results.tracks.length">
        <h4>Tracks</h4>
        <button
          v-for="track in results.tracks"
          :key="track.id"
          class="track-row"
          type="button"
          @click="$emit('play-track', track)"
        >
          <span class="track-title">{{ track.title }}</span>
          <span class="muted">{{ track.artist }} · {{ track.album }}</span>
        </button>
      </div>

      <div v-if="results.albums.length">
        <h4>Albums</h4>
        <button
          v-for="album in results.albums"
          :key="album.id"
          class="result-row"
          type="button"
          @click="$emit('select-album', album.id)"
        >
          <span>{{ album.title }}</span>
          <span class="muted">{{ album.artist }}</span>
        </button>
      </div>

      <div v-if="results.artists.length">
        <h4>Artists</h4>
        <button
          v-for="artist in results.artists"
          :key="artist.id"
          class="result-row"
          type="button"
          @click="$emit('select-artist', artist.id)"
        >
          <span>{{ artist.name }}</span>
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton } from 'naive-ui'
import type { SearchResponse, TrackResult } from '../api/client'

const props = defineProps<{
  query: string
  results: SearchResponse
  loading: boolean
}>()

defineEmits<{
  close: []
  'play-track': [track: TrackResult]
  'select-album': [id: string]
  'select-artist': [id: string]
}>()

const isEmpty = computed(() => {
  return (
    props.results.tracks.length === 0 &&
    props.results.albums.length === 0 &&
    props.results.artists.length === 0
  )
})
</script>
