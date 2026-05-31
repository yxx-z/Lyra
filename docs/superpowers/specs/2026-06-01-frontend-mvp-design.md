# Lyra Frontend MVP 设计文档

> 版本：1.0 · 日期：2026-06-01 · 状态：已批准

---

## 目标

实现 Lyra Web UI 的第一个可用版本，范围为 **Library First + 管理入口**：

- 登录并保存 Bearer token
- 浏览专辑墙、专辑详情和曲目列表
- 浏览艺术家及其专辑
- 搜索曲目、专辑、艺术家
- 播放曲目，底部播放器常驻
- 查看扫描状态并手动触发扫描

对应 PRD：US-03、US-04、US-12、US-14、US-18、US-19、US-32。

---

## 设计方向

采用 **Library First** 单页布局。用户登录后直接看到专辑墙，播放行为是主路径，扫描和状态管理作为顶部/侧边入口存在。

不引入 Vue Router。当前前端仍是很薄的 Vue 3 应用，MVP 用本地 state 和少量组件即可表达主要工作流。后续如果需要深链接、浏览历史或多页面结构，再引入 router。

---

## 文件结构

```
web/src/
├── App.vue
├── api/
│   └── client.ts
├── components/
│   ├── LoginView.vue
│   ├── LibraryShell.vue
│   ├── AlbumGrid.vue
│   ├── AlbumDetail.vue
│   ├── ArtistBrowser.vue
│   ├── SearchPanel.vue
│   ├── ScanPanel.vue
│   └── PlayerBar.vue
└── style.css
```

`App.vue` 负责认证状态、当前视图和全局数据加载。组件只接收必要 props 并 emit 用户动作，避免在组件内分散 token 管理。

---

## 状态模型

```ts
type ViewMode = 'albums' | 'artists' | 'scan'

type PlayerState = {
  trackId: string
  title: string
  artist?: string
  album?: string
  streamUrl: string
} | null
```

`localStorage["lyra.token"]` 保存登录 token。启动时若存在 token，直接加载库数据；任一 API 返回 401 时清除 token 并回到登录页。

---

## API 对接

`web/src/api/client.ts` 提供集中封装：

- `login(username, password)`
- `listAlbums()`
- `getAlbum(id)`
- `listArtists()`
- `getArtist(id)`
- `search(q)`
- `getScanStatus()`
- `triggerScan()`

所有受保护请求自动加：

```
Authorization: Bearer <token>
```

封面和音频不单独下载为 blob；直接使用后端 URL：

- 封面：`/api/v1/cover/:albumId`
- 音频：`/api/v1/tracks/:trackId/stream`

浏览器 `<img>` 和 `<audio>` 请求不能附加 Bearer header。为避免前端做错误承诺，本次实现先直接引用封面和音频 URL；如果服务启用认证，这两个资源可能返回 401。开发和演示阶段可使用 `auth.disable: true` 验证播放链路。后续应在后端支持 cookie session、token query 签名，或专门的静态媒体访问策略。

---

## 页面结构

### 登录页

居中紧凑表单：

- 品牌名 Lyra
- 用户名输入，默认可填 `admin`
- 密码输入
- 登录按钮
- 登录失败显示错误

### 主界面

整体高度为 `100vh`，分三层：

1. 左侧窄导航：专辑、艺术家、扫描
2. 顶部工具条：搜索框、刷新、退出
3. 主内容区 + 底部播放器

主内容区默认是专辑墙。点击专辑后打开右侧详情面板，展示封面、专辑信息和曲目列表。

### 搜索

顶部搜索框输入关键词后展示搜索面板，分组显示曲目、专辑、艺术家：

- 点击曲目：立即播放
- 点击专辑：打开专辑详情
- 点击艺术家：切到艺术家视图并打开该艺术家

### 艺术家

左侧列表展示艺术家名称和专辑数；右侧显示选中艺术家的专辑。点击专辑复用专辑详情面板。

### 扫描

扫描面板展示：

- 是否正在扫描
- total / processed / errors
- started_at
- “开始扫描”按钮

扫描运行时禁用按钮。触发扫描成功后轮询状态，直到 `running=false`。

---

## 播放器

底部常驻播放器使用原生 `<audio controls>`，MVP 提供：

- 当前曲目标题、艺术家、专辑
- 浏览器原生播放/暂停/进度控制
- 点击曲目后更新 `src` 并自动播放

上一首、下一首、随机、循环不在本次实现范围内；后续需要队列模型后再做。

---

## 错误与空状态

- API 401：清 token，回登录页
- 网络错误：顶部或当前面板展示简短错误
- 空专辑：显示“暂无专辑，先扫描音乐库”
- 空搜索：显示“没有匹配结果”
- 封面 404：显示色块占位，不打断浏览
- 扫描冲突 409：显示“扫描正在进行中”，并刷新状态

---

## 视觉原则

界面应是音乐资料库工具，不做营销式首页。风格克制、信息密度适中：

- 深色优先，保留跟随系统色彩的余地
- 专辑封面是主要视觉元素
- 卡片圆角不超过 8px
- 按钮、输入、列表采用 Naive UI 基础组件
- 移动端降级为单列：导航压缩到顶部，详情面板全屏覆盖或纵向堆叠

---

## 测试策略

- `npm run build` 验证 TypeScript 和 Vite 构建
- `make build` 验证前端能输出到 `ui/dist` 并被 Go embed 构建
- 手动开发冒烟：
  - 登录失败显示错误
  - `auth.disable=true` 时能加载专辑列表
  - 搜索能展示结果
  - 点击曲目能设置 audio src
  - 扫描按钮能触发 API 并刷新状态

---

## 不在本次范围内

- Vue Router
- 播放队列、上一首/下一首、随机/循环
- 歌词显示
- 收藏、播放统计
- 拖拽上传歌词或元数据编辑
- 解决媒体 URL 鉴权的最终后端方案
