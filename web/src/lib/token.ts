// 面板凭据的本地存储。
//
// 默认只放在 sessionStorage：关掉标签页就没了，在共用的电脑上不会留下长期凭据。
// 登录时勾选「在本浏览器保持登录」才落到 localStorage。
// 先记住、后又取消勾选的情况下两种存储会同时存在旧值，因此读取时两个都查，
// 写入时先把两边都清干净。

const token_key = 'wrtdeck.token'
const remember_key = 'wrtdeck.token.remember'

// 隐私模式下两种存储都可能写入失败，此时退化成只在当前页面内有效
let memory_token = ''

// 读取当前凭据，没有则返回空串
export function get_token(): string {
  if (memory_token) {
    return memory_token
  }
  try {
    return localStorage.getItem(token_key) ?? sessionStorage.getItem(token_key) ?? ''
  } catch {
    return ''
  }
}

// 保存凭据；remember 为真时落到 localStorage，否则只活到标签页关闭
export function set_token(value: string, remember = false): void {
  memory_token = value
  try {
    localStorage.removeItem(token_key)
    localStorage.removeItem(remember_key)
    sessionStorage.removeItem(token_key)
    sessionStorage.removeItem(remember_key)
    if (!value) {
      return
    }
    const store = remember ? localStorage : sessionStorage
    store.setItem(token_key, value)
    store.setItem(remember_key, remember ? '1' : '0')
  } catch {
    /* 隐私模式下忽略写入失败 */
  }
}

// 清除凭据
export function clear_token(): void {
  set_token('', false)
}

// 上次是否勾选了「在本浏览器保持登录」
export function remembers(): boolean {
  try {
    return localStorage.getItem(remember_key) === '1'
  } catch {
    return false
  }
}
