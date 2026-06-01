# Lyrics Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add backend lyrics CRUD APIs and automatically import same-name `.lrc` sidecar files during library scans.

**Architecture:** Store lyrics in the existing `lyrics` table keyed by `track_id`. Add an authenticated track lyrics handler under `/api/v1/tracks/{id}/lyrics`, and extend scanner ingestion to read a `.lrc` file next to each audio file after the track row is upserted.

**Tech Stack:** Go `net/http`, chi router, SQLite, existing scanner/ingester package, Go tests.

---

### Task 1: Lyrics API Tests

**Files:**
- Create: `internal/api/v1/lyrics_test.go`

- [ ] **Step 1: Write failing tests for GET/PUT/DELETE lyrics**

Create tests that instantiate `NewLyricsHandler`, use existing `insertTestData`, and verify:
- `GET` returns a saved lyrics row.
- `GET` returns `404` when the track exists but no lyrics row exists.
- `PUT` upserts manual lyrics and returns the saved response.
- `PUT` rejects an empty payload with `400`.
- `PUT` returns `404` for a missing track.
- `DELETE` removes a lyrics row and returns `204`.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/api/v1 -run 'TestLyrics' -count=1
```

Expected: build failure because `NewLyricsHandler` is not implemented yet.

### Task 2: Lyrics API Implementation

**Files:**
- Create: `internal/api/v1/lyrics.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Implement `LyricsHandler`**

Add:
- `LyricsResponse`
- `LyricsRequest`
- `NewLyricsHandler`
- `GetLyrics`
- `PutLyrics`
- `DeleteLyrics`

Behavior:
- All operations first require `tracks.id = ? AND tracks.is_available = 1`.
- `GET` returns `404` for missing track or missing lyrics.
- `PUT` trims `source`, defaults empty source to `manual`, and requires at least one non-empty `lrc_content` or `yrc_content`.
- `PUT` uses SQLite upsert into `lyrics`.
- `DELETE` is idempotent after track existence is verified and returns `204`.

- [ ] **Step 2: Register routes**

Add these under authenticated `/api/v1`:

```go
lyrics := v1.NewLyricsHandler(db)
r.Get("/tracks/{id}/lyrics", lyrics.GetLyrics)
r.Put("/tracks/{id}/lyrics", lyrics.PutLyrics)
r.Delete("/tracks/{id}/lyrics", lyrics.DeleteLyrics)
```

- [ ] **Step 3: Run lyrics API tests**

Run:

```bash
go test ./internal/api/v1 -run 'TestLyrics' -count=1
```

Expected: pass.

### Task 3: Same-Name `.lrc` Import Tests

**Files:**
- Modify: `internal/scanner/ingester_test.go`

- [ ] **Step 1: Write failing tests for sidecar lyrics import**

Add tests that verify:
- Ingesting `/music/a.flac` imports `/music/a.lrc` into `lyrics.lrc_content` with source `sidecar`.
- Re-ingesting a track with no `.lrc` file does not delete a previously saved lyrics row.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/scanner -run 'TestIngest_ImportsSidecarLyrics|TestIngest_DoesNotDeleteLyricsWhenSidecarMissing' -count=1
```

Expected: fail because sidecar lyrics are not imported.

### Task 4: Same-Name `.lrc` Import Implementation

**Files:**
- Modify: `internal/scanner/ingester.go`

- [ ] **Step 1: Return track ID from track upsert**

Change `upsertTrack` to return the inserted or existing track ID. Since the statement already uses `ON CONFLICT(file_path)`, select the row by `file_path` after the upsert.

- [ ] **Step 2: Read sidecar `.lrc`**

After `upsertTrack`, check for `strings.TrimSuffix(meta.FilePath, filepath.Ext(meta.FilePath)) + ".lrc"`. If it exists and can be read, upsert into `lyrics(track_id,lrc_content,source,updated_at)` with source `sidecar`. If it does not exist, leave any existing lyrics untouched.

- [ ] **Step 3: Run scanner tests**

Run:

```bash
go test ./internal/scanner -run 'TestIngest_ImportsSidecarLyrics|TestIngest_DoesNotDeleteLyricsWhenSidecarMissing' -count=1
```

Expected: pass.

### Task 5: Full Verification

**Files:**
- Verify all touched Go packages.

- [ ] **Step 1: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: pass.

- [ ] **Step 2: Review git diff**

Run:

```bash
git diff --stat
git status --short
```

Expected: only lyrics backend, scanner import, and plan/test files are changed.
