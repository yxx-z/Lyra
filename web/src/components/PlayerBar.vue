<template>
  <footer class="player-bar">
    <!-- 1. 左侧：正在播放曲目档案区 -->
    <div class="now-playing">
      <div class="player-cover-wrapper">
        <img
          v-if="player.currentTrack && player.currentTrack.coverUrl && !coverBroken"
          :src="player.currentTrack.coverUrl"
          alt="Currently playing album cover"
          class="player-cover"
          @error="coverBroken = true"
        />
        <div v-else class="player-cover placeholder-cover" style="font-size: 14px;">
          ♪
        </div>
      </div>

      <div class="now-playing-info">
        <span class="now-playing-title" :title="player.currentTrack?.title || '未在播放'">
          {{ player.currentTrack?.title || '没有什么在播放' }}
        </span>
        <span class="now-playing-artist" :title="subtitle">
          {{ subtitle }}
        </span>
      </div>
    </div>

    <!-- 2. 中间：核心控制与 Timeline 滑块 -->
    <div class="player-controls-container">
      <!-- 按钮面板 -->
      <div class="player-action-buttons">
        <!-- 随机播放 -->
        <button
          :class="{ active: player.shuffle }"
          class="player-btn"
          title="随机播放"
          type="button"
          :disabled="!player.currentTrack"
          @click="player.shuffle = !player.shuffle"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="16 3 21 3 21 8" />
            <line x1="4" y1="20" x2="21" y2="3" />
            <polyline points="21 16 21 21 16 21" />
            <path d="M15 15l6 6M4 4l5 5" />
          </svg>
        </button>

        <!-- 上一首 -->
        <button
          class="player-btn"
          title="上一首"
          type="button"
          :disabled="!player.currentTrack"
          @click="player.prev"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="19 20 9 12 19 4" />
            <line x1="5" y1="19" x2="5" y2="5" />
          </svg>
        </button>

        <!-- 播放/暂停 (核心圆形高发光按钮) -->
        <button
          :class="{ playing: player.isPlaying }"
          class="player-btn-playpause"
          :title="player.isPlaying ? '暂停' : '播放'"
          type="button"
          :disabled="!player.currentTrack"
          @click="player.togglePlay"
        >
          <svg v-if="player.isPlaying" viewBox="0 0 24 24">
            <rect x="6" y="4" width="4" height="16" rx="1" />
            <rect x="14" y="4" width="4" height="16" rx="1" />
          </svg>
          <svg v-else viewBox="0 0 24 24" style="margin-left: 2px;">
            <path d="M8 5v14l11-7z" />
          </svg>
        </button>

        <!-- 下一首 -->
        <button
          class="player-btn"
          title="下一首"
          type="button"
          :disabled="!player.currentTrack"
          @click="player.next"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="5 4 15 12 5 20" />
            <line x1="19" y1="5" x2="19" y2="19" />
          </svg>
        </button>

        <!-- 列表循环 (点击在 顺序/全循环/单曲循环 间轮转) -->
        <button
          :class="{ active: player.repeatMode !== 'none' }"
          class="player-btn"
          :title="repeatModeTitle"
          type="button"
          :disabled="!player.currentTrack"
          @click="toggleRepeatMode"
        >
          <svg v-if="player.repeatMode === 'one'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color: var(--accent);">
            <polyline points="17 1 21 5 17 9" />
            <path d="M3 11V9a4 4 0 0 1 4-4h14" />
            <polyline points="7 23 3 19 7 15" />
            <path d="M21 13v2a4 4 0 0 1-4 4H3" />
            <text x="9.5" y="14" font-size="7" font-weight="900" fill="currentColor" stroke="none">1</text>
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="17 1 21 5 17 9" />
            <path d="M3 11V9a4 4 0 0 1 4-4h14" />
            <polyline points="7 23 3 19 7 15" />
            <path d="M21 13v2a4 4 0 0 1-4 4H3" />
          </svg>
        </button>
      </div>

      <!-- 进度条滑动总成 -->
      <div class="player-timeline-wrapper">
        <span class="time-display">{{ formatTime(player.currentTime) }}</span>
        
        <!-- 手感极佳自绘 timeline -->
        <div class="slider-container">
          <div class="slider-fill" :style="{ width: progressPercent + '%' }"></div>
          <div class="slider-thumb" :style="{ left: progressPercent + '%' }"></div>
          
          <!-- 覆盖隐藏的原生 inputrange 用于滑块定位 -->
          <input
            :disabled="!player.currentTrack"
            class="hidden-range-input"
            min="0"
            :max="player.duration || 100"
            step="0.1"
            :value="player.currentTime"
            type="range"
            @input="onSeek"
          />
        </div>

        <span class="time-display">{{ formatTime(player.duration) }}</span>
      </div>
    </div>

    <!-- 3. 右侧：拟物音量控制与附加功能 -->
    <div class="player-utilities">
      <div class="volume-control-wrapper">
        <!-- 音量静音喇叭按钮 (动态展示三态图) -->
        <button
          class="volume-icon-btn"
          title="静音/取消静音"
          type="button"
          :disabled="!player.currentTrack"
          @click="player.toggleMute"
        >
          <svg v-if="player.isMuted || player.volume === 0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
            <line x1="23" y1="9" x2="17" y2="15" />
            <line x1="17" y1="9" x2="23" y2="15" />
          </svg>
          <svg v-else-if="player.volume < 0.4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
            <path d="M15.54 8.46a5 5 0 0 1 0 7.07" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
            <path d="M19.07 4.93a10 10 0 0 1 0 14.14M15.54 8.46a5 5 0 0 1 0 7.07" />
          </svg>
        </button>

        <!-- 音量滑块总成 -->
        <div class="slider-container" style="flex: 1; height: 3px;">
          <div class="slider-fill" :style="{ width: volumePercent + '%' }"></div>
          <div class="slider-thumb" :style="{ left: volumePercent + '%' }"></div>
          <input
            :disabled="!player.currentTrack"
            class="hidden-range-input"
            min="0"
            max="1"
            step="0.01"
            :value="player.isMuted ? 0 : player.volume"
            type="range"
            @input="onVolumeChange"
          />
        </div>
      </div>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { usePlayerStore } from '../stores/player'

