<template>
  <div class="lyrics-panel-wrapper" :class="{ 'is-open': isOpen }">
    <!-- 1. Apple Music 式流动封面高斯模糊背景层 -->
    <div
      v-if="playerStore.currentTrack && playerStore.currentTrack.coverUrl && !coverBroken"
      class="lyrics-backdrop"
      :style="{ backgroundImage: `url(${playerStore.currentTrack.coverUrl})` }"
    ></div>

    <!-- 2. 右上角精美关闭按钮 -->
    <button class="lyrics-close-btn" type="button" title="收折歌词" @click="$emit('close')">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <line x1="18" y1="6" x2="6" y2="18" />
        <line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>

    <!-- 3. 主体分栏交互大盘 -->
    <div v-if="playerStore.currentTrack" class="lyrics-container">
      <!-- 左栏：胶片封面呼吸展区 -->
      <div class="lyrics-cover-col">
        <img
          v-if="playerStore.currentTrack.coverUrl && !coverBroken"
          :src="playerStore.currentTrack.coverUrl"
          alt="Large Album Artwork"
          class="lyrics-large-cover"
          :class="{ 'is-playing': playerStore.isPlaying }"
          @error="coverBroken = true"
        />
        <div v-else class="lyrics-large-cover placeholder-cover" style="font-size: 40px; display: grid; place-items: center; background: linear-gradient(135deg, #1f2937, #111827);">
          ♪
        </div>

        <div class="lyrics-song-info">
          <h3 class="lyrics-song-title">{{ playerStore.currentTrack.title }}</h3>
          <p class="lyrics-song-artist">{{ subtitle }}</p>
          <button
            class="lyrics-heart-btn"
            :class="{ starred }"
            type="button"
            :title="starred ? '取消收藏' : '收藏'"
            @click="toggleStar"
          >{{ starred ? '♥' : '♡' }} {{ starred ? '已收藏' : '收藏' }}</button>
        </div>
      </div>

      <!-- 右栏：滚动歌词主滑槽 -->
      <div class="lyrics-list-col">
        <!-- A. 加载中状态 -->
        <div v-if="isLoading" class="empty-state" style="border: 0; background: transparent; height: 100%;">
          <svg class="animate-spin" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 12px; color: var(--accent);">
            <line x1="12" y1="2" x2="12" y2="6" />
            <line x1="12" y1="18" x2="12" y2="22" />
            <line x1="4.93" y1="4.93" x2="7.76" y2="7.76" />
            <line x1="16.24" y1="16.24" x2="19.07" y2="19.07" />
          </svg>
          <p class="muted">正在调取与解析歌词...</p>
        </div>

        <!-- B. 无歌词/纯器乐兜底容错 -->
        <div v-else-if="error === 'no_lyrics' || lrcLines.length === 0" class="empty-state" style="border: 0; background: transparent; height: 100%;">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="width: 44px; height: 44px; margin-bottom: 16px; color: var(--text-dim); opacity: 0.6;">
            <path d="M9 18V5l12-2v13" />
            <circle cx="6" cy="18" r="3" />
            <circle cx="18" cy="16" r="3" />
          </svg>
          <p style="font-size: 16px; font-weight: 600; margin-bottom: 4px; color: var(--text);">纯器乐演奏，请闭上双眼静静聆听</p>
          <p class="muted" style="font-size: 13px;">本首音乐目前尚未同步到滚动歌词文本数据</p>
          <button
            class="custom-btn-primary"
            style="width: auto; padding: 10px 22px; font-size: 14px; margin-top: 18px; display: inline-flex; align-items: center; gap: 8px;"
            type="button"
            :disabled="scraping"
            @click="handleScrape"
          >
            <span v-if="scraping" class="loading-spinner" aria-label="刮削中"></span>
            <span>{{ scraping ? '正在获取歌词…' : '🔍 获取歌词' }}</span>
          </button>
          <p v-if="scrapeMessage" class="muted" style="font-size: 12px; margin-top: 10px;">{{ scrapeMessage }}</p>
        </div>

        <!-- C. 标准滚动歌词面板 -->
        <div v-else ref="scrollerRef" class="lyrics-scroller" @wheel="markUserScroll" @touchmove="markUserScroll">
          <div v-if="!synced" style="display: flex; flex-direction: column; align-items: center; gap: 6px; padding: 8px 0 16px;">
            <button
              class="custom-btn-primary"
              style="width: auto; padding: 8px 18px; font-size: 13px; display: inline-flex; align-items: center; gap: 8px;"
              type="button"
              :disabled="upgrading"
              @click="handleUpgrade"
            >
              <span v-if="upgrading" class="loading-spinner" aria-label="升级中"></span>
              <span>{{ upgrading ? '升级中…' : '⏱ 升级为同步歌词' }}</span>
            </button>
            <span v-if="upgradeMessage" class="muted" style="font-size: 12px;">{{ upgradeMessage }}</span>
          </div>
          <button
            v-for="(line, idx) in lrcLines"
            :key="idx"
            :class="{ active: idx === currentLineIndex, 'is-static': !synced }"
            class="lyric-line"
            type="button"
            @click="seekToLine(line.time)"
          >
            {{ line.text || '• • •' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, computed, nextTick } from 'vue'
import { usePlayerStore } from '../stores/player'
import { ApiError, type ApiClient } from '../api/client'

// 约定 LRC 解析的单行歌词数据接口
interface LyricLine {
  time: number
  text: string
}

const props = defineProps<{
  isOpen: boolean
  api: ApiClient // 共享父组件配置好的 Api 实例
}>()

defineEmits<{
  close: []
}>()

const playerStore = usePlayerStore()
const coverBroken = ref(false)
const lrcLines = ref<LyricLine[]>([])
const isLoading = ref(false)
const error = ref<string | null>(null)
const currentLineIndex = ref(-1)
const scraping = ref(false)
const scrapeMessage = ref('')
const upgrading = ref(false)
const upgradeMessage = ref('')
// 是否带时间轴（可滚动同步 + 点击跳转）；纯文本歌词为 false，静态展示
const synced = ref(false)
// 歌词滚动容器引用（仅滚动它本身，避免 scrollIntoView 连带滚动祖先/window 把关闭按钮顶走）
const scrollerRef = ref<HTMLElement | null>(null)
// 当前歌曲是否已收藏
const starred = ref(false)
// 用户正在手动滚动歌词：期间暂停自动对焦，方便靠歌词找进度
const userScrolling = ref(false)
let userScrollTimer: ReturnType<typeof setTimeout> | null = null

// 标记用户手动滚动；4 秒内不自动把歌词拉回当前演唱句
function markUserScroll() {
  userScrolling.value = true
  if (userScrollTimer) clearTimeout(userScrollTimer)
  userScrollTimer = setTimeout(() => {
    userScrolling.value = false
  }, 4000)
}

// 拉取当前歌曲收藏状态
async function loadStarStatus() {
  const track = playerStore.currentTrack
  if (!track) {
    starred.value = false
    return
  }
  try {
    starred.value = (await props.api.getStarStatus('song', track.trackId)).starred
  } catch {
    starred.value = false
  }
}

// 歌词界面红心：收藏/取消当前歌曲（乐观更新，失败回滚）
async function toggleStar() {
  const track = playerStore.currentTrack
  if (!track) return
  const next = !starred.value
  starred.value = next
  try {
    if (next) {
      await props.api.star('song', track.trackId)
    } else {
      await props.api.unstar('song', track.trackId)
    }
  } catch {
    starred.value = !next
  }
}

const subtitle = computed(() => {
  if (!playerStore.currentTrack) return ''
  return [playerStore.currentTrack.artist, playerStore.currentTrack.album].filter(Boolean).join(' · ')
})

// LRC 歌词解析高精度正则算法 (支持一行多时间归集)
// 副作用：根据是否解析出时间轴，设置 synced（决定能否滚动同步/点击跳转）
function parseLrc(lrcText: string): LyricLine[] {
  if (!lrcText) {
    synced.value = false
    return []
  }
  const lines = lrcText.split('\n')
  const result: LyricLine[] = []
  const timeRegex = /\[(\d+):(\d+)(?:\.(\d+))?\]/g

  for (const line of lines) {
    const text = line.replace(timeRegex, '').trim()
    timeRegex.lastIndex = 0
    let match
    while ((match = timeRegex.exec(line)) !== null) {
      const min = parseInt(match[1], 10)
      const sec = parseInt(match[2], 10)
      const ms = match[3] ? parseInt(match[3].padEnd(3, '0').slice(0, 3), 10) : 0
      const totalSeconds = min * 60 + sec + ms / 1000
      result.push({ time: totalSeconds, text })
    }
  }

  if (result.length > 0) {
    synced.value = true
    // 严格按时间点升序排序，保证对焦不跳跃
    return result.sort((a, b) => a.time - b.time)
  }

  // 纯文本回退（如内嵌标签歌词，无时间轴）：逐行静态展示
  synced.value = false
  return lines
    .map((l) => l.trim())
    .filter((l) => l.length > 0)
    .map((l) => ({ time: -1, text: l }))
}

// 动态拉取歌词
async function loadLyrics() {
  const track = playerStore.currentTrack
  if (!track) {
    lrcLines.value = []
    return
  }

  isLoading.value = true
  error.value = null
  lrcLines.value = []
  coverBroken.value = false
  scrapeMessage.value = ''
  upgradeMessage.value = ''
  void loadStarStatus()

  try {
    const res = await props.api.getLyrics(track.trackId)
    if (res.has_lrc && res.lrc_content) {
      lrcLines.value = parseLrc(res.lrc_content)
    } else {
      error.value = 'no_lyrics'
    }
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) {
      console.warn('Lyrics fetch failed. Gracefully showing no-lyrics layout: ', err)
    }
    error.value = 'no_lyrics'
  } finally {
    isLoading.value = false
    currentLineIndex.value = -1
    syncLyricsIndex()
  }
}

