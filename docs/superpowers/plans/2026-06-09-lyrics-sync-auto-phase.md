# 自动同步歌词升级阶段 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 扫描时批量把纯文本歌词升级为 LRCLIB 同步歌词，用 `lyrics.sync_checked` 标记防止对无同步版的曲目反复查询。

**Architecture:** 迁移加 `lyrics.sync_checked`。`LyricsService.UpgradeStaleLyrics(ctx)` 批量查未检查的纯文本歌词、复用 `UpgradeToSynced` 升级、标记 sync_checked、800ms 节流可中断。扫描器新增 `lyrics_sync` 阶段（歌词刮削后、指纹前）。前端 ScanPanel 显示阶段与计数。

**Tech Stack:** Go 1.25（regexp, modernc.org/sqlite）、Vue 3 + TypeScript。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-09-lyrics-sync-auto-phase-design.md`。

**关键既有代码：**
- `internal/lyrics/upgrade.go`：`hasTimestamps(lrc) bool`、`UpgradeToSynced(ctx, trackID) (UpgradeOutcome, error)`（跳过 embedded、仅同步替换；`UpgradeOutcome{Status:"upgraded"|"no_synced", Source}`；missing→ErrTrackNotFound）。当前 import：`context errors regexp strings`。
- `internal/lyrics/service.go`：`LyricsService{db, providers}`。
- `internal/lyrics/service_test.go`（同包）：`fakeProvider{name,result,err,calls *int}`、`newServiceTestDB(t) *sql.DB`（内存库 + track `t1`，无 lyrics 行）。
- `lyrics` 表：`track_id, lrc_content, yrc_content, source, updated_at`（迁移 004 后 + `sync_checked`）。
- `internal/db/migrations/`：001/002/003 存在，下一个 **004**。`db_test.go` 有列存在性测试范例。
- `internal/scanner/scanner.go`：`ScanStatus` 有 `LyricsScraped/AlbumsScraped/Fingerprinted`。`doScan` 阶段顺序当前 scraping → fingerprint → metadata（约 226-238 行），三个 `if s.scrapeEnabled && s.services.X != nil` 块。`fingerprintPending` 等是可直接测试的方法。
- `internal/scanner/scanner_test.go`：`scanStubProvider{res lyrics.Result; err error}`（`Name()=="stub"`），用 `lyrics.NewLyricsService(d, scanStubProvider{...})` + `NewScanner(d, cfg, "", ScrapeServices{Lyrics: svc}, true)`。
- 前端：`client.ts` `ScanStatus` 类型（有 phase/lyrics_scraped/albums_scraped/fingerprinted）；`App.vue` `reactive<ScanStatus>({...})` 初始化器；`ScanPanel.vue` `phaseLabel` computed + 统计卡。

---

## 文件结构

```
internal/db/migrations/004_lyrics_sync_checked.up.sql   新建
internal/db/schema.sql                                  改：lyrics 加 sync_checked + 索引
internal/db/db_test.go                                  改：加列存在性测试
internal/lyrics/upgrade.go                              改：加 UpgradeStaleLyrics + markSyncChecked
internal/lyrics/upgrade_test.go                         改：加 UpgradeStaleLyrics 测试
internal/scanner/scanner.go                             改：ScanStatus.LyricsUpgraded + lyrics_sync 阶段 + upgradeLyricsPending
internal/scanner/scanner_test.go                        改：加阶段测试
web/src/api/client.ts                                   改：ScanStatus 加 lyrics_upgraded
web/src/App.vue                                         改：scanStatus 初始化器加字段
web/src/components/ScanPanel.vue                        改：phaseLabel + 统计卡
```

---

### Task 1: 迁移 004 — lyrics.sync_checked

**Files:** Create `internal/db/migrations/004_lyrics_sync_checked.up.sql`; Modify `internal/db/schema.sql`, `internal/db/db_test.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/db/db_test.go` 末尾追加：
```go
func TestOpen_LyricsHasSyncCheckedColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name FROM pragma_table_info('lyrics') WHERE name='sync_checked'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("lyrics 表应有 sync_checked 列")
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -run TestOpen_LyricsHasSyncCheckedColumn -v`
Expected: FAIL（列不存在）。

- [ ] **Step 3: 写迁移 + 同步 schema**

创建 `internal/db/migrations/004_lyrics_sync_checked.up.sql`：
```sql
ALTER TABLE lyrics ADD COLUMN sync_checked INTEGER DEFAULT 0;
CREATE INDEX idx_lyrics_sync_checked ON lyrics(sync_checked);
```
在 `internal/db/schema.sql` 的 lyrics 表定义里，`updated_at` 行之后加 `sync_checked`，并在表后加索引。把：
```sql
CREATE TABLE lyrics (
    track_id    TEXT PRIMARY KEY REFERENCES tracks(id),
    lrc_content TEXT,
    yrc_content TEXT,
    source      TEXT,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```
改为：
```sql
CREATE TABLE lyrics (
    track_id     TEXT PRIMARY KEY REFERENCES tracks(id),
    lrc_content  TEXT,
    yrc_content  TEXT,
    source       TEXT,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    sync_checked INTEGER DEFAULT 0
);
CREATE INDEX idx_lyrics_sync_checked ON lyrics(sync_checked);
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -v`
Expected: PASS（全部 db 测试，含迁移幂等）。

- [ ] **Step 5: 提交**
```bash
git add internal/db/migrations/004_lyrics_sync_checked.up.sql internal/db/schema.sql internal/db/db_test.go
git commit -m "feat(db): lyrics 加 sync_checked 列（迁移 004）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: UpgradeStaleLyrics 批处理

**Files:** Modify `internal/lyrics/upgrade.go`, `internal/lyrics/upgrade_test.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/lyrics/upgrade_test.go` 追加（`fakeProvider`/`newServiceTestDB` 同包可用）：
```go
func TestUpgradeStaleLyrics_UpgradesPlainText(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:01.00]同步", Source: "lrclib"}}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 1 {
		t.Errorf("应升级 1 首，得到 %d", n)
	}
	var got string
	var checked int
	d.QueryRow(`SELECT lrc_content, sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&got, &checked)
	if got != "[00:01.00]同步" || checked != 1 {
		t.Errorf("应替换为同步且置 checked=1，得到 lrc=%q checked=%d", got, checked)
	}
}

