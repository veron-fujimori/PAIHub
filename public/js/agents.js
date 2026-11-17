import { $, STORAGE_KEYS, escapeHtml, state } from './state.js';
import { api, toast } from './api.js';

let agentHooks = {
  onAgentAutoSwitched: null,
};

export function registerAgentHooks(hooks) {
  agentHooks = { ...agentHooks, ...hooks };
}

export function getDisconnectedAgents() {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.disconnectedAgents);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

export function isAgentConnected(agentId) {
  return !getDisconnectedAgents().includes(agentId);
}

export function toggleAgentConnection(agentId) {
  const list = new Set(getDisconnectedAgents());
  if (list.has(agentId)) list.delete(agentId);
  else list.add(agentId);
  localStorage.setItem(STORAGE_KEYS.disconnectedAgents, JSON.stringify([...list]));
}

export function isAgentReady(ag) {
  if (!ag) return false;
  const disconnected = !isAgentConnected(ag.id);
  const noModels = ag.id === 'ollama' && (!ag.models || ag.models.length === 0);
  return !!(ag.installed && !noModels && !disconnected);
}

export async function loadAgents() {
  const list = await api('/api/agents');
  state.agents = list;

  const sel = $('agentSelect');
  const readyAgents = list.filter(isAgentReady);
  sel.innerHTML = readyAgents.length > 0
    ? readyAgents.map((a) => `<option value="${escapeHtml(a.id)}">${escapeHtml(a.name)}</option>`).join('')
    : '<option value="" disabled selected>No AI CLI ready</option>';

  renderAgentsCrud();
  renderLoginButtons();
  if (state.selectedAgentId) {
    populateModelAndEffortFor(state.selectedAgentId, state.selectedModel, state.selectedEffort);
  }
}

export function applySavedDefaultsToComposer(sessions) {
  const readyList = state.agents.filter(isAgentReady);
  const lastAgent = localStorage.getItem(STORAGE_KEYS.lastAgent);
  const targetAgent = (lastAgent && readyList.some((a) => a.id === lastAgent))
    ? lastAgent
    : readyList.some((a) => a.id === state.defaults.defaultAgent)
      ? state.defaults.defaultAgent
      : (readyList[0]?.id || state.agents[0]?.id || 'agy');

  state.selectedAgentId = targetAgent;
  localStorage.setItem(STORAGE_KEYS.lastAgent, targetAgent);

  const lastModel = localStorage.getItem(`${STORAGE_KEYS.lastModelPrefix}${targetAgent}`);
  const lastEffort = localStorage.getItem(`${STORAGE_KEYS.lastEffortPrefix}${targetAgent}`);
  state.selectedModel = lastModel || state.defaults.defaultModel || '';
  state.selectedEffort = lastEffort || state.defaults.defaultEffort || '';

  $('agentSelect').value = state.selectedAgentId;
  populateModelAndEffortFor(state.selectedAgentId, state.selectedModel, state.selectedEffort);

  const matching = (sessions || []).filter((s) => (s.agentId || 'agy') === state.selectedAgentId);
  if (!state.activeSessionId || !matching.some((s) => s.id === state.activeSessionId)) {
    state.activeSessionId = matching.length > 0 ? matching[0].id : null;
  }
}

export function populateModelAndEffortFor(agentId, chosenModel = '', chosenEffort = '') {
  const ag = state.agents.find((a) => a.id === agentId);
  const modSel = $('modelSelect');
  const effSel = $('effortSelect');

  const models = (ag?.models && ag.models.length > 0) ? ag.models : [];
  if (models.length === 0) {
    modSel.innerHTML = '<option value="" disabled selected>No models available</option>';
    state.selectedModel = '';
  } else {
    modSel.innerHTML = models.map((m) => `<option value="${escapeHtml(m.id)}">${escapeHtml(m.label)}</option>`).join('');
    const nextModel = models.some((m) => m.id === chosenModel) ? chosenModel : models[0].id;
    modSel.value = nextModel;
    state.selectedModel = nextModel;
  }

  const efforts = (ag?.efforts && ag.effortFlag) ? ag.efforts.filter((e) => e !== '') : [];
  const effortLabels = { low: 'Low', medium: 'Medium', high: 'High', xhigh: 'XHigh', max: 'Max' };
  if (efforts.length === 0) {
    effSel.classList.add('hidden');
    effSel.innerHTML = '';
    state.selectedEffort = '';
  } else {
    effSel.classList.remove('hidden');
    effSel.innerHTML = efforts.map((e) => `<option value="${escapeHtml(e)}">${escapeHtml(effortLabels[e] || e)}</option>`).join('');
    const nextEffort = efforts.includes(chosenEffort) ? chosenEffort : efforts[0];
    effSel.value = nextEffort;
    state.selectedEffort = nextEffort;
  }

  updateComposerAvailability(ag);
}

