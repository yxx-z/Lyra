# 纯文本歌词升级为同步歌词设计文档

> 版本：1.0 · 日期：2026-06-09 · 状态：已批准

---

## 目标

当曲目当前歌词是纯文本（无时间轴，多为内嵌标签歌词）时，提供按需操作去 LRCLIB 查同步版（带时间轴 LRC），**仅在找到同步版时替换**，否则保持原样。

对应 PRD：US-24（歌词同步显示）的补强。

**可行性已验证**：LRCLIB 对周杰伦《晴天》《东风破》有同步歌词；蔡琴《魂萦旧梦》无同步（仅纯文本）。覆盖率不均，故设计为"找到同步才替换、否则 no-op"。

---

## 范围

**本次做：**
- `LyricsService.UpgradeToSynced(trackID)`：跳过 embedded、向网络源(lrclib)查同步版，命中才替换
- `hasTimestamps(lrc)` 辅助判断 LRC 是否带时间轴
- 接口 `POST /api/v1/tracks/{id}/lyrics/upgrade`
- 前端歌词面板纯文本模式下的「升级为同步歌词」按钮

**本次不做（YAGNI）：**
- 自动升级阶段（本轮仅按需）
- 网易云（冻结中）
- 手动编辑/粘贴歌词

---

## 后端

### hasTimestamps（`internal/lyrics` 包内辅助）

```go
// hasTimestamps 判断 LRC 文本是否含 [mm:ss] 时间轴（即同步歌词）。
func hasTimestamps(lrc string) bool
```
- 正则 `\[\d+:\d+`（匹配 `[0:00`/`[00:00.00` 等）；命中即视为同步。

### LyricsService.UpgradeToSynced

```go
// UpgradeOutcome 报告升级结果。
type UpgradeOutcome struct {
    Status string // "upgraded" | "no_synced"
    Source string
}

func (s *LyricsService) UpgradeToSynced(ctx context.Context, trackID string) (UpgradeOutcome, error)
```

**流程：**
```
1. loadTrack(trackID)；不存在 → ErrTrackNotFound
2. 构造 Query（title/artist/album/duration/file_path，同 ScrapeTrack）
3. 遍历 s.providers，跳过 p.Name()=="embedded"：
     res, ferr := p.Fetch(ctx, q)
     ferr 为 ErrNotFound/ErrInvalidQuery → continue
     其它 ferr → 返回该 error（provider 异常）
     成功：若“同步”（strings.TrimSpace(res.YRCContent)!="" 或 hasTimestamps(res.LRCContent)）：
         saveLyrics(trackID, res)        // 复用现有 upsert，覆盖原纯文本
         updateStatus(trackID, "done")
         返回 {Status:"upgraded", Source:res.Source}
     否则（命中但仍是纯文本）→ continue
4. 无任何同步版 → 返回 {Status:"no_synced"}（不修改现有歌词）
```

**说明：** 跳过 embedded 是因为它返回的正是要替换的纯文本；网络源（lrclib，将来 netease）才可能给同步版。复用 `loadTrack`/`saveLyrics`/`updateStatus`。

### HTTP 接口

`POST /api/v1/tracks/{id}/lyrics/upgrade`（给现有 `ScrapeHandler` 加方法 `UpgradeLyrics`，它已持有 `*LyricsService`）：
```
outcome, err := service.UpgradeToSynced(r.Context(), trackID)
switch {
  err is ErrTrackNotFound   → 404
  err != nil（provider 异常） → 502「同步歌词升级失败」
  其它（upgraded/no_synced） → 200 {track_id, status, source}
}
```
路由在 router.go 注册（紧邻现有 `/tracks/{id}/scrape`）。响应结构复用 `ScrapeResponse`（track_id/status/source）。

---

## 前端 `LyricsPanel.vue`

- 当前为**纯文本静态模式**（`!synced.value && lrcLines.value.length > 0`，即既有纯文本展示分支）时，在歌词区上方显示「⏱ 升级为同步歌词」按钮（复用 `.loading-spinner` + currentColor）。
- `handleUpgrade()`：
  ```
  upgrading=true
  try:
    res = await api.upgradeLyrics(track.trackId)
    if res.status==="upgraded": await loadLyrics()  // 重新拉取→变同步→滚动
                                upgradeMessage="已升级为同步歌词"
    else（no_synced）:          upgradeMessage="未找到同步版本"
  catch 404: upgradeMessage="未找到同步版本"
  catch 其它: upgradeMessage="升级失败，请重试"
  finally: upgrading=false
  ```
- 切歌时重置 `upgradeMessage`（并入现有 watch）。
- `client.ts`：加 `upgradeLyrics(trackId)` → `POST /tracks/{id}/lyrics/upgrade`，复用/新增响应类型（含 `status`/`source`）。

不影响"完全无歌词"分支（仍是现有「获取歌词」按钮）。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| hasTimestamps：带 `[mm:ss]` → true；纯文本 → false | 纯函数表测 |
| UpgradeToSynced：mock provider 返回同步 LRC → 替换、upgraded | 内存 sqlite + mock provider |
| UpgradeToSynced：provider 只返回纯文本 → no_synced、原歌词不变 | mock |
| UpgradeToSynced：跳过 embedded（embedded mock 不被调用 / 即便返回纯文本也跳过） | mock 两 provider，断言取 lrclib 同步结果 |
| UpgradeToSynced：YRC 命中也算同步 | mock 返回 YRCContent |
| UpgradeToSynced：曲目不存在 → ErrTrackNotFound | 内存 sqlite |
| HTTP upgrade：200(upgraded/no_synced)/404/502 各分支 | httptest |
| 前端 | `make build-frontend`（vue-tsc + vite）通过 |

**全部 mock/桩，不打真网络。**

---

## 不在本次范围内

- 自动升级扫描阶段
- 网易云同步/YRC 源
- 手动编辑歌词 UI
