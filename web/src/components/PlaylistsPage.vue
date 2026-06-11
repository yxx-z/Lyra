<template>
  <div class="account-settings playlists-page">
    <!-- 面板头部 -->
    <div class="account-settings-header">
      <h2>歌单</h2>
    </div>

    <!-- 状态消息 -->
    <p v-if="msg" :class="msgError ? 'pl-msg pl-msg-error' : 'pl-msg pl-msg-ok'">{{ msg }}</p>

    <!-- 双栏主体 -->
    <div class="pl-columns">
      <!-- 左栏：歌单列表 + 新建 -->
      <div class="pl-left">
        <div class="pl-list">
          <div
            v-for="p in lists"
            :key="p.id"
            class="pl-list-row"
            :class="{ active: selected?.id === p.id }"
            role="button"
            tabindex="0"
            @click="open(p.id)"
            @keydown.enter="open(p.id)"
            @keydown.space.prevent="open(p.id)"
          >
            <span class="pl-list-name">{{ p.name }}</span>
            <span class="pl-list-count muted">{{ p.song_count }} 首</span>
            <span class="pl-list-actions">
              <button
                class="pl-icon-btn"
                type="button"
                title="改名"
                @click.stop="rename(p)"
              >✎</button>
              <button
                class="pl-icon-btn pl-icon-btn-danger"
                type="button"
                title="删除"
                @click.stop="remove(p)"
              >✕</button>
            </span>
          </div>
          <div v-if="lists.length === 0" class="muted pl-empty">暂无歌单</div>
        </div>

        <!-- 新建歌单 -->
        <div class="pl-create">
          <input
            v-model="newName"
            class="pl-create-input"
            type="text"
            placeholder="新建歌单名称…"
            @keyup.enter="create"
          />
          <button class="pl-create-btn" type="button" @click="create">新建</button>
        </div>
      </div>

      <!-- 右栏：歌单详情 -->
      <div class="pl-right">
        <template v-if="selected">
          <div class="pl-detail-header">
            <span class="pl-detail-name">{{ selected.name }}</span>
            <span class="muted pl-detail-count">{{ selected.tracks.length }} 首</span>
          </div>

          <div v-if="selected.tracks.length === 0" class="muted pl-empty">此歌单暂无曲目</div>

          <div class="pl-track-list">
            <div
              v-for="(t, i) in selected.tracks"
              :key="t.id"
              class="pl-track-row"
              :class="{ 'drag-over': dragOverIdx === i }"
              draggable="true"
              @dragstart="onDragStart(i)"
              @dragover.prevent="dragOverIdx = i"
              @dragleave="dragOverIdx = null"
              @drop="onDrop(i)"
              @click="$emit('play-track', t)"
            >
              <span class="pl-drag-handle muted" title="拖拽重排">⠿</span>
              <span class="pl-track-title">{{ t.title }}</span>
              <span class="muted pl-track-meta">{{ t.artist }} · {{ t.album }}</span>
              <button
                class="pl-icon-btn pl-icon-btn-danger pl-track-remove"
                type="button"
                title="移除"
                @click.stop="removeTrack(i)"
              >✕</button>
            </div>
          </div>
        </template>

        <div v-else class="muted pl-empty pl-empty-hint">← 选择左侧歌单查看曲目</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { ApiClient, FavTrack, PlaylistSummary, PlaylistDetail } from '../api/client'

const props = defineProps<{ api: ApiClient }>()
const emit = defineEmits<{ 'play-track': [track: FavTrack] }>()

const lists = ref<PlaylistSummary[]>([])
const selected = ref<PlaylistDetail | null>(null)
const newName = ref('')
const msg = ref('')
const msgError = ref(false)
function show(t: string, e = false) { msg.value = t; msgError.value = e }
function errMsg(e: unknown) { return e instanceof Error ? e.message : '操作失败' }

async function reload() {
  try { lists.value = (await props.api.listPlaylists()).playlists } catch (e) { show(errMsg(e), true) }
}
onMounted(reload)

async function open(id: string) {
  try { selected.value = await props.api.getPlaylist(id) } catch (e) { show(errMsg(e), true) }
}
async function create() {
  const name = newName.value.trim()
  if (!name) return
  try {
    const { id } = await props.api.createPlaylist(name)
    newName.value = ''
    await reload()
    await open(id)
  } catch (e) { show(errMsg(e), true) }
}
async function rename(p: PlaylistSummary) {
  const name = window.prompt('新名称', p.name)
  if (!name || !name.trim()) return
  try {
    await props.api.updatePlaylist(p.id, { name: name.trim() })
    await reload()
    if (selected.value?.id === p.id) await open(p.id)
  } catch (e) { show(errMsg(e), true) }
}
async function remove(p: PlaylistSummary) {
  if (!window.confirm(`删除歌单「${p.name}」？`)) return
  try {
    await props.api.deletePlaylist(p.id)
    if (selected.value?.id === p.id) selected.value = null
    await reload()
  } catch (e) { show(errMsg(e), true) }
}
async function removeTrack(idx: number) {
  if (!selected.value) return
  const cur = selected.value.id
  const ids = selected.value.tracks.filter((_, i) => i !== idx).map(t => t.id)
  try { await props.api.setPlaylistTracks(cur, ids); await open(cur); await reload() }
  catch (e) { show(errMsg(e), true) }
}

