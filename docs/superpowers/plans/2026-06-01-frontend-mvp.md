# Frontend MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Lyra's first usable Web UI: login, Library First browsing, search, artist browsing, scan management, and a persistent bottom player.

**Architecture:** Keep this as a Vue 3 single-page app without Vue Router. `App.vue` owns auth, selected view, selected album, search state, and player state; focused child components render each panel and emit user actions. `web/src/api/client.ts` centralizes API types and Bearer-token fetch handling.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Vite, Naive UI, native `<audio>`, existing Go REST API.

---

## File Structure

```
web/src/
├── App.vue                         # Top-level state, data loading, view switching
├── api/
│   └── client.ts                   # API types, token storage, request helpers
├── components/
│   ├── LoginView.vue               # Login form
│   ├── LibraryShell.vue            # Main app chrome: sidebar, toolbar, slots
│   ├── AlbumGrid.vue               # Album wall
│   ├── AlbumDetail.vue             # Album metadata + track list
│   ├── ArtistBrowser.vue           # Artist list + albums for selected artist
│   ├── SearchPanel.vue             # Search grouped results
│   ├── ScanPanel.vue               # Scan status + trigger button
│   └── PlayerBar.vue               # Persistent native audio player
└── style.css                       # Full app styling, replacing Vite starter styles
```

---

## Task 1: API Client

**Files:**
- Create: `web/src/api/client.ts`

- [ ] **Step 1: Create the API client file**

Create `web/src/api/client.ts`:

```ts
export type ViewMode = 'albums' | 'artists' | 'scan'

export type AlbumSummary = {
  id: string
  title: string
  artist: string
  artist_id: string
  year: number
  track_count: number
  cover_url: string
}

export type TrackSummary = {
  id: string
  title: string
  track_number: number
  disc_number: number
  duration: number
  format: string
  bitrate: number
  stream_url: string
}

export type AlbumDetail = AlbumSummary & {
  tracks: TrackSummary[]
}

export type ArtistSummary = {
  id: string
  name: string
  album_count: number
}

export type ArtistDetail = {
  id: string
  name: string
  albums: AlbumSummary[]
}

export type TrackResult = {
  id: string
  title: string
  artist: string
  album: string
  album_id: string
  duration: number
  stream_url: string
}

export type AlbumResult = {
  id: string
  title: string
  artist: string
  cover_url: string
}

export type ArtistResult = {
  id: string
  name: string
}

export type SearchResponse = {
  tracks: TrackResult[]
  albums: AlbumResult[]
  artists: ArtistResult[]
}

export type ScanStatus = {
  running: boolean
  total: number
  processed: number
  errors: number
  started_at: string
}

export type PlayerTrack = {
  trackId: string
  title: string
  artist?: string
  album?: string
  streamUrl: string
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export class ApiClient {
  private token: string | null

  constructor(token: string | null) {
    this.token = token
  }

  setToken(token: string | null) {
    this.token = token
  }

  getToken() {
    return this.token
  }

  async login(username: string, password: string): Promise<string> {
    const data = await this.request<{ token: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
      headers: { 'Content-Type': 'application/json' },
      auth: false,
    })
    this.token = data.token
    return data.token
  }

  listAlbums() {
    return this.request<{ albums: AlbumSummary[] }>('/api/v1/albums')
  }

  getAlbum(id: string) {
    return this.request<AlbumDetail>(`/api/v1/albums/${encodeURIComponent(id)}`)
  }

  listArtists() {
    return this.request<{ artists: ArtistSummary[] }>('/api/v1/artists')
  }

  getArtist(id: string) {
    return this.request<ArtistDetail>(`/api/v1/artists/${encodeURIComponent(id)}`)
  }

  search(q: string) {
    return this.request<SearchResponse>(`/api/v1/search?q=${encodeURIComponent(q)}`)
  }

  getScanStatus() {
    return this.request<ScanStatus>('/api/v1/library/scan/status')
  }

  triggerScan() {
    return this.request<{ ok: boolean }>('/api/v1/library/scan', { method: 'POST' })
  }

  private async request<T>(
    path: string,
    options: RequestInit & { auth?: boolean } = {},
  ): Promise<T> {
    const headers = new Headers(options.headers)
    if (options.auth !== false && this.token) {
      headers.set('Authorization', `Bearer ${this.token}`)
    }

    const response = await fetch(path, {
      ...options,
      headers,
    })

    if (!response.ok) {
      let message = response.statusText
      try {
        const body = (await response.json()) as { error?: string }
        if (body.error) {
          message = body.error
        }
      } catch {
        // Keep status text when the server returns a non-JSON response.
      }
      throw new ApiError(response.status, message)
    }

    return (await response.json()) as T
  }
}

export const tokenStorage = {
  key: 'lyra.token',
  load(): string | null {
    return localStorage.getItem(this.key)
  },
  save(token: string) {
    localStorage.setItem(this.key, token)
  },
  clear() {
    localStorage.removeItem(this.key)
  },
}
```

