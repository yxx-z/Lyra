# 网易云歌词 + YRC 逐字歌词设计文档

> 版本：1.0 · 日期：2026-06-08 · 状态：**实现冻结（未合并）**

---

> ## ⚠️ 实现状态：冻结于分支 `feat/netease-yrc`（未合并 master）
>
> 实现已完成（eapi 加密、搜索、取词、YRC 解析、前端逐字扫光，全部带测试），但**联调验证发现致命限制后决定不合并**：
>
> - **匿名访问网易云的搜索只返回 UGC 翻唱，正版授权曲库（周杰伦、蔡琴等华语流行）被版权门控、搜不到**。换加密 eapi 与非加密老接口结果一致，证实是版权门控而非端点问题。按 id 直接取词能拿到正版歌词，但搜索发现不了正版 id，而刮削必须靠搜索。
> - 因此匿名 netease 对最主流的正版华语歌**零产出**；YRC 本身也极稀有（连周杰伦原版《晴天》都无 yrc 字段）。
> - 要解锁正版搜索需用户提供登录 Cookie（MUSIC_U），属大改动且脆弱，暂不做。
>
> **复活条件**：实现网易云登录态（MUSIC_U Cookie）支持后，本分支代码可直接复用。
> 仅「纯文本歌词逐行展示」修复已 cherry-pick 进 master（commit `81649ae`）。

## 目标

1. **新增网易云 Provider**：在 Go 内实现网易云加密请求层（零外部依赖），接入 `internal/lyrics` 既有 Provider 链。
2. **YRC 逐字歌词**：拉取网易云 YRC（逐字时间轴），后端解析为归一化 JSON 存入 `yrc_content`。
3. **前端逐字扫光渲染**：歌词面板按 YRC 实现 Apple Music 式逐字渐变填充（karaoke）。

对应 PRD：US-25（YRC 逐字歌词，随网易云 API 同期上线，v0.3）、4.3（歌词来源优先级）。

---

## 范围

**本次做：**
- `internal/lyrics/netease.go`：NeteaseProvider（实现既有 `Provider` 接口）
- `internal/lyrics/netease_crypto.go`：网易云 eapi 加密请求层
- `internal/lyrics/netease_yrc.go`：YRC 解析器（原始 YRC → 归一化 JSON）
- 链顺序调整为 `embedded → netease → lrclib`，按 `netease.enabled` 条件接入
- 前端 LyricsPanel YRC 逐字扫光渲染

**本次不做（YAGNI）：**
- 翻译歌词（tlyric）/ 罗马音渲染
- 手动编辑 YRC 的前端 UI
- 其它逐字源（QQ 音乐等）

---

## 关键技术前提：YRC 走 eapi，不是 weapi

逐字 YRC 只能从网易云 `song/lyric/v1` 接口获取，该接口走 **eapi** 加密（AES-ECB + digest），不是 weapi。weapi 的老 `song/lyric` 只返回普通 LRC 和老式 klyric，无 YRC。

因此加密层实现 **eapi**：搜索（cloudsearch）和取词（song/lyric/v1）均走 eapi。算法为公开成熟的 AES-ECB（key `e82ckenh8dichen8`）+ MD5 digest，封装在 `netease_crypto.go`，对 Provider 暴露一个「POST eapi 接口」的薄函数。

**风险说明**：网易云为非官方接口，可能因风控/算法变更失效。所有失败路径均回落为 `ErrNotFound`（链继续到 lrclib）或普通 error，绝不中断扫描刮削阶段。整源由 `scraper.netease.enabled` 开关控制。

---

## 文件结构

```
internal/lyrics/
├── provider.go         不变（Provider 接口 + Query/Result + 错误类型）
├── embedded.go         不变
├── lrclib.go           不变
├── service.go          不变（空内容防御已兼容 yrc-only：TrimSpace(LRC)=="" && YRC=="" 才跳过）
├── netease.go          新增：NeteaseProvider
├── netease_crypto.go   新增：eapi 加密请求层
└── netease_yrc.go      新增：YRC 解析器

internal/api/router.go  改：按 netease.enabled 条件构造并插入 NeteaseProvider
cmd/server/main.go      改：同上，链顺序 embedded → netease → lrclib

web/src/components/LyricsPanel.vue  改：YRC 逐字扫光渲染
```

