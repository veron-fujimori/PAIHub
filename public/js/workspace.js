import { $, STORAGE_KEYS, basename, escapeHtml, state } from './state.js';
import { api, toast } from './api.js';
import { loadSessions, renderSidebarSessions } from './sessions.js';
import { renderChatHistory } from './chat.js';

export async function loadWorkspaces() {
  const data = await api('/api/workspaces');
  applyWorkspaceSummary(data);
}

export function applyWorkspaceSummary(data) {
  state.workspace = data;
  state.filesCurrentPath = data.activeWorkspace;
  state.defaults = {
    defaultAgent: data.defaultAgent || 'agy',
    defaultModel: data.defaultModel || '',
    defaultEffort: data.defaultEffort || '',
  };

  $('workspaceName').textContent = basename(data.activeWorkspace) || data.activeWorkspace;
  $('workspaceBtn').title = data.activeWorkspace;

  const selfBtn = $('selfEditQuickBtn');
  if (selfBtn) {
    selfBtn.classList.toggle('active', !!data.isSelfEdit);
  }

  if (!state.activeSessionId) {
    const activeAgent = state.selectedAgentId || localStorage.getItem(STORAGE_KEYS.lastAgent) || 'agy';
    const existing = loadSessions().filter((s) => (s.agentId || 'agy') === activeAgent);
    if (existing.length > 0) {
      state.activeSessionId = existing[0].id;
    }
  }

  renderSidebarSessions();
  renderChatHistory();
}

export async function openWorkspaceModal(tabName = 'local') {
  $('workspaceModal')?.classList.remove('hidden');
  switchModalTab(tabName);
  await browseModalPath(state.workspace?.activeWorkspace || '');
}

export function switchModalTab(tabName) {
  document.querySelectorAll('.modal-tab').forEach((t) => t.classList.toggle('active', t.dataset.mtab === tabName));
  $('mtabLocal')?.classList.toggle('active', tabName === 'local');
  $('mtabGithub')?.classList.toggle('active', tabName === 'github');
  if (tabName === 'github') loadGitHubRepos();
}

export async function browseModalPath(targetPath) {
  try {
    const res = await api(`/api/workspaces/browse?path=${encodeURIComponent(targetPath || '')}`);
    state.browseCurrentPath = res.path;
    $('browsePathInput').value = res.path;
    if (!$('ghDestParentInput').value) {
      const docs = (res.shortcuts || []).find((s) => s.label === 'Documents');
      $('ghDestParentInput').value = docs ? docs.path : res.path;
    }
  } catch (err) {
    toast(err.message, 'err');
  }
}

export async function selectWorkspace(path) {
  try {
    const data = await api('/api/workspaces/select', { method: 'POST', body: JSON.stringify({ path }) });
    state.ghDirectRepo = '';
    applyWorkspaceSummary(data);
    $('workspaceModal')?.classList.add('hidden');
  } catch (err) {
    toast(err.message, 'err');
  }
}

export async function loadGitHubRepos() {
  $('ghAuthInfo').textContent = 'Loading...';
  $('ghLoginCard')?.classList.add('hidden');
  try {
    const res = await api('/api/github/repos');
    if (!res.authenticated) {
      $('ghAuthInfo').textContent = 'Not signed in to GitHub';
      $('ghLoginCard')?.classList.remove('hidden');
      $('ghSearchInput')?.classList.add('hidden');
      $('ghRepoList')?.classList.add('hidden');
      state.ghRepos = [];
      return;
    }
    $('ghLoginCard')?.classList.add('hidden');
    $('ghSearchInput')?.classList.remove('hidden');
    $('ghRepoList')?.classList.remove('hidden');
    $('ghAuthInfo').textContent = `@${res.user} (${(res.repos || []).length})`;
    state.ghRepos = res.repos || [];

    const orgSel = $('ghOrgSelect');
    if (orgSel) {
      const prevOrg = orgSel.value;
      const orgs = Array.from(new Set(state.ghRepos.map((r) => (r.nameWithOwner || '').split('/')[0]).filter(Boolean))).sort((a, b) => a.localeCompare(b));
      orgSel.innerHTML = '<option value="">All</option>' + orgs.map((o) => `<option value="${escapeHtml(o)}">${escapeHtml(o)}</option>`).join('');
      if (orgs.includes(prevOrg)) orgSel.value = prevOrg;
    }
    renderGitHubRepos();
  } catch (err) {
    $('ghAuthInfo').textContent = err.message;
  }
}

export function renderGitHubRepos() {
  const query = ($('ghSearchInput')?.value || '').toLowerCase();
  const org = $('ghOrgSelect')?.value || '';
  const filtered = state.ghRepos.filter((r) => {
    const owner = (r.nameWithOwner || '').split('/')[0];
    if (org && owner !== org) return false;
    return r.nameWithOwner.toLowerCase().includes(query);
  });

  $('ghRepoList').innerHTML = filtered
    .map((r) => `
      <div class="gh-repo-item">
        <div class="min-w-0">
          <strong class="small">${escapeHtml(r.nameWithOwner)}</strong>
          <span class="soft-badge">${r.isPrivate ? 'Private' : 'Public'}</span>
        </div>
        <div class="row-actions">
          <button type="button" class="btn btn-xs btn-subtle direct-gh-btn" data-repo="${escapeHtml(r.nameWithOwner)}" title="Use directly via gh CLI without cloning">Direct</button>
          <button type="button" class="btn btn-xs btn-accent clone-gh-btn" data-repo="${escapeHtml(r.nameWithOwner)}">Clone</button>
        </div>
      </div>
    `)
    .join('') || '<div class="empty-note">Empty</div>';

  $('ghRepoList').querySelectorAll('.direct-gh-btn').forEach((b) => b.addEventListener('click', () => selectGitHubDirectRepo(b.dataset.repo)));
  $('ghRepoList').querySelectorAll('.clone-gh-btn').forEach((b) => b.addEventListener('click', () => cloneAndSelectRepo(b.dataset.repo)));
}

function selectGitHubDirectRepo(repo) {
  state.ghDirectRepo = repo;
  localStorage.setItem(STORAGE_KEYS.ghDirectRepo, repo);
  $('workspaceName').textContent = `gh: ${repo}`;
  $('workspaceBtn').title = `GitHub Direct: ${repo}`;
  $('workspaceModal')?.classList.add('hidden');
  toast(`gh Direct mode active for ${repo}`);
}

export async function cloneAndSelectRepo(repo) {
  const destParent = $('ghDestParentInput')?.value.trim() || '';
  try {
    toast(`Cloning ${repo}...`);
    const data = await api('/api/github/clone', { method: 'POST', body: JSON.stringify({ repo, destParent }) });
    applyWorkspaceSummary(data);
    $('workspaceModal')?.classList.add('hidden');
  } catch (err) {
    toast(err.message, 'err');
  }
}
