<template>
  <section class="artist-browser">
    <!-- 左侧：歌手列表索引 -->
    <div class="artist-list">
      <button
        v-for="artist in artists"
        :key="artist.id"
        :class="{ active: selectedArtist?.id === artist.id }"
        class="artist-row"
        type="button"
        @click="$emit('select-artist', artist.id)"
      >
        <span style="font-weight: 600;">{{ artist.name || '未知歌手' }}</span>
        <span class="badge">{{ artist.album_count }} 张专辑</span>
      </button>
    </div>

    <!-- 右侧：选中歌手的专辑陈列室 -->
    <div class="artist-albums">
      <div v-if="!selectedArtist" class="empty-state" style="height: 100%;">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="margin-bottom: 16px; color: var(--text-dim);">
          <circle cx="12" cy="12" r="10" />
          <path d="M12 8v8" />
          <path d="M8 12h8" />
        </svg>
        <p style="font-weight: 500;">选择一位歌手查看其音乐专辑</p>
      </div>

      <template v-else>
        <div style="border-bottom: 1px solid var(--border-glass); padding-bottom: 16px; margin-bottom: 24px;">
          <p class="eyebrow" style="color: var(--accent);">ARTIST RECORD SHELF</p>
          <h3>{{ selectedArtist.name }}</h3>
          <p class="muted" style="font-size: 13px; margin-top: 4px;">
            共收录该歌手的 {{ selectedArtist.albums?.length || 0 }} 部高品质实体音乐专辑
          </p>
          <div style="margin-top: 12px;">
            <button v-if="isAdmin && selectedArtist" class="danger-btn" type="button" @click="confirmingDelete = true">删除歌手</button>
          </div>
          <!-- 删除确认区域 -->
          <div v-if="confirmingDelete && selectedArtist" class="delete-confirm">
            <p class="warn">⚠ 确认删除歌手「{{ selectedArtist.name }}」？将删除该歌手名下的<strong>全部专辑与曲目</strong>，不可恢复。</p>
            <label><input type="checkbox" v-model="alsoDeleteFiles" /> 同时删除硬盘文件</label>
            <p v-if="alsoDeleteFiles" class="warn">⚠ 硬盘文件将被永久删除；若音乐目录为只读挂载会删除失败。</p>
            <div class="delete-actions">
              <button class="danger-btn" type="button" :disabled="deleting" @click="doDelete">确认删除</button>
              <button type="button" :disabled="deleting" @click="confirmingDelete = false; alsoDeleteFiles = false">取消</button>
            </div>
          </div>
        </div>

        <div class="compact-album-grid">
          <button
            v-for="album in selectedArtist.albums"
            :key="album.id"
            class="album-card"
            type="button"
            @click="$emit('select-album', album.id)"
          >
            <!-- 缩略圆角封面，支持快捷一键切歌 -->
            <div class="album-card-cover-wrapper">
              <img
                v-if="album.cover_url && !brokenCovers.has(album.id)"
                :src="album.cover_url"
                alt="Album cover"
                class="album-cover"
                @error="markCoverBroken(album.id)"
              />
              <div v-else class="album-cover placeholder-cover">
                {{ album.title.slice(0, 2).toUpperCase() }}
              </div>

              <!-- 一键播放蒙层 (阻断冒泡) -->
              <div class="quick-play-overlay" @click.stop="$emit('quick-play-album', album.id)">
                <button class="quick-play-btn" type="button" title="播放此专辑" style="width: 38px; height: 38px;">
                  <svg viewBox="0 0 24 24" style="width: 16px; height: 16px;">
                    <path d="M8 5v14l11-7z" />
                  </svg>
                </button>
              </div>
            </div>

            <!-- 卡片信息 -->
            <div class="album-info-container">
              <span class="album-title" :title="album.title">{{ album.title }}</span>
              <span class="album-meta">{{ album.track_count }} 首曲目</span>
            </div>
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ArtistDetail, ArtistSummary, ApiClient } from '../api/client'

const props = defineProps<{
  artists: ArtistSummary[]
  selectedArtist: ArtistDetail | null
  api: ApiClient
  isAdmin?: boolean
}>()

const emit = defineEmits<{
  'select-artist': [id: string]
  'select-album': [id: string]
  'quick-play-album': [id: string]
  'deleted': [fileErrors: string[]]
}>()

const brokenCovers = ref(new Set<string>())
const confirmingDelete = ref(false)
const alsoDeleteFiles = ref(false)
const deleting = ref(false)

function markCoverBroken(id: string) {
  brokenCovers.value = new Set([...brokenCovers.value, id])
}

async function doDelete() {
  if (!props.selectedArtist) return
  deleting.value = true
  try {
    const res = await props.api.deleteArtist(props.selectedArtist.id, alsoDeleteFiles.value)
    confirmingDelete.value = false
    alsoDeleteFiles.value = false
    emit('deleted', res.fileErrors || [])
  } catch {
    // 保持确认区打开
  } finally {
    deleting.value = false
  }
}
</script>

<style scoped>
/* 管理员危险操作按钮（红色） */
.danger-btn {
  background: rgba(248, 113, 113, 0.15);
  border: 1px solid rgba(248, 113, 113, 0.4);
  color: var(--danger, #f87171);
  font-size: 13px;
  padding: 6px 14px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}

.danger-btn:hover:not(:disabled) {
  background: rgba(248, 113, 113, 0.28);
  border-color: rgba(248, 113, 113, 0.7);
}

.danger-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 删除确认卡片 */
.delete-confirm {
  margin-top: 12px;
  padding: 14px 16px;
  border: 1px solid rgba(248, 113, 113, 0.35);
  border-radius: 8px;
  background: rgba(248, 113, 113, 0.06);
  font-size: 13px;
  line-height: 1.5;
}

.delete-confirm p {
  margin: 0 0 8px;
}

.delete-confirm label {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  user-select: none;
}

/* 警告红字 */
.warn {
  color: var(--danger, #f87171);
  font-size: 12px;
  margin-top: 6px !important;
}

/* 确认操作按钮行 */
.delete-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.delete-actions button:last-child {
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.12);
  color: var(--text-dim, rgba(255, 255, 255, 0.6));
  font-size: 13px;
  padding: 6px 14px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
}

.delete-actions button:last-child:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.1);
}

.delete-actions button:last-child:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
