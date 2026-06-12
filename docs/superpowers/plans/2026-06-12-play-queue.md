# 播放队列（全部播放 / 下一首播放 / 队列面板）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 列表"全部播放"+点某首连播、单曲"下一首播放"、播放条右侧"播放队列"面板（看/跳播/移除/拖拽重排）。

**Architecture:** 纯前端。`stores/player.ts` 加 `playNext`/`removeFromQueue`/`moveInQueue`；列表组件加全部播放并改为整列表入队连播（复用 `playTrack(item, queue)`）；新增 `PlayNextButton` 小组件挂曲目行；`PlayerBar` 右侧加队列入口 → 新 `QueuePanel`。

**Tech Stack:** Vue 3 + Pinia（无 JS 测试框架）。验证 = `make build-frontend`（vue-tsc 类型检查）+ `go build ./...` + 真机。

**关键约束：**
- 无前端单测：每个任务以"实现 → `make build-frontend && go build ./...` 通过(零 TS 错误) → 真机手测"为准。Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。
- 队列项类型 `ExtendedPlayerTrack`（含 `trackId,title,artist,album,streamUrl,coverUrl`）。`playTrack(track, newQueue)` 已能替换队列并按 trackId 定位起播。
- `git` 命令用仓库根：`cd /home/yxx/develop/Lyra && git ...`（注意 shell cwd 可能在 web/src）。

---

## File Structure

```
web/src/stores/player.ts                 改：playNext/removeFromQueue/moveInQueue + 导出
web/src/components/QueuePanel.vue         新：播放队列面板（看/跳播/移除/拖拽）
web/src/components/PlayerBar.vue          改：右侧「播放队列」按钮 + 渲染 QueuePanel
web/src/components/PlayNextButton.vue     新：单曲「下一首播放」小按钮
web/src/components/PlaylistsPage.vue      改：全部播放 + play-list + 行内 PlayNextButton
web/src/components/FavoritesPanel.vue     改：每 tab 全部播放 + play-list + 行内 PlayNextButton
web/src/components/AlbumDetail.vue        改：曲目行 PlayNextButton
web/src/components/SearchPanel.vue        改：曲目行 PlayNextButton
web/src/App.vue                            改：onPlayList；给 PlaylistsPage/FavoritesPanel 接 @play-list
```

---

## Task 1: 播放 store 队列动作

**Files:** Modify `web/src/stores/player.ts`

- [ ] **Step 1: 实现三个动作** — 在 `player.ts` 的 `playAtIndex`/`next` 等函数附近新增：
```ts
  // 下一首播放：插到当前曲之后（不打断当前）；队列空则直接开播。
  function playNext(item: ExtendedPlayerTrack) {
    if (queue.value.length === 0) {
      playTrack(item, [item])
      return
    }
    queue.value.splice(currentIndex.value + 1, 0, item)
  }

  // 从队列移除某项（UI 不对当前曲提供移除）。保持 currentTrack 指向不变。
  function removeFromQueue(index: number) {
    if (index < 0 || index >= queue.value.length || index === currentIndex.value) return
    queue.value.splice(index, 1)
    if (index < currentIndex.value) currentIndex.value--
  }

  // 队列内拖拽重排：用对象引用重定位 currentIndex，保证正在播的曲不被打断。
  function moveInQueue(from: number, to: number) {
    if (from < 0 || from >= queue.value.length || to < 0 || to >= queue.value.length || from === to) return
    const cur = queue.value[currentIndex.value]
    const [moved] = queue.value.splice(from, 1)
    queue.value.splice(to, 0, moved)
    currentIndex.value = queue.value.indexOf(cur)
  }
```

- [ ] **Step 2: 导出** — 在文件末尾 `return { ... }` 块里加 `playNext, removeFromQueue, moveInQueue`（放在 `playAtIndex, next, prev,` 附近）。

- [ ] **Step 3: 构建验证**
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...
```
Expected: 无 TS 错误；Go 构建通过。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src/stores/player.ts && git commit -m "feat(web): 播放 store 加 playNext/removeFromQueue/moveInQueue"
```

---

## Task 2: 列表"全部播放" + 点某首连播

**Files:** Modify `web/src/App.vue`、`web/src/components/PlaylistsPage.vue`、`web/src/components/FavoritesPanel.vue`

