import { $, ICONS, STORAGE_KEYS, escapeHtml, state } from './state.js';
import { api, streamSSE, toast } from './api.js';
import { renderMarkdown } from './markdown.js';
import {
  createNewSession,
  getActiveSession,
  loadChatHistory,
  saveChatHistory,
  saveChatHistoryFor,
} from './sessions.js';
import { openDrawer } from './drawer.js';
import { updateComposerAvailability } from './agents.js';

export function updateSendButtonState() {
  const btn = $('sendBtn');
  const input = $('promptInput');
  if (!btn || !input) return;

  const hasText = input.value.trim().length > 0;
  if (state.isStreaming && !hasText) {
    btn.innerHTML = ICONS.stop;
    btn.title = 'Stop process';
    btn.dataset.mode = 'stop';
    btn.disabled = false;
  } else {
    btn.innerHTML = ICONS.send;
    btn.title = state.isStreaming ? 'Add to queue' : 'Send';
    btn.dataset.mode = 'send';
  }
}

export function renderChatQueue() {
  const bar = $('chatQueueBar');
  if (!bar) return;
  const q = state.chatQueue || [];
  if (q.length === 0) {
    bar.classList.add('hidden');
    bar.innerHTML = '';
    return;
  }

  bar.classList.remove('hidden');
  bar.innerHTML = q
    .map((item, idx) => `
      <div class="queue-chip">
        <span class="queue-chip-label">Queue</span>
        <strong class="queue-chip-text">${escapeHtml(item.prompt)}</strong>
        <button type="button" class="btn-ghost btn-xs queue-del-btn" data-q-idx="${idx}" title="Cancel">×</button>
      </div>
    `)
    .join('');

  bar.querySelectorAll('.queue-del-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      state.chatQueue.splice(Number(btn.dataset.qIdx), 1);
      renderChatQueue();
    });
  });
}

export function renderChatHistory() {
  const container = $('chatMessages');
  const items = loadChatHistory();
  container.innerHTML = '';

  if (items.length === 0) {
    const welcome = document.createElement('div');
    welcome.className = 'welcome-box';
    welcome.innerHTML = '<h1>What would you like to build?</h1>';
    container.appendChild(welcome);
    return;
  }

  for (const item of items) {
    if (item.role === 'user') {
      const div = document.createElement('div');
      div.className = 'msg-user';
      div.textContent = item.text;
      container.appendChild(div);
    } else {
      container.appendChild(buildAiMessageElement(item));
    }
  }
  container.scrollTop = container.scrollHeight;
}

function buildAiMessageElement(item) {
  const card = document.createElement('div');
  card.className = 'msg-ai';

  const statusClass = item.running ? 'running' : item.error ? 'err' : 'ok';
  const statusLabel = item.running ? 'Writing...' : item.error ? item.error : 'Done';
  const agObj = state.agents.find((a) => a.id === item.agentId);
  const modelOpt = (agObj?.models || []).find((m) => (typeof m === 'string' ? m : m.id) === item.model);
  const modelLabel = modelOpt ? (typeof modelOpt === 'string' ? modelOpt : modelOpt.label || modelOpt.id) : item.model;
  const effortText = item.effort && modelLabel && !modelLabel.toLowerCase().includes(item.effort.toLowerCase()) ? ` · ${item.effort}` : '';
  const modelTag = modelLabel
    ? `<span class="meta-tag model-pill">${escapeHtml(modelLabel)}${escapeHtml(effortText)}</span>`
    : (item.effort ? `<span class="meta-tag model-pill">${escapeHtml(item.effort)}</span>` : '');

  card.innerHTML = `
    <div class="msg-ai-meta">
      <span class="status-dot ${statusClass}"></span>
      <strong>${escapeHtml(item.agentName || item.agentId || 'AI')}</strong>
      ${modelTag}
      <span class="status-text">· ${escapeHtml(statusLabel)}</span>
    </div>
    <div class="msg-ai-body">${renderMarkdown(item.output || '')}</div>
    ${!item.running ? buildAiFooterHtml(item) : ''}
  `;

  card.querySelectorAll('[data-action="terminal"]').forEach((btn) => {
    btn.addEventListener('click', () => openDrawer('terminal'));
  });
  card.querySelectorAll('[data-action="login-agent"]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      try {
        await api('/api/terminal/login-window', { method: 'POST', body: JSON.stringify({ target: item.agentId }) });
        toast('Login window opened on PC.');
      } catch (err) {
        toast(err.message, 'err');
      }
    });
  });

  return card;
}

function buildAiFooterHtml(item) {
  const parts = [];
  const errLower = ((item.error || '') + ' ' + (item.output || '')).toLowerCase();
  if (item.error && (errLower.includes('login') || errLower.includes('auth') || errLower.includes('unauthorized') || errLower.includes('sign in'))) {
    parts.push(`<button type="button" class="btn btn-xs btn-accent" data-action="login-agent">Login ${escapeHtml(item.agentName || item.agentId)}</button>`);
    parts.push(`<button type="button" class="btn btn-xs btn-subtle" data-action="terminal">Terminal</button>`);
  }
  if (item.hasChanges) {
    parts.push(`<span class="small muted">${item.changesCount || 0} file(s) changed</span>`);
  }
  if (item.autoPushMessage) {
    parts.push(`<span class="small muted">${escapeHtml(item.autoPushMessage)}</span>`);
  }
  return parts.length > 0 ? `<div class="msg-ai-actions">${parts.join('')}</div>` : '';
}

