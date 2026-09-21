// 面板被 LuCI 内嵌时的运行环境。
//
// 面板页面由设备自带的 Web 服务器直接服务，和 LuCI 处在同一个源上：
// 同一个域名、同一个端口，因此也自动继承了同一套加密方式——公网域名下
// 就是 HTTPS，局域网里就跟着设备当前的样子，不需要面板自己再判断一次。
//
// API 走同源下的一个 CGI 网关（见 internal/gateway）。网关前缀由 LuCI 用
// 查询参数告诉面板，面板自己绝不去猜设备的 IP 与端口：页面被反向代理到
// 公网域名之后，任何「本机地址 + 8080」的推断都必然落空。

// api_base 返回面板 API 的前缀。
// 独立访问（比如开发时直接开 127.0.0.1:8080）时为空串，API 就在当前源的根下。
export function api_base(): string {
  const raw = new URLSearchParams(window.location.search).get('api') ?? ''
  // 只认同源相对路径。带上主机名的地址会让面板把凭据发到另一台服务器上，
  // 而一个查询参数是任何链接都改得动的，不能给它这种机会。
  if (!raw.startsWith('/') || raw.startsWith('//')) {
    return ''
  }
  return raw.replace(/\/+$/, '')
}

// embedded 判断当前页面是不是被 LuCI 嵌进来的
export function embedded(): boolean {
  return new URLSearchParams(window.location.search).get('embed') === '1'
}
