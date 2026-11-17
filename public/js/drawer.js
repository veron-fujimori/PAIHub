import { $, ICONS, escapeHtml, formatBytes, state } from './state.js';
import { api, streamSSE, toast } from './api.js';

export function openDrawer(tab = 'files') {
  $('drawerBackdrop')?.classList.remove('hidden');
  $('workDrawer')?.classList.remove('hidden');
  switchDrawerTab(tab);
}

export function closeDrawer() {
  $('drawerBackdrop')?.classList.add('hidden');
  $('workDrawer')?.classList.add('hidden');
}

export function switchDrawerTab(tab) {
  const nextTab = tab === 'terminal' ? 'terminal' : 'files';
  state.activeDrawerTab = nextTab;
  document.querySelectorAll('.right-tab').forEach((t) => t.classList.toggle('active', t.dataset.tab === nextTab));
  $('tabFiles')?.classList.toggle('active', nextTab === 'files');
  $('tabTerminal')?.classList.toggle('active', nextTab === 'terminal');
  if (nextTab === 'files') loadFilesTree(state.filesCurrentPath || state.workspace?.activeWorkspace);
  if (nextTab === 'terminal') loadToolsStatus();
}

export async function loadFilesTree(dirPath) {
  try {
    const res = await api(`/api/workspaces/browse?path=${encodeURIComponent(dirPath || '')}`);
    state.filesCurrentPath = res.path;
    $('filesCurrentPath').textContent = res.path;
    const list = $('filesTreeList');
    list.innerHTML = (res.entries || [])
      .map((e) => `
        <div class="tree-item" data-path="${escapeHtml(e.path)}" data-dir="${e.isDir}">
          <span class="tree-item-left"><span class="muted">${e.isDir ? ICONS.folder : ICONS.file}</span><span class="truncate">${escapeHtml(e.name)}</span></span>
          <span class="mono small muted">${e.isDir ? '' : formatBytes(e.size)}</span>
        </div>
      `)
      .join('') || '<div class="empty-note">Empty</div>';

    list.querySelectorAll('.tree-item').forEach((row) => {
      row.addEventListener('click', () => {
        if (row.dataset.dir === 'true') loadFilesTree(row.dataset.path);
        else openFileEditor(row.dataset.path);
      });
    });
  } catch (err) {
    toast(err.message, 'err');
  }
}

export async function openFileEditor(filePath) {
  try {
    const f = await api(`/api/workspaces/file?path=${encodeURIComponent(filePath)}`);
    if (f.isBinary) {
      toast('Binary file', 'err');
      return;
    }
    state.editingFilePath = f.path;
    $('editingFileName').textContent = f.path;
    $('fileEditorContent').value = f.content;
    $('fileEditorBox').classList.remove('hidden');
  } catch (err) {
    toast(err.message, 'err');
  }
}

export async function loadToolsStatus() {
  try {
    const tools = await api('/api/terminal/tools');
    $('toolsGrid').innerHTML = tools
      .map((t) => `
        <span class="tool-pill ${t.installed ? 'ok' : 'miss'}" title="${escapeHtml(t.resolvedPath || 'Not found')}">
          <span class="dot"></span><span>${escapeHtml(t.name)}</span>
        </span>
      `)
      .join('');
  } catch (err) {
    toast(err.message, 'err');
  }
}

export async function runTerminalCommand(commandLine) {
  if (!commandLine.trim()) return;
  const outEl = $('terminalOutput');
  outEl.textContent += `\n> ${commandLine}\n`;
  $('terminalStopBtn')?.classList.remove('hidden');
  try {
    await streamSSE('/api/terminal/exec', { command: commandLine, dir: state.workspace?.activeWorkspace || '' }, (event, payload) => {
      if (event === 'chunk') {
        outEl.textContent += payload.text || '';
        outEl.parentElement.scrollTop = outEl.parentElement.scrollHeight;
      } else if (event === 'done' && payload.error) {
        outEl.textContent += `\n${payload.error}\n`;
      }
    });
  } catch (err) {
    outEl.textContent += `\n${err.message}\n`;
  } finally {
    $('terminalStopBtn')?.classList.add('hidden');
  }
}
