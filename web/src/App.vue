<template>
  <div>
    <!-- 0. 首次启动引导页 -->
    <SetupView
      v-if="needsSetup"
      :loading="setupLoading"
      :error="setupError"
      @setup="handleSetup"
    />

    <!-- 1. 登录模式 -->
    <LoginView
      v-else-if="showLogin"
      :loading="loginLoading"
      :error="loginError"
      @login="handleLogin"
    />

    <!-- 2. 全局主系统 -->
    <LibraryShell
      v-else
      :mode="mode"
      @change-mode="changeMode"
      @refresh="refreshCurrentView"
      @logout="void logout()"
      @open-settings="showSettings = true"
      @search="runSearch"
    >
      <!-- 高品质手写浮动 Alert 提示栏 -->
      <div v-if="globalError" class="global-alert">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 20px; height: 20px; color: var(--danger); flex-shrink: 0; margin-top: 1px;">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
        <div style="flex: 1; font-size: 13px; font-weight: 500; line-height: 1.4;">
          {{ globalError }}
        </div>
        <button class="global-alert-close" type="button" @click="globalError = ''">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 14px; height: 14px;">
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>

      <!-- 账户设置面板 -->
      <AccountSettings
        v-if="showSettings"
        :api="api"
        @close="showSettings = false"
      />

      <!-- 搜索展示面板 (最高优先级覆盖主面板) -->
      <SearchPanel
        v-if="!showSettings && searchQuery"
        :query="searchQuery"
        :results="searchResults"
        :loading="searchLoading"
        @close="closeSearch"
        @play-track="playSearchTrack"
        @select-album="selectAlbum"
        @select-artist="selectArtistFromSearch"
      />

      <!-- 模块 A: 专辑库双栏侧滑交互 -->
      <div
        v-else-if="mode === 'albums'"
        :class="{ 'has-detail': selectedAlbum }"
        class="content-grid"
      >
        <AlbumGrid
          :albums="albums"
          :selected-album-id="selectedAlbum?.id || ''"
          :loading="albumsLoading"
          @select="selectAlbum"
          @quick-play="playEntireAlbum"
        />
        <AlbumDetail
          :album="selectedAlbum"
          :api="api"
          @play="playAlbumTrack"
          @refresh="refreshSelectedAlbum"
        />
      </div>

      <!-- 模块 B: 歌手收纳展板 -->
      <ArtistBrowser
        v-else-if="mode === 'artists'"
        :artists="artists"
        :selected-artist="selectedArtist"
        @select-artist="selectArtist"
        @select-album="selectAlbum"
        @quick-play-album="playEntireAlbum"
      />

      <!-- 模块 C: 系统扫描管理 -->
      <ScanPanel
        v-else
        :status="scanStatus"
        :triggering="scanTriggering"
        @trigger="triggerScan"
      />

      <!-- 3. 常驻高保真控制台插槽 -->
      <template #player>
        <PlayerBar
          :is-lyrics-open="isLyricsOpen"
          @toggle-lyrics="isLyricsOpen = !isLyricsOpen"
        />
      </template>
    </LibraryShell>

    <!-- 4. 全屏沉浸式滚动歌词浮层 -->
    <LyricsPanel
      :is-open="isLyricsOpen"
      :api="api"
      @close="isLyricsOpen = false"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { usePlayerStore } from './stores/player'
import { ApiClient, ApiError, tokenStorage } from './api/client'
import type {
  AlbumDetail as AlbumDetailType,
  AlbumSummary,
  ArtistDetail,
  ArtistSummary,
  ScanStatus,
  SearchResponse,
  TrackResult,
  TrackSummary,
  ViewMode,
} from './api/client'
import AccountSettings from './components/AccountSettings.vue'
import AlbumDetail from './components/AlbumDetail.vue'
import AlbumGrid from './components/AlbumGrid.vue'
import ArtistBrowser from './components/ArtistBrowser.vue'
import LibraryShell from './components/LibraryShell.vue'
import LoginView from './components/LoginView.vue'
import LyricsPanel from './components/LyricsPanel.vue'
import PlayerBar from './components/PlayerBar.vue'
import ScanPanel from './components/ScanPanel.vue'
import SearchPanel from './components/SearchPanel.vue'
import SetupView from './components/SetupView.vue'

// 引入全局音频 Store
const playerStore = usePlayerStore()

const token = ref(tokenStorage.load())
const anonymousAccess = ref(false)
const api = new ApiClient(token.value)
const mode = ref<ViewMode>('albums')
const loginLoading = ref(false)
const loginError = ref('')
const globalError = ref('')

