import { $, STORAGE_KEYS, basename, closeMobileSidebar, escapeHtml, state } from './state.js';
import { api } from './api.js';

let sessionsCache = null;
let sessionHooks = {
  onSelectSession: null,
  onHistoryCleared: null,
};

export function registerSessionHooks(hooks) {
  sessionHooks = { ...sessionHooks, ...hooks };
}

export async function syncSessionsFromServer() {
  try {
    const data = await api('/api/sessions');
    const serverList = Array.isArray(data.sessions) ? data.sessions : [];
    if (serverList.length > 0) {
      sessionsCache = serverList;
      try {
        localStorage.setItem(STORAGE_KEYS.sessions, JSON.stringify(serverList.slice(0, 40)));
      } catch {}
      return;
    }

    const raw = localStorage.getItem(STORAGE_KEYS.sessions);
    const localList = raw ? JSON.parse(raw) : [];
    if (Array.isArray(localList) && localList.length > 0) {
      sessionsCache = localList.slice(0, 40);
      await api('/api/sessions', { method: 'PUT', body: JSON.stringify({ sessions: sessionsCache }) });
    } else {
      sessionsCache = [];
    }
  } catch {}
}

export function loadSessions() {
  if (Array.isArray(sessionsCache)) return sessionsCache;
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.sessions);
    const list = raw ? JSON.parse(raw) : [];
    return Array.isArray(list) ? list : [];
  } catch {
    return [];
  }
}

function saveSessions(list) {
  const trimmed = list.slice(0, 40);
  sessionsCache = trimmed;
  try {
    localStorage.setItem(STORAGE_KEYS.sessions, JSON.stringify(trimmed));
  } catch {}
  api('/api/sessions', { method: 'PUT', body: JSON.stringify({ sessions: trimmed }) }).catch(() => {});
}

export function getActiveSession() {
  const list = loadSessions();
  if (state.activeSessionId) {
    return list.find((s) => s.id === state.activeSessionId) || null;
  }
  return null;
}

