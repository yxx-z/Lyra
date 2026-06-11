export type ViewMode = 'albums' | 'artists' | 'scan' | 'favorites' | 'playlists' | 'settings'

export type AlbumSummary = {
  id: string
  title: string
  artist: string
  artist_id: string
  year: number
  genre: string
  release_date: string
  track_count: number
  cover_url: string
  starred?: boolean
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
  starred?: boolean
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
  starred?: boolean
}

export type AlbumResult = {
  id: string
  title: string
  artist: string
  cover_url: string
  starred?: boolean
}

export type FavTrack = {
  id: string
  title: string
  album: string
  album_id: string
  artist: string
  duration: number
  stream_url: string
  cover_url: string
}

export type FavAlbum = {
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
  phase: string
  lyrics_scraped: number
  albums_scraped: number
  fingerprinted: number
  lyrics_upgraded: number
}

export type PlayerTrack = {
  trackId: string
  title: string
  artist?: string
  album?: string
  streamUrl: string
}

export type LyricsResponse = {
  track_id: string
  lrc_content: string
  yrc_content: string
  source: string
  updated_at: string
  has_lrc: boolean
  has_yrc: boolean
}

export type LyricsPayload = {
  lrc_content?: string
  yrc_content?: string
  source?: string
}

export type ScrapeResponse = {
  track_id: string
  status: string
  source?: string
  message?: string
}

export type AlbumScrapeResponse = {
  album_id: string
  status: string
  mbid?: string
  has_cover: boolean
}

// ── 歌单类型 ──────────────────────────────────────────────
export type PlaylistSummary = { id: string; name: string; comment: string; song_count: number; duration: number; created: string; changed: string }
export type PlaylistDetail = PlaylistSummary & { tracks: FavTrack[] }