// 首次启动引导状态
const needsSetup = ref(false)
const setupLoading = ref(false)
const setupError = ref('')

// 账户设置面板开关
const showSettings = ref(false)

// 沉浸式全屏歌词是否开启状态
const isLyricsOpen = ref(false)

const albums = ref<AlbumSummary[]>([])
const selectedAlbum = ref<AlbumDetailType | null>(null)
const albumsLoading = ref(false)

const artists = ref<ArtistSummary[]>([])
const selectedArtist = ref<ArtistDetail | null>(null)

const emptySearch: SearchResponse = { tracks: [], albums: [], artists: [] }
const searchQuery = ref('')
const searchResults = ref<SearchResponse>(emptySearch)
const searchLoading = ref(false)

const scanStatus = reactive<ScanStatus>({
  running: false,
  total: 0,
  processed: 0,
  errors: 0,
  started_at: '',
  phase: 'idle',
  lyrics_scraped: 0,
  albums_scraped: 0,
  fingerprinted: 0,
  lyrics_upgraded: 0,
})
const scanTriggering = ref(false)
let scanPollTimer: ReturnType<typeof setInterval> | null = null

// 扫描在后台异步运行，需持续轮询状态直到 running 变为 false。
// startScanPolling 幂等：已在轮询时不重复创建定时器。
function startScanPolling() {
  if (scanPollTimer !== null) return
  scanPollTimer = setInterval(() => {
    void loadScanStatus()
  }, 1000)
}

function stopScanPolling() {
  if (scanPollTimer !== null) {
    clearInterval(scanPollTimer)
    scanPollTimer = null
  }
}

onMounted(() => {
  void boot()
})

onUnmounted(() => {
  stopScanPolling()
})

async function boot() {
  // 首先检查是否需要首次初始化
  try {
    const status = await api.getSetupStatus()
    if (status.needsSetup) {
      needsSetup.value = true
      return
    }
  } catch {
    // 查询失败则按已初始化处理，继续原逻辑
  }

  if (token.value) {
    try {
      await api.refreshSession()
      await loadInitialData()
    } catch (error) {
      handleApiError(error)
    }
    return
  }

  try {
    const response = await api.listAlbums()
    albums.value = response.albums
    anonymousAccess.value = true
    await Promise.all([loadArtists(), loadScanStatus()])
    if (response.albums[0]) {
      await selectAlbum(response.albums[0].id)
    }
  } catch {
    anonymousAccess.value = false
  }
}

async function handleLogin(payload: { username: string; password: string }) {
  loginLoading.value = true
  loginError.value = ''
  try {
    const nextToken = await api.login(payload.username, payload.password)
    tokenStorage.save(nextToken)
    token.value = nextToken
    await loadInitialData()
  } catch (error) {
    loginError.value = messageFromError(error)
  } finally {
    loginLoading.value = false
  }
}

async function handleSetup(payload: { username: string; password: string }) {
  setupLoading.value = true
  setupError.value = ''
  try {
    const nextToken = await api.setup(payload.username, payload.password)
    tokenStorage.save(nextToken)
    token.value = nextToken
    needsSetup.value = false
    await loadInitialData()
  } catch (error) {
    setupError.value = messageFromError(error)
  } finally {
    setupLoading.value = false
  }
}

async function loadInitialData() {
  await Promise.all([loadAlbums(), loadArtists(), loadScanStatus()])
}

async function loadAlbums() {
  albumsLoading.value = true
  try {
    const response = await api.listAlbums()
    albums.value = response.albums
    if (!selectedAlbum.value && response.albums[0]) {
      await selectAlbum(response.albums[0].id)
    }
  } catch (error) {
    handleApiError(error)
  } finally {
    albumsLoading.value = false
  }
}

async function selectAlbum(id: string) {
  try {
    selectedAlbum.value = await api.getAlbum(id)
    mode.value = 'albums'
    searchQuery.value = ''
  } catch (error) {
    handleApiError(error)
  }
}

async function refreshSelectedAlbum() {
  if (!selectedAlbum.value) return
  try {
    selectedAlbum.value = await api.getAlbum(selectedAlbum.value.id)
  } catch (error) {
    handleApiError(error)
  }
}

async function loadArtists() {
  try {
    const response = await api.listArtists()
    artists.value = response.artists
  } catch (error) {
    handleApiError(error)
  }
}

async function selectArtist(id: string) {
  try {
    selectedArtist.value = await api.getArtist(id)
  } catch (error) {
    handleApiError(error)
  }
}

async function selectArtistFromSearch(id: string) {
  mode.value = 'artists'
  searchQuery.value = ''
  await selectArtist(id)
}

