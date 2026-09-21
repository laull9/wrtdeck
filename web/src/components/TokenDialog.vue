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
import { login_with_token } from '../lib/auth'
import { get_token } from '../lib/token'

// 对话框显隐由父组件控制
const props = defineProps<{ open: boolean }>()
// 保存完成后通知父组件刷新
const emit = defineEmits<{ 'update:open': [boolean]; saved: [] }>()

// 输入框中的 Token 草稿
const draft = ref('')
const busy = ref(false)
const error = ref('')

// 每次打开时把已保存的 Token 填进输入框
watch(
  () => props.open,
  (open) => {
    if (open) {
      draft.value = get_token()
      error.value = ''
    }
  },
)

// 用长期 API Token 登录，校验通过才算成功
async function save(): Promise<void> {
  if (busy.value) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    await login_with_token(draft.value)
    emit('saved')
    emit('update:open', false)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogRoot :open="props.open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm dark:bg-black/70" />
      <DialogContent
        class="fixed left-1/2 top-1/2 z-50 w-[min(26rem,92vw)] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-floating p-5 shadow-2xl"
      >
        <DialogTitle class="text-base font-semibold text-ink">用 API Token 登录</DialogTitle>
        <DialogDescription class="mt-1 text-sm leading-relaxed text-ink-muted">
          这是给脚本与运维准备的长期凭据，不会过期。日常使用请用口令登录，
          口令登录的会话可以随时登出，也不用担心凭据长期留在浏览器里。
        </DialogDescription>

        <p class="mt-3 text-sm leading-relaxed text-ink-muted">
          获取方式：在设备上执行 <code class="code-chip">/etc/init.d/wrtdeck token</code>。
        </p>

        <input
          v-model="draft"
          type="password"
          autocomplete="off"
          placeholder="粘贴 Token"
          class="field readout mt-4 w-full"
          :disabled="busy"
          @keydown.enter="save"
        />

        <p v-if="error" class="field-error mt-2">{{ error }}</p>

        <div class="mt-5 flex items-center justify-end gap-2">
          <button type="button" class="btn btn-outline" @click="emit('update:open', false)">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy || !draft" @click="save">
            {{ busy ? '校验中…' : '使用该 Token' }}
          </button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