> 先读三个文件：App.vue 的 `onFavPlay`/`<PlaylistsPage>`/`<FavoritesPanel>` 渲染处；PlaylistsPage 的 `play-track` emit（曲目行 line ~81）与详情头部;FavoritesPanel 的三个 tab 列表(favTracks/recentTracks/topTracks)与 `play-track` emit。

- [ ] **Step 1: App.vue 加 onPlayList** — 新增（`FavTrack` 已 import）：
```ts
function onPlayList(tracks: FavTrack[], startIndex: number) {
  if (!tracks.length) return
  const queue = tracks.map((t) => ({
    trackId: t.id,
    title: t.title,
    artist: t.artist,
    album: t.album,
    streamUrl: t.stream_url,
    coverUrl: t.cover_url,
  }))
  const start = startIndex >= 0 && startIndex < queue.length ? startIndex : 0
  playerStore.playTrack(queue[start], queue)
}
```
（保留 `onFavPlay` 不删；下面把列表改用 `@play-list`。）

- [ ] **Step 2: PlaylistsPage.vue** —
  - `defineEmits` 增 `(e: 'play-list', tracks: FavTrack[], startIndex: number): void`（保留或移除原 `play-track`，下面不再用它）。
  - 详情区（`selected` 存在时）标题旁加「▶ 全部播放」按钮：`<button class="custom-btn-primary" v-if="selected && selected.tracks.length" @click="$emit('play-list', selected.tracks, 0)">▶ 全部播放</button>`。
  - 把曲目行的播放触发由 `@click="$emit('play-track', t)"` 改为 `@click="$emit('play-list', selected.tracks, i)"`（`i` 为 `v-for="(t, i) in selected.tracks"` 的下标——确认 v-for 带了索引 `i`，没有就加）。
- [ ] **Step 3: FavoritesPanel.vue** —
  - `defineEmits` 增 `(e: 'play-list', tracks: FavTrack[], startIndex: number): void`。
  - 三个 tab 的列表各自顶部加「▶ 全部播放」按钮，传对应数组：收藏 tab → `favTracks`，最近 → `recentTracks`，最常听 → `topTracks`。例：`<button class="custom-btn-primary" v-if="favTracks.length" @click="$emit('play-list', favTracks, 0)">▶ 全部播放</button>`（每 tab 一个，传各自数组）。
  - 把每个列表行的播放触发 `@click="$emit('play-track', track)"`（含 keydown.enter/space 那几处）改为 `@click="$emit('play-list', <该tab数组>, i)"`（给 `v-for` 补 `(track, i)` 索引；keydown 同步改）。
- [ ] **Step 4: App.vue 接线** — `<PlaylistsPage ... @play-list="onPlayList" />`、`<FavoritesPanel ... @play-list="onPlayList" />`（保留原 `@play-track` 也可，但播放改走 play-list）。

- [ ] **Step 5: 构建验证** — `make build-frontend && go build ./...` → 通过。

- [ ] **Step 6: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 歌单/收藏列表全部播放 + 点某首连播整列表"
```

---

## Task 3: 单曲「下一首播放」按钮

**Files:** Create `web/src/components/PlayNextButton.vue`；Modify `AlbumDetail.vue`、`SearchPanel.vue`、`FavoritesPanel.vue`、`PlaylistsPage.vue`

- [ ] **Step 1: 创建 PlayNextButton.vue**：
```vue
<template>
  <button class="play-next-btn" type="button" title="下一首播放" @click.stop="add">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <polygon points="5 4 15 12 5 20 5 4" />
      <line x1="19" y1="5" x2="19" y2="19" />
    </svg>
    <span v-if="tip" class="pn-tip">{{ tip }}</span>
  </button>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { usePlayerStore } from '../stores/player'

const props = defineProps<{
  trackId: string
  title: string
  artist?: string
  album?: string
  streamUrl: string
  coverUrl?: string
}>()

const player = usePlayerStore()
const tip = ref('')

function add() {
  player.playNext({
    trackId: props.trackId,
    title: props.title,
    artist: props.artist ?? '',
    album: props.album ?? '',
    streamUrl: props.streamUrl,
    coverUrl: props.coverUrl ?? '',
  })
  tip.value = '已设为下一首'
  setTimeout(() => (tip.value = ''), 1500)
}
</script>

