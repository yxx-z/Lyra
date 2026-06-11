<template>
  <aside class="detail-panel">
    <!-- 流动渐变高斯模糊背景层 (经典 Apple Music 虚化氛围) -->
    <div
      v-if="album && album.cover_url && !coverBroken"
      class="detail-backdrop"
      :style="{ backgroundImage: `url(${coverSrc})` }"
    ></div>

    <!-- 空数据展示 -->
    <div v-if="!album" class="empty-state">
      <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 16px; color: var(--text-dim);">
        <path d="M9 18V5l12-2v13" />
        <circle cx="6" cy="18" r="3" />
        <circle cx="18" cy="16" r="3" />
      </svg>
      <p style="font-weight: 500;">选择一张专辑查看曲目详情</p>
    </div>

    <!-- 详情内容展现 -->
    <template v-else>
      <div class="detail-header">
        <img
          v-if="album.cover_url && !coverBroken"
          :src="coverSrc"
          alt="Album cover artwork"
          class="detail-cover"
          @error="coverBroken = true"
        />
        <div v-else class="detail-cover placeholder-cover">
          {{ album.title.slice(0, 2).toUpperCase() }}
        </div>
        
        <div class="detail-title-info">
          <p class="eyebrow" style="color: var(--accent);">
            {{ album.release_date || album.year || '未知年份' }}<span v-if="album.genre"> · {{ album.genre }}</span> · ALBUM
          </p>
          <h3>{{ album.title }}</h3>
          <p class="muted" style="margin-top: 4px;">
            艺术家：<span class="artist-link">{{ album.artist || '未知艺术家' }}</span>
          </p>
          <p class="muted" style="font-size: 12px; margin-top: 2px;">
            共收录 {{ album.tracks?.length || 0 }} 首高品质曲目
          </p>
          <div style="display: flex; align-items: center; gap: 10px; margin-top: 12px;">
            <button
              class="custom-btn-primary"
              style="width: auto; padding: 8px 18px; font-size: 13px; display: inline-flex; align-items: center; gap: 8px;"
              type="button"
              :disabled="scraping"
              @click="handleScrape"
            >
              <span v-if="scraping" class="loading-spinner" aria-label="刮削中"></span>
              <span>{{ scraping ? '刮削中…' : '🔍 刮削元数据' }}</span>
            </button>
            <!-- 专辑红心收藏按钮 -->
            <button
              class="heart-btn album-heart"
              :class="{ starred: album.starred }"
              type="button"
              :title="album.starred ? '取消收藏专辑' : '收藏专辑'"
              @click="toggleAlbumStar"
            >{{ album.starred ? '♥' : '♡' }}</button>
            <!-- 管理员专辑删除入口 -->
            <button v-if="isAdmin" class="danger-btn" type="button" @click="confirmingDelete = true">删除专辑</button>
          </div>
          <p v-if="scrapeMessage" class="muted" style="font-size: 12px; margin-top: 8px;">{{ scrapeMessage }}</p>
          <!-- 删除确认区域 -->
          <div v-if="confirmingDelete" class="delete-confirm">
            <p>确认删除专辑「{{ album.title }}」？这会从音乐库移除该专辑及其曲目。</p>
            <label><input type="checkbox" v-model="alsoDeleteFiles" /> 同时删除硬盘文件</label>
            <p v-if="alsoDeleteFiles" class="warn">⚠ 文件将被永久删除且不可恢复；若音乐目录为只读挂载会删除失败。</p>
            <div class="delete-actions">
              <button class="danger-btn" type="button" :disabled="deleting" @click="doDelete">确认删除</button>
              <button type="button" :disabled="deleting" @click="confirmingDelete = false; alsoDeleteFiles = false">取消</button>
            </div>
          </div>
        </div>
      </div>

      <!-- 精美曲目列表 -->
      <div class="track-list">
        <!-- 使用 div 包装行，避免 button 嵌套（row 内本来就有 heart-btn 等交互按钮） -->
        <div
          v-for="track in album.tracks"
          :key="track.id"
          :class="{ active: playerStore.currentTrack?.trackId === track.id }"
          class="track-row"
        >
          <!-- 可点击的播放区域：序号 + 歌名 -->
          <div class="track-play-area" role="button" tabindex="0" @click="$emit('play', track)" @keydown.enter="$emit('play', track)" @keydown.space.prevent="$emit('play', track)">
            <!-- 左侧：序号/跳动的均衡器 -->
            <span class="track-number">
              <div
                v-if="playerStore.currentTrack?.trackId === track.id && playerStore.isPlaying"
                class="equalizer-icon"
                title="正在播放"
              >
                <span class="eq-bar"></span>
                <span class="eq-bar"></span>
                <span class="eq-bar"></span>
              </div>
              <span v-else>{{ track.track_number || '-' }}</span>
            </span>

            <!-- 中间：歌名与音质信息 -->
            <span class="track-title" :title="track.title">
              {{ track.title }}
              <span
                v-if="track.format || track.bitrate"
                class="muted"
                style="font-size: 10px; font-weight: normal; margin-left: 6px; padding: 1px 4px; border-radius: 4px; background: rgba(255,255,255,0.04); border: 1px solid rgba(255,255,255,0.03);"
              >
                {{ track.format.toUpperCase() }} {{ Math.round(track.bitrate / 1000) }}K
              </span>
            </span>
          </div>

          <!-- 右侧操作区：红心 + 添加到歌单 + 时长 -->
          <div class="track-actions">
            <button
              class="heart-btn"
              :class="{ starred: track.starred }"
              type="button"
              :title="track.starred ? '取消收藏' : '收藏'"
              @click.stop="toggleTrackStar(track)"
            >{{ track.starred ? '♥' : '♡' }}</button>
            <AddToPlaylist :api="api" :track-id="track.id" />
            <span class="track-duration">{{ formatDuration(track.duration) }}</span>
          </div>
        </div>
      </div>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { usePlayerStore } from '../stores/player'