- [ ] **Step 2: Verify TypeScript accepts the client**

Run:

```bash
cd web && npm run build
```

Expected: TypeScript succeeds. Vite may still build the placeholder UI.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add API client"
```

---

## Task 2: Login View

**Files:**
- Create: `web/src/components/LoginView.vue`

- [ ] **Step 1: Create login component**

Create `web/src/components/LoginView.vue`:

```vue
<template>
  <main class="login-screen">
    <section class="login-panel">
      <div>
        <p class="eyebrow">Self-hosted music</p>
        <h1>Lyra</h1>
        <p class="muted">Sign in to browse and play your local library.</p>
      </div>

      <n-form class="login-form" @submit.prevent="submit">
        <n-form-item label="Username">
          <n-input v-model:value="username" autocomplete="username" placeholder="admin" />
        </n-form-item>
        <n-form-item label="Password">
          <n-input
            v-model:value="password"
            autocomplete="current-password"
            placeholder="Password"
            type="password"
          />
        </n-form-item>
        <n-alert v-if="error" type="error" :bordered="false">
          {{ error }}
        </n-alert>
        <n-button attr-type="submit" block type="primary" :loading="loading">
          Sign in
        </n-button>
      </n-form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { NAlert, NButton, NForm, NFormItem, NInput } from 'naive-ui'

const emit = defineEmits<{
  login: [payload: { username: string; password: string }]
}>()

defineProps<{
  loading: boolean
  error: string
}>()

const username = ref('admin')
const password = ref('')

function submit() {
  emit('login', { username: username.value.trim(), password: password.value })
}
</script>
```

- [ ] **Step 2: Build**

Run:

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/LoginView.vue
git commit -m "feat(web): add login view"
```

---

## Task 3: Library Shell

**Files:**
- Create: `web/src/components/LibraryShell.vue`

- [ ] **Step 1: Create shell component**

Create `web/src/components/LibraryShell.vue`:

```vue
<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">Lyra</div>
      <button
        v-for="item in navItems"
        :key="item.mode"
        class="nav-button"
        :class="{ active: mode === item.mode }"
        type="button"
        @click="$emit('change-mode', item.mode)"
      >
        <span class="nav-icon">{{ item.icon }}</span>
        <span>{{ item.label }}</span>
      </button>
    </aside>

    <section class="workspace">
      <header class="topbar">
        <div>
          <p class="eyebrow">{{ title }}</p>
          <h2>{{ heading }}</h2>
        </div>
        <form class="search-form" @submit.prevent="submitSearch">
          <n-input v-model:value="searchText" clearable placeholder="Search tracks, albums, artists" />
        </form>
        <n-button secondary @click="$emit('refresh')">Refresh</n-button>
        <n-button quaternary @click="$emit('logout')">Sign out</n-button>
      </header>

      <main class="content-grid">
        <slot />
      </main>

      <slot name="player" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NInput } from 'naive-ui'
import type { ViewMode } from '../api/client'

const props = defineProps<{
  mode: ViewMode
}>()

const emit = defineEmits<{
  'change-mode': [mode: ViewMode]
  refresh: []
  logout: []
  search: [query: string]
}>()

const searchText = ref('')

const navItems: Array<{ mode: ViewMode; label: string; icon: string }> = [
  { mode: 'albums', label: 'Albums', icon: '▦' },
  { mode: 'artists', label: 'Artists', icon: '◎' },
  { mode: 'scan', label: 'Scan', icon: '↻' },
]

const title = computed(() => {
  if (props.mode === 'artists') return 'Artists'
  if (props.mode === 'scan') return 'Library management'
  return 'Library'
})

const heading = computed(() => {
  if (props.mode === 'artists') return 'Browse by artist'
  if (props.mode === 'scan') return 'Scan status'
  return 'Albums'
})

function submitSearch() {
  emit('search', searchText.value.trim())
}
</script>
```