func TestUpgradeStaleLyrics_NoSyncedMarksChecked(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "还是纯文本", Source: "lrclib"}}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("无同步版应升级 0 首，得到 %d", n)
	}
	var got string
	var checked int
	d.QueryRow(`SELECT lrc_content, sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&got, &checked)
	if got != "纯文本一行" || checked != 1 {
		t.Errorf("原文应保留但置 checked=1，得到 lrc=%q checked=%d", got, checked)
	}
}

func TestUpgradeStaleLyrics_AlreadySyncedMarksOnly(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','[00:01.00]已同步','lrclib',0)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:02.00]x", Source: "lrclib"}, calls: &calls}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("已同步不计升级，得到 %d", n)
	}
	if calls != 0 {
		t.Errorf("已同步不应调 provider，实际 %d 次", calls)
	}
	var checked int
	d.QueryRow(`SELECT sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&checked)
	if checked != 1 {
		t.Errorf("已同步也应置 checked=1，得到 %d", checked)
	}
}

func TestUpgradeStaleLyrics_TransientErrorNoMark(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	lrc := &fakeProvider{name: "lrclib", err: errors.New("network boom")}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("批处理本身不应报错: %v", err)
	}
	if n != 0 {
		t.Errorf("瞬时错误升级 0 首，得到 %d", n)
	}
	var checked int
	d.QueryRow(`SELECT sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&checked)
	if checked != 0 {
		t.Errorf("瞬时错误不应置 checked，得到 %d", checked)
	}
}

func TestUpgradeStaleLyrics_SkipsAlreadyChecked(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',1)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:01.00]x", Source: "lrclib"}, calls: &calls}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 || calls != 0 {
		t.Errorf("已 checked 的不应处理，得到 n=%d calls=%d", n, calls)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run TestUpgradeStaleLyrics -v`
Expected: 编译失败（`undefined: UpgradeStaleLyrics`）。

- [ ] **Step 3: 实现** — 在 `internal/lyrics/upgrade.go` 顶部 import 加 `"log/slog"` 和 `"time"`：
```go
import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"
)
```
追加到文件末尾：
```go
// markSyncChecked 标记某曲目已做过自动同步升级检查。
func (s *LyricsService) markSyncChecked(trackID string) {
	if _, err := s.db.Exec(`UPDATE lyrics SET sync_checked=1 WHERE track_id=?`, trackID); err != nil {
		slog.Warn("标记 sync_checked 失败", "track", trackID, "err", err)
	}
}

// UpgradeStaleLyrics 批量把未检查的纯文本歌词升级为同步版，返回成功升级数。
// 已同步的候选只标记不联网；无同步版也标记（避免反复查询）；瞬时错误不标记（下次重试）。
func (s *LyricsService) UpgradeStaleLyrics(ctx context.Context) (int, error) {
	rows, err := s.db.Query(`
		SELECT track_id, COALESCE(lrc_content,'') FROM lyrics
		WHERE sync_checked=0 AND COALESCE(yrc_content,'')='' AND TRIM(COALESCE(lrc_content,''))<>''`)
	if err != nil {
		return 0, err
	}
	type cand struct{ id, lrc string }
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.lrc); err == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(cands) == 0 {
		return 0, nil
	}
	slog.Info("开始后台同步歌词升级", "待检查", len(cands))

	upgraded := 0
	for _, c := range cands {
		select {
		case <-ctx.Done():
			return upgraded, nil
		default:
		}
		if hasTimestamps(c.lrc) {
			// 已是同步歌词：只标记，不联网
			s.markSyncChecked(c.id)
			continue
		}
		out, err := s.UpgradeToSynced(ctx, c.id)
		if err != nil {
			// 瞬时错误（网络/provider 异常）：不标记，下次扫描重试
			slog.Warn("同步歌词升级失败", "track", c.id, "err", err)
			continue
		}
		s.markSyncChecked(c.id)
		if out.Status == "upgraded" {
			upgraded++
		}
		// 仅在真打了网络后节流（LRCLIB 限速礼貌）
		select {
		case <-time.After(800 * time.Millisecond):
		case <-ctx.Done():
			return upgraded, nil
		}
	}
	slog.Info("后台同步歌词升级结束", "升级", upgraded)
	return upgraded, nil
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -v`
Expected: PASS（本包全部）。

- [ ] **Step 5: 提交**
```bash
git add internal/lyrics/upgrade.go internal/lyrics/upgrade_test.go
git commit -m "feat(lyrics): UpgradeStaleLyrics 批量升级 + sync_checked 标记"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: 扫描器 lyrics_sync 阶段

**Files:** Modify `internal/scanner/scanner.go`, `internal/scanner/scanner_test.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/scanner/scanner_test.go` 追加（`scanStubProvider` 同文件可用；其 `Name()=="stub"` 不会被 UpgradeToSynced 跳过）：
```go
func TestUpgradeLyricsPending_CountsUpgraded(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,file_path,format,duration,is_available,scrape_status) VALUES('t1','歌','a1','/tmp/x.mp3','mp3',200,1,'done')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	svc := lyrics.NewLyricsService(d, scanStubProvider{res: lyrics.Result{LRCContent: "[00:01.00]同步", Source: "lrclib"}})
	s := NewScanner(d, config.LibraryConfig{}, "", ScrapeServices{Lyrics: svc}, true)

	s.upgradeLyricsPending(context.Background())

	if got := s.Status().LyricsUpgraded; got != 1 {
		t.Errorf("LyricsUpgraded = %d, want 1", got)
	}
	var lrc string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&lrc)
	if lrc != "[00:01.00]同步" {
		t.Errorf("应升级为同步，得到 %q", lrc)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/scanner/ -run TestUpgradeLyricsPending -v`
Expected: 编译失败（`s.upgradeLyricsPending` / `Status().LyricsUpgraded` 未定义）。

- [ ] **Step 3: 改 scanner.go**

a) `ScanStatus` 加字段（`Fingerprinted` 之后）：
```go
	Fingerprinted int64 `json:"fingerprinted"`
	LyricsUpgraded int64 `json:"lyrics_upgraded"`
```

b) `Scanner` 计数器区加 `lyricsUpgraded atomic.Int64`（`fingerprinted atomic.Int64` 旁）。

c) `Status()` 返回加 `LyricsUpgraded: s.lyricsUpgraded.Load(),`（`Fingerprinted` 之后）。

d) `doScan` 重置区加 `s.lyricsUpgraded.Store(0)`；并在歌词刮削阶段块（`scrapePending`）之后、指纹阶段块之前插入：
```go
	if s.scrapeEnabled && s.services.Lyrics != nil {
		s.phase.Store("scraping")
		s.scrapePending(ctx)
	}
	if s.scrapeEnabled && s.services.Lyrics != nil {
		s.phase.Store("lyrics_sync")
		s.upgradeLyricsPending(ctx)
	}
	if s.scrapeEnabled && s.services.Fingerprint != nil {
		s.phase.Store("fingerprint")
		s.fingerprintPending(ctx)
	}
```
（即在现有 scraping 块与 fingerprint 块之间插入 lyrics_sync 块；metadata 块保持在 fingerprint 之后不变。）

e) 文件末尾追加：
```go
func (s *Scanner) upgradeLyricsPending(ctx context.Context) {
	n, err := s.services.Lyrics.UpgradeStaleLyrics(ctx)
	if err != nil {
		s.errors.Add(1)
		return
	}
	s.lyricsUpgraded.Store(int64(n))
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/scanner/ -v 2>&1 | tail -15`
Expected: PASS（含新测试；约 0.8s 节流一次）。

- [ ] **Step 5: 提交**
```bash
git add internal/scanner/scanner.go internal/scanner/scanner_test.go
git commit -m "feat(scanner): lyrics_sync 阶段（歌词→升级同步→指纹→元数据）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: 前端 ScanPanel 阶段 + 计数

**Files:** Modify `web/src/api/client.ts`, `web/src/App.vue`, `web/src/components/ScanPanel.vue`

- [ ] **Step 1: client.ts ScanStatus 加字段** — 把 `ScanStatus` 类型的 `fingerprinted: number` 之后加：
```ts
  fingerprinted: number
  lyrics_upgraded: number
```

- [ ] **Step 2: App.vue 初始化器加字段** — 在 `reactive<ScanStatus>({...})` 的 `fingerprinted: 0,` 之后加：
```ts
  fingerprinted: 0,
  lyrics_upgraded: 0,
```

- [ ] **Step 3: ScanPanel.vue — phaseLabel + 统计卡**

`phaseLabel` computed 的 switch 里，`case 'metadata':` 之后、`default:` 之前加：
```ts
    case 'lyrics_sync':
      return '升级同步歌词中'
```
统计网格里「已识别指纹」卡之后加：
```html
      <div class="scan-stats-card">
        <dt>已升级同步</dt>
        <dd style="color: var(--accent);">{{ status.lyrics_upgraded }}</dd>
      </div>
```

- [ ] **Step 4: 构建确认通过** — `make build-frontend`
Expected: vue-tsc + vite 通过。

- [ ] **Step 5: 提交**
```bash
git add web/src/api/client.ts web/src/App.vue web/src/components/ScanPanel.vue
git commit -m "feat(web): 扫描面板显示升级同步歌词阶段与计数"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功；`make build-frontend` 成功
- 扫描时纯文本歌词曲目自动尝试升级：有同步版替换、无同步版置 sync_checked=1（不再反复查）、瞬时错误下次重试
- 阶段顺序 歌词 → 升级同步 → 指纹 → 元数据
- 前端面板显示「升级同步歌词中」+「已升级同步」计数
- 全部测试 mock/桩，不打真网络

## 验证（手动，docker）

1. `make docker-build && docker compose up -d`
2. 重置 `UPDATE lyrics SET sync_checked=0` 后触发扫描
3. 日志阶段经历 …→ 升级同步 → 指纹 → 元数据；周杰伦纯文本歌词应升级为带时间轴
4. 查 `SELECT track_id,sync_checked FROM lyrics`：均为 1（已检查）
