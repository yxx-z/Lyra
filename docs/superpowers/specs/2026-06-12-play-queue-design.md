# 播放队列：全部播放 / 下一首播放 / 队列面板 设计文档

> 版本：1.0 · 日期：2026-06-12 · 状态：已批准

---

## 背景

当前播放有两处缺口：
1. 歌单 / 收藏 / 最近播放 / 最常听 列表**没有"全部播放"**，且点列表里某首只播这一首（`onFavPlay` 用 `playTrack(item, [item])` 单首队列），不会接着连播后面的。
2. 没有查看 / 管理**当前播放队列**的入口。

专辑页本来就是连播（`playAlbumTrack` 排整张专辑为队列），不在本次改动。

纯前端功能（`stores/player.ts` + 组件），无后端改动。

---

## 范围

**做**：

- **列表全部播放 + 点某首连播**：歌单页、收藏/最近/最常听面板 → 「▶ 全部播放」按钮；点某首 = 把整列表排成队列、从该首开始连播。
- **单曲「下一首播放」**：曲目行小按钮，把该曲**插到当前播放曲之后**。入口：专辑曲目行、搜索结果、收藏/歌单曲目行。
- **播放队列面板**：底部播放条右侧加「播放队列」入口，点开展示后续队列；支持**点击跳播、移除、拖拽重排**，高亮当前曲。

**不做**（YAGNI）：保存队列到服务端、跨设备同步队列、"随机/循环"模式改动（沿用现有）。

---

## 播放 Store（`web/src/stores/player.ts`）

现状：`queue: ExtendedPlayerTrack[]`、`currentIndex`、`currentTrack`、`playTrack(track, newQueue?)`（传 newQueue 即替换队列并按 trackId 定位起播）、`playAtIndex(i)`、`next/prev`。新增并导出三个动作：

- **`playNext(item: ExtendedPlayerTrack)`**：
  - 队列空 → `playTrack(item, [item])`（直接开播）。
  - 否则插到当前曲之后：`queue.value.splice(currentIndex.value + 1, 0, item)`（不改 currentIndex/currentTrack，不打断当前播放）。
- **`removeFromQueue(index: number)`**：
  - 仅用于移除**非当前曲**（UI 不在当前曲那行显示移除）。`queue.value.splice(index, 1)`；若 `index < currentIndex.value` 则 `currentIndex.value--`（保持 currentTrack 指向不变）。
- **`moveInQueue(from: number, to: number)`**：
  - 先记当前曲对象引用 `cur := queue.value[currentIndex.value]`；`splice` 把 from 移到 to；移动后 `currentIndex.value = queue.value.indexOf(cur)`（用对象引用重定位，保证正在播的曲不被打断、索引正确）。

队列项形状沿用 `ExtendedPlayerTrack`（`{trackId,title,artist,album,streamUrl,coverUrl,...}`）。

---

## 列表全部播放 / 连播

- `App.vue` 新增 `onPlayList(tracks: FavTrack[], startIndex: number)`：把 `tracks` 映射成队列项（同 `onFavPlay` 的字段映射：`{trackId:id,title,artist,album,streamUrl:stream_url,coverUrl:cover_url}`），`playerStore.playTrack(queue[startIndex], queue)`。
- `PlaylistsPage.vue` / `FavoritesPanel.vue`：
  - 每个列表（歌单详情、收藏的歌曲、最近播放、最常听）顶部加「▶ 全部播放」按钮 → `emit('play-list', tracks, 0)`。
  - 点某曲改为 `emit('play-list', tracks, i)`（i 为该曲在该列表中的下标）。
  - App 在 `<PlaylistsPage>`/`<FavoritesPanel>` 上接 `@play-list="onPlayList"`（替换/补充原 `@play-track`）。
- 专辑页不动（已连播）。

---

## 单曲「下一首播放」入口

- 一个可复用小组件 `PlayNextButton.vue`（prop：一个能映射成队列项的曲目对象，或直接收 `{trackId,title,artist,album,streamUrl,coverUrl}`）：点击 `playerStore.playNext(item)` + 轻提示「已设为下一首」。
- 挂在：专辑曲目行（AlbumDetail，与红心/添加到歌单并列）、搜索结果曲目、收藏/歌单曲目行。
- 各处按本组件已有的曲目字段构造队列项传入（album 行用专辑上下文补 artist/album/cover）。

---

## 播放队列面板

- `PlayerBar.vue` 右侧加「播放队列」图标按钮（列表图标），点击 toggle 一个队列面板（`showQueue`）。
- 新组件 `QueuePanel.vue`（直接读 `playerStore`）：
  - 列出 `playerStore.queue`，**高亮 `currentIndex`**（当前播放）。
  - 行点击 → `playerStore.playAtIndex(i)`（跳播）。
  - 每行（**当前曲那行不显示移除**）一个移除按钮 → `playerStore.removeFromQueue(i)`。
  - HTML5 `draggable` 拖拽重排 → `playerStore.moveInQueue(from, to)`。
  - 顶部标题「播放队列」+ 关闭。空队列时提示「队列为空」。
- 面板定位：浮在播放条上方右侧（绝对定位/弹层),点击外部或关闭按钮收起。

---

## 测试

纯前端，无 Go 改动。以 `make build-frontend`（vue-tsc 类型检查）+ 真机验证为准：

- 全部播放：歌单/收藏点「全部播放」→ 整列表入队从头连播;放完自动下一首。
- 点列表中间某首 → 从该首起、后续继续连播。
- 下一首播放：播放中点某曲「下一首播放」→ 该曲插到当前之后,当前不被打断,下一首即它。
- 队列面板:点播放条右侧入口 → 看到后续队列、高亮当前;点某行跳播;移除某行后队列更新且当前曲不乱;拖拽重排后顺序变、当前曲继续播。

---

## 不在本次范围内

- 队列服务端持久化 / 跨设备同步。
- 随机/循环模式逻辑改动。
- 队列项右键菜单等高级交互。
