<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { get_token, set_token } from '../api'

// 对话框显隐由父组件控制
const props = defineProps<{ open: boolean }>()
// 保存完成后通知父组件重连
const emit = defineEmits<{ 'update:open': [boolean]; saved: [] }>()

// 输入框中的 Token 草稿
const draft = ref('')

// 每次打开时把已保存的 Token 填进输入框
watch(
  () => props.open,
  (open) => {
    if (open) {
      draft.value = get_token()
    }
  },
)

// 保存 Token 并关闭对话框
function save(): void {
  set_token(draft.value.trim())
  emit('saved')
  emit('update:open', false)
}

// 清空 Token，回到无鉴权状态
function clear(): void {
  draft.value = ''
  set_token('')
  emit('saved')
  emit('update:open', false)
}
</script>

<template>
  <DialogRoot :open="props.open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm dark:bg-black/70" />
      <DialogContent
        class="fixed left-1/2 top-1/2 z-50 w-[min(26rem,92vw)] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-floating p-5 shadow-2xl"
      >
        <DialogTitle class="text-base font-semibold text-ink">API 访问 Token</DialogTitle>
        <DialogDescription class="mt-1 text-sm leading-relaxed text-ink-muted">
          服务默认启用随机 Bearer Token。可在服务端执行
          <code class="code-chip">owdash -print-token</code>
          查看当前 Token，或直接读取数据目录下的 secrets.json。
        </DialogDescription>

        <input
          v-model="draft"
          type="password"
          autocomplete="off"
          placeholder="粘贴 Token"
          class="field readout mt-4 w-full"
          @keydown.enter="save"
        />

        <div class="mt-5 flex items-center justify-end gap-2">
          <button type="button" class="btn btn-ghost" @click="clear">清空</button>
          <button type="button" class="btn btn-outline" @click="emit('update:open', false)">取消</button>
          <button type="button" class="btn btn-primary" @click="save">保存</button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
