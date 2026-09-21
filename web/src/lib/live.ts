// 面板的实时通道。
//
// 首选 SSE：面板被直接访问时，它能把状态变化即时推过来。但面板还可能被
// 设备自带的 Web 服务器以 CGI 的方式转发，那一段是短命进程，一条长连接
// 就等于在路由器上常驻一个进程，而它未必肯把事件送得出来。
//
// 因此分两种情况：
//   - 直接访问：先试 SSE，连上后迟迟收不到事件就自动换成定时拉取；
//   - 被 LuCI 内嵌：直接走定时拉取，不试长连接——路由器的内存比
//     「少一次轮询」金贵，而且省掉试错的那几秒与一次失败的请求。
// 定时拉取只在页面可见时进行，切到后台立刻停掉。

import { open_events } from '../api'
import type { EventHandlers } from '../api'
import { embedded } from './embed'

// 通道状态：推送 / 轮询 / 断开
export type LiveMode = 'sse' | 'poll' | 'off'

export interface LiveOptions extends EventHandlers {
  // poll 是降级后的一次全量拉取，返回值用于决定要不要退避
  poll?: () => Promise<void>
  // on_mode 报告当前走的是哪条通道
  on_mode?: (mode: LiveMode) => void
}

// 等待首个事件的最长时间，超时就认为这条长连接送不出东西
const sse_probe_ms = 4000
// 轮询的基础间隔与上限，连续失败时按倍数退避
const poll_base_ms = 5000
const poll_max_ms = 30000

// start_live 开启实时通道，返回关闭函数
export function start_live(options: LiveOptions): () => void {
  let closed = false
  let mode: LiveMode = 'off'
  let close_sse: (() => void) | null = null
  let probe_timer = 0
  let poll_timer = 0
  let poll_interval = poll_base_ms
  let got_event = false

  // 清理所有定时器，切通道与关闭时都要用
  function clear_timers(): void {
    window.clearTimeout(probe_timer)
    window.clearTimeout(poll_timer)
    probe_timer = 0
    poll_timer = 0
  }

  // 切换通道并通知外部，重复设置同一条通道时不打扰界面
  function set_mode(next: LiveMode): void {
    if (mode === next) {
      return
    }
    mode = next
    options.on_mode?.(next)
  }

  // 页面可见时才拉取，隐藏时干脆不排下一轮
  function visible(): boolean {
    return document.visibilityState !== 'hidden'
  }

  // 排下一轮轮询；页面不可见时停下来，等重新可见再排
  function schedule_poll(delay: number): void {
    window.clearTimeout(poll_timer)
    if (closed || !visible()) {
      return
    }
    poll_timer = window.setTimeout(run_poll, delay)
  }

  // 执行一次拉取，成功就恢复基础间隔，失败则成倍退避
  async function run_poll(): Promise<void> {
    if (closed) {
      return
    }
    if (!options.poll) {
      set_mode('off')
      return
    }
    try {
      await options.poll()
      poll_interval = poll_base_ms
      set_mode('poll')
    } catch {
      options.on_state?.(false)
      poll_interval = Math.min(poll_interval * 2, poll_max_ms)
    }
    schedule_poll(poll_interval)
  }

  // 收到任何一个事件都说明这条长连接是通的
  function mark_alive(): void {
    got_event = true
    window.clearTimeout(probe_timer)
  }

  // 打开长连接，并在超时未收到事件时降级
  function start_sse(): void {
    got_event = false
    close_sse = open_events({
      on_state: (state) => {
        // 只连上不算数：中间的缓冲层可能让连接一直开着却没有任何数据，
        // 真正能说明问题的信号是「收到了一个事件」，见 mark_alive
        if (!state) {
          options.on_state?.(false)
        }
      },
      on_hello: (payload) => {
        mark_alive()
        set_mode('sse')
        options.on_state?.(true)
        options.on_hello?.(payload)
      },
      on_source: (state) => {
        mark_alive()
        set_mode('sse')
        options.on_source?.(state)
      },
      on_action: (payload) => {
        mark_alive()
        set_mode('sse')
        options.on_action?.(payload)
      },
      on_registry: (payload) => {
        mark_alive()
        set_mode('sse')
        options.on_registry?.(payload)
      },
    })
    probe_timer = window.setTimeout(() => {
      if (!got_event && !closed) {
        stop_sse()
        poll_interval = poll_base_ms
        void run_poll()
      }
    }, sse_probe_ms)
  }

  // 关掉长连接，之后不再重开
  function stop_sse(): void {
    window.clearTimeout(probe_timer)
    probe_timer = 0
    close_sse?.()
    close_sse = null
  }

  // 页面重新可见时立刻拉一次，不用等下一个轮询周期
  function on_visibility(): void {
    if (closed || mode !== 'poll' || !visible()) {
      return
    }
    window.clearTimeout(poll_timer)
    void run_poll()
  }

  document.addEventListener('visibilitychange', on_visibility)
  if (embedded()) {
    // 被 LuCI 内嵌：这条路上的 API 由设备 Web 服务器以 CGI 转发，
    // 一条长连接会在路由器上常驻一个进程，直接走轮询更划算
    void run_poll()
  } else {
    start_sse()
  }

  return () => {
    closed = true
    stop_sse()
    clear_timers()
    document.removeEventListener('visibilitychange', on_visibility)
    set_mode('off')
  }
}