import { ApiError, type ApiClient, type AlbumDetail, type TrackSummary } from '../api/client'
import AddToPlaylist from './AddToPlaylist.vue'

const props = defineProps<{
  album: AlbumDetail | null
  api: ApiClient
  isAdmin?: boolean
}>()

const emit = defineEmits<{
  play: [track: TrackSummary]
  refresh: []
  deleted: [fileErrors: string[]]
}>()

// 引入全局播放 Store，监测当前歌曲和播放动画
const playerStore = usePlayerStore()

function formatDuration(seconds: number) {
  if (!seconds) return '--:--'
  const minutes = Math.floor(seconds / 60)
  const rest = String(seconds % 60).padStart(2, '0')
  return `${minutes}:${rest}`
}

const coverBroken = ref(false)
const scraping = ref(false)
const scrapeMessage = ref('')
const coverVersion = ref(0)
const confirmingDelete = ref(false)
const alsoDeleteFiles = ref(false)
const deleting = ref(false)

// 带版本号的封面 URL：刮削后 bump 版本强制浏览器重取（同 URL 否则命中缓存）
const coverSrc = computed(() =>
  props.album ? `${props.album.cover_url}?v=${coverVersion.value}` : '',
)

async function handleScrape() {
  if (!props.album || scraping.value) return
  scraping.value = true
  scrapeMessage.value = ''
  try {
    const res = await props.api.scrapeAlbum(props.album.id)
    if (res.status === 'done') {
      emit('refresh')
      coverVersion.value++
      coverBroken.value = false
      scrapeMessage.value = '已更新'
    } else {
      scrapeMessage.value = '未匹配到专辑'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      scrapeMessage.value = '未匹配到专辑'
    } else {
      scrapeMessage.value = '刮削失败，请重试'
    }
  } finally {
    scraping.value = false
  }
}