// 触发服务端刮削歌词，成功后重载
async function handleScrape() {
  const track = playerStore.currentTrack
  if (!track || scraping.value) return
  scraping.value = true
  scrapeMessage.value = ''
  try {
    const res = await props.api.scrapeTrack(track.trackId)
    if (res.status === 'done' || res.status === 'skipped') {
      await loadLyrics()
      if (lrcLines.value.length === 0) {
        scrapeMessage.value = '未找到可显示的歌词'
      }
    } else {
      scrapeMessage.value = '未找到歌词'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      scrapeMessage.value = '未找到歌词'
    } else {
      scrapeMessage.value = '获取失败，请稍后重试'
    }
  } finally {
    scraping.value = false
  }
}

// 纯文本歌词升级为同步歌词
async function handleUpgrade() {
  const track = playerStore.currentTrack
  if (!track || upgrading.value) return
  upgrading.value = true
  upgradeMessage.value = ''
  try {
    const res = await props.api.upgradeLyrics(track.trackId)
    if (res.status === 'upgraded') {
      await loadLyrics()
      upgradeMessage.value = '已升级为同步歌词'
    } else {
      upgradeMessage.value = '未找到同步版本'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      upgradeMessage.value = '未找到同步版本'
    } else {
      upgradeMessage.value = '升级失败，请重试'
    }
  } finally {
    upgrading.value = false
  }
}

