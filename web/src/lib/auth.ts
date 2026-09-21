// 面板的会话状态。
//
// 三种凭据来源对应三种状态：
//   - 未登录：界面展示登录页；
//   - 会话凭据：口令登录或兑换登录码得到，会过期，改口令后作废；
//   - API Token：从设备上手工粘贴的长期凭据，供脚本场景使用。
//
// 服务端仍是初始口令时，会话凭据只能用于改口令，界面据此弹出不可跳过的改密框。

import { reactive } from 'vue'
import { api, ApiError } from '../api'
import { embedded } from './embed'
import { ack_parent, code_from_url, strip_url_credentials, wait_for_handoff } from './handoff'
import { clear_token, get_token, remembers, set_token } from './token'

// 凭据种类
export type AuthMode = '' | 'session' | 'api'

// 会话状态，界面直接绑定它
export const auth = reactive({
  // 启动引导是否结束，未结束时不渲染任何业务界面
  ready: false,
  token: '',
  mode: '' as AuthMode,
  // 本轮登录是不是由同源网关代办的（浏览器手里并没有凭据）
  via_gateway: false,
  // 服务端要求先改口令
  must_change: false,
  // 服务端关闭了鉴权（开发模式），此时无需登录
  open: false,
  // 服务端版本，登录页也要显示，因此由引导阶段填写
  version: '',
  // 服务端是否启用 HTTPS，登录页据此提示口令明文传输的风险
  tls: false,
  // 会话有效期（分钟）
  session_ttl_minutes: 720,
})

// 口令登录
export async function login(password: string, remember: boolean): Promise<void> {
  const result = await api.login(password, remember)
  apply_session(result.token, 'session', result.must_change_password, remember)
}

// 用长期 API Token 登录，是脚本场景与「登录码也拿不到」时的兜底入口
export async function login_with_token(value: string): Promise<void> {
  const token = value.trim()
  if (!token) {
    throw new Error('请先填入 Token')
  }
  const previous = get_token()
  set_token(token, remembers())
  auth.token = token
  auth.mode = 'api'
  auth.via_gateway = false
  try {
    const me = await api.session_me()
    auth.must_change = me.must_change_password
  } catch (err) {
    // 校验不通过就回滚，不要把一个坏凭据留在浏览器里
    set_token(previous, remembers())
    auth.token = previous
    auth.mode = previous ? 'api' : ''
    throw err
  }
}

// 改口令。成功后会拿到一张新凭据，旧凭据已被服务端作废。
export async function change_password(current: string, next: string): Promise<void> {
  const result = await api.change_password(current, next)
  auth.must_change = false
  if (result.token) {
    set_token(result.token, remembers())
    auth.token = result.token
    auth.mode = 'session'
  }
}

// 登出：先让服务端作废会话，再清掉本地凭据。服务端不可达时也要能本地登出。
export async function logout(): Promise<void> {
  if (auth.mode === 'session') {
    try {
      await api.logout()
    } catch {
      /* 忽略：本地凭据无论如何都要清掉 */
    }
  }
  forget()
}

// 只清本地凭据，不通知服务端
export function forget(): void {
  clear_token()
  auth.token = ''
  auth.mode = ''
  auth.via_gateway = false
  auth.must_change = false
}

// 服务端告知需要改口令时调用
export function require_password_change(): void {
  auth.must_change = true
}

// 启动引导：按「服务状态 → 登录码 → 父窗口交接 → 本地凭据」的顺序走一遍。
// 只有确实拿不到凭据时才把用户送到登录页，正常使用中不会平白多一次输入。
export async function bootstrap(): Promise<void> {
  // 先问一次服务状态：它免鉴权，能一次性拿到「要不要登录、要不要改初始口令、
  // 当前是不是 HTTPS」，登录页需要这些信息才能给出对味的提示。
  try {
    const health = await api.health()
    auth.open = health.server.auth_disabled
    auth.must_change = health.server.must_change_password
    auth.tls = health.server.tls_enabled
    auth.session_ttl_minutes = health.server.session_ttl_minutes
    auth.version = health.server.version
  } catch {
    /* 服务不可达，登录页会给出提示 */
  }

  // 地址栏里的一次性登录码优先级最高：它只能用一次，进来就先兑掉并把地址栏抹干净
  const url_code = code_from_url()
  if (url_code) {
    strip_url_credentials()
    if (await redeem(url_code)) {
      auth.ready = true
      return
    }
  } else {
    strip_url_credentials()
  }

  // 被 LuCI 内嵌时，同源网关可能已经用 LuCI 的登录状态替浏览器办好了凭据。
  // 这一步放在父窗口交接之前：成了就不必再等一轮消息，面板一进来就是可用的。
  if (embedded() && (await probe_gateway())) {
    auth.ready = true
    return
  }

  // 父窗口没有代办成功时，由 LuCI 薄壳递过来一个一次性登录码
  const handed_code = await wait_for_handoff()
  if (handed_code && (await redeem(handed_code))) {
    auth.ready = true
    return
  }

  // 本地已有凭据：向服务端确认它是否还有效
  const stored = get_token()
  if (stored) {
    auth.token = stored
    try {
      const me = await api.session_me()
      auth.mode = me.mode === 'api' ? 'api' : 'session'
      auth.must_change = me.must_change_password
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        forget()
      } else {
        // 服务不可达时先放行，首屏的错误提示会把原因说清楚
        auth.mode = 'session'
      }
    }
  }
  auth.ready = true
}

// 问一次自己的身份，判断同源网关有没有替这次访问办过凭据。
//
// 浏览器手里其实什么都没有：凭据是网关在服务端补上的，因此本地存储不会有痕迹，
// 只能靠「这个请求居然通过了」来确认。没有网关时这里就是一次普通的 401，无副作用。
async function probe_gateway(): Promise<boolean> {
  try {
    const me = await api.session_me()
    auth.mode = me.mode === 'api' ? 'api' : 'session'
    auth.via_gateway = true
    auth.must_change = me.must_change_password
    // 凭据已经到手，告诉父窗口不必再递登录码了。
    // 少了这一步，薄壳会按重试间隔把码投满一轮才自己停下。
    ack_parent()
    return true
  } catch {
    return false
  }
}

// 用一次性登录码换会话凭据，失败返回 false（登录码过期是常态，不打扰用户）
async function redeem(code: string): Promise<boolean> {
  try {
    const result = await api.redeem_code(code)
    apply_session(result.token, 'session', result.must_change_password, remembers())
    return true
  } catch {
    return false
  }
}

// 把一次登录结果落到状态与本地存储里，并告诉父窗口不必再重试
function apply_session(token: string, mode: AuthMode, must_change: boolean, remember: boolean): void {
  set_token(token, remember)
  auth.token = token
  auth.mode = mode
  auth.via_gateway = false
  auth.must_change = must_change
  ack_parent()
}
