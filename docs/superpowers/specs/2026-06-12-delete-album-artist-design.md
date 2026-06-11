# 删除专辑 / 歌手 设计文档

> 版本：1.0 · 日期：2026-06-12 · 状态：已批准

---

## 背景

用户需要在前端删除专辑或歌手（直接动机：清理因扫描去重 bug 产生的重复空专辑，以及日常管理误入库的内容）。当前 Lyra 没有任何删除库内容的入口。

约束：docker-compose 当前把音乐目录挂为只读（`/home/yxx/music:/music:ro`），所以"删除硬盘文件"在容器内会因只读而失败，除非用户改为可写挂载。删文件做成**尽力而为**：DB 一定先删，文件删除失败仅提示、不回滚。

---

## 范围

**做**：

- 管理员专属的删除专辑、删除歌手两个能力（Web/v1 端；Subsonic 协议无此操作，不涉及）。
- 默认仅从库（数据库）移除；可由用户勾选"同时删除硬盘文件"。
- 删歌手 = 连带删除该歌手名下所有专辑与曲目（前端明确二次确认）。
- 正确处理外键删除顺序与依赖清理。

**不做**（YAGNI）：

- 批量删除、回收站/撤销、删空目录。
- 删除单曲（本次只到专辑/歌手粒度；单曲删除另议）。
- Subsonic 端删除。

---

## 外键现状（决定删除顺序）

`foreign_keys=ON`。引用关系：

- `tracks.album_id REFERENCES albums(id)` — **无级联**
- `tracks.artist_id REFERENCES artists(id)` — **无级联**
- `albums.artist_id REFERENCES artists(id)` — **无级联**
- `lyrics.track_id REFERENCES tracks(id)` — **无级联**
- `bookmarks.track_id` / `play_stats.track_id` / `playlist_tracks.track_id` — `ON DELETE CASCADE`（自动清）
- `starred.item_id`（album/artist/song）— 无外键（多态），删除后残留孤儿行（`childByID`/可用性过滤会跳过，但行残留）

因此删除必须在事务内**按序手动删**：先删依赖（lyrics），再删 tracks（其余经级联自动清），再删 album / artist。并顺带清理 starred 孤儿行。

---

## 后端

### 删除逻辑（事务内）

新增 `internal/api/v1/library_delete.go`（或并入 albums/artists handler，见落点）。核心两个操作：

**删专辑 `deleteAlbum(albumID, deleteFiles)`**：

1. 查该专辑全部曲目的 `id` 与 `file_path`（先排空 rows，再后续操作 —— modernc 单连接约束）。
2. 开事务：
   - `DELETE FROM lyrics WHERE track_id IN (该专辑曲目)`
   - `DELETE FROM starred WHERE item_type='song' AND item_id IN (曲目)`
   - `DELETE FROM tracks WHERE album_id=?`（bookmarks/play_stats/playlist_tracks 级联）
   - `DELETE FROM starred WHERE item_type='album' AND item_id=?`
   - `DELETE FROM albums WHERE id=?`
   - 提交。
3. 若 `deleteFiles`：对收集到的 `file_path` 逐个 `os.Remove`，失败收集到 `fileErrors`（不回滚 DB）。

**删歌手 `deleteArtist(artistID, deleteFiles)`**：

1. 收集"待删曲目"= 该歌手名下专辑里的曲目 ∪ `tracks.artist_id=该歌手` 的曲目；收集其 `file_path`。收集该歌手的 `albums.id`。
2. 事务：删这些曲目的 lyrics、starred(song)；删这些 tracks；删 `albums WHERE artist_id=?` 及其 starred(album)；删 `starred WHERE item_type='artist' AND item_id=?`；删 `artists WHERE id=?`；提交。
3. `deleteFiles` 时同上逐个 `os.Remove`。

> 实现以一个可复用内部函数 `deleteTracksByIDs(tx, ids)`（删 lyrics + starred(song) + tracks）收口，专辑/歌手删除都调用它，避免重复。

