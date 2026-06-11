<template>
  <div class="add-to-playlist" @click.stop>
    <button class="atp-btn" type="button" title="添加到歌单" @click="toggle">＋</button>
    <div v-if="open" class="atp-menu">
      <button class="atp-item atp-new" type="button" @click="createAndAdd">＋ 新建歌单…</button>
      <button v-for="p in lists" :key="p.id" class="atp-item" type="button" @click="add(p.id)">{{ p.name }}</button>
      <p v-if="lists.length === 0" class="atp-empty">暂无歌单</p>
    </div>
    <span v-if="tip" class="atp-tip">{{ tip }}</span>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import type { ApiClient, PlaylistSummary } from '../api/client'

const props = defineProps<{ api: ApiClient; trackId: string }>()
const open = ref(false)
const lists = ref<PlaylistSummary[]>([])
const tip = ref('')

function flash(t: string) { tip.value = t; setTimeout(() => { tip.value = '' }, 1500) }

async function toggle() {
  open.value = !open.value
  if (open.value) {
    try { lists.value = (await props.api.listPlaylists()).playlists } catch { lists.value = [] }
  }
}
async function add(id: string) {
  open.value = false
  try { await props.api.addToPlaylist(id, [props.trackId]); flash('已添加') } catch { flash('添加失败') }
}
async function createAndAdd() {
  open.value = false
  const name = window.prompt('新歌单名称')
  if (!name || !name.trim()) return
  try {
    const { id } = await props.api.createPlaylist(name.trim())
    await props.api.addToPlaylist(id, [props.trackId])
    flash('已创建并添加')
  } catch { flash('操作失败') }
}

// 点击组件外部关闭下拉
function onDocClick() { open.value = false }
document.addEventListener('click', onDocClick)
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<style scoped>
.add-to-playlist { position: relative; display: inline-flex; align-items: center; }
.atp-btn {
  background: none; border: none; cursor: pointer;
  color: var(--text-dim, rgba(255,255,255,0.4)); font-size: 16px; line-height: 1;
  padding: 2px 6px; border-radius: 4px; transition: color 0.15s;
}
.atp-btn:hover { color: var(--accent, #6ee7b7); }
.atp-menu {
  position: absolute; right: 0; top: 100%; z-index: 50; margin-top: 4px;
  min-width: 160px; max-height: 260px; overflow-y: auto;
  background: var(--surface, #1b1f27); border: 1px solid var(--border, #333);
  border-radius: 8px; padding: 4px; box-shadow: 0 8px 24px rgba(0,0,0,0.4);
}
.atp-item {
  display: block; width: 100%; text-align: left; background: none; border: none;
  color: var(--text, #fff); padding: 7px 10px; border-radius: 6px; cursor: pointer; font-size: 13px;
}
.atp-item:hover { background: rgba(255,255,255,0.06); }
.atp-new { color: var(--accent, #6ee7b7); }
.atp-empty { color: var(--text-muted, #888); font-size: 12px; padding: 7px 10px; margin: 0; }
.atp-tip { position: absolute; right: 0; top: 100%; margin-top: 4px; font-size: 11px; color: var(--success, #30a46c); white-space: nowrap; }
</style>
