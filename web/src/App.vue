<template>
  <n-config-provider :theme="darkTheme">
    <n-message-provider>
      <LoginView v-if="!token" :loading="loginLoading" :error="loginError" @login="handleLogin" />

      <LibraryShell
        v-else
        :mode="mode"
        @change-mode="changeMode"
        @refresh="refreshCurrentView"
        @logout="logout"
        @search="runSearch"
      >
        <n-alert
          v-if="globalError"
          class="global-alert"
          type="error"
          :bordered="false"
          closable
          @close="globalError = ''"
        >
          {{ globalError }}
        </n-alert>

        <SearchPanel
          v-if="searchQuery"
          :query="searchQuery"
          :results="searchResults"
          :loading="searchLoading"
          @close="closeSearch"
          @play-track="playSearchTrack"
          @select-album="selectAlbum"
          @select-artist="selectArtistFromSearch"
        />

        <template v-else-if="mode === 'albums'">
          <AlbumGrid
            :albums="albums"
            :selected-album-id="selectedAlbum?.id || ''"
            :loading="albumsLoading"
            @select="selectAlbum"
          />
          <AlbumDetail :album="selectedAlbum" @play="playAlbumTrack" />
        </template>

        <ArtistBrowser
          v-else-if="mode === 'artists'"
          :artists="artists"
          :selected-artist="selectedArtist"
          @select-artist="selectArtist"
          @select-album="selectAlbum"
        />

        <ScanPanel
          v-else
          :status="scanStatus"
          :triggering="scanTriggering"
          @trigger="triggerScan"
        />

        <template #player>
          <PlayerBar :track="playerTrack" />
        </template>
      </LibraryShell>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { NAlert, NConfigProvider, NMessageProvider, darkTheme } from 'naive-ui'
import { ApiClient, ApiError, tokenStorage } from './api/client'
import type {
  AlbumDetail as AlbumDetailType,
  AlbumSummary,
  ArtistDetail,
  ArtistSummary,
  PlayerTrack,
  ScanStatus,
  SearchResponse,
  TrackResult,
  TrackSummary,
  ViewMode,
} from './api/client'
import AlbumDetail from './components/AlbumDetail.vue'
import AlbumGrid from './components/AlbumGrid.vue'
import ArtistBrowser from './components/ArtistBrowser.vue'
import LibraryShell from './components/LibraryShell.vue'
import LoginView from './components/LoginView.vue'
import PlayerBar from './components/PlayerBar.vue'
import ScanPanel from './components/ScanPanel.vue'
import SearchPanel from './components/SearchPanel.vue'

const token = ref(tokenStorage.load())
const api = new ApiClient(token.value)
const mode = ref<ViewMode>('albums')
const loginLoading = ref(false)
const loginError = ref('')
const globalError = ref('')

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
})
const scanTriggering = ref(false)
const playerTrack = ref<PlayerTrack | null>(null)

onMounted(() => {
  if (token.value) {
    void loadInitialData()
  }
})

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

function playAlbumTrack(track: TrackSummary) {
  playerTrack.value = {
    trackId: track.id,
    title: track.title,
    artist: selectedAlbum.value?.artist,
    album: selectedAlbum.value?.title,
    streamUrl: track.stream_url,
  }
}

function playSearchTrack(track: TrackResult) {
  playerTrack.value = {
    trackId: track.id,
    title: track.title,
    artist: track.artist,
    album: track.album,
    streamUrl: track.stream_url,
  }
}

function logout() {
  tokenStorage.clear()
  token.value = null
  api.setToken(null)
  selectedAlbum.value = null
  selectedArtist.value = null
  playerTrack.value = null
}

function handleApiError(error: unknown) {
  if (error instanceof ApiError && error.status === 401) {
    logout()
    loginError.value = 'Session expired. Sign in again.'
    return
  }
  globalError.value = messageFromError(error)
}

function messageFromError(error: unknown) {
  if (error instanceof Error) return error.message
  return 'Request failed'
}
</script>