---

## NeteaseProvider（netease.go）

实现既有接口：
```go
func (p *NeteaseProvider) Name() string { return "netease" }
func (p *NeteaseProvider) Fetch(ctx context.Context, q Query) (Result, error)
```

构造：`NewNeteaseProvider(httpClient *http.Client)`（httpClient 为 nil 时用默认 10s 超时；便于测试注入 httptest）。

### Fetch 流程

```
1. q.TrackName / q.ArtistName 为空 → ErrInvalidQuery
2. 搜索：eapi cloudsearch 查 "曲名 艺术家"（type=1 歌曲，limit=10）
     → 候选 songs[]：{id, name, artists[].name, duration(ms)}
3. 匹配（见下）：选中一首 → song.id；无满足 → ErrNotFound
4. 取词：eapi song/lyric/v1 取 song.id 的 {lrc.lyric, yrc.lyric}
5. yrc 非空 → 解析为归一化 JSON（netease_yrc.go）→ YRCContent
   lrc 非空 → LRCContent（网易云普通 LRC，原样）
   两者皆空 → ErrNotFound
6. 返回 Result{LRCContent, YRCContent, Source:"netease"}
```

网络/JSON 解码/加解密异常 → 返回普通 error（非 ErrNotFound），由 HTTP 层映射 502；扫描刮削阶段只累加 errors 不中断。

### 匹配逻辑

```
归一化（normalize）：去首尾空格、转小写、全角转半角、去除多余空白
对每个候选 song：
  时长差 = |本地 q.Duration(秒) - song.duration/1000|
  标题命中 = normalize(song.name) 与 normalize(q.TrackName) 互相包含
  若 时长差 ≤ 3 且 标题命中 → 选中（取第一个满足者）
都不满足 → ErrNotFound
```

`q.Duration` 已是秒（tracks.duration 为整数秒）。候选 duration 为毫秒，需 /1000。

---

## YRC 解析器（netease_yrc.go）

### 原始格式

网易云 YRC 每行：
```
[行起ms,行长ms](字起ms,字长ms,0)字(字起ms,字长ms,0)字...
```
开头可能混入 `{"t":0,"c":[{"tx":"作词: ..."}]}` 等 JSON 元信息行 —— **丢弃**（无 `[ms,ms]` 行级时间头的行一律跳过）。

### 归一化 JSON（存入 yrc_content，时间单位=秒，float）

```json
{"lines":[
  {"start":12.1,"end":15.1,"words":[
    {"start":12.1,"end":12.4,"text":"作"},
    {"start":12.4,"end":12.7,"text":"词"}
  ]}
]}
```

- `line.start` = 行起 / 1000；`line.end` = (行起 + 行长) / 1000
- `word.start` = 字起 / 1000；`word.end` = (字起 + 字长) / 1000
- 解析签名：`func parseYRC(raw string) (string, error)` 返回归一化 JSON 字符串；无任何有效歌词行 → 返回空串（Provider 据此判定无 yrc）

### 解析健壮性

- 容忍行尾换行/空行
- 单行正则提取行头 `\[(\d+),(\d+)\]` 与逐字块 `\((\d+),(\d+),\d+\)([^(]*)`
- 任意行解析失败：跳过该行而非整体失败（尽力解析）

---

## 链与开关

- 顺序：`embedded → netease → lrclib`（命中即停）
- netease 命中时 `Result` 同时填 `LRCContent`（网易云 lrc）与 `YRCContent`（归一化 yrc）；前端有 yrc 用 yrc、无则退回 lrc
- `cfg.Scraper.Netease.Enabled == false` 时**不**把 NeteaseProvider 加入链
- 两处构造点必须同步：`cmd/server/main.go` 与 `internal/api/router.go`

