'use strict';
'require view';
'require rpc';
'require ui';

/*
 * WrtDeck 在 LuCI 里的薄壳：
 *   - 把面板嵌进 LuCI 的内容区，用户登录 LuCI 之后点进菜单就能直接看到面板；
 *   - 免密交接：父窗口把 rpcd 拿到的一次性登录码用 postMessage 交给面板，
 *     面板兑换成会话凭据后回执，地址栏里不会出现任何长期凭据；
 *   - 交接不成功时如实说明原因，面板会落到口令登录页，用户仍然进得去；
 *   - 服务没起来时给出启动按钮，面板没装时给出安装提示。
 *
 * 消息类型与面板 web/src/lib/handoff.ts 一一对应，改一处要同步另一处。
 */

var handoff_message = 'owdash.handoff';
var ready_message = 'owdash.ready';
var accepted_message = 'owdash.accepted';

// 面板可能比父窗口晚就绪，凭据要多投递几次；面板报到或回执时会重新计数
var resend_total = 12;
var resend_interval = 500;

var call_status = rpc.declare({ object: 'wrtdeck', method: 'status', expect: {} });
var call_handoff = rpc.declare({ object: 'wrtdeck', method: 'handoff', expect: {} });
var call_control = rpc.declare({ object: 'wrtdeck', method: 'control', params: [ 'action' ], expect: {} });

// 向目标窗口反复投递登录码，收到面板回执后停止
function attach(target_getter, credentials) {
	var state = { attempts: 0, accepted: false, timer: null };
	var payload = { type: handoff_message, code: credentials.code || '' };

	// 没有登录码就没什么可投的，面板会自己走到口令登录页
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
			target.postMessage(payload, '*');
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

	// 面板地址由浏览器拼：浏览器知道自己用的是哪个主机名，设备端不必猜自己的地址
	panelURL: function(status) {
		var port = status && status.port ? status.port : '8080';
		return 'http://' + window.location.hostname + ':' + port + '/';
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

	// 新标签页打开同样走凭据交接
	handleOpenTab: function(credentials) {
		var popup = window.open(this.panelURL(this.status), 'wrtdeck');
		if (!popup) {
			ui.addNotification(null, E('p', {}, '浏览器拦截了新窗口，请允许弹出窗口后重试'), 'warning');
			return;
		}
		attach(function() { return popup; }, credentials);
	},

	render: function(data) {
		var status = data[0] || {};
		var credentials = data[1] || {};
		this.status = status;

		var children = [
			E('h2', {}, 'WrtDeck'),
			E('p', { 'class': 'cbi-map-descr' }, [
				'设备控制面板，监听 ', E('code', {}, 'http://' + window.location.hostname + ':' + (status.port || '8080') + '/'),
				'。登录 LuCI 之后从这里进入，不需要再输一次面板口令。'
			])
		];

		if (!status.installed) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '还没有安装 WrtDeck 面板本体（owdash）。'),
				E('p', {}, '可执行：', E('code', {}, 'apk add owdash'), '（24.10 及更早版本用 ', E('code', {}, 'opkg install owdash'), '）')
			]));
			return E(children);
		}

		var controls = E('div', { 'style': 'display:flex;gap:8px;align-items:center;margin:12px 0' }, [
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
		]);
		children.push(controls);

		if (status.password_pending) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '面板仍在使用初始口令，任何人都能试着用 admin 登进来。'),
				E('p', {}, '请进入面板登录一次并立即修改口令；改完之后这里会自动恢复免密进入。'),
				E('p', {}, '忘记口令时可在设备上执行 ', E('code', {}, '/etc/init.d/owdash password'), ' 重置。')
			]));
		}
		else if (credentials.reason) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '本次未能自动交接登录凭据：' + credentials.reason + '。'),
				E('p', {}, '面板会展示口令登录页，直接从那里登录即可。')
			]));
		}

		if (!status.running) {
			children.push(E('div', { 'class': 'alert-message warning' }, [
				E('p', {}, '服务当前没有运行。'),
				E('button', {
					'class': 'btn cbi-button cbi-button-apply',
					'click': ui.createHandlerFn(this, 'handleService', 'start')
				}, '启动 WrtDeck')
			]));
			return E(children);
		}

		var frame = E('iframe', {
			'id': 'wrtdeck-frame',
			'title': 'WrtDeck 面板',
			'src': this.panelURL(status),
			'style': 'width:100%;height:72vh;min-height:520px;border:1px solid rgba(128,128,128,0.35);border-radius:8px;background:transparent'
		});
		var delivery = attach(function() { return frame.contentWindow; }, credentials);
		frame.addEventListener('load', function() { delivery.poke(); });
		children.push(frame);

		return E(children);
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
