<template>
  <div class="play-next-btn-wrap" @click.stop>
    <button class="pnb-btn" type="button" title="下一首播放" @click.stop="handleClick">
      <!-- 「下一首播放」图标：竖线 + 播放三角 -->
      <svg viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg" class="pnb-icon" aria-hidden="true">
        <!-- 左侧竖线 -->
        <rect x="2" y="2" width="2" height="12" rx="1" />
        <!-- 右侧向右三角 -->
        <polygon points="6,3 14,8 6,13" />
      </svg>
    </button>
    <span v-if="tip" class="pnb-tip">已设为下一首</span>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { usePlayerStore } from '../stores/player'

const props = defineProps<{
  trackId: string
  title: string
  artist?: string
  album?: string
  streamUrl: string
  coverUrl?: string
}>()

const tip = ref(false)

function handleClick() {
  const player = usePlayerStore()
  player.playNext({
    trackId: props.trackId,
    title: props.title,
    artist: props.artist,
    album: props.album,
    streamUrl: props.streamUrl,
    coverUrl: props.coverUrl,
  })
  tip.value = true
  setTimeout(() => { tip.value = false }, 1500)
}
</script>

<style scoped>
.play-next-btn-wrap {
  position: relative;
  display: inline-flex;
  align-items: center;
}

.pnb-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-dim, rgba(255, 255, 255, 0.4));
  padding: 2px 4px;
  border-radius: 4px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s;
  line-height: 1;
}

.pnb-btn:hover {
  color: var(--accent, #6ee7b7);
}

.pnb-icon {
  width: 14px;
  height: 14px;
  display: block;
}

.pnb-tip {
  position: absolute;
  right: 0;
  top: 100%;
  margin-top: 4px;
  font-size: 11px;
  color: var(--success, #30a46c);
  white-space: nowrap;
  pointer-events: none;
  z-index: 10;
}
</style>