构造示意（main.go / router.go 一致）：
```go
providers := []lyrics.Provider{lyrics.NewEmbeddedProvider()}
if cfg.Scraper.Netease.Enabled {
    providers = append(providers, lyrics.NewNeteaseProvider(nil))
}
providers = append(providers, lyrics.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil))
lyricsService := lyrics.NewLyricsService(database, providers...)
```

---

## 前端渲染（LyricsPanel.vue）

### 数据加载（loadLyrics）

```
res = getLyrics(trackId)
若 res.has_yrc 且 res.yrc_content：
    yrcData = JSON.parse(res.yrc_content)  // {lines:[{start,end,words:[{start,end,text}]}]}
    mode = 'yrc'
否则 res.has_lrc 且 res.lrc_content：
    lrcLines = parseLrc(...)               // 现有逻辑（含纯文本回退）
    mode = 'lrc' | 'plain'
否则：error = 'no_lyrics'
```

JSON.parse 失败 → 降级当作无 yrc，退回 lrc/纯文本路径（不抛错）。

### 逐字扫光（Apple Music 式）

- 当前行索引：遍历 yrc lines，`currentTime >= line.start` 的最后一行（与现有 syncLyricsIndex 同思路）
- 当前行内每个字算填充百分比：
  - `currentTime <= word.start` → 0%
  - `currentTime >= word.end` → 100%
  - 之间 → `(currentTime - word.start) / (word.end - word.start) * 100`
- 渲染：每个字一个 span，用 `background: linear-gradient(已唱色, 未唱色)` + `background-clip:text; -webkit-text-fill-color:transparent`，按填充 % 控制渐变断点，实现从左到右扫光
- 非当前行：整行未唱态（暗淡），不逐字
- 点击行 → seek 到 `line.start`（复用现有 seek 路径）
- 填充百分比计算抽成纯函数 `wordFillPercent(word, currentTime)` 便于推理

### 模式选择优先级

`yrc`（逐字扫光）> `lrc`（现有逐行高亮）> `plain`（纯文本静态）> `no_lyrics`（兜底 + 获取歌词按钮）。三种「有歌词」模式互斥渲染。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| YRC 解析：正常多行多字 → 期望 JSON | 纯函数单测，样例串硬编码 |
| YRC 解析：含 JSON 元信息行 → 被丢弃 | 断言输出不含元信息 |
| YRC 解析：空 / 无有效行 → 返回空串 | 边界 |
| 匹配：时长差 ≤3 且标题含 → 选中 | 表驱动 |
| 匹配：时长差 >3 或标题不含 → ErrNotFound | 表驱动 |
| Provider.Fetch：httptest 灌伪造 search+lyric 响应 → 命中返回 lrc+yrc | 不打真网络，注入 httpClient |
| Provider.Fetch：搜索无匹配 → ErrNotFound | httptest |
| Provider.Fetch：lyric 返回空 lrc+yrc → ErrNotFound | httptest |
| Provider.Fetch：网络异常 → 普通 error | httptest 返回 500 |
| eapi 加密：已知输入 → 稳定输出（往返/向量） | 单测（不依赖网络） |
| 前端 | `vue-tsc` 构建通过；wordFillPercent 纯函数逻辑清晰 |

---

## 错误语义（沿用既有约定）

- `ErrInvalidQuery` — 缺曲名/艺术家
- `ErrNotFound` — 搜索无匹配 / 取词为空 → 链继续到下一 provider
- 普通 error（网络/加解密/解码）→ 链向上传播，HTTP 层 502；扫描阶段只累加 errors，不中断
- netease 全程不影响已入库曲目与其它 provider

---

## 不在本次范围内

- 翻译歌词 / 罗马音
- 手动编辑/粘贴 YRC UI
- QQ 音乐等其它逐字源
- 网易云登录态 / 会员专享歌词（仅取公开歌词）
