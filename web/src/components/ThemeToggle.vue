<script setup lang="ts">
import { computed } from 'vue'
import { theme, toggle_theme } from '../lib/theme'

// 深色下露出太阳（点回浅色），浅色下露出月亮（点去深色）
const is_dark = computed(() => theme.value === 'dark')

// 无障碍文案，同时用作鼠标悬停提示
const label = computed(() => (is_dark.value ? '切换到浅色主题' : '切换到深色主题'))
</script>

<template>
  <button
    type="button"
    class="btn btn-outline btn-icon"
    :title="label"
    :aria-label="label"
    :aria-pressed="is_dark"
    @click="toggle_theme"
  >
    <svg
      v-if="!is_dark"
      class="h-4 w-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
    </svg>
    <svg
      v-else
      class="h-4 w-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="4" />
      <path
        d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"
      />
    </svg>
  </button>
</template>
