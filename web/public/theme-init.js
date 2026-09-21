// 首帧主题引导。
//
// 它必须在页面渲染前同步执行（因此是普通脚本、并且放在 head 里），
// 否则刷新时会先闪一下另一套配色。
//
// 之所以不写成内联脚本：服务端会下发 script-src 'self' 的 CSP，
// 内联脚本会被浏览器直接拦掉，主题就会闪。键名与取值逻辑必须与 src/lib/theme.ts 保持一致。
;(function () {
  var dark = false
  try {
    var saved = window.localStorage.getItem('wrtdeck.theme')
    dark =
      saved === 'dark' ||
      (saved !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  } catch (err) {
    /* 隐私模式下 localStorage 不可用，退回跟随系统 */
    dark = window.matchMedia('(prefers-color-scheme: dark)').matches
  }
  if (dark) {
    document.documentElement.classList.add('dark')
  }
  var meta = document.querySelector('meta[name="theme-color"]')
  if (meta) {
    meta.setAttribute('content', dark ? '#08090c' : '#f4f5f7')
  }
})()