async function runSearch(query: string) {
  if (!query) {
    closeSearch()
    return
  }
  searchQuery.value = query
  searchLoading.value = true
  try {
    searchResults.value = await api.search(query)
  } catch (error) {
    handleApiError(error)
  } finally {
    searchLoading.value = false
  }
}

function closeSearch() {
  searchQuery.value = ''
  searchResults.value = emptySearch
}

async function loadScanStatus() {
  try {
    Object.assign(scanStatus, await api.getScanStatus())
    // 扫描进行中则持续轮询，结束后停止——避免 UI 冻结在早期快照。
    if (scanStatus.running) {
      startScanPolling()
    } else {
      stopScanPolling()
    }
  } catch (error) {
    handleApiError(error)
  }
}

async function triggerScan() {
  scanTriggering.value = true
  try {
    await api.triggerScan()
    await loadScanStatus()
  } catch (error) {
    handleApiError(error)
  } finally {
    scanTriggering.value = false
  }
}

function changeMode(nextMode: ViewMode) {
  mode.value = nextMode
  searchQuery.value = ''
  if (nextMode === 'artists' && !selectedArtist.value && artists.value[0]) {
    void selectArtist(artists.value[0].id)
  }
  if (nextMode === 'scan') {
    void loadScanStatus()
  }
}

function refreshCurrentView() {
  if (mode.value === 'artists') {
    void loadArtists()
  } else if (mode.value === 'scan') {
    void loadScanStatus()
  } else {
    void loadAlbums()
  }
}

// 核心功能：用户在专辑列表里点击某首歌，触发专辑内全盘连播
function playAlbumTrack(track: TrackSummary) {
  if (!selectedAlbum.value) return

  // 拼装连续播放队列，并映射字段类型
  const queue = selectedAlbum.value.tracks.map((t) => ({
    trackId: t.id,
    title: t.title,
    artist: selectedAlbum.value?.artist,
    album: selectedAlbum.value?.title,
    streamUrl: t.stream_url,
    coverUrl: selectedAlbum.value?.cover_url,
  }))

  const activeTrack = queue.find((t) => t.trackId === track.id)
  if (activeTrack) {
    playerStore.playTrack(activeTrack, queue)
  }
}

// 核心高阶功能：在网格中一键快捷播放整张专辑，实现全自动连续开播
async function playEntireAlbum(albumId: string) {
  try {
    const albumDetail = await api.getAlbum(albumId)
    if (albumDetail && albumDetail.tracks && albumDetail.tracks.length > 0) {
      // 组装整盘曲目为播放队列
      const queue = albumDetail.tracks.map((t) => ({
        trackId: t.id,
        title: t.title,
        artist: albumDetail.artist,
        album: albumDetail.title,
        streamUrl: t.stream_url,
        coverUrl: albumDetail.cover_url,
      }))
      
      // 默认直接开播整张专辑的第一首
      playerStore.playTrack(queue[0], queue)
    }
  } catch (error) {
    handleApiError(error)
  }
}

// 搜索曲目快捷开播 (支持在搜索到的 Tracks 之间连播)
function playSearchTrack(track: TrackResult) {
  const activeTrack = {
    trackId: track.id,
    title: track.title,
    artist: track.artist,
    album: track.album,
    streamUrl: track.stream_url,
    coverUrl: '/api/v1/cover/' + track.album_id,
  }

  const queue = searchResults.value.tracks.map((t) => ({
    trackId: t.id,
    title: t.title,
    artist: t.artist,
    album: t.album,
    streamUrl: t.stream_url,
    coverUrl: '/api/v1/cover/' + t.album_id,
  }))

  playerStore.playTrack(activeTrack, queue)
}

async function logout() {
  try {
    await api.logout()
  } catch {
    // Local logout should still clear UI state if the server is unreachable.
  }
  tokenStorage.clear()
  token.value = null
  anonymousAccess.value = false
  api.setToken(null)
  selectedAlbum.value = null
  selectedArtist.value = null
  isLyricsOpen.value = false // 登出时自动收折歌词面板
  showSettings.value = false // 登出时关闭账户设置面板
  playerStore.$reset() // 清空全局播放状态
}

function handleApiError(error: unknown) {
  if (error instanceof ApiError && error.status === 401) {
    void logout()
    loginError.value = '登录会话已过期，请重新登录。'
    return
  }
  globalError.value = messageFromError(error)
}

function messageFromError(error: unknown) {
  if (error instanceof Error) return error.message
  return '请求服务失败，请检查网络或配置'
}

const showLogin = computed(() => !token.value && !anonymousAccess.value)
</script>