// 卡拉OK级时间轴查找锁定
function syncLyricsIndex() {
  if (!synced.value || lrcLines.value.length === 0) return
  const time = playerStore.currentTime
  let index = -1

  for (let i = 0; i < lrcLines.value.length; i++) {
    if (time >= lrcLines.value[i].time) {
      index = i
    } else {
      break
    }
  }

  const nextIndex = index !== -1 ? index : 0
  if (currentLineIndex.value !== nextIndex) {
    currentLineIndex.value = nextIndex
    scrollToActiveLine()
  }
}

// 平滑滚动对焦至中线——只滚动歌词容器本身，不用 scrollIntoView
// （后者会连带滚动祖先/window，而本面板祖先有 transform，会把 fixed 的关闭按钮顶出视野）。
// 用户手动滚动期间（userScrolling）跳过，避免抢占。
function scrollToActiveLine() {
  if (userScrolling.value) return
  nextTick(() => {
    const scroller = scrollerRef.value
    if (!scroller) return
    const activeEl = scroller.querySelector('.lyric-line.active') as HTMLElement | null
    if (!activeEl) return
    const sRect = scroller.getBoundingClientRect()
    const aRect = activeEl.getBoundingClientRect()
    const delta = (aRect.top - sRect.top) - (scroller.clientHeight / 2 - activeEl.clientHeight / 2)
    scroller.scrollTo({ top: scroller.scrollTop + delta, behavior: 'smooth' })
  })
}

// 歌词点击跳转播放 (Seek-on-Click)；纯文本歌词无时间轴，不可跳转
function seekToLine(time: number) {
  if (!synced.value || time < 0) return
  playerStore.seek(time)
  // 显式点击歌词是“去这一句”的意图：清除手动滚动抑制并强行对焦
  userScrolling.value = false
  if (userScrollTimer) clearTimeout(userScrollTimer)
  syncLyricsIndex()
}

// 侦听歌曲时间更新以滚动
watch(() => playerStore.currentTime, () => {
  if (props.isOpen) {
    syncLyricsIndex()
  }
})

// 监视切歌与面板开启
watch(() => playerStore.currentTrack?.trackId, () => {
  if (props.isOpen) {
    void loadLyrics()
  }
})

watch(() => props.isOpen, (newVal) => {
  if (newVal) {
    void loadLyrics()
  }
})
</script>

<style scoped>
.animate-spin {
  animation: spin 1.2s linear infinite;
}
/* 纯文本歌词无时间轴，非交互：默认光标、不高亮 */
.lyric-line.is-static {
  cursor: default;
}
/* 歌词界面收藏红心 */
.lyrics-heart-btn {
  margin-top: 10px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 16px;
  font-size: 13px;
  font-weight: 600;
  border-radius: 999px;
  border: 1px solid var(--border-glass, rgba(255, 255, 255, 0.12));
  background: rgba(255, 255, 255, 0.05);
  color: var(--text-muted, rgba(255, 255, 255, 0.6));
  cursor: pointer;
  transition: color 0.2s, border-color 0.2s, background 0.2s;
}
.lyrics-heart-btn:hover {
  color: var(--danger, #f87171);
  border-color: var(--danger, #f87171);
}
.lyrics-heart-btn.starred {
  color: var(--danger, #f87171);
  border-color: var(--danger, #f87171);
  background: rgba(248, 113, 113, 0.1);
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
