import type {
  ActionFinishedPayload,
  DashboardResponse,
  Entry,
  HealthResponse,
  HelloPayload,
  RegistryChangedPayload,
  RunResponse,
  SessionResponse,
  SourceState,
} from './types'
import { get_token } from './lib/token'

// ApiError 携带 HTTP 状态码与后端错误码，便于区分鉴权失败、登录限速与需要改口令
export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

// request 是统一的接口调用封装，自动附带凭据并解析错误体
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
    const detail = (payload as { error?: { message?: string; code?: string } } | null)?.error
    throw new ApiError(
      response.status,
      detail?.code ?? '',
      detail?.message ?? `请求失败（HTTP ${response.status}）`,
    )
  }
  return payload as T
}

// api 汇总后端全部接口
export const api = {
  // 健康检查，不需要鉴权，前端启动时用它判断鉴权状态与是否需要改初始口令
  health: () => request<HealthResponse>('/health'),
  // 口令登录，成功返回会话凭据
  login: (password: string, remember: boolean) =>
    request<SessionResponse>('/session/login', {
      method: 'POST',
      body: JSON.stringify({ password, remember }),
    }),
  // 查询当前凭据的状态；未登录时返回 401，前端用它确认本地凭据是否还有效
  session_me: () => request<SessionResponse>('/session/me'),
  // 修改口令；口令变更后服务端会作废全部旧会话，因此可能返回一张新凭据
  change_password: (current_password: string, new_password: string) =>
    request<SessionResponse>('/session/password', {
      method: 'POST',
      body: JSON.stringify({ current_password, new_password }),
    }),
  // 登出，作废服务端会话
  logout: () => request<{ ok: boolean }>('/session/logout', { method: 'POST', body: '{}' }),
  // 用一次性登录码换取会话凭据，登录码用后即废
  redeem_code: (code: string) =>
    request<SessionResponse>('/session/redeem', {
      method: 'POST',
      body: JSON.stringify({ code }),
    }),
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
