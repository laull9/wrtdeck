// 从 LuCI 薄壳进入面板时的凭据交接。
//
// 两种交接方式，都只传「一次性登录码」，不传长期凭据：
//   1. 地址栏带 ?code= 或 #code= 跳转过来；
//   2. 面板被 iframe 嵌入，父窗口用 postMessage 递过来。
// 登录码只能用一次、60 秒过期，且兑换接口只认同源请求，
// 因此它出现在地址栏或消息里都不会变成一条可复用的后门。

// 父窗口用来交接凭据的消息类型
const handoff_message = 'wrtdeck.handoff'
// 面板告诉父窗口「我加载好了，可以发了」，父窗口收到后重发一次交接消息
const ready_message = 'wrtdeck.ready'
// 面板告诉父窗口「凭据已处理，不用再发」，父窗口据此停止重试
const accepted_message = 'wrtdeck.accepted'

// 等待父窗口交接的最长时间，超时就继续走后面的引导路径
const handoff_wait_ms = 1500

// 等待中的交接流程，收到父窗口消息时唤醒
let pending_handoff: ((code: string) => void) | null = null

// 常驻监听父窗口的交接消息。
// 只接受来自 window.parent 的消息：其它来源即使猜到消息格式，也换不到任何东西。
window.addEventListener('message', (event: MessageEvent) => {
  if (event.source !== window.parent || !pending_handoff) {
    return
  }
  const data = event.data as { type?: string; code?: unknown } | null
  if (!data || data.type !== handoff_message) {
    return
  }
  pending_handoff(typeof data.code === 'string' ? data.code : '')
})

// 从地址栏取出一次性登录码，同时兼容写在 hash 里的形态
export function code_from_url(): string {
  const search = new URLSearchParams(window.location.search)
  if (search.get('code')) {
    return search.get('code') ?? ''
  }
  const hash = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  return hash.get('code') ?? ''
}

// 抹掉地址栏里的登录码与 Token，避免它们进入浏览器历史、书签或用户分享的链接
export function strip_url_credentials(): void {
  const url = new URL(window.location.href)
  let dirty = false
  for (const key of ['code', 'token']) {
    if (url.searchParams.has(key)) {
      url.searchParams.delete(key)
      dirty = true
    }
  }
  const hash = new URLSearchParams(url.hash.replace(/^#/, ''))
  if (hash.has('code') || hash.has('token')) {
    url.hash = ''
    dirty = true
  }
  if (dirty) {
    window.history.replaceState(null, '', url.pathname + url.search)
  }
}

// 告诉父窗口凭据已处理，父窗口据此停止重试
export function ack_parent(): void {
  if (window.parent !== window) {
    window.parent.postMessage({ type: accepted_message }, '*')
  }
}

// 等待父窗口交接登录码；不在 iframe 里时立即返回空串
export function wait_for_handoff(): Promise<string> {
  if (window.parent === window) {
    return Promise.resolve('')
  }
  return new Promise((resolve) => {
    let timer = 0
    const finish = (code: string): void => {
      pending_handoff = null
      window.clearTimeout(timer)
      resolve(code)
    }
    pending_handoff = finish
    timer = window.setTimeout(() => finish(''), handoff_wait_ms)
    // 主动报到：父窗口可能比我们早加载完，早发的那轮消息会落空
    window.parent.postMessage({ type: ready_message }, '*')
  })
}
