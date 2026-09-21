'use strict';
'require view';
'require rpc';
'require ui';

/*
 * WrtDeck 在 LuCI 里的入口：把面板原样嵌进当前页面，而不是给一个跳转链接。
 *
 * 面板页面与它调用的 API 都走 LuCI 自己的那个源：
 *   - 页面由设备自带的 Web 服务器从 /www/wrtdeck 直接服务；
 *   - API 由同源下的一个 CGI 网关（cgi_prefix/wrtdeck-api）转交给只监听回环的面板本体。
 * 浏览器因此全程只跟一个主机名、一个端口打交道，加密方式也自动跟随 LuCI：
 * 页面被反向代理到公网域名时，不会再出现「让浏览器去连设备 IP 的 8080」
 * 这种必然失败的推断——面板自己也不知道自己被挂在了哪个域名后面。
 *
 * 凭据交接与之前一致：rpcd 用设备本机的凭据换一张一次性登录码，父窗口用
 * postMessage 递给面板，面板兑换成会话凭据后回执。若同源网关已经用 LuCI 的
 * 登录状态替浏览器办好了凭据，面板会跳过这一步，用户全程无感。
 *
 * 消息类型与面板 web/src/lib/handoff.ts 一一对应，改一处要同步另一处。
 */

var handoff_message = 'wrtdeck.handoff';
var ready_message = 'wrtdeck.ready';
var accepted_message = 'wrtdeck.accepted';

// 面板可能比父窗口晚就绪，登录码要多投递几次；面板报到或回执时会重新计数
var resend_total = 12;
var resend_interval = 500;

// 面板页面在 Web 根目录下的位置，与面板导出资源时的目标目录一致
var panel_dir = 'wrtdeck';
// 同源网关的 CGI 名，与 /www/<cgi_prefix>/wrtdeck-api 对应
var gateway_name = 'wrtdeck-api';

var call_status = rpc.declare({ object: 'wrtdeck', method: 'status', expect: {} });
var call_handoff = rpc.declare({ object: 'wrtdeck', method: 'handoff', expect: {} });
var call_control = rpc.declare({ object: 'wrtdeck', method: 'control', params: [ 'action' ], expect: {} });

// 归一化路径前缀：补上头部的斜杠、去掉尾部的斜杠，空值退回给定的默认值
function normalize_prefix(value, fallback) {
	var text = typeof value === 'string' ? value.replace(/^\s+|\s+$/g, '') : '';
	if (!text) {
		text = fallback;
	}
	if (text.charAt(0) !== '/') {
		text = '/' + text;
	}
	return text.replace(/\/+$/, '');
}

// 面板页面地址：面板在本体启动时被导出到 Web 根目录下的 panel_dir，
// **与 CGI 前缀无关**——挂在 cgi_prefix 下的只有 API 网关那一份网关脚本。
// 把 CGI 前缀也拼进页面地址会得到一个必然 404 的 URL（/cgi-bin/wrtdeck/...），
// 现象是 LuCI 里那个面板框一片空白。同源根相对路径的好处是自动继承 LuCI 的
// 域名、端口与加密方式。写死 index.html 而不依赖目录索引，因为索引文件名在
// Web 服务器上是可配置的。
function panel_page() {
	return '/' + panel_dir + '/index.html';
}

// 面板 API 前缀：网关把 cgi_prefix 下的这一段转交给面板本体
function panel_api(cgi_prefix) {
	return normalize_prefix(cgi_prefix, '/cgi-bin') + '/' + gateway_name;
}

// 面板页面需要知道 API 前缀，用查询参数告诉它；
// embed=1 让它知道自己正被别的页面嵌着，从而走免登录与降级通道。
function panel_src(status) {
	return panel_page() +
		'?embed=1&api=' + encodeURIComponent(panel_api(status.cgi_prefix));
}

