<script setup lang="ts">
// 表单分区：标题、说明与内容卡片，右上角可放操作按钮
defineProps<{
  title: string
  desc?: string
}>()
</script>

<template>
  <section class="panel p-4">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 class="text-base font-semibold text-ink-strong">{{ title }}</h3>
        <p v-if="desc" class="mt-0.5 text-sm leading-relaxed text-ink-faint">{{ desc }}</p>
      </div>
      <slot name="action" />
    </div>
    <div class="mt-4 flex flex-col gap-4">
      <slot />
    </div>
  </section>
</template>