<style scoped>
.play-next-btn { position: relative; background: none; border: none; cursor: pointer; color: var(--text-dim, rgba(255,255,255,0.4)); padding: 2px 6px; border-radius: 4px; transition: color 0.15s; }
.play-next-btn:hover { color: var(--accent, #6ee7b7); }
.play-next-btn svg { width: 15px; height: 15px; }
.pn-tip { position: absolute; right: 0; top: 100%; margin-top: 4px; font-size: 11px; color: var(--success, #30a46c); white-space: nowrap; }
</style>
```
> `ExtendedPlayerTrack` 还有其它可选字段；`playNext` 接收的对象只需上述核心字段即可（与 onFavPlay/onPlayList 构造的形状一致）。若 TS 因 `ExtendedPlayerTrack` 有额外必填字段报错，按其定义补齐或将对象 `as ExtendedPlayerTrack`（先读 `stores/player.ts` 的 `ExtendedPlayerTrack`/`PlayerTrack` 定义确认字段）。

- [ ] **Step 2: 挂到曲目行** — import `PlayNextButton`，在各曲目行的操作区（红心/添加到歌单旁）加 `<PlayNextButton ... />`，按各组件曲目字段构造 props：
  - **AlbumDetail.vue**（`v-for="track in album.tracks"`，有 `album` 上下文）：
    `<PlayNextButton :track-id="track.id" :title="track.title" :artist="album.artist" :album="album.title" :stream-url="track.stream_url" :cover-url="album.cover_url" />`
  - **SearchPanel.vue**（曲目结果，先读其曲目字段名；通常有 id/title/artist/album/stream_url，封面可用 `'/api/v1/cover/' + albumId` 或留空）：
    `<PlayNextButton :track-id="t.id" :title="t.title" :artist="t.artist" :album="t.album" :stream-url="t.stream_url" />`
  - **FavoritesPanel.vue / PlaylistsPage.vue**（FavTrack）：
    `<PlayNextButton :track-id="track.id" :title="track.title" :artist="track.artist" :album="track.album" :stream-url="track.stream_url" :cover-url="track.cover_url" />`
  注意放在行内"操作区"且不要触发整行播放（按钮已 `@click.stop`；若行是 `<button>` 会有 button-in-button，需把这些行的操作区放在播放区之外的兄弟容器——参照这些组件此前红心/AddToPlaylist 的放法,它们已是 `div[role=button]` 播放区 + 兄弟操作区结构,照挂即可）。

- [ ] **Step 3: 构建验证** — `make build-frontend && go build ./...` → 通过。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 单曲「下一首播放」按钮（专辑/搜索/收藏/歌单曲目行）"
```

---

## Task 4: 播放队列面板

**Files:** Create `web/src/components/QueuePanel.vue`；Modify `web/src/components/PlayerBar.vue`

> 先读 PlayerBar.vue：它用 `const player = usePlayerStore()`；右侧有 volume 等控件区。要在右侧加一个队列按钮，并渲染队列面板。

- [ ] **Step 1: 创建 QueuePanel.vue**：
```vue
<template>
  <div class="queue-panel">
    <div class="queue-header">
      <h3>播放队列</h3>
      <button class="link-btn" type="button" @click="$emit('close')">关闭</button>
    </div>
    <p v-if="player.queue.length === 0" class="muted queue-empty">队列为空</p>
    <ul v-else class="queue-list">
      <li
        v-for="(t, i) in player.queue"
        :key="i"
        class="queue-item"
        :class="{ current: i === player.currentIndex, 'drag-over': i === dragOver }"
        draggable="true"
        @dragstart="dragFrom = i"
        @dragover.prevent="dragOver = i"
        @dragleave="dragOver = -1"
        @drop="onDrop(i)"
      >
        <span class="queue-play" role="button" tabindex="0" @click="player.playAtIndex(i)" @keydown.enter="player.playAtIndex(i)">
          <span class="queue-title">{{ t.title }}</span>
          <span class="queue-artist muted">{{ t.artist }}</span>
        </span>
        <button v-if="i !== player.currentIndex" class="queue-remove" type="button" title="移除" @click.stop="player.removeFromQueue(i)">✕</button>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { usePlayerStore } from '../stores/player'

defineEmits<{ close: [] }>()
const player = usePlayerStore()
const dragFrom = ref(-1)
const dragOver = ref(-1)

function onDrop(i: number) {
  if (dragFrom.value >= 0 && dragFrom.value !== i) {
    player.moveInQueue(dragFrom.value, i)
  }
  dragFrom.value = -1
  dragOver.value = -1
}
</script>

<style scoped>
.queue-panel { position: absolute; bottom: 100%; right: 12px; margin-bottom: 8px; width: 320px; max-height: 50vh; overflow-y: auto; background: var(--surface, #1b1f27); border: 1px solid var(--border, #333); border-radius: 10px; padding: 10px; box-shadow: 0 12px 32px rgba(0,0,0,0.5); z-index: 600; }
.queue-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.queue-header h3 { font-size: 14px; }
.queue-empty { font-size: 13px; padding: 8px; }
.queue-list { list-style: none; margin: 0; padding: 0; }
.queue-item { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 6px; }
.queue-item.current { background: rgba(110,231,183,0.12); }
.queue-item.drag-over { outline: 1px dashed var(--accent, #6ee7b7); }
.queue-play { flex: 1; display: flex; flex-direction: column; cursor: pointer; min-width: 0; }
.queue-title { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.queue-artist { font-size: 11px; }
.queue-remove { background: none; border: none; color: var(--text-dim, #888); cursor: pointer; }
.queue-remove:hover { color: var(--danger, #f87171); }
.link-btn { background: none; border: none; color: var(--text-muted, #888); cursor: pointer; font-size: 12px; }
</style>
```

- [ ] **Step 2: PlayerBar.vue 接入** — import `QueuePanel`、`ref`（如未引入）；加 `const showQueue = ref(false)`。在右侧控件区（volume 那一带）加一个队列按钮：
```vue
<button class="player-btn" :class="{ active: showQueue }" type="button" title="播放队列" @click="showQueue = !showQueue">
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    <line x1="8" y1="6" x2="21" y2="6" /><line x1="8" y1="12" x2="21" y2="12" /><line x1="8" y1="18" x2="21" y2="18" />
    <line x1="3" y1="6" x2="3.01" y2="6" /><line x1="3" y1="12" x2="3.01" y2="12" /><line x1="3" y1="18" x2="3.01" y2="18" />
  </svg>
</button>
```
并在 `footer.player-bar` 内合适位置（相对定位的容器内）渲染：
```vue
<QueuePanel v-if="showQueue" @close="showQueue = false" />
```
确保 `.player-bar`（或承载面板的容器）是 `position: relative`，使 QueuePanel 的 `position:absolute; bottom:100%` 浮在播放条上方右侧（若 `.player-bar` 非 relative，给放 QueuePanel 的容器加相对定位，或把面板挂在右侧控件容器内并设其 relative）。

- [ ] **Step 3: 构建验证** — `make build-frontend && go build ./...` → 通过。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 播放条「播放队列」面板（看/跳播/移除/拖拽重排）"
```

---

## Self-Review（计划自检）

- **Spec 覆盖**：playNext/removeFromQueue/moveInQueue(T1) ✓；列表全部播放 + 点某首连播(T2 onPlayList + play-list) ✓；单曲下一首(T3 PlayNextButton 挂四处行) ✓；队列面板看/跳播/移除/拖拽 + 高亮当前(T4 QueuePanel + PlayerBar 入口) ✓。
- **占位符**：无 TODO/TBD；组件给出完整代码；挂载点处给了"读现有红心/AddToPlaylist 放法照挂"的具体指引（这些组件已是 div[role=button] 播放区 + 兄弟操作区，避免 button-in-button）。
- **类型一致**：`playNext(item)`/`removeFromQueue(index)`/`moveInQueue(from,to)`(T1) 与 QueuePanel(T4)、PlayNextButton(T3) 调用一致；`onPlayList(tracks: FavTrack[], startIndex)`(T2) 与列表 emit `play-list(tracks, index)` 一致；队列项字段 `{trackId,title,artist,album,streamUrl,coverUrl}` 跨 onFavPlay/onPlayList/PlayNextButton 一致。
- **已知约束**：无前端测试框架,各任务以构建(零 TS 错误)+真机验证为准;移除不对当前曲开放(store 与 UI 双重保证);拖拽用对象引用维护 currentIndex。