### 端点（`/api/v1/admin` 组，链 `SessionAuth`→`RequireAdmin`）

- `DELETE /api/v1/admin/albums/{id}?deleteFiles=true|false`
- `DELETE /api/v1/admin/artists/{id}?deleteFiles=true|false`
- 不存在的 id → 404；成功 → `{"ok":true, "filesDeleted":N, "fileErrors":["<path>: <err>", ...]}`。
- `deleteFiles` 默认 false（缺省即仅删库）。

Handler 落点：给 `AlbumsHandler` 加 `DeleteAlbum`、`ArtistsHandler` 加 `DeleteArtist`（它们已持 `*sql.DB`）。删除辅助函数放同包 `library_delete.go`。

---

## 前端

- `App.vue` 把 `currentUser?.isAdmin` 传给 `AlbumDetail`（已有 `:api`）与 `ArtistBrowser`。
- API client 新增：
  - `deleteAlbum(id, deleteFiles): Promise<{ok:boolean; filesDeleted:number; fileErrors:string[]}>`（`DELETE /api/v1/admin/albums/{id}?deleteFiles=...`）
  - `deleteArtist(id, deleteFiles): Promise<...>`（同形）
- **AlbumDetail.vue**（管理员可见「删除专辑」按钮，放标题操作区，红色危险样式）：点击弹确认（用现有手写弹窗风格或 window.confirm + 一个勾选）。含「☐ 同时删除硬盘文件」勾选项。确认后调 `deleteAlbum`；成功 emit 一个 `deleted` 事件让 App 清空选中并刷新专辑列表；若 `fileErrors` 非空，用 globalError/提示展示「库已删除，但 N 个文件未能删除（音乐目录可能为只读挂载）」。
- **ArtistBrowser.vue**（管理员可见「删除歌手」按钮）：二次确认文案强调「将删除该歌手的全部专辑与曲目」+ 勾选删文件。成功后 emit `deleted`，App 刷新歌手与专辑列表、清选中。
- 删除按钮仅 `isAdmin` 时渲染。

> 确认交互：勾选「同时删除硬盘文件」时，确认文案额外标红提示不可恢复。优先复用项目已有的弹窗/按钮样式；window.confirm 亦可接受但无法承载勾选项，故用一个小型内联确认区（带 checkbox）。

---

## 测试

| 测试 | 方式 |
|------|------|
| 删专辑：库移除专辑+其曲目；bookmarks/play_stats/playlist_tracks 经级联清空；lyrics 清空 | httptest + 内存 sqlite，seed 专辑/曲目/各依赖 |
| 删专辑后 starred(album/song) 孤儿行被清 | 同上 |
| 删专辑 deleteFiles=true：临时文件被删除 + filesDeleted 计数 | 用 t.TempDir() 造真实文件 |
| 删文件失败（如文件不存在/只读）：DB 仍已清理，fileErrors 非空 | httptest |
| 删歌手：连带删其所有专辑与曲目（含 artist_id 指向它但归属其它专辑的曲目）、最后删 artist | httptest，seed 多专辑 |
| 不存在 id → 404 | httptest |
| RequireAdmin：普通用户 DELETE → 403（由中间件，已有测试覆盖；这里走 admin 组即可） | httptest（管理员路径） |
| 删后再扫描同文件（若文件未删）会重新入库；文件已删则不再出现 | 说明性，由扫描既有测试 + 真机验证 |

全部 httptest + 内存 sqlite + 临时文件，不打网络。前端构建 + 真机验证（docker，需要删文件则改音乐目录为可写挂载）。

---

## 不在本次范围内

- 单曲删除、批量删除、撤销/回收站、删空目录。
- Subsonic 端删除入口。
- 自动清理"删专辑后变空的歌手"（空歌手不会出现在 getArtists 列表，无害）。