- [ ] **Step 2: Build**

Run:

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/LibraryShell.vue
git commit -m "feat(web): add library shell"
```

---

## Task 4: Album Components

**Files:**
- Create: `web/src/components/AlbumGrid.vue`
- Create: `web/src/components/AlbumDetail.vue`

- [ ] **Step 1: Create album grid**

Create `web/src/components/AlbumGrid.vue`:

```vue
<template>
  <section class="album-grid-panel">
    <div v-if="loading" class="empty-state">Loading albums...</div>
    <div v-else-if="albums.length === 0" class="empty-state">No albums yet. Scan your music library first.</div>
    <div v-else class="album-grid">
      <button
        v-for="album in albums"
        :key="album.id"
        class="album-card"
        :class="{ active: selectedAlbumId === album.id }"
        type="button"
        @click="$emit('select', album.id)"
      >
        <img
          v-if="album.cover_url"
          :src="album.cover_url"
          alt=""
          class="album-cover"
          @error="hideBrokenImage"
        />
        <div v-else class="album-cover placeholder-cover">{{ initials(album.title) }}</div>
        <span class="album-title">{{ album.title }}</span>
        <span class="album-meta">{{ album.artist || 'Unknown artist' }} · {{ album.track_count }} tracks</span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { AlbumSummary } from '../api/client'

defineProps<{
  albums: AlbumSummary[]
  selectedAlbumId: string
  loading: boolean
}>()

defineEmits<{
  select: [id: string]
}>()

function initials(value: string) {
  return value.trim().slice(0, 2).toUpperCase() || 'LY'
}

function hideBrokenImage(event: Event) {
  const image = event.target as HTMLImageElement
  image.style.display = 'none'
}
</script>
```

- [ ] **Step 2: Create album detail**

Create `web/src/components/AlbumDetail.vue`:

```vue
<template>
  <aside class="detail-panel">
    <div v-if="!album" class="empty-state">Select an album to see tracks.</div>
    <template v-else>
      <div class="detail-header">
        <img v-if="album.cover_url" :src="album.cover_url" alt="" class="detail-cover" @error="hideBrokenImage" />
        <div v-else class="detail-cover placeholder-cover">{{ album.title.slice(0, 2).toUpperCase() }}</div>
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
import type { AlbumDetail, TrackSummary } from '../api/client'