export type AdminUser = {
  id: string
  username: string
  isAdmin: boolean
  hasSubsonicPassword: boolean
  createdAt: string
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

  getSetupStatus(): Promise<{ needsSetup: boolean }> {
    return this.request<{ needsSetup: boolean }>('/api/v1/setup/status', { auth: false })
  }

  async setup(username: string, password: string): Promise<string> {
    const data = await this.request<{ token: string }>('/api/v1/setup', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
      headers: { 'Content-Type': 'application/json' },
      auth: false,
    })
    this.token = data.token
    return data.token
  }

  getMe(): Promise<{ username: string; isAdmin: boolean }> {
    return this.request<{ username: string; isAdmin: boolean }>('/api/v1/auth/me')
  }

  changePassword(oldPassword: string, newPassword: string): Promise<void> {
    return this.request<void>('/api/v1/account/password', {
      method: 'POST',
      body: JSON.stringify({ oldPassword, newPassword }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  setSubsonicPassword(password: string): Promise<void> {
    return this.request<void>('/api/v1/account/subsonic-password', {
      method: 'POST',
      body: JSON.stringify({ password }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  async logout() {
    await this.request<{ ok: boolean }>('/api/v1/auth/logout', {
      method: 'POST',
      auth: false,
    })
    this.token = null
  }

  refreshSession() {
    return this.request<{ ok: boolean }>('/api/v1/auth/session', { method: 'POST' })
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

  getLyrics(trackId: string) {
    return this.request<LyricsResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics`)
  }

  saveLyrics(trackId: string, payload: LyricsPayload) {
    return this.request<LyricsResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics`, {
      method: 'PUT',
      body: JSON.stringify(payload),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  deleteLyrics(trackId: string) {
    return this.request<void>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics`, {
      method: 'DELETE',
    })
  }

  scrapeTrack(trackId: string) {
    return this.request<ScrapeResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/scrape`, {
      method: 'POST',
    })
  }

  upgradeLyrics(trackId: string) {
    return this.request<ScrapeResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics/upgrade`, {
      method: 'POST',
    })
  }

  scrapeAlbum(albumId: string) {
    return this.request<AlbumScrapeResponse>(`/api/v1/albums/${encodeURIComponent(albumId)}/scrape`, {
      method: 'POST',
    })
  }

  // ── 注册 ──────────────────────────────────────────────────
  getRegisterStatus(): Promise<{ allowRegistration: boolean }> {
    return this.request<{ allowRegistration: boolean }>('/api/v1/register/status', { auth: false })
  }

  async register(username: string, password: string): Promise<string> {
    const data = await this.request<{ token: string }>('/api/v1/register', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
      headers: { 'Content-Type': 'application/json' },
      auth: false,
    })
    this.token = data.token
    return data.token
  }

  // ── 管理员：用户管理 ──────────────────────────────────────
  listUsers(): Promise<{ users: AdminUser[] }> {
    return this.request<{ users: AdminUser[] }>('/api/v1/admin/users')
  }

  createUser(username: string, password: string, isAdmin: boolean): Promise<void> {
    return this.request<void>('/api/v1/admin/users', {
      method: 'POST',
      body: JSON.stringify({ username, password, isAdmin }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  deleteUser(id: string): Promise<void> {
    return this.request<void>(`/api/v1/admin/users/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }

  resetUserPassword(id: string, password: string): Promise<void> {
    return this.request<void>(`/api/v1/admin/users/${encodeURIComponent(id)}/password`, {
      method: 'POST',
      body: JSON.stringify({ password }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  setUserRole(id: string, isAdmin: boolean): Promise<void> {
    return this.request<void>(`/api/v1/admin/users/${encodeURIComponent(id)}/role`, {
      method: 'POST',
      body: JSON.stringify({ isAdmin }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  // ── 管理员：全局设置 ──────────────────────────────────────
  getAdminSettings(): Promise<{ allowRegistration: boolean }> {
    return this.request<{ allowRegistration: boolean }>('/api/v1/admin/settings')
  }

  setAdminSettings(allowRegistration: boolean): Promise<void> {
    return this.request<void>('/api/v1/admin/settings', {
      method: 'POST',
      body: JSON.stringify({ allowRegistration }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  // ── 收藏与播放统计 ──────────────────────────────────────
  star(type: 'song' | 'album' | 'artist', id: string): Promise<void> {
    return this.request<void>('/api/v1/star', {
      method: 'POST',
      body: JSON.stringify({ type, id }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  unstar(type: 'song' | 'album' | 'artist', id: string): Promise<void> {
    return this.request<void>('/api/v1/unstar', {
      method: 'POST',
      body: JSON.stringify({ type, id }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  scrobble(trackId: string): Promise<void> {
    return this.request<void>(`/api/v1/tracks/${encodeURIComponent(trackId)}/scrobble`, {
      method: 'POST',
    })
  }

  getStarStatus(type: 'song' | 'album' | 'artist', id: string): Promise<{ starred: boolean }> {
    return this.request<{ starred: boolean }>(
      `/api/v1/star?type=${encodeURIComponent(type)}&id=${encodeURIComponent(id)}`,
      { method: 'GET' },
    )
  }

  getFavorites(): Promise<{ tracks: FavTrack[]; albums: FavAlbum[] }> {
    return this.request<{ tracks: FavTrack[]; albums: FavAlbum[] }>('/api/v1/favorites')
  }

  getRecentlyPlayed(): Promise<{ tracks: FavTrack[] }> {
    return this.request<{ tracks: FavTrack[] }>('/api/v1/recently-played')
  }

  getMostPlayed(): Promise<{ tracks: FavTrack[] }> {
    return this.request<{ tracks: FavTrack[] }>('/api/v1/most-played')
  }

  // ── 歌单 ──────────────────────────────────────────────
  listPlaylists(): Promise<{ playlists: PlaylistSummary[] }> {
    return this.request<{ playlists: PlaylistSummary[] }>('/api/v1/playlists')
  }

  createPlaylist(name: string): Promise<{ id: string }> {
    return this.request<{ id: string }>('/api/v1/playlists', {
      method: 'POST',
      body: JSON.stringify({ name }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  getPlaylist(id: string): Promise<PlaylistDetail> {
    return this.request<PlaylistDetail>(`/api/v1/playlists/${encodeURIComponent(id)}`)
  }

  updatePlaylist(id: string, patch: { name?: string; comment?: string }): Promise<void> {
    return this.request<void>(`/api/v1/playlists/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  deletePlaylist(id: string): Promise<void> {
    return this.request<void>(`/api/v1/playlists/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }

  addToPlaylist(id: string, trackIds: string[]): Promise<void> {
    return this.request<void>(`/api/v1/playlists/${encodeURIComponent(id)}/tracks`, {
      method: 'POST',
      body: JSON.stringify({ trackIds }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  setPlaylistTracks(id: string, trackIds: string[]): Promise<void> {
    return this.request<void>(`/api/v1/playlists/${encodeURIComponent(id)}/tracks`, {
      method: 'PUT',
      body: JSON.stringify({ trackIds }),
      headers: { 'Content-Type': 'application/json' },
    })
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

    if (response.status === 204) {
      return undefined as T
    }

    const text = await response.text()
    return (text ? JSON.parse(text) : undefined) as T
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
