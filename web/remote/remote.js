'use strict';

// The phone side of gexi's LAN remote: pair once with a code, then read a
// session's terminal and type into it. It speaks the same JSON methods the
// app's control socket does.

const app = document.getElementById('app');
const zh = (navigator.language || 'en').toLowerCase().startsWith('zh');

const L = zh ? {
  pairTitle: '连接这台 Mac',
  pairBody: '在 Mac 上打开 gexi › 设置 › 远程，输入那里显示的 6 位配对码。',
  pairAction: '连接',
  codeNeeds6: '配对码是 6 位数字',
  pairFailed: '无法连接',
  wrongCode: '配对码不对',
  tooMany: '尝试次数太多，请等一分钟',
  remoteOff: '这台 Mac 没有开启远程访问',
  notPaired: '配对已失效，请重新配对',
  requestFailed: '请求失败',
  sessions: '会话',
  noSessions: '这台 Mac 上还没有会话',
  back: '返回',
  refresh: '刷新',
  session: '会话',
  inputPlaceholder: '输入后按回车',
  send: '发送',
} : {
  pairTitle: 'Connect to this Mac',
  pairBody: 'On the Mac, open gexi › Settings › Remote and enter the 6-digit pairing code shown there.',
  pairAction: 'Connect',
  codeNeeds6: 'The pairing code is 6 digits',
  pairFailed: 'Could not connect',
  wrongCode: 'That pairing code is not right',
  tooMany: 'Too many tries; wait a minute',
  remoteOff: 'This Mac does not have remote access on',
  notPaired: 'This device is no longer paired',
  requestFailed: 'Request failed',
  sessions: 'Sessions',
  noSessions: 'No sessions on this Mac yet',
  back: 'Back',
  refresh: 'Refresh',
  session: 'Session',
  inputPlaceholder: 'Type, then Return',
  send: 'Send',
};

const state = {
  token: localStorage.getItem('gexi.token') || '',
  host: localStorage.getItem('gexi.host') || 'gexi',
  sessions: [],
  panes: [],
  session: null,
  paneID: '',
  text: '',
  title: '',
  cwd: '',
  timer: null,
  autoScroll: true,
  polling: false,
};

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function stopPolling() {
  if (state.timer) {
    clearInterval(state.timer);
    state.timer = null;
  }
}

function unpair() {
  localStorage.removeItem('gexi.token');
  localStorage.removeItem('gexi.host');
  state.token = '';
  stopPolling();
  state.session = null;
  showPairing(L.notPaired);
}

async function call(method, params) {
  const res = await fetch('/api/rpc', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer ' + state.token,
    },
    body: JSON.stringify({ method, params: params || {} }),
  });
  if (res.status === 401) {
    unpair();
    const err = new Error(L.notPaired);
    err.unpaired = true;
    throw err;
  }
  let body = null;
  try { body = await res.json(); } catch (_) { /* not JSON */ }
  if (!res.ok) throw new Error((body && body.error) || L.requestFailed);
  if (!body || body.ok !== true) throw new Error((body && body.error) || L.requestFailed);
  return body.result;
}

function setBanner(message) {
  const banner = document.getElementById('banner');
  if (!banner) return;
  banner.textContent = message || '';
  banner.classList.toggle('hidden', !message);
}

function pairError(message) {
  if (/wrong pairing code/i.test(message)) return L.wrongCode;
  if (/too many/i.test(message)) return L.tooMany;
  if (/remote access is off/i.test(message)) return L.remoteOff;
  return message;
}

// MARK: pairing