// 拖拽重排
const dragIdx = ref<number | null>(null)
const dragOverIdx = ref<number | null>(null)

function onDragStart(i: number) { dragIdx.value = i }
async function onDrop(target: number) {
  dragOverIdx.value = null
  if (!selected.value || dragIdx.value === null || dragIdx.value === target) { dragIdx.value = null; return }
  const arr = [...selected.value.tracks]
  const [moved] = arr.splice(dragIdx.value, 1)
  arr.splice(target, 0, moved)
  selected.value.tracks = arr
  dragIdx.value = null
  const cur = selected.value.id
  try { await props.api.setPlaylistTracks(cur, arr.map(t => t.id)) }
  catch (e) { show(errMsg(e), true); await open(cur) }
}
</script>

<style scoped>
/* 双栏布局 */
.playlists-page {
  max-width: 960px;
}

.pl-columns {
  display: flex;
  gap: 24px;
  min-height: 320px;
}

/* ── 左栏 ── */
.pl-left {
  width: 240px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.pl-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
}

.pl-list-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border-radius: 8px;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  color: var(--text);
  transition: background 0.15s;
}

.pl-list-row:hover {
  background: rgba(255, 255, 255, 0.05);
}

.pl-list-row.active {
  background: rgba(255, 255, 255, 0.08);
  color: var(--accent, #a78bfa);
}

.pl-list-name {
  font-size: 13px;
  font-weight: 600;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.pl-list-count {
  font-size: 11px;
  flex-shrink: 0;
}

.pl-list-actions {
  display: flex;
  gap: 2px;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.pl-list-row:hover .pl-list-actions {
  opacity: 1;
}

/* ── 新建输入行 ── */
.pl-create {
  display: flex;
  gap: 6px;
}

.pl-create-input {
  flex: 1;
  min-width: 0;
  padding: 6px 10px;
  font-size: 13px;
  border-radius: 7px;
  border: 1px solid var(--border-glass, rgba(255,255,255,0.1));
  background: rgba(255, 255, 255, 0.04);
  color: var(--text);
  outline: none;
  transition: border-color 0.15s;
}

.pl-create-input:focus {
  border-color: var(--accent, #a78bfa);
}

.pl-create-input::placeholder {
  color: var(--text-muted, rgba(255,255,255,0.35));
}

.pl-create-btn {
  padding: 6px 12px;
  font-size: 13px;
  font-weight: 600;
  border-radius: 7px;
  border: none;
  background: var(--accent, #a78bfa);
  color: #fff;
  cursor: pointer;
  transition: opacity 0.15s;
  flex-shrink: 0;
}

.pl-create-btn:hover {
  opacity: 0.85;
}

/* ── 右栏 ── */
.pl-right {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.pl-detail-header {
  display: flex;
  align-items: baseline;
  gap: 10px;
  border-bottom: 1px solid var(--border-glass, rgba(255,255,255,0.08));
  padding-bottom: 10px;
}

.pl-detail-name {
  font-size: 16px;
  font-weight: 700;
  color: var(--text);
}

.pl-detail-count {
  font-size: 12px;
}

/* ── 曲目列表 ── */
.pl-track-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.pl-track-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  color: var(--text);
  transition: background 0.15s;
  user-select: none;
}

.pl-track-row:hover {
  background: rgba(255, 255, 255, 0.05);
}

.pl-track-row.drag-over {
  background: rgba(167, 139, 250, 0.15);
  outline: 1px dashed var(--accent, #a78bfa);
}

.pl-drag-handle {
  font-size: 14px;
  cursor: grab;
  flex-shrink: 0;
  line-height: 1;
}

.pl-drag-handle:active {
  cursor: grabbing;
}

.pl-track-title {
  font-size: 13px;
  font-weight: 600;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.pl-track-meta {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 200px;
  flex-shrink: 0;
}

.pl-track-remove {
  opacity: 0;
  transition: opacity 0.15s;
  flex-shrink: 0;
}

.pl-track-row:hover .pl-track-remove {
  opacity: 1;
}

/* ── 图标按钮 ── */
.pl-icon-btn {
  width: 22px;
  height: 22px;
  border-radius: 5px;
  border: none;
  background: none;
  color: var(--text-muted, rgba(255,255,255,0.45));
  cursor: pointer;
  font-size: 12px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s, background 0.15s;
}

.pl-icon-btn:hover {
  color: var(--text);
  background: rgba(255, 255, 255, 0.08);
}

.pl-icon-btn-danger:hover {
  color: #f87171;
  background: rgba(239, 68, 68, 0.12);
}

/* ── 状态消息 ── */
.pl-msg {
  font-size: 13px;
  padding: 8px 12px;
  border-radius: 6px;
  margin: 0;
}

.pl-msg-ok {
  background: rgba(52, 211, 153, 0.1);
  color: #6ee7b7;
  border: 1px solid rgba(52, 211, 153, 0.2);
}

.pl-msg-error {
  background: rgba(239, 68, 68, 0.12);
  color: #f87171;
  border: 1px solid rgba(239, 68, 68, 0.25);
}

/* ── 空态提示 ── */
.pl-empty {
  font-size: 13px;
  padding: 12px 0;
}

.pl-empty-hint {
  padding: 32px 0;
  text-align: center;
}
</style>
