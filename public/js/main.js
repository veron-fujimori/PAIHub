import { $, STORAGE_KEYS, closeMobileSidebar, state } from './state.js';
import { api, connectEventsSSE, loadStats, toast, updatePinSettingsUI } from './api.js';
import { loadSessions, registerSessionHooks, renderSidebarSessions, createNewSession, syncSessionsFromServer } from './sessions.js';
import { applySavedDefaultsToComposer, loadAgents, populateModelAndEffortFor, registerAgentHooks } from './agents.js';
import { renderChatHistory, renderChatQueue, sendPrompt, updateSendButtonState } from './chat.js';
import { applyWorkspaceSummary, cloneAndSelectRepo, loadGitHubRepos, loadWorkspaces, openWorkspaceModal, renderGitHubRepos, selectWorkspace, switchModalTab } from './workspace.js';
import { closeDrawer, loadFilesTree, loadToolsStatus, runTerminalCommand, switchDrawerTab } from './drawer.js';
import { initCustomSelects } from './select.js';

function applyTheme(theme) {
  state.theme = theme === 'light' ? 'light' : 'dark';
  document.documentElement.setAttribute('data-theme', state.theme);
  localStorage.setItem(STORAGE_KEYS.theme, state.theme);
  $('themeIconSun')?.classList.toggle('hidden', state.theme !== 'light');
  $('themeIconMoon')?.classList.toggle('hidden', state.theme !== 'dark');
  const label = $('themeLabel');
  if (label) label.textContent = state.theme === 'light' ? 'Light' : 'Dark';
}

function toggleSidebar() {
  if (window.innerWidth <= 860) {
    const opening = !$('appShell')?.classList.contains('sidebar-open-mobile');
    $('appShell')?.classList.toggle('sidebar-open-mobile', opening);
    $('sidebarBackdrop')?.classList.toggle('hidden', !opening);
  } else {
    $('appShell')?.classList.toggle('sidebar-collapsed');
  }
}

async function pickWorkspaceNative() {
  try {
    const res = await api('/api/workspaces/pick-native', { method: 'POST' });
    if (res.path) await selectWorkspace(res.path);
  } catch {
    openWorkspaceModal('local');
  }
}

