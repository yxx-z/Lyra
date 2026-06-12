<template>
  <div class="queue-panel">
    <!-- 面板头部 -->
    <div class="queue-header">
      <span class="queue-title">播放队列</span>
      <button type="button" class="queue-close-btn" title="关闭" @click="emit('close')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    <!-- 队列为空状态 -->
    <div v-if="player.queue.length === 0" class="queue-empty">
      队列为空
    </div>

    <!-- 队列列表 -->
    <ul v-else class="queue-list">
      <li
        v-for="(item, i) in player.queue"
        :key="item.trackId + '-' + i"
        class="queue-item"
        :class="{ current: i === player.currentIndex }"
        draggable="true"
        @click="player.playAtIndex(i)"
        @dragstart="dragIndex = i"
        @dragover.prevent
        @drop.stop="onDrop(i)"
      >
        <!-- 当前播放标记 -->
        <span class="queue-item-indicator">
          <svg v-if="i === player.currentIndex" viewBox="0 0 24 24" fill="currentColor" stroke="none">
            <path d="M8 5v14l11-7z" />
          </svg>
          <span v-else class="queue-item-num">{{ i + 1 }}</span>
        </span>

        <!-- 曲目信息 -->
        <span class="queue-item-info">
          <span class="queue-item-title">{{ item.title }}</span>
          <span v-if="item.artist" class="queue-item-artist">{{ item.artist }}</span>
        </span>

        <!-- 移除按钮（当前曲目不显示） -->
        <button
          v-if="i !== player.currentIndex"
          type="button"
          class="queue-remove-btn"
          title="从队列移除"
          @click.stop="player.removeFromQueue(i)"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
        <!-- 当前曲目占位，保持对齐 -->
        <span v-else class="queue-remove-placeholder"></span>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { usePlayerStore } from '../stores/player'

const player = usePlayerStore()

const emit = defineEmits<{ (e: 'close'): void }>()

const dragIndex = ref(-1)

function onDrop(toIndex: number) {
  if (dragIndex.value !== -1 && dragIndex.value !== toIndex) {
    player.moveInQueue(dragIndex.value, toIndex)
  }
  dragIndex.value = -1
}
</script>

<style scoped>
.queue-panel {
  position: absolute;
  bottom: 100%;
  right: 12px;
  margin-bottom: 8px;
  width: 320px;
  max-height: 420px;
  display: flex;
  flex-direction: column;
  background: rgba(17, 20, 29, 0.95);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid var(--border-glass);
  border-radius: 16px;
  box-shadow: var(--shadow-premium);
  z-index: 50;
  overflow: hidden;
}

/* 头部 */
.queue-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 12px;
  border-bottom: 1px solid var(--border-glass);
  flex-shrink: 0;
}

.queue-title {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.queue-close-btn {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
}
.queue-close-btn svg {
  width: 14px;
  height: 14px;
}
.queue-close-btn:hover {
  color: var(--text);
  background: rgba(255, 255, 255, 0.06);
}

/* 空状态 */
.queue-empty {
  padding: 40px 16px;
  text-align: center;
  color: var(--text-dim);
  font-size: 14px;
}

/* 可滚动列表 */
.queue-list {
  list-style: none;
  overflow-y: auto;
  flex: 1;
  padding: 6px 8px;
}

/* 单行条目 */
.queue-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 8px;
  border-radius: 10px;
  border: 1px solid transparent;
  cursor: pointer;
  transition: all 0.15s ease;
  user-select: none;
}
.queue-item:hover {
  background: rgba(255, 255, 255, 0.05);
  border-color: var(--border-glass);
}
.queue-item.current {
  background: rgba(16, 185, 129, 0.08);
  border-color: rgba(16, 185, 129, 0.2);
  color: var(--accent);
}

/* 序号 / 播放指示器 */
.queue-item-indicator {
  width: 22px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.queue-item-indicator svg {
  width: 14px;
  height: 14px;
  color: var(--accent);
}
.queue-item-num {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-dim);
  min-width: 14px;
  text-align: center;
}

/* 标题 + 艺术家 */
.queue-item-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.queue-item-title {
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.queue-item-artist {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.queue-item.current .queue-item-artist {
  color: rgba(16, 185, 129, 0.7);
}

/* 移除按钮 */
.queue-remove-btn {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 50%;
  color: var(--text-dim);
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: all 0.15s ease;
}
.queue-remove-btn svg {
  width: 12px;
  height: 12px;
}
.queue-item:hover .queue-remove-btn {
  opacity: 1;
}
.queue-remove-btn:hover {
  color: var(--danger, #ef4444);
  background: rgba(239, 68, 68, 0.1);
}

/* 当前曲目占位（保持按钮列宽一致） */
.queue-remove-placeholder {
  width: 24px;
  flex-shrink: 0;
}
</style>