function showPairing(message) {
  stopPolling();
  state.session = null;
  app.innerHTML = `
    <section class="pairing">
      <div class="brand">gexi</div>
      <h1>${esc(L.pairTitle)}</h1>
      <p>${esc(L.pairBody)}</p>
      <input id="code" class="code-input" type="tel" inputmode="numeric" pattern="[0-9]*"
             maxlength="6" autocomplete="one-time-code" placeholder="000000">
      <div id="pairError" class="error">${esc(message || '')}</div>
      <button id="pairBtn" class="primary">${esc(L.pairAction)}</button>
    </section>`;

  const input = document.getElementById('code');
  const error = document.getElementById('pairError');
  const button = document.getElementById('pairBtn');

  const submit = async () => {
    const code = (input.value || '').trim();
    if (code.length !== 6) {
      error.textContent = L.codeNeeds6;
      return;
    }
    error.textContent = '';
    button.disabled = true;
    try {
      const res = await fetch('/api/pair', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code, device: deviceLabel() }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(pairError(body.error || L.pairFailed));
      state.token = body.token;
      state.host = body.host || 'gexi';
      localStorage.setItem('gexi.token', state.token);
      localStorage.setItem('gexi.host', state.host);
      showSessions();
    } catch (err) {
      error.textContent = err.message || L.pairFailed;
      button.disabled = false;
    }
  };

  button.onclick = submit;
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter') submit();
  });
  input.focus();
}

function deviceLabel() {
  const ua = navigator.userAgent || '';
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua)) return 'iPad';
  if (/Android/.test(ua)) return 'Android';
  if (/Macintosh/.test(ua)) return 'Mac browser';
  return 'browser';
}

// MARK: sessions

function showSessions() {
  stopPolling();
  state.session = null;
  app.innerHTML = `
    <section class="screen">
      <header>
        <div class="grow">
          <div class="title">${esc(state.host)}</div>
          <div class="subtitle">${esc(L.sessions)}</div>
        </div>
        <button id="refresh" class="icon-btn" aria-label="${esc(L.refresh)}">⟳</button>
      </header>
      <div id="banner" class="banner hidden"></div>
      <div id="list" class="list"></div>
    </section>`;

  document.getElementById('refresh').onclick = () => loadSessions();
  loadSessions();
}

async function loadSessions() {
  try {
    const results = await Promise.all([call('session.list'), call('pane.list', { all: true })]);
    state.sessions = results[0] || [];
    state.panes = results[1] || [];
    setBanner('');
    renderSessions();
  } catch (err) {
    if (!err.unpaired) setBanner(err.message);
  }
}

function panesOf(session) {
  const ids = session.panes || [];
  return state.panes.filter(p => ids.includes(p.id));
}

function activityOf(session) {
  const panes = panesOf(session);
  if (panes.some(p => p.activity && p.activity.state === 'waiting')) return 'waiting';
  if (panes.some(p => p.activity && p.activity.state === 'working')) return 'working';
  return '';
}

function renderSessions() {
  const list = document.getElementById('list');
  if (!list) return;
  if (!state.sessions.length) {
    list.innerHTML = `<div class="empty">${esc(L.noSessions)}</div>`;
    return;
  }
  const frag = document.createDocumentFragment();
  for (const session of state.sessions) {
    const panes = panesOf(session);
    const path = panes.length ? panes[0].cwd : '';
    const row = document.createElement('div');
    row.className = 'row';
    row.innerHTML = `
      <div class="dot ${activityOf(session)}"></div>
      <div class="main">
        <div class="name">${esc(session.title || L.session)}</div>
        <div class="path">${esc(path)}</div>
      </div>`;
    row.onclick = () => openSession(session);
    frag.appendChild(row);
  }
  list.replaceChildren(frag);
}

// MARK: terminal

const KEYS = [
  ['esc', '\u001b'],
  ['^C', '\u0003'],
  ['tab', '\t'],
  ['←', '\u001b[D'],
  ['↑', '\u001b[A'],
  ['↓', '\u001b[B'],
  ['→', '\u001b[C'],
  ['⏎', '\r'],
];