export function updateComposerAvailability(ag) {
  const promptEl = $('promptInput');
  const sendEl = $('sendBtn');
  if (!promptEl || !sendEl) return;

  const noOllamaModels = ag && ag.id === 'ollama' && (!ag.models || ag.models.length === 0);
  const disconnected = ag && !isAgentConnected(ag.id);
  const ready = !!(ag && ag.installed && !noOllamaModels && !disconnected);

  promptEl.disabled = !ready;
  sendEl.disabled = !ready;
  if (!ag) {
    promptEl.placeholder = 'Select an AI CLI first...';
  } else if (disconnected) {
    promptEl.placeholder = `${ag.name} is disconnected (Connect in Settings -> AI CLI)...`;
  } else if (!ag.installed) {
    promptEl.placeholder = `${ag.name} is not installed on this PC...`;
  } else if (noOllamaModels) {
    promptEl.placeholder = 'Ollama is not running or has no local models...';
  } else {
    promptEl.placeholder = 'Write a prompt for AI...';
  }
}

export function renderLoginButtons() {
  const row = $('loginButtonsRow');
  if (!row) return;
  const items = [{ target: 'gh', label: 'GitHub Login' }, ...state.agents.map((a) => ({ target: a.id, label: `Login ${a.name}` }))];
  row.innerHTML = items
    .map((i) => `<button type="button" class="btn btn-xs btn-subtle login-win-btn" data-target="${escapeHtml(i.target)}">${escapeHtml(i.label)}</button>`)
    .join('');

  row.querySelectorAll('.login-win-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      try {
        await api('/api/terminal/login-window', { method: 'POST', body: JSON.stringify({ target: btn.dataset.target }) });
        toast('Login window opened.');
      } catch (err) {
        toast(err.message, 'err');
      }
    });
  });
}

export function renderAgentsCrud() {
  const list = $('agentsCrudList');
  if (!list) return;

  list.innerHTML = state.agents
    .map((a) => {
      const connected = a.installed && isAgentConnected(a.id);
      return `
        <div class="agent-crud-row">
          <div>
            <strong>${escapeHtml(a.name)}</strong>
          </div>
          <div class="row-actions">
            <button
              type="button"
              class="btn btn-xs ${connected ? 'btn-subtle' : 'btn-accent'} toggle-connect-agent-btn"
              data-id="${escapeHtml(a.id)}"
              data-installed="${a.installed ? '1' : '0'}"
              ${a.installed ? '' : 'disabled title="CLI is not installed on this PC"'}
            >${connected ? 'Disconnect' : 'Connect'}</button>
          </div>
        </div>
      `;
    })
    .join('');

  list.querySelectorAll('.toggle-connect-agent-btn').forEach((b) => {
    b.addEventListener('click', async () => {
      if (b.dataset.installed !== '1') {
        toast('CLI is not installed on this PC', 'err');
        return;
      }
      toggleAgentConnection(b.dataset.id);
      await loadAgents();
      if (state.selectedAgentId === b.dataset.id && !isAgentConnected(b.dataset.id)) {
        const fallback = state.agents.find((x) => x.installed && isAgentConnected(x.id));
        if (fallback) {
          state.selectedAgentId = fallback.id;
          localStorage.setItem(STORAGE_KEYS.lastAgent, fallback.id);
          $('agentSelect').value = fallback.id;
          populateModelAndEffortFor(
            fallback.id,
            localStorage.getItem(`${STORAGE_KEYS.lastModelPrefix}${fallback.id}`) || '',
            localStorage.getItem(`${STORAGE_KEYS.lastEffortPrefix}${fallback.id}`) || ''
          );
          agentHooks.onAgentAutoSwitched?.();
        }
      }
    });
  });
}
