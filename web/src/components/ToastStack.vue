<script setup lang="ts">
// 提示条列表，由 App 维护状态
defineProps<{
  items: { id: number; text: string; tone: 'ok' | 'error' }[]
}>()
</script>

<template>
  <div class="pointer-events-none fixed bottom-5 right-5 z-50 flex flex-col items-end gap-2">
    <TransitionGroup
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="translate-y-2 opacity-0"
      leave-active-class="transition duration-150 ease-in"
      leave-to-class="translate-y-1 opacity-0"
    >
      <div
        v-for="item in items"
        :key="item.id"
        class="rounded-lg border px-3.5 py-2 text-base shadow-lg backdrop-blur"
        :class="
          item.tone === 'ok'
            ? 'border-emerald-800/70 bg-emerald-950/80 text-emerald-200'
            : 'border-rose-800/70 bg-rose-950/80 text-rose-200'
        "
      >
        {{ item.text }}
      </div>
    </TransitionGroup>
  </div>
</template>
