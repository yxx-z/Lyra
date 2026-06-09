export type ViewMode = 'albums' | 'artists' | 'scan'

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
  phase: string
  lyrics_scraped: number
  albums_scraped: number
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

  scrapeAlbum(albumId: string) {
    return this.request<AlbumScrapeResponse>(`/api/v1/albums/${encodeURIComponent(albumId)}/scrape`, {
      method: 'POST',
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
