import type {
  ActionFinishedPayload,
  DashboardResponse,
  Entry,
  HelloPayload,
  RegistryChangedPayload,
  RunResponse,
  SourceState,
} from './types'

// Token 存放在 localStorage，避免每次刷新都要重新输入
const token_key = 'owdash.api_token'

// 读取本地保存的 Token
export function get_token(): string {
  try {
    return localStorage.getItem(token_key) ?? ''
  } catch {
    return ''
  }
}

// 保存或清除本地 Token
export function set_token(value: string): void {
  try {
    if (value) {
      localStorage.setItem(token_key, value)
    } else {
      localStorage.removeItem(token_key)
    }
  } catch {
    /* 隐私模式下忽略写入失败 */
  }
}

// ApiError 携带 HTTP 状态码，便于区分鉴权失败
export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// request 是统一的接口调用封装，自动附带 Token 并解析错误体
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  const token = get_token()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  if (init.body) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(`/api/v1${path}`, { ...init, headers })
  if (response.status === 204) {
    return undefined as T
  }
  const text = await response.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }
  if (!response.ok) {
    const detail = (payload as { error?: { message?: string } } | null)?.error?.message
    throw new ApiError(response.status, detail ?? `请求失败（HTTP ${response.status}）`)
  }
  return payload as T
}

// api 汇总后端全部接口
export const api = {
  // 获取 Dashboard 首屏数据
  dashboard: () => request<DashboardResponse>('/dashboard'),
  // 获取注册表
  registry: () => request<{ entries: Entry[] }>('/registry'),
  // 注册或更新一条注册项
  put_entry: (entry: Entry) =>
    request<Entry>(`/registry/${encodeURIComponent(entry.id)}`, {
      method: 'PUT',
      body: JSON.stringify(entry),
    }),
  // 删除一条注册项
  delete_entry: (id: string) =>
    request<void>(`/registry/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  // 执行一个动作
  run_action: (id: string, params: Record<string, unknown>) =>
    request<RunResponse>(`/actions/${encodeURIComponent(id)}/run`, {
      method: 'POST',
      body: JSON.stringify({ params }),
    }),
  // 手动刷新一个信息源
  refresh_source: (id: string) =>
    request<{ id: string; status: string }>(`/sources/${encodeURIComponent(id)}/refresh`, {
      method: 'POST',
    }),
}

// SSE 事件回调集合
export interface EventHandlers {
  on_hello?: (payload: HelloPayload) => void
  on_source?: (state: SourceState) => void
  on_action?: (payload: ActionFinishedPayload) => void
  on_registry?: (payload: RegistryChangedPayload) => void
  on_state?: (connected: boolean) => void
}

// open_events 建立 SSE 长连接，浏览器会自动重连
// 注意：服务端把载荷直接放在 data 字段，不存在 type/at 外层信封
export function open_events(handlers: EventHandlers): () => void {
  const token = get_token()
  // EventSource 无法自定义请求头，因此通过查询参数传递 Token
  const url = token ? `/api/v1/events?token=${encodeURIComponent(token)}` : '/api/v1/events'
  const source = new EventSource(url)

  source.addEventListener('open', () => handlers.on_state?.(true))
  source.addEventListener('hello', (event) => {
    handlers.on_state?.(true)
    const payload = parse_data<HelloPayload>(event)
    if (payload) {
      handlers.on_hello?.(payload)
    }
  })
  source.addEventListener('source.updated', (event) => {
    const state = parse_data<SourceState>(event)
    if (state) {
      handlers.on_source?.(state)
    }
  })
  source.addEventListener('action.finished', (event) => {
    const payload = parse_data<ActionFinishedPayload>(event)
    if (payload) {
      handlers.on_action?.(payload)
    }
  })
  source.addEventListener('registry.changed', (event) => {
    const payload = parse_data<RegistryChangedPayload>(event)
    if (payload) {
      handlers.on_registry?.(payload)
    }
  })
  source.addEventListener('error', () => handlers.on_state?.(false))

  return () => source.close()
}

// parse_data 安全解析 SSE 帧携带的 JSON
function parse_data<T>(event: Event): T | null {
  const raw = (event as MessageEvent<string>).data
  if (!raw) {
    return null
  }
  try {
    return JSON.parse(raw) as T
  } catch {
    return null
  }
}