// 访问是否走的是明文。
// 面板继承了 LuCI 的加密方式，这里只是把「你现在是明文访问」如实说出来：
// 域名是公网的却用明文打开时，口令与凭据都会在半路上裸奔。
//
// 只读 window.location.origin，不去碰主机名与端口：面板的地址不从这里来，
// 这里也不该长出任何一段可以拼地址的代码。
function plaintext_risk() {
	var origin = String(window.location.origin || '').toLowerCase();
	// 已经加密，没什么可提醒的
	if (origin.indexOf('https:') === 0) {
		return false;
	}
	// origin 形如「协议 + :// + 主机 + 可选端口」，这里只关心主机那一段
	var host = origin.replace(/^[a-z]+:\/\/+/, '').split('/')[0].split(':')[0];
	if (!host || host === 'localhost') {
		return false;
	}
	// 局域网地址（IPv4 与方括号写法的 IPv6）：明文访问正是设备默认的样子
	if (/^\d+\.\d+\.\d+\.\d+$/.test(host) || host.indexOf('[') >= 0) {
		return false;
	}
	return !/(\.local|\.lan|\.internal|\.home)$/.test(host);
}

// 向目标窗口反复投递一次性登录码，收到面板回执后停止
function attach(target_getter, credentials, origin) {
	var state = { attempts: 0, accepted: false, timer: null };
	var payload = { type: handoff_message, code: credentials.code || '' };

	// 没有登录码就没什么可投的；面板会自己走同源网关或口令登录页
	if (!payload.code) {
		return { poke: function() {} };
	}

	function send() {
		if (state.accepted || state.attempts >= resend_total) {
			return;
		}
		state.attempts += 1;
		var target = target_getter();
		if (target) {
			target.postMessage(payload, origin);
		}
	}

	// 收到面板消息：报到就重新投一轮，回执就彻底停下
	window.addEventListener('message', function(event) {
		var target = target_getter();
		if (!target || event.source !== target || !event.data || typeof event.data.type !== 'string') {
			return;
		}
		if (event.data.type === ready_message) {
			state.attempts = 0;
			send();
		}
		else if (event.data.type === accepted_message) {
			state.accepted = true;
		}
	});

	state.timer = window.setInterval(function() {
		if (state.accepted || state.attempts >= resend_total) {
			window.clearInterval(state.timer);
			return;
		}
		send();
	}, resend_interval);

	return {
		// 页面每次加载完都把计数归零再投一轮，覆盖面板刷新与延迟加载两种情况
		poke: function() {
			if (state.accepted) {
				return;
			}
			state.attempts = 0;
			send();
		}
	};
}

