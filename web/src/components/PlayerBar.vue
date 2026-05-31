<template>
  <footer class="player-bar">
    <div class="now-playing">
      <div class="play-indicator">♪</div>
      <div>
        <strong>{{ track?.title || 'Nothing playing' }}</strong>
        <p class="muted">{{ subtitle }}</p>
      </div>
    </div>
    <audio ref="audioRef" class="audio-control" :src="track?.streamUrl" controls @loadedmetadata="playIfReady" />
  </footer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PlayerTrack } from '../api/client'

const props = defineProps<{
  track: PlayerTrack | null
}>()

const audioRef = ref<HTMLAudioElement | null>(null)

const subtitle = computed(() => {
  if (!props.track) return 'Select a track to start playback'
  return [props.track.artist, props.track.album].filter(Boolean).join(' · ')
})

watch(
  () => props.track?.streamUrl,
  () => {
    void playIfReady()
  },
)

async function playIfReady() {
  if (!audioRef.value || !props.track) return
  try {
    await audioRef.value.play()
  } catch {
    // Browsers may block autoplay until the user interacts with controls.
  }
}
</script>