// 专辑红心切换：直接就地改 album.starred（同一对象引用，回流到父级 selectedAlbum，
// 切走再回来即使组件重挂也能保留，无需整页刷新）。
async function toggleAlbumStar() {
  if (!props.album) return
  const next = !props.album.starred
  props.album.starred = next
  try {
    if (next) {
      await props.api.star('album', props.album.id)
    } else {
      await props.api.unstar('album', props.album.id)
    }
  } catch {
    // 失败时回滚
    props.album.starred = !next
  }
}

// 曲目红心切换
async function toggleTrackStar(track: TrackSummary) {
  const next = !track.starred
  track.starred = next
  try {
    if (next) {
      await props.api.star('song', track.id)
    } else {
      await props.api.unstar('song', track.id)
    }
  } catch {
    // 失败时回滚
    track.starred = !next
  }
}

async function doDelete() {
  if (!props.album) return
  deleting.value = true
  try {
    const res = await props.api.deleteAlbum(props.album.id, alsoDeleteFiles.value)
    confirmingDelete.value = false
    alsoDeleteFiles.value = false
    emit('deleted', res.fileErrors || [])
  } catch {
    // 失败时保持确认区打开；交由上层 globalError 或此处提示
  } finally {
    deleting.value = false
  }
}

watch(
  () => props.album?.id,
  () => {
    coverBroken.value = false
    scrapeMessage.value = ''
    confirmingDelete.value = false
    alsoDeleteFiles.value = false
  },
  { immediate: true },
)
</script>

<style scoped>
/* 曲目行：flex 容器，播放区域 + 操作区 */
.track-row {
  display: flex;
  align-items: center;
}

/* 播放区域占满剩余空间，鼠标显示手型 */
.track-play-area {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  cursor: pointer;
}

.track-play-area:focus-visible {
  outline: 2px solid var(--accent, #6ee7b7);
  outline-offset: 2px;
  border-radius: 4px;
}

/* 操作区：不缩放，横向排列 */
.track-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

/* 红心收藏按钮 */
.heart-btn {
  background: none;
  border: none;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  color: var(--text-dim, rgba(255, 255, 255, 0.3));
  padding: 2px 4px;
  border-radius: 4px;
  transition: color 0.15s, transform 0.1s;
  flex-shrink: 0;
}

.heart-btn:hover {
  color: var(--danger, #f87171);
}

.heart-btn.starred {
  color: var(--danger, #f87171);
}

.heart-btn.album-heart {
  font-size: 20px;
  padding: 4px 6px;
}

/* 管理员危险操作按钮（红色） */
.danger-btn {
  background: rgba(248, 113, 113, 0.15);
  border: 1px solid rgba(248, 113, 113, 0.4);
  color: var(--danger, #f87171);
  font-size: 13px;
  padding: 6px 14px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}

.danger-btn:hover:not(:disabled) {
  background: rgba(248, 113, 113, 0.28);
  border-color: rgba(248, 113, 113, 0.7);
}

.danger-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 删除确认卡片 */
.delete-confirm {
  margin-top: 12px;
  padding: 14px 16px;
  border: 1px solid rgba(248, 113, 113, 0.35);
  border-radius: 8px;
  background: rgba(248, 113, 113, 0.06);
  font-size: 13px;
  line-height: 1.5;
}

.delete-confirm p {
  margin: 0 0 8px;
}

.delete-confirm label {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  user-select: none;
}

/* 警告红字 */
.warn {
  color: var(--danger, #f87171);
  font-size: 12px;
  margin-top: 6px !important;
}

/* 确认操作按钮行 */
.delete-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.delete-actions button:last-child {
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.12);
  color: var(--text-dim, rgba(255, 255, 255, 0.6));
  font-size: 13px;
  padding: 6px 14px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
}

.delete-actions button:last-child:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.1);
}

.delete-actions button:last-child:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