return view.extend({
	load: function() {
		return Promise.all([ call_status(), call_handoff() ]);
	},

	// 启停服务后刷新页面状态
	handleService: function(action) {
		var self = this;
		return call_control(action).then(function(result) {
			ui.addNotification(null, E('p', {}, result && result.message ? result.message : action), 'info');
			window.setTimeout(function() {
				window.location.reload();
			}, 1200);
			return self.load();
		}).catch(function(error) {
			ui.addNotification(null, E('p', {}, '操作失败：' + error), 'error');
		});
	},

	// 新标签页打开同样走凭据交接：面板与 LuCI 同源，postMessage 照样说得上话
	handleOpenTab: function(credentials) {
		var popup = window.open(this.src, 'wrtdeck');
		if (!popup) {
			ui.addNotification(null, E('p', {}, '浏览器拦截了新窗口，请允许弹出窗口后重试'), 'warning');
			return;
		}
		attach(function() { return popup; }, credentials, window.location.origin);
	},

	// 面板页面还没落到 Web 根目录时的提示与补救
	renderNotReady: function(status, children) {
		if (!status.installed) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '还没有安装 WrtDeck 面板本体（wrtdeck）。'),
				E('p', {}, '可执行：', E('code', {}, 'apk add wrtdeck'),
					'（24.10 及更早版本用 ', E('code', {}, 'opkg install wrtdeck'), '）')
			]));
			return E(children);
		}
		children.push(E('div', { 'class': 'alert-message warning' }, [
			E('p', {}, '面板页面还没有就绪：设备上找不到 ',
				E('code', {}, '/www/' + panel_dir + '/index.html'), '。'),
			E('p', {}, '面板在本体启动时把页面导出到 Web 根目录，重启一次服务即可完成。')
		]));
		children.push(E('button', {
			'class': 'btn cbi-button cbi-button-apply',
			'click': ui.createHandlerFn(this, 'handleService', 'restart')
		}, '重启 WrtDeck 并导出页面'));
		return E(children);
	},

	render: function(data) {
		var status = data[0] || {};
		var credentials = data[1] || {};
		this.src = panel_src(status);

		var children = [
			E('h2', {}, 'WrtDeck'),
			E('p', { 'class': 'cbi-map-descr' }, [
				'设备控制面板，直接内嵌在本页里，与 LuCI 同一个地址、同一个端口，',
				'加密方式也跟随 LuCI：', E('code', {}, window.location.origin), '。',
				'从 LuCI 登录后进入不需要再输一次面板口令。'
			])
		];

		// 面板页面没就绪时先把原因说清楚，不必让用户面对一个空白框
		if (!status.installed || !status.web_ready) {
			return this.renderNotReady(status, children);
		}

		children.push(E('div', { 'style': 'display:flex;gap:8px;align-items:center;margin:12px 0;flex-wrap:wrap' }, [
			E('button', {
				'class': 'btn cbi-button cbi-button-action',
				'disabled': status.running ? '' : null,
				'click': ui.createHandlerFn(this, 'handleService', 'restart')
			}, '重启服务'),
			E('button', {
				'class': 'btn cbi-button cbi-button-apply',
				'disabled': status.running ? '' : null,
				'click': ui.createHandlerFn(this, 'handleOpenTab', credentials)
			}, '在新标签页打开'),
			E('span', { 'style': 'margin-left:auto;color:#6b7280' },
				(status.running ? '服务运行中' : '服务未运行') + (status.version ? ' · ' + status.version : ''))
		]));

		if (plaintext_risk()) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '当前通过明文 HTTP 访问这个域名，登录 LuCI 与使用面板的凭据都会在链路上明文传输。'),
				E('p', {}, '面板不会自己另开端口、也不会另起一套证书，它的加密方式完全跟随 LuCI。',
					'要让面板与 LuCI 一起走 HTTPS，请为 ', E('code', {}, '/etc/config/uhttpd'),
					' 配置证书并打开 ', E('code', {}, 'redirect_https'), '，',
					'或把整个 LuCI 放在 HTTPS 反向代理之后。')
			]));
		}

		if (status.password_pending) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '面板仍在使用初始口令。面板本体只监听本机地址、不对外开放，',
					'但初始口令是公开信息，建议进入面板后立即修改。'),
				E('p', {}, '忘记口令时可在设备上执行 ', E('code', {}, '/etc/init.d/wrtdeck password'), ' 重置。')
			]));
		}

		if (!status.running) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '服务当前没有运行，面板页面与接口都不可达。'),
				E('button', {
					'class': 'btn cbi-button cbi-button-apply',
					'click': ui.createHandlerFn(this, 'handleService', 'start')
				}, '启动 WrtDeck')
			]));
			return E(children);
		}

		// 同源网关没就绪时面板仍能打开，只是进不去就说明是这条路断了
		if (!status.gateway_ready) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '同源接口网关不在 Web 服务器的 CGI 前缀下，面板将无法自动进入。'),
				E('p', {}, '安装脚本会按 ', E('code', {}, 'uhttpd.main.cgi_prefix'),
					' 补一次入口；若仍不行，请确认该选项非空。')
			]));
		}

		if (credentials.reason) {
			children.push(E('div', { 'class': 'alert-message notice' }, [
				E('p', {}, '本次未能自动交接登录凭据：' + credentials.reason + '。'),
				E('p', {}, '面板会改用 LuCI 的登录状态免登录进入；两条路都不成时，它会展示口令登录页。')
			]));
		}

		var frame = E('iframe', {
			'id': 'wrtdeck-frame',
			'title': 'WrtDeck 面板',
			'src': this.src,
			'style': 'width:100%;height:72vh;min-height:520px;border:1px solid rgba(128,128,128,0.35);border-radius:8px;background:transparent'
		});
		var delivery = attach(function() { return frame.contentWindow; }, credentials, window.location.origin);
		frame.addEventListener('load', function() { delivery.poke(); });
		children.push(frame);

		return E(children);
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
