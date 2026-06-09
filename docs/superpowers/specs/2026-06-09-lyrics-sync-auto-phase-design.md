# 自动同步歌词升级阶段设计文档

> 版本：1.0 · 日期：2026-06-09 · 状态：已批准

---

## 目标

扫描时自动把纯文本歌词的曲目升级为 LRCLIB 同步歌词（复用已有 `UpgradeToSynced`），并用 `lyrics.sync_checked` 标记避免对无同步版的曲目反复查询 LRCLIB。

承接「纯文本→同步歌词升级」（已实现按需按钮 + `UpgradeToSynced`），本次补「自动扫描阶段」。

---

## 范围

**本次做：**
- 迁移 004：`lyrics.sync_checked` 列 + 索引
- `LyricsService.UpgradeStaleLyrics(ctx)` 批处理（查候选 → 升级 → 标记）
- 扫描器新增「升级同步」阶段（歌词刮削后、指纹前）
- 前端 ScanPanel 阶段标签 + 计数

**本次不做（YAGNI）：**
- `sync_checked` 在手动编辑/重刮后自动复位（用户可用已有按钮重试）
- 前端新按钮（复用已有「升级为同步歌词」按钮）

---

## 迁移 004

`internal/db/migrations/004_lyrics_sync_checked.up.sql`：
```sql
ALTER TABLE lyrics ADD COLUMN sync_checked INTEGER DEFAULT 0;
CREATE INDEX idx_lyrics_sync_checked ON lyrics(sync_checked);
```
`schema.sql` 同步加列与索引。`sync_checked=0` 表示尚未自动升级检查；`1` 表示已检查（无论是否找到同步版）。

---

## LyricsService.UpgradeStaleLyrics

```go
// UpgradeStaleLyrics 批量把未检查的纯文本歌词升级为同步版，返回成功升级数。
func (s *LyricsService) UpgradeStaleLyrics(ctx context.Context) (int, error)
```

**流程：**
```
1. 查候选：
   SELECT track_id, COALESCE(lrc_content,'') FROM lyrics
   WHERE sync_checked=0 AND COALESCE(yrc_content,'')='' AND TRIM(COALESCE(lrc_content,''))<>''
   收集 (id, lrc)，关闭 rows，检查 rows.Err()
2. 逐条（每轮先 select{ctx.Done(): return upgraded, nil; default}）：
   a. hasTimestamps(lrc) 为 true（已是同步，理论上不该是候选，防御）：
        markSyncChecked(id)；continue（不查网络、不节流）
   b. 纯文本：
        out, err := s.UpgradeToSynced(ctx, id)
        err != nil（瞬时网络/provider 异常）→ 记日志、不置标记、不计数（下次扫描重试）、不节流跳过
        out.Status=="upgraded" → markSyncChecked(id)；upgraded++
        out.Status=="no_synced" → markSyncChecked(id)
        节流：select{ <-time.After(800ms): ; <-ctx.Done(): return upgraded, nil }（仅在 b 真打了网络后）
3. 返回 upgraded
```

**辅助：**
```go
func (s *LyricsService) markSyncChecked(trackID string) error // UPDATE lyrics SET sync_checked=1 WHERE track_id=?
```

**设计取舍：** 用批处理方法（而非扫描器自循环）——候选筛选涉及 `hasTimestamps`、`sync_checked` 等 lyrics 内部细节，放 lyrics 包更内聚；ctx 中断/节流在方法内，`Stop()` 照常生效。复用既有 `hasTimestamps`、`UpgradeToSynced`（后者跳过 embedded、仅同步替换）。

`ErrTrackNotFound`：候选来自 lyrics 表 JOIN 不到的极少数情况，`UpgradeToSynced` 会返回它——视为瞬时/跳过（不置标记，记日志），不中断批处理。

---

## 扫描器集成（`internal/scanner/scanner.go`）

- `ScanStatus` 加 `LyricsUpgraded int64 json:"lyrics_upgraded"`；`Scanner` 加 `lyricsUpgraded atomic.Int64`；`Status()` 返回填充；`doScan` 重置区 `s.lyricsUpgraded.Store(0)`。
- `doScan` 在歌词刮削阶段（`scrapePending`）之后、指纹阶段之前插入：
```go
	if s.scrapeEnabled && s.services.Lyrics != nil {
		s.phase.Store("lyrics_sync")
		if n, err := s.services.Lyrics.UpgradeStaleLyrics(ctx); err != nil {
			s.errors.Add(1)
		} else {
			s.lyricsUpgraded.Store(int64(n))
		}
	}
```
阶段顺序变为：歌词刮削(scraping) → **升级同步(lyrics_sync)** → 指纹(fingerprint) → 元数据(metadata) → idle。门控同歌词阶段（`scrapeEnabled && services.Lyrics != nil`）。

---

## 前端（`ScanPanel.vue` + `client.ts` + `App.vue`）

- `client.ts` `ScanStatus` 类型加 `lyrics_upgraded: number`。
- `App.vue` 的 `reactive<ScanStatus>({...})` 初始化器加 `lyrics_upgraded: 0`。
- `ScanPanel.vue`：`phaseLabel` 加 `case 'lyrics_sync': return '升级同步歌词中'`；统计区加一张卡「已升级同步：{{ status.lyrics_upgraded }}」（与已刮歌词/专辑/指纹并列）。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 迁移：lyrics 有 sync_checked 列 | `go test ./internal/db/...` |
| UpgradeStaleLyrics：纯文本+有同步源 → 升级、sync_checked=1、upgraded=1 | 内存 sqlite + mock provider |
| UpgradeStaleLyrics：纯文本+无同步 → no_synced、sync_checked=1、upgraded=0 | mock |
| UpgradeStaleLyrics：已同步的候选 → 直接 sync_checked=1、不调 provider | mock（calls 计数=0） |
| UpgradeStaleLyrics：瞬时错误 → 不置 sync_checked、可重试 | mock 返回普通 error |
| UpgradeStaleLyrics：sync_checked=1 的行不被处理 | 预置 checked=1，断言不动 |
| 扫描器 lyrics_sync 阶段：调用 UpgradeStaleLyrics、LyricsUpgraded 计数 | 内存 sqlite + 真实 LyricsService(mock provider) |
| 前端 | `make build-frontend`（vue-tsc + vite）通过 |

**全部 mock/桩，不打真网络。**

---

## 不在本次范围内

- `sync_checked` 自动复位
- 前端新按钮 / 单独触发
- 网易云同步源