const player = usePlayerStore()
const coverBroken = ref(false)

const subtitle = computed(() => {
  if (!player.currentTrack) return '选择曲目以开始聆听'
  return [player.currentTrack.artist, player.currentTrack.album].filter(Boolean).join(' · ')
})

const repeatModeTitle = computed(() => {
  if (player.repeatMode === 'one') return '单曲循环'
  if (player.repeatMode === 'all') return '列表循环'
  return '顺序播放'
})

const progressPercent = computed(() => {
  if (!player.duration) return 0
  return Math.min(100, (player.currentTime / player.duration) * 100)
})

const volumePercent = computed(() => {
  if (player.isMuted) return 0
  return player.volume * 100
})

watch(
  () => player.currentTrack?.trackId,
  () => {
    coverBroken.value = false
  },
)

function toggleRepeatMode() {
  if (player.repeatMode === 'none') {
    player.repeatMode = 'all'
  } else if (player.repeatMode === 'all') {
    player.repeatMode = 'one'
  } else {
    player.repeatMode = 'none'
  }
}

function formatTime(seconds: number) {
  if (isNaN(seconds) || seconds === undefined) return '00:00'
  const rounded = Math.round(seconds)
  const m = Math.floor(rounded / 60)
  const s = rounded % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

function onSeek(event: Event) {
  const target = event.target as HTMLInputElement
  player.seek(parseFloat(target.value))
}

function onVolumeChange(event: Event) {
  const target = event.target as HTMLInputElement
  player.setVolume(parseFloat(target.value))
}
</script>