defineProps<{
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

function hideBrokenImage(event: Event) {
  const image = event.target as HTMLImageElement
  image.style.display = 'none'
}
</script>
```

- [ ] **Step 3: Build**

Run:

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/AlbumGrid.vue web/src/components/AlbumDetail.vue
git commit -m "feat(web): add album browser components"
```

---

## Task 5: Artist, Search, Scan, and Player Components

**Files:**
- Create: `web/src/components/ArtistBrowser.vue`
- Create: `web/src/components/SearchPanel.vue`
- Create: `web/src/components/ScanPanel.vue`
- Create: `web/src/components/PlayerBar.vue`

- [ ] **Step 1: Create artist browser**

Create `web/src/components/ArtistBrowser.vue`:

```vue
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
            <img v-if="album.cover_url" :src="album.cover_url" alt="" class="album-cover" @error="hideBrokenImage" />
            <div v-else class="album-cover placeholder-cover">{{ album.title.slice(0, 2).toUpperCase() }}</div>
            <span class="album-title">{{ album.title }}</span>
            <span class="album-meta">{{ album.track_count }} tracks</span>
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ArtistDetail, ArtistSummary } from '../api/client'

defineProps<{
  artists: ArtistSummary[]
  selectedArtist: ArtistDetail | null
}>()

defineEmits<{
  'select-artist': [id: string]
  'select-album': [id: string]
}>()

function hideBrokenImage(event: Event) {
  const image = event.target as HTMLImageElement
  image.style.display = 'none'
}
</script>
```

- [ ] **Step 2: Create search panel**

Create `web/src/components/SearchPanel.vue`:

```vue
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
        <button v-for="track in results.tracks" :key="track.id" class="track-row" type="button" @click="$emit('play-track', track)">
          <span class="track-title">{{ track.title }}</span>
          <span class="muted">{{ track.artist }} · {{ track.album }}</span>
        </button>
      </div>

      <div v-if="results.albums.length">
        <h4>Albums</h4>
        <button v-for="album in results.albums" :key="album.id" class="result-row" type="button" @click="$emit('select-album', album.id)">
          <span>{{ album.title }}</span>
          <span class="muted">{{ album.artist }}</span>
        </button>
      </div>

      <div v-if="results.artists.length">
        <h4>Artists</h4>
        <button v-for="artist in results.artists" :key="artist.id" class="result-row" type="button" @click="$emit('select-artist', artist.id)">
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
  return props.results.tracks.length === 0 && props.results.albums.length === 0 && props.results.artists.length === 0
})
</script>
```

- [ ] **Step 3: Create scan panel**

Create `web/src/components/ScanPanel.vue`:

```vue
<template>
  <section class="scan-panel">
    <div class="scan-card">
      <div>
        <p class="eyebrow">Scanner</p>
        <h3>{{ status.running ? 'Scan running' : 'Scanner idle' }}</h3>
        <p class="muted">Processed {{ status.processed }} of {{ status.total }} files · {{ status.errors }} errors</p>
      </div>
      <n-button type="primary" :loading="triggering" :disabled="status.running" @click="$emit('trigger')">
        Start scan
      </n-button>
    </div>

    <n-progress
      :percentage="percentage"
      :processing="status.running"
      :status="status.errors > 0 ? 'warning' : 'success'"
    />

    <dl class="scan-stats">
      <div><dt>Total</dt><dd>{{ status.total }}</dd></div>
      <div><dt>Processed</dt><dd>{{ status.processed }}</dd></div>
      <div><dt>Errors</dt><dd>{{ status.errors }}</dd></div>
      <div><dt>Started</dt><dd>{{ startedAt }}</dd></div>
    </dl>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NProgress } from 'naive-ui'
import type { ScanStatus } from '../api/client'

const props = defineProps<{
  status: ScanStatus
  triggering: boolean
}>()

defineEmits<{
  trigger: []
}>()

const percentage = computed(() => {
  if (!props.status.total) return 0
  return Math.min(100, Math.round((props.status.processed / props.status.total) * 100))
})

const startedAt = computed(() => {
  if (!props.status.started_at || props.status.started_at.startsWith('0001-')) return '-'
  return new Date(props.status.started_at).toLocaleString()
})
</script>
```

- [ ] **Step 4: Create player bar**

Create `web/src/components/PlayerBar.vue`:

```vue
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
```

- [ ] **Step 5: Build**

Run:

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/ArtistBrowser.vue web/src/components/SearchPanel.vue web/src/components/ScanPanel.vue web/src/components/PlayerBar.vue
git commit -m "feat(web): add library panels and player"
```

---

## Task 6: Wire App State

**Files:**
- Replace: `web/src/App.vue`

- [ ] **Step 1: Replace App.vue**

Replace `web/src/App.vue` with:

```vue
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
        <n-alert v-if="globalError" class="global-alert" type="error" :bordered="false" closable @close="globalError = ''">
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
  AlbumDetail,
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
const selectedAlbum = ref<AlbumDetail | null>(null)
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
```

- [ ] **Step 2: Build**

Run:

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/App.vue
git commit -m "feat(web): wire frontend app state"
```

---

## Task 7: App Styling

**Files:**
- Replace: `web/src/style.css`

- [ ] **Step 1: Replace CSS**

Replace `web/src/style.css` with:

```css
:root {
  --bg: #101214;
  --panel: #171a1f;
  --panel-2: #1f242b;
  --panel-3: #262c35;
  --text: #e6eaee;
  --muted: #9aa4ae;
  --border: #303741;
  --accent: #2f7d72;
  --accent-2: #d6a85c;
  --danger: #d76363;
  font-family:
    Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: var(--text);
  background: var(--bg);
  color-scheme: dark;
  font-synthesis: none;
  text-rendering: optimizeLegibility;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  min-width: 320px;
  min-height: 100vh;
  background: var(--bg);
}

button {
  font: inherit;
}

#app {
  min-height: 100vh;
}

.muted {
  color: var(--muted);
}

.eyebrow {
  margin: 0 0 4px;
  color: var(--accent-2);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}

.login-screen {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background:
    linear-gradient(rgba(16, 18, 20, 0.72), rgba(16, 18, 20, 0.92)),
    url("./assets/hero.png") center/cover;
}

.login-panel {
  width: min(420px, 100%);
  display: grid;
  gap: 24px;
  padding: 28px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: rgba(23, 26, 31, 0.92);
  box-shadow: 0 24px 70px rgba(0, 0, 0, 0.32);
}

.login-panel h1,
.topbar h2,
.detail-header h3,
.artist-albums h3,
.scan-card h3,
.search-panel h3 {
  margin: 0;
}

.login-panel h1 {
  font-size: 46px;
  line-height: 1;
}

.login-form {
  display: grid;
  gap: 10px;
}

.app-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  background: var(--bg);
}

.sidebar {
  border-right: 1px solid var(--border);
  background: #121519;
  padding: 18px 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.brand {
  margin-bottom: 14px;
  font-size: 18px;
  font-weight: 800;
  text-align: center;
}

.nav-button {
  min-height: 62px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--muted);
  display: grid;
  place-items: center;
  gap: 4px;
  cursor: pointer;
}

.nav-button.active,
.nav-button:hover {
  color: var(--text);
  background: var(--panel-2);
  border-color: var(--border);
}

.nav-icon {
  font-size: 20px;
}

.workspace {
  min-width: 0;
  min-height: 100vh;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto;
}

.topbar {
  min-height: 74px;
  padding: 14px 22px;
  border-bottom: 1px solid var(--border);
  background: rgba(16, 18, 20, 0.94);
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(220px, 420px) auto auto;
  align-items: center;
  gap: 14px;
}

.search-form {
  min-width: 0;
}

.content-grid {
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 380px;
  gap: 0;
  overflow: hidden;
}

.global-alert {
  position: fixed;
  top: 88px;
  right: 24px;
  z-index: 20;
  width: min(420px, calc(100vw - 48px));
}

.album-grid-panel,
.artist-browser,
.search-panel,
.scan-panel {
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: 22px;
}

.album-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(156px, 1fr));
  gap: 18px;
}

.album-card {
  min-width: 0;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--text);
  text-align: left;
  padding: 10px;
  display: grid;
  gap: 8px;
  cursor: pointer;
}

.album-card:hover,
.album-card.active {
  background: var(--panel);
  border-color: var(--border);
}

.album-card.compact {
  padding: 8px;
}

.album-cover {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 6px;
  background: var(--panel-2);
}

.placeholder-cover {
  display: grid;
  place-items: center;
  color: var(--text);
  font-weight: 800;
  background: linear-gradient(135deg, #315f57, #684f65);
}

.album-title,
.track-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.album-title {
  font-weight: 700;
}

.album-meta {
  color: var(--muted);
  font-size: 13px;
}

.detail-panel {
  min-height: 0;
  overflow: auto;
  border-left: 1px solid var(--border);
  background: var(--panel);
  padding: 22px;
}

.detail-header {
  display: grid;
  grid-template-columns: 116px minmax(0, 1fr);
  gap: 16px;
  align-items: end;
  margin-bottom: 22px;
}

.detail-cover {
  width: 116px;
  aspect-ratio: 1;
  border-radius: 8px;
  object-fit: cover;
}

.track-list,
.search-results,
.artist-list {
  display: grid;
  gap: 8px;
}

.track-row,
.result-row,
.artist-row {
  width: 100%;
  min-width: 0;
  min-height: 42px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--text);
  display: grid;
  align-items: center;
  gap: 10px;
  cursor: pointer;
}

.track-row {
  grid-template-columns: 34px minmax(0, 1fr) auto;
}

.result-row,
.artist-row {
  grid-template-columns: minmax(0, 1fr) auto;
  padding: 0 10px;
  text-align: left;
}

.track-row:hover,
.result-row:hover,
.artist-row:hover,
.artist-row.active {
  background: var(--panel-2);
  border-color: var(--border);
}

.track-number,
.track-duration {
  color: var(--muted);
  font-size: 13px;
}

.artist-browser {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: 20px;
}

.artist-list {
  align-content: start;
}

.artist-albums {
  min-width: 0;
}

.compact-album-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 14px;
  margin-top: 16px;
}

.search-panel,
.scan-panel {
  grid-column: 1 / -1;
}

.panel-header,
.scan-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}

.search-results h4 {
  margin: 20px 0 10px;
  color: var(--muted);
}

.scan-card {
  padding: 18px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}

.scan-stats {
  margin: 22px 0 0;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.scan-stats div {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}

.scan-stats dt {
  color: var(--muted);
  font-size: 12px;
}

.scan-stats dd {
  margin: 6px 0 0;
  font-size: 20px;
  font-weight: 800;
}

.empty-state {
  min-height: 180px;
  display: grid;
  place-items: center;
  color: var(--muted);
  border: 1px dashed var(--border);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.02);
}

.player-bar {
  min-height: 78px;
  border-top: 1px solid var(--border);
  background: #121519;
  padding: 12px 18px;
  display: grid;
  grid-template-columns: minmax(220px, 360px) minmax(0, 1fr);
  gap: 20px;
  align-items: center;
}

.now-playing {
  min-width: 0;
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr);
  gap: 12px;
  align-items: center;
}

.play-indicator {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  background: var(--panel-2);
  color: var(--accent-2);
}

.audio-control {
  width: 100%;
}

@media (max-width: 900px) {
  .app-shell {
    grid-template-columns: 1fr;
  }

  .sidebar {
    border-right: 0;
    border-bottom: 1px solid var(--border);
    flex-direction: row;
    align-items: center;
    overflow-x: auto;
  }

  .brand {
    margin: 0 12px 0 0;
  }

  .nav-button {
    min-height: 44px;
    min-width: 92px;
    grid-auto-flow: column;
  }

  .topbar {
    grid-template-columns: 1fr;
  }

  .content-grid {
    grid-template-columns: 1fr;
  }

  .detail-panel {
    border-left: 0;
    border-top: 1px solid var(--border);
  }

  .artist-browser {
    grid-template-columns: 1fr;
  }

  .scan-stats,
  .player-bar {
    grid-template-columns: 1fr;
  }
}
```

- [ ] **Step 2: Build**

Run:

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/style.css
git commit -m "feat(web): style frontend MVP"
```

---

## Task 8: Verification and Embedded Build

**Files:**
- No source changes expected unless verification reveals issues.

- [ ] **Step 1: Run frontend build**

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 2: Run full project build**

```bash
make build
```

Expected: PASS. If `ui/dist/.gitkeep` is deleted by Vite, restore it with `touch ui/dist/.gitkeep` after build.

- [ ] **Step 3: Run Go tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Start development servers for manual review**

In one terminal:

```bash
make dev-backend
```

In another terminal:

```bash
make dev-frontend
```

Expected: backend on `http://localhost:4533`, frontend on `http://localhost:5173`.

- [ ] **Step 5: Manual smoke checklist**

Use `http://localhost:5173`:

- Login failure shows an error when credentials are wrong.
- With a valid token or `auth.disable=true`, album grid loads.
- Search shows grouped results.
- Clicking a track updates the bottom player.
- Scan view displays scan status and trigger button.

- [ ] **Step 6: Commit verification fixes if any**

If code changes were needed during verification:

```bash
git add web/src ui/dist/.gitkeep
git commit -m "fix(web): address frontend MVP verification issues"
```

If no code changes were needed, do not create an empty commit.

---

## Self-Review

**Spec coverage:**
- Login and token storage: Tasks 1, 2, 6.
- Album browsing and details: Tasks 4, 6, 7.
- Artist browsing: Tasks 5, 6, 7.
- Search: Tasks 5, 6, 7.
- Player: Tasks 5, 6, 7.
- Scan status and trigger: Tasks 5, 6, 7.
- Verification: Task 8.

**Known limitation:** Media and cover URLs still use direct browser requests and cannot attach Bearer headers. This matches the approved spec; a backend auth strategy is out of scope for this plan.
