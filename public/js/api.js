import { $, escapeHtml } from './state.js';

let eventSource = null;

export function toast(msg, type = 'ok') {
  const el = document.createElement('div');
  el.className = `toast ${type}`;
  el.textContent = msg;
  $('toastContainer')?.appendChild(el);
  setTimeout(() => el.remove(), 3800);
}

export async function api(url, opts = {}) {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    ...opts,
  });
  if (res.status === 401) {
    $('lockOverlay')?.classList.remove('hidden');
    throw new Error('PIN required');
  }
  const text = await res.text();
  let data = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { raw: text };
  }
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

export async function streamSSE(url, bodyObj, onEvent) {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(bodyObj),
  });
  if (res.status === 401) {
    $('lockOverlay')?.classList.remove('hidden');
    throw new Error('PIN required');
  }
  if (!res.ok || !res.body) {
    const errText = await res.text();
    throw new Error(errText || `HTTP ${res.status}`);
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sepIdx;
    while ((sepIdx = buffer.indexOf('\n\n')) !== -1) {
      const rawBlock = buffer.slice(0, sepIdx);
      buffer = buffer.slice(sepIdx + 2);
      let eventName = 'message';
      const dataLines = [];
      for (const line of rawBlock.split('\n')) {
        if (line.startsWith('event:')) eventName = line.slice(6).trim();
        else if (line.startsWith('data:')) dataLines.push(line.slice(5).trim());
      }
      if (dataLines.length > 0) {
        const joined = dataLines.join('\n');
        try {
          onEvent(eventName, JSON.parse(joined));
        } catch {
          onEvent(eventName, joined);
        }
      }
    }
  }
}

export function connectEventsSSE(onWorkspaceChanged) {
  if (eventSource) eventSource.close();
  const es = new EventSource('/api/events');
  eventSource = es;

  es.addEventListener('ui-reload', () => {
    setTimeout(() => window.location.reload(), 450);
  });
  es.addEventListener('workspace-changed', (e) => {
    try {
      onWorkspaceChanged?.(JSON.parse(e.data));
    } catch {}
  });
}

export function updatePinSettingsUI(pinRequired) {
  const inputEl = $('pinSettingInput');
  if (inputEl) {
    inputEl.placeholder = pinRequired ? 'New PIN' : 'Set PIN';
  }
  const statusEl = $('pinStatusText');
  if (statusEl) {
    statusEl.textContent = pinRequired ? 'PIN protection is active.' : 'No PIN set.';
  }
  $('disablePinBtn')?.classList.toggle('hidden', !pinRequired);
}

export async function loadStats() {
  try {
    const s = await api('/api/system/stats');
    updatePinSettingsUI(Boolean(s.pinRequired));
    const box = $('accessUrlsList');
    if (box) {
      const urls = [`http://localhost:${s.port}`, ...(s.localIPs || []).map((ip) => `http://${ip}:${s.port}`)];
      box.innerHTML = urls
        .map((u) => `<button type="button" class="btn btn-xs btn-subtle mono copy-url-btn" data-url="${escapeHtml(u)}">${escapeHtml(u)}</button>`)
        .join('');
      box.querySelectorAll('.copy-url-btn').forEach((b) => {
        b.addEventListener('click', async () => {
          try {
            await navigator.clipboard.writeText(b.dataset.url);
            toast(`Copied: ${b.dataset.url}`);
          } catch {
            toast(b.dataset.url);
          }
        });
      });
    }
  } catch {}
}