function openSession(session) {
  state.session = session;
  state.text = '';
  state.title = session.title || L.session;
  state.cwd = '';
  state.autoScroll = true;

  const panes = panesOf(session);
  const focused = panes.find(p => p.focused) || panes[0];
  state.paneID = focused ? focused.id : '';

  app.innerHTML = `
    <section class="screen">
      <header>
        <button id="back" class="icon-btn" aria-label="${esc(L.back)}">‹</button>
        <div class="grow">
          <div class="title" id="termTitle">${esc(state.title)}</div>
          <div class="subtitle" id="termPath"></div>
        </div>
      </header>
      <div id="paneChips" class="chips hidden"></div>
      <div id="banner" class="banner hidden"></div>
      <div class="term-scroll" id="scroll"><pre class="term" id="term"></pre></div>
      <div class="keys" id="keys"></div>
      <form class="bar" id="bar">
        <input id="input" enterkeyhint="send" autocomplete="off" autocapitalize="off"
               spellcheck="false" placeholder="${esc(L.inputPlaceholder)}">
        <button type="submit">${esc(L.send)}</button>
      </form>
    </section>`;

  document.getElementById('back').onclick = () => showSessions();

  const scroll = document.getElementById('scroll');
  scroll.addEventListener('scroll', () => {
    state.autoScroll = scroll.scrollTop + scroll.clientHeight >= scroll.scrollHeight - 32;
  });

  const keys = document.getElementById('keys');
  for (const [label, seq] of KEYS) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'key';
    button.textContent = label;
    button.onclick = () => sendKey(seq);
    keys.appendChild(button);
  }

  renderPaneChips(panes);
  startPolling();

  const form = document.getElementById('bar');
  form.onsubmit = e => {
    e.preventDefault();
    const input = document.getElementById('input');
    const text = input.value;
    if (!text) return;
    input.value = '';
    sendKey(text + '\r');
    input.focus();
  };
}

function renderPaneChips(panes) {
  const chips = document.getElementById('paneChips');
  if (!chips) return;
  if (panes.length < 2) {
    chips.classList.add('hidden');
    return;
  }
  chips.classList.remove('hidden');
  chips.replaceChildren();
  for (const pane of panes) {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = 'chip' + (pane.id === state.paneID ? ' active' : '');
    chip.textContent = pane.title || pane.cwd || 'pane';
    chip.onclick = () => {
      state.paneID = pane.id;
      state.text = '';
      state.autoScroll = true;
      for (const el of chips.querySelectorAll('.chip')) el.classList.remove('active');
      chip.classList.add('active');
      poll();
    };
    chips.appendChild(chip);
  }
}

function startPolling() {
  stopPolling();
  poll();
  state.timer = setInterval(poll, 1000);
}

async function poll() {
  if (!state.paneID || state.polling) return;
  state.polling = true;
  try {
    const view = await call('pane.text', { pane: state.paneID, limit: 131072 });
    const text = view.text || '';
    const title = view.title || (state.session && state.session.title) || L.session;
    const cwd = view.cwd || '';
    if (text === state.text && title === state.title && cwd === state.cwd) return;
    state.text = text;
    state.title = title;
    state.cwd = cwd;
    setBanner('');
    renderTerm();
  } catch (err) {
    if (!err.unpaired) setBanner(err.message);
  } finally {
    state.polling = false;
  }
}

function renderTerm() {
  const term = document.getElementById('term');
  if (!term) return;
  const scroll = document.getElementById('scroll');
  const follow = state.autoScroll;
  term.textContent = state.text;
  const title = document.getElementById('termTitle');
  const path = document.getElementById('termPath');
  if (title) title.textContent = state.title;
  if (path) path.textContent = state.cwd;
  if (follow && scroll) scroll.scrollTop = scroll.scrollHeight;
}

async function sendKey(text) {
  if (!state.paneID || !text) return;
  try {
    await call('pane.send', { pane: state.paneID, text });
  } catch (err) {
    if (!err.unpaired) setBanner(err.message);
  }
}

// MARK: boot

async function boot() {
  if (!state.token) {
    showPairing('');
    return;
  }
  try {
    await call('ping');
    showSessions();
  } catch (err) {
    if (!err.unpaired) showPairing(err.message);
  }
}

boot();
