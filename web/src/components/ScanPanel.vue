<template>
  <section class="scan-panel">
    <!-- 头部卡片板块 -->
    <div class="scan-card" style="margin-bottom: 24px;">
      <div>
        <p class="eyebrow" style="color: var(--accent);">DATABASE SCANNER</p>
        <h3 style="font-size: 22px; font-weight: 800; margin-bottom: 6px;">
          {{ status.running ? '正在扫描您的音乐库...' : '音乐扫描仪处于空闲状态' }}
        </h3>
        <p class="muted" style="font-size: 13px;">
          已处理 {{ status.processed }} 个音频文件 &middot; 累计发现 {{ status.total }} 个曲目资源 &middot; 阶段：{{ phaseLabel }}
        </p>
      </div>

      <button
        :disabled="status.running || triggering"
        class="custom-btn-primary"
        style="width: auto; padding: 10px 24px; font-size: 14px; box-shadow: 0 4px 12px rgba(16, 185, 129, 0.2);"
        type="button"
        @click="$emit('trigger')"
      >
        <span v-if="status.running || triggering" style="display: flex; align-items: center; gap: 8px;">
          <!-- 旋转 Loading 指示 -->
          <svg class="animate-spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <line x1="12" y1="2" x2="12" y2="6" />
            <line x1="12" y1="18" x2="12" y2="22" />
            <line x1="4.93" y1="4.93" x2="7.76" y2="7.76" />
            <line x1="16.24" y1="16.24" x2="19.07" y2="19.07" />
          </svg>
          正在扫描库...
        </span>
        <span v-else>立即启动扫描</span>
      </button>
    </div>

    <!-- 自定义高阶横向流光呼吸进度条 -->
    <div style="margin-bottom: 32px; display: flex; flex-direction: column; gap: 10px;">
      <div style="display: flex; justify-content: space-between; align-items: center; font-size: 13px; font-weight: 600;">
        <span class="muted">处理进度</span>
        <span style="color: var(--accent);">{{ percentage }}%</span>
      </div>
      <div class="progress-bar-container">
        <div
          :class="{
            warning: status.errors > 0,
            processing: status.running
          }"
          class="progress-bar-fill"
          :style="{ width: percentage + '%' }"
        ></div>
      </div>
    </div>

    <!-- 扫描参数网格看板 -->
    <dl class="scan-stats">
      <div class="scan-stats-card">
        <dt>总文件数</dt>
        <dd>{{ status.total }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>已解析成功</dt>
        <dd style="color: var(--accent);">{{ status.processed }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>错误文件数</dt>
        <dd :style="{ color: status.errors > 0 ? 'var(--danger)' : 'var(--text-muted)' }">
          {{ status.errors }}
        </dd>
      </div>
      <div class="scan-stats-card">
        <dt>已刮歌词</dt>
        <dd style="color: var(--accent);">{{ status.lyrics_scraped }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>已刮专辑</dt>
        <dd style="color: var(--accent);">{{ status.albums_scraped }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>已识别指纹</dt>
        <dd style="color: var(--accent);">{{ status.fingerprinted }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>已升级同步</dt>
        <dd style="color: var(--accent);">{{ status.lyrics_upgraded }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>扫描启动时间</dt>
        <dd style="font-size: 15px; font-weight: 600; padding: 6px 0; word-break: break-all;">
          {{ startedAt }}
        </dd>
      </div>
    </dl>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ScanStatus } from '../api/client'

const props = defineProps<{
  status: ScanStatus
  triggering: boolean
}>()

defineEmits<{
  trigger: []
}>()

const percentage = computed(() => {
  if (!props.status.total) return 0
  return Math.min(100, Math.round((props.status.processed / props.status.total) * 100))
})

const startedAt = computed(() => {
  if (!props.status.started_at || props.status.started_at.startsWith('0001-')) return '-'
  return new Date(props.status.started_at).toLocaleString()
})

const phaseLabel = computed(() => {
  switch (props.status.phase) {
    case 'scanning':
      return '正在扫描'
    case 'scraping':
      return '刮削歌词中'
    case 'metadata':
      return '刮削专辑元数据中'
    case 'fingerprint':
      return '指纹识别中'
    case 'lyrics_sync':
      return '升级同步歌词中'
    default:
      return '空闲'
  }
})
</script>

<style scoped>
.animate-spin {
  animation: spin 1.2s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
