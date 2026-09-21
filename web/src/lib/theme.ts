import { ref } from 'vue'

// 主题取值：界面只有深浅两套，没有第三态
export type Theme = 'light' | 'dark'

// 本地存储键名，必须与 index.html 内联脚本里的字面量一致，否则刷新会闪一下
const storage_key = 'wrtdeck.theme'

// 两套主题对应的移动端地址栏配色，取值与 style.css 里的 --c-canvas 一致
const chrome_color: Record<Theme, string> = { light: '#f4f5f7', dark: '#08090c' }

// 系统配色偏好，只在用户没有显式选择过时作为依据
const media = window.matchMedia('(prefers-color-scheme: dark)')

// 当前生效的主题，供组件读取
export const theme = ref<Theme>('light')

// 是否已有显式选择：有值时系统配色变化不再自动跟随
let pinned = false

// 读取已保存的主题，非法值一律当作没保存过
function stored(): Theme | null {
  try {
    const raw = window.localStorage.getItem(storage_key)
    return raw === 'light' || raw === 'dark' ? raw : null
  } catch {
    // 隐私模式下读取会抛错，退回跟随系统
    return null
  }
}

// 计算当前应当生效的主题
function resolve(): Theme {
  return stored() ?? (media.matches ? 'dark' : 'light')
}

// 落地主题：切换 html 上的类并同步地址栏配色
function apply(next: Theme): void {
  theme.value = next
  document.documentElement.classList.toggle('dark', next === 'dark')
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', chrome_color[next])
}

// 切换主题，并把这次选择记住
export function toggle_theme(): void {
  pinned = true
  const next: Theme = theme.value === 'dark' ? 'light' : 'dark'
  try {
    window.localStorage.setItem(storage_key, next)
  } catch {
    // 隐私模式下写不进去，本次切换依然生效，只是刷新后回到跟随系统
  }
  apply(next)
}

// 跟随系统配色，仅在用户还没手动选择过时生效
media.addEventListener('change', () => {
  if (!pinned) {
    apply(resolve())
  }
})

// 启动时对齐一次，保证地址栏配色与首帧已经生效的主题一致
pinned = stored() !== null
apply(resolve())