export async function sendPrompt(promptText, queuedOpts = null) {
  if (state.isStreaming || !promptText.trim()) return;

  const trimmedPrompt = promptText.trim();
  if (trimmedPrompt === '/clear') {
    createNewSession();
    toast('Chat session cleared.');
    return;
  }
  if (trimmedPrompt === '/compact' || trimmedPrompt.startsWith('/compact ')) {
    const extra = trimmedPrompt.slice('/compact'.length).trim();
    promptText =
      'Summarize our progress, decisions, and key context from this conversation concisely in bullet points so we can continue seamlessly.' +
      (extra ? ` Focus: ${extra}` : '');
  }

  const agentId = queuedOpts?.agentId || $('agentSelect').value;
  const model = queuedOpts?.model ?? $('modelSelect').value;
  const effort = queuedOpts?.effort ?? $('effortSelect').value;
  const agentObj = state.agents.find((a) => a.id === agentId);
  const history = loadChatHistory();
  const resume = history.length > 0;

  history.push({ role: 'user', text: promptText });
  const aiItem = {
    role: 'ai',
    agentId,
    agentName: agentObj?.name || agentId,
    model,
    effort,
    output: '',
    running: true,
    error: '',
  };
  history.push(aiItem);
  saveChatHistory(history);
  const targetSessionId = state.activeSessionId;

  renderChatHistory();
  const container = $('chatMessages');
  const aiCard = container.lastElementChild;
  const aiBody = aiCard.querySelector('.msg-ai-body');

  state.isStreaming = true;
  $('sendBtn').disabled = false;
  updateSendButtonState();

  let renderRaf = 0;
  const scheduleRender = () => {
    if (renderRaf) return;
    renderRaf = requestAnimationFrame(() => {
      renderRaf = 0;
      aiBody.innerHTML = renderMarkdown(aiItem.output);
      container.scrollTop = container.scrollHeight;
    });
  };

  try {
    let finalPrompt = promptText;
    if (agentId === 'ollama' && history.length > 2) {
      const prior = history.slice(0, -2).slice(-10);
      const ctxLines = prior
        .map((m) => {
          const text = (m.role === 'user' ? m.text : m.output) || '';
          return text.trim() ? `${m.role === 'user' ? 'User' : 'Assistant'}: ${text.trim()}` : '';
        })
        .filter(Boolean);
      if (ctxLines.length > 0) {
        finalPrompt = `Previous conversation history:\n${ctxLines.join('\n\n')}\n\nUser: ${promptText}`;
      }
    }

    await streamSSE(
      '/api/chat/stream',
      {
        agentId,
        prompt: finalPrompt,
        resume,
        workspace: state.workspace?.activeWorkspace || '',
        model,
        effort,
        sessionId: getActiveSession()?.cliSessionId || '',
        turbo: $('permissionModeSelect')?.value === 'turbo',
        ghDirect: !!state.ghDirectRepo,
        ghDirectRepo: state.ghDirectRepo || localStorage.getItem(STORAGE_KEYS.ghDirectRepo) || '',
      },
      (event, payload) => {
        if (event === 'chunk') {
          const text = payload.text || '';
          if (text) {
            aiItem.output += text;
            scheduleRender();
          }
        } else if (event === 'status') {
          aiItem.output += `\n${payload.message}\n`;
          scheduleRender();
        } else if (event === 'done') {
          aiItem.running = false;
          aiItem.error = payload.error || '';
          aiItem.autoPushMessage = payload.autoPushMessage || '';
          if (payload.git) {
            aiItem.hasChanges = !!payload.git.hasChanges;
            aiItem.changesCount = (payload.git.changes || []).length;
          }
          if (!aiItem.output && !aiItem.error) aiItem.output = '(Done)';
          if (aiItem.error && !aiItem.output) aiItem.output = aiItem.error;
          saveChatHistoryFor(targetSessionId, history);
          if (state.activeSessionId === targetSessionId) renderChatHistory();
          if (payload.autoPushed) toast('Auto-push completed.');
        }
      }
    );
  } catch (err) {
    aiItem.running = false;
    aiItem.error = err.message;
    aiItem.output += `\n${err.message}`;
    saveChatHistoryFor(targetSessionId, history);
    if (state.activeSessionId === targetSessionId) renderChatHistory();
  } finally {
    state.isStreaming = false;
    updateSendButtonState();
    updateComposerAvailability(state.agents.find((a) => a.id === $('agentSelect').value));
    if (state.chatQueue && state.chatQueue.length > 0) {
      const next = state.chatQueue.shift();
      renderChatQueue();
      sendPrompt(next.prompt, next);
    }
  }
}