function bindEvents() {
  initCustomSelects();
  registerSessionHooks({
    onSelectSession(target) {
      const targetAgent = target.agentId || 'agy';
      if (targetAgent !== state.selectedAgentId && state.agents.some((a) => a.id === targetAgent)) {
        state.selectedAgentId = targetAgent;
        localStorage.setItem(STORAGE_KEYS.lastAgent, targetAgent);
        $('agentSelect').value = targetAgent;
        const lastModel = localStorage.getItem(`${STORAGE_KEYS.lastModelPrefix}${targetAgent}`) || '';
        const lastEffort = localStorage.getItem(`${STORAGE_KEYS.lastEffortPrefix}${targetAgent}`) || state.selectedEffort;
        populateModelAndEffortFor(targetAgent, lastModel, lastEffort);
      }
      if (target.workspace && target.workspace.toLowerCase() !== (state.workspace?.activeWorkspace || '').toLowerCase()) {
        selectWorkspace(target.workspace);
      } else {
        renderSidebarSessions();
        renderChatHistory();
      }
    },
    onHistoryCleared() {
      renderChatHistory();
    },
  });

  registerAgentHooks({
    onAgentAutoSwitched() {
      renderSidebarSessions();
      renderChatHistory();
    },
  });

  $('loginForm')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    $('loginError')?.classList.add('hidden');
    try {
      await api('/api/auth/login', { method: 'POST', body: JSON.stringify({ pin: $('loginPinInput').value }) });
      $('loginPinInput').value = '';
      await loadAll();
    } catch (err) {
      $('loginError').textContent = err.message;
      $('loginError')?.classList.remove('hidden');
    }
  });

  $('sidebarToggleBtn')?.addEventListener('click', toggleSidebar);
  $('sidebarBackdrop')?.addEventListener('click', closeMobileSidebar);

  $('newChatBtn')?.addEventListener('click', async () => {
    state.chatQueue = [];
    renderChatQueue();
    if (state.isStreaming) {
      try {
        await api('/api/chat/stop', { method: 'POST', body: '{}' });
      } catch {}
      state.isStreaming = false;
      $('sendBtn').disabled = false;
    }
    createNewSession();
    closeMobileSidebar();
    $('promptInput')?.focus();
  });

  $('themeToggleBtn')?.addEventListener('click', () => applyTheme(state.theme === 'dark' ? 'light' : 'dark'));
  $('workspaceBtn')?.addEventListener('click', pickWorkspaceNative);
  $('openWorkspaceBtn')?.addEventListener('click', () => {
    closeMobileSidebar();
    openWorkspaceModal('github');
  });
  $('selfEditQuickBtn')?.addEventListener('click', () => {
    closeMobileSidebar();
    const target = state.workspace?.pluginsDir || state.workspace?.selfDir;
    if (target) selectWorkspace(target);
  });
  $('openInteractiveCliBtn')?.addEventListener('click', async () => {
    closeMobileSidebar();
    const agentId = $('agentSelect')?.value;
    if (!agentId) {
      toast('Please select an AI CLI first.', 'err');
      return;
    }
    const model = $('modelSelect')?.value || '';
    const effort = $('effortSelect')?.value || '';
    try {
      await api('/api/terminal/login-window', { method: 'POST', body: JSON.stringify({ target: agentId, model, effort }) });
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('agentSelect')?.addEventListener('change', (e) => {
    state.selectedAgentId = e.target.value;
    localStorage.setItem(STORAGE_KEYS.lastAgent, state.selectedAgentId);
    const lastModel = localStorage.getItem(`${STORAGE_KEYS.lastModelPrefix}${state.selectedAgentId}`) || '';
    const lastEffort = localStorage.getItem(`${STORAGE_KEYS.lastEffortPrefix}${state.selectedAgentId}`) || state.selectedEffort;
    populateModelAndEffortFor(state.selectedAgentId, lastModel, lastEffort);
    const matching = loadSessions().filter((s) => (s.agentId || 'agy') === state.selectedAgentId);
    state.activeSessionId = matching.length > 0 ? matching[0].id : null;
    renderSidebarSessions();
    renderChatHistory();
  });

  $('modelSelect')?.addEventListener('change', (e) => {
    state.selectedModel = e.target.value;
    if (state.selectedAgentId) {
      localStorage.setItem(`${STORAGE_KEYS.lastModelPrefix}${state.selectedAgentId}`, state.selectedModel);
    }
  });

  $('effortSelect')?.addEventListener('change', (e) => {
    state.selectedEffort = e.target.value;
    if (state.selectedAgentId) {
      localStorage.setItem(`${STORAGE_KEYS.lastEffortPrefix}${state.selectedAgentId}`, state.selectedEffort);
    }
  });

  const permSel = $('permissionModeSelect');
  if (permSel) {
    const savedPerm = localStorage.getItem(STORAGE_KEYS.permissionMode) || 'auto';
    permSel.value = savedPerm === 'normal' ? 'auto' : savedPerm;
    permSel.addEventListener('change', (e) => {
      localStorage.setItem(STORAGE_KEYS.permissionMode, e.target.value);
    });
  }

  $('closeDrawerBtn')?.addEventListener('click', closeDrawer);
  $('drawerBackdrop')?.addEventListener('click', closeDrawer);

  $('settingsBtn')?.addEventListener('click', () => {
    closeMobileSidebar();
    loadStats();
    loadAgents();
    $('settingsModal')?.classList.remove('hidden');
  });

  async function savePinSetting(pin) {
    try {
      const res = await api('/api/auth/pin', { method: 'POST', body: JSON.stringify({ pin }) });
      if ($('pinSettingInput')) $('pinSettingInput').value = '';
      updatePinSettingsUI(Boolean(res.pinRequired));
      toast(res.pinRequired ? 'PIN saved.' : 'PIN disabled.');
    } catch (err) {
      toast(err.message, 'err');
    }
  }

  $('pinSettingsForm')?.addEventListener('submit', (e) => {
    e.preventDefault();
    const pin = ($('pinSettingInput')?.value || '').trim();
    if (!pin) {
      toast('Please enter a PIN.', 'err');
      $('pinSettingInput')?.focus();
      return;
    }
    savePinSetting(pin);
  });
  $('disablePinBtn')?.addEventListener('click', () => savePinSetting(''));

  document.querySelectorAll('.modal-close').forEach((btn) => {
    btn.addEventListener('click', () => $(btn.dataset.close)?.classList.add('hidden'));
  });
  document.querySelectorAll('.right-tab').forEach((btn) => {
    btn.addEventListener('click', () => switchDrawerTab(btn.dataset.tab));
  });
  document.querySelectorAll('.modal-tab').forEach((btn) => {
    btn.addEventListener('click', () => switchModalTab(btn.dataset.mtab));
  });

  $('promptInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      $('chatForm')?.requestSubmit();
    }
  });
  $('promptInput')?.addEventListener('input', updateSendButtonState);

  $('chatForm')?.addEventListener('submit', (e) => {
    e.preventDefault();
    const val = $('promptInput').value.trim();
    if (!val) return;
    $('promptInput').value = '';
    if (state.isStreaming) {
      state.chatQueue = state.chatQueue || [];
      state.chatQueue.push({
        prompt: val,
        agentId: $('agentSelect').value,
        model: $('modelSelect').value,
        effort: $('effortSelect').value,
      });
      renderChatQueue();
      return;
    }
    sendPrompt(val);
  });

  $('sendBtn')?.addEventListener('click', (e) => {
    if (state.isStreaming && !$('promptInput').value.trim()) {
      e.preventDefault();
      api('/api/chat/stop', { method: 'POST', body: '{}' });
    }
  });

  $('filesRefreshBtn')?.addEventListener('click', () => loadFilesTree(state.filesCurrentPath));
  $('filesUpBtn')?.addEventListener('click', () => {
    const p = state.filesCurrentPath;
    if (!p) return;
    const parent = p.replace(/[\\/][^\\/]+$/, '') || p;
    loadFilesTree(parent.endsWith(':') ? `${parent}\\` : parent);
  });
  $('closeFileBtn')?.addEventListener('click', () => $('fileEditorBox')?.classList.add('hidden'));
  $('saveFileBtn')?.addEventListener('click', async () => {
    if (!state.editingFilePath) return;
    try {
      await api('/api/workspaces/file', {
        method: 'POST',
        body: JSON.stringify({ path: state.editingFilePath, content: $('fileEditorContent').value }),
      });
      toast('Saved.');
    } catch (err) {
      toast(err.message, 'err');
    }
  });

  $('refreshToolsBtn')?.addEventListener('click', loadToolsStatus);
  $('terminalForm')?.addEventListener('submit', (e) => {
    e.preventDefault();
    const cmd = $('terminalInput').value.trim();
    if (!cmd) return;
    $('terminalInput').value = '';
    runTerminalCommand(cmd);
  });
  $('terminalStopBtn')?.addEventListener('click', () => api('/api/terminal/stop', { method: 'POST', body: '{}' }));

  $('browseGoBtn')?.addEventListener('click', () => {
    const p = $('browsePathInput')?.value.trim();
    if (p) selectWorkspace(p);
  });
  $('pickNativeFolderBtn')?.addEventListener('click', async () => {
    try {
      const res = await api('/api/workspaces/pick-native', { method: 'POST' });
      if (res.path) {
        await selectWorkspace(res.path);
        $('workspaceModal')?.classList.add('hidden');
      }
    } catch (err) {
      toast(err.message, 'err');
    }
  });
  $('ghRecheckBtn')?.addEventListener('click', loadGitHubRepos);
  $('ghQuickLoginBtn')?.addEventListener('click', async () => {
    try {
      await api('/api/terminal/login-window', { method: 'POST', body: JSON.stringify({ target: 'gh' }) });
      toast('Complete login in the PC terminal window, then click Recheck.');
    } catch (err) {
      toast(err.message, 'err');
    }
  });
  $('ghOrgSelect')?.addEventListener('change', renderGitHubRepos);
  $('ghSearchInput')?.addEventListener('input', renderGitHubRepos);
  $('ghBrowseDestBtn')?.addEventListener('click', async () => {
    try {
      const res = await api('/api/workspaces/pick-native', { method: 'POST' });
      if (res.path) $('ghDestParentInput').value = res.path;
    } catch (err) {
      toast(err.message, 'err');
    }
  });
  $('ghManualCloneBtn')?.addEventListener('click', () => {
    const repo = $('ghCloneUrlInput')?.value.trim();
    if (repo) cloneAndSelectRepo(repo);
  });

  $('chatMessages')?.addEventListener('click', async (e) => {
    const btn = e.target.closest('.md-copy-btn');
    if (!btn) return;
    const codeEl = btn.closest('.md-code-wrap')?.querySelector('code');
    if (!codeEl) return;
    try {
      await navigator.clipboard.writeText(codeEl.textContent || '');
      const prev = btn.textContent;
      btn.textContent = 'Copied';
      setTimeout(() => {
        btn.textContent = prev;
      }, 1200);
    } catch {}
  });
}

async function loadAll() {
  $('lockOverlay')?.classList.add('hidden');
  await syncSessionsFromServer();
  await Promise.all([loadWorkspaces(), loadAgents(), loadStats()]);
  applySavedDefaultsToComposer(loadSessions());
  renderSidebarSessions();
  renderChatHistory();
  connectEventsSSE(applyWorkspaceSummary);
}

async function init() {
  applyTheme(state.theme);
  bindEvents();
  try {
    const auth = await api('/api/auth/status');
    if (auth.pinRequired && !auth.authenticated) {
      $('lockOverlay')?.classList.remove('hidden');
      return;
    }
    await loadAll();
  } catch (err) {
    toast(err.message, 'err');
  }
}

init();
