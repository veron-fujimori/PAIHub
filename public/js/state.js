export const $ = (id) => document.getElementById(id);

export const STORAGE_KEYS = {
  theme: 'paihub_theme',
  sessions: 'paihub_sessions_v1',
  lastAgent: 'paihub_last_agent',
  lastModelPrefix: 'paihub_last_model_',
  lastEffortPrefix: 'paihub_last_effort_',
  permissionMode: 'paihub_permission_mode',
  ghDirectRepo: 'paihub_gh_direct_repo',
  disconnectedAgents: 'paihub_disconnected_agents',
};

export const ICONS = {
  folder: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6z"/></svg>`,
  file: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>`,
  send: `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 19V5"/><path d="M5 12l7-7 7 7"/></svg>`,
  stop: `<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`,
};

export const state = {
  workspace: null,
  agents: [],
  selectedAgentId: '',
  selectedModel: '',
  selectedEffort: '',
  defaults: { defaultAgent: 'agy', defaultModel: '', defaultEffort: 'medium' },
  isStreaming: false,
  activeSessionId: null,
  chatQueue: [],
  browseCurrentPath: '',
  filesCurrentPath: '',
  editingFilePath: '',
  ghRepos: [],
  ghDirectRepo: '',
  activeDrawerTab: 'files',
  theme: localStorage.getItem(STORAGE_KEYS.theme) || 'dark',
};

export function escapeHtml(str) {
  return String(str ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export function basename(p) {
  if (!p) return '';
  const cleaned = p.replace(/[\\/]+$/, '');
  const parts = cleaned.split(/[\\/]/);
  return parts[parts.length - 1] || cleaned;
}

export function formatBytes(b) {
  if (!b) return '0 B';
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  return `${(b / 1024 / 1024).toFixed(1)} MB`;
}

export function closeMobileSidebar() {
  $('appShell')?.classList.remove('sidebar-open-mobile');
  $('sidebarBackdrop')?.classList.add('hidden');
}