export function createNewSession() {
  const id = `chat_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;
  const session = {
    id,
    title: 'New Chat',
    agentId: state.selectedAgentId || 'agy',
    workspace: state.workspace?.activeWorkspace || '',
    messages: [],
    updatedAt: Date.now(),
  };
  saveSessions([session, ...loadSessions()]);
  state.activeSessionId = id;
  renderSidebarSessions();
  sessionHooks.onHistoryCleared?.();
}

export function loadChatHistory() {
  const s = getActiveSession();
  return s ? (s.messages || []) : [];
}

export function saveChatHistory(items) {
  const list = loadSessions();
  let idx = list.findIndex((s) => s.id === state.activeSessionId);
  if (idx === -1) {
    createNewSession();
    const fresh = loadSessions();
    idx = 0;
    applyHistoryToSession(fresh[idx], items);
    fresh[idx].updatedAt = Date.now();
    saveSessions(fresh);
    renderSidebarSessions();
    return;
  }

  applyHistoryToSession(list[idx], items);
  list[idx].updatedAt = Date.now();
  const [item] = list.splice(idx, 1);
  list.unshift(item);
  saveSessions(list);
  renderSidebarSessions();
}

export function saveChatHistoryFor(sessionId, items) {
  if (!sessionId) return;
  const list = loadSessions();
  const idx = list.findIndex((s) => s.id === sessionId);
  if (idx === -1) return;
  list[idx].messages = items.slice(-40);
  list[idx].updatedAt = Date.now();
  saveSessions(list);
  renderSidebarSessions();
}

function applyHistoryToSession(session, items) {
  session.messages = items.slice(-40);
  if (session.title === 'New Chat' && items.length > 0) {
    const firstUser = items.find((m) => m.role === 'user');
    if (firstUser && firstUser.text) {
      const clean = firstUser.text.replace(/\s+/g, ' ').trim();
      session.title = clean.length > 42 ? `${clean.slice(0, 39)}...` : clean;
    }
  }
}

export function renderSidebarSessions() {
  const list = $('sidebarProjectsList');
  if (!list) return;
  const sessions = loadSessions();

  if (sessions.length === 0) {
    list.innerHTML = '<div class="small muted sidebar-empty">No chat history yet</div>';
    return;
  }

  list.innerHTML = sessions
    .map((s) => {
      const isActive = s.id === state.activeSessionId;
      const wsName = basename(s.workspace || '') || 'Workspace';
      const agId = s.agentId || 'agy';
      const displayTitle = s.title || 'New Chat';
      return `
        <div class="sidebar-item ${isActive ? 'active' : ''}" data-session-id="${escapeHtml(s.id)}" title="${escapeHtml(displayTitle)} (${escapeHtml(agId)} · ${escapeHtml(wsName)})">
          <span class="truncate flex-1">${escapeHtml(displayTitle)}</span>
          <span class="soft-badge session-agent-badge">${escapeHtml(agId)}</span>
          <div class="session-more-wrap">
            <button type="button" class="btn-ghost btn-xs session-more-btn" data-more-id="${escapeHtml(s.id)}" aria-label="Chat options">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <circle cx="5" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="12" r="2"/>
              </svg>
            </button>
            <div class="session-menu" data-menu-id="${escapeHtml(s.id)}">
              <button type="button" class="session-menu-item session-menu-rename" data-edit-id="${escapeHtml(s.id)}">Rename</button>
              <button type="button" class="session-menu-item danger session-menu-delete" data-del-id="${escapeHtml(s.id)}">Delete</button>
            </div>
          </div>
        </div>
      `;
    })
    .join('');

  list.querySelectorAll('[data-session-id]').forEach((row) => {
    row.addEventListener('click', (e) => {
      if (e.target.closest('.session-more-wrap')) return;
      const target = loadSessions().find((s) => s.id === row.dataset.sessionId);
      if (!target) return;
      state.activeSessionId = target.id;
      sessionHooks.onSelectSession?.(target);
      closeMobileSidebar();
    });
  });

  list.querySelectorAll('.session-more-btn').forEach((btn) => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      const wrap = btn.closest('.session-more-wrap');
      const menu = wrap?.querySelector('.session-menu');
      if (!menu) return;
      const wasOpen = menu.classList.contains('open');
      list.querySelectorAll('.session-menu.open').forEach((m) => m.classList.remove('open'));
      if (!wasOpen) {
        const rect = btn.getBoundingClientRect();
        menu.style.left = `${Math.min(rect.right + 6, window.innerWidth - 130)}px`;
        menu.style.top = `${Math.min(rect.top - 4, window.innerHeight - 90)}px`;
        menu.classList.add('open');
      }
    });
  });

  list.querySelectorAll('.session-menu-rename').forEach((btn) => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      list.querySelectorAll('.session-menu.open').forEach((m) => m.classList.remove('open'));
      startInlineRename(list, btn.dataset.editId);
    });
  });

  list.querySelectorAll('.session-menu-delete').forEach((btn) => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      list.querySelectorAll('.session-menu.open').forEach((m) => m.classList.remove('open'));
      const delId = btn.dataset.delId;
      const remaining = loadSessions().filter((s) => s.id !== delId);
      saveSessions(remaining);
      if (state.activeSessionId === delId) {
        state.activeSessionId = remaining.length > 0 ? remaining[0].id : null;
      }
      renderSidebarSessions();
      sessionHooks.onHistoryCleared?.();
    });
  });

  if (!window._sessionMenuOutsideBound) {
    window._sessionMenuOutsideBound = true;
    document.addEventListener('click', (e) => {
      if (!e.target.closest('.session-more-wrap')) {
        document.querySelectorAll('.session-menu.open').forEach((m) => m.classList.remove('open'));
      }
    });
  }
}

function startInlineRename(listEl, editId) {
  const row = listEl.querySelector(`[data-session-id="${CSS.escape(editId)}"]`);
  const titleSpan = row?.querySelector('.truncate');
  const sessions = loadSessions();
  const target = sessions.find((s) => s.id === editId);
  if (!row || !titleSpan || !target) return;

  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'session-rename-input';
  input.value = target.title || 'New Chat';
  titleSpan.replaceWith(input);
  input.focus();
  input.select();

  let done = false;
  const commit = (save) => {
    if (done) return;
    done = true;
    if (save) {
      const clean = input.value.trim();
      if (clean && clean !== target.title) {
        target.title = clean;
        saveSessions(sessions);
      }
    }
    renderSidebarSessions();
  };
  input.addEventListener('click', (ev) => ev.stopPropagation());
  input.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter') {
      ev.preventDefault();
      commit(true);
    }
    if (ev.key === 'Escape') {
      ev.preventDefault();
      commit(false);
    }
  });
  input.addEventListener('blur', () => commit(true));
}
