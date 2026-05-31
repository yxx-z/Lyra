<!-- web/src/App.vue -->
<template>
  <n-config-provider :theme="darkTheme">
    <n-layout style="height: 100vh">
      <n-layout-content>
        <div style="display:flex;align-items:center;justify-content:center;height:100vh;flex-direction:column;gap:1rem">
          <h1 style="font-size:3rem;margin:0">Lyra</h1>
          <p style="color:#888;margin:0">自托管音乐服务器 —— 即将上线</p>
        </div>
      </n-layout-content>
    </n-layout>
  </n-config-provider>
</template>

<script setup lang="ts">
import { NConfigProvider, NLayout, NLayoutContent, darkTheme } from 'naive-ui'
</script>
