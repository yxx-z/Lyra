<template>
  <section class="scan-panel">
    <div class="scan-card">
      <div>
        <p class="eyebrow">Scanner</p>
        <h3>{{ status.running ? 'Scan running' : 'Scanner idle' }}</h3>
        <p class="muted">
          Processed {{ status.processed }} of {{ status.total }} files · {{ status.errors }} errors
        </p>
      </div>
      <n-button type="primary" :loading="triggering" :disabled="status.running" @click="$emit('trigger')">
        Start scan
      </n-button>
    </div>

    <n-progress
      :percentage="percentage"
      :processing="status.running"
      :status="status.errors > 0 ? 'warning' : 'success'"
    />

    <dl class="scan-stats">
      <div>
        <dt>Total</dt>
        <dd>{{ status.total }}</dd>
      </div>
      <div>
        <dt>Processed</dt>
        <dd>{{ status.processed }}</dd>
      </div>
      <div>
        <dt>Errors</dt>
        <dd>{{ status.errors }}</dd>
      </div>
      <div>
        <dt>Started</dt>
        <dd>{{ startedAt }}</dd>
      </div>
    </dl>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NProgress } from 'naive-ui'
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
</script>
