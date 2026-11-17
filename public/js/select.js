import { escapeHtml } from './state.js';

const instances = new Map();
let activeMenu = null;
let globalsBound = false;

function getChevronSVG() {
  return '<svg class="custom-select-chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>';
}

function getCheckSVG() {
  return '<svg class="custom-select-check" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="20 6 9 17 4 12"/></svg>';
}

export function closeActiveSelectMenu() {
  if (!activeMenu) return;
  activeMenu.wrapper.classList.remove('open');
  activeMenu.trigger.setAttribute('aria-expanded', 'false');
  activeMenu.menu.remove();
  activeMenu = null;
}

function positionMenu(trigger, menu) {
  const rect = trigger.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const margin = 8;
  const gap = 6;

  menu.style.minWidth = `${Math.max(Math.round(rect.width), 168)}px`;
  menu.style.maxHeight = `${Math.min(272, vh - margin * 2)}px`;

  const menuRect = menu.getBoundingClientRect();
  const spaceBelow = vh - rect.bottom - margin - gap;
  const spaceAbove = rect.top - margin - gap;
  const preferTop = !!trigger.closest('.composer-model-bar');
  const openTop = preferTop ? (spaceAbove >= 110 || spaceAbove >= spaceBelow) : (spaceBelow < menuRect.height && spaceAbove > spaceBelow);

  menu.dataset.side = openTop ? 'top' : 'bottom';
  if (openTop) {
    menu.style.maxHeight = `${Math.min(272, Math.max(120, spaceAbove))}px`;
  } else {
    menu.style.maxHeight = `${Math.min(272, Math.max(120, spaceBelow))}px`;
  }

  const finalRect = menu.getBoundingClientRect();
  const top = openTop
    ? Math.max(margin, rect.top - finalRect.height - gap)
    : Math.min(vh - finalRect.height - margin, rect.bottom + gap);
  const left = Math.max(margin, Math.min(rect.left, vw - finalRect.width - margin));

  menu.style.top = `${Math.round(top)}px`;
  menu.style.left = `${Math.round(left)}px`;
}

function syncSelect(select) {
  const inst = instances.get(select);
  if (!inst) return;
  const { wrapper, trigger, labelEl } = inst;

  wrapper.classList.toggle('hidden', select.classList.contains('hidden'));
  const options = Array.from(select.options);
  const hasEnabled = options.some((o) => !o.disabled);
  trigger.disabled = select.disabled || !hasEnabled;

  const selected = select.options[select.selectedIndex] || options[0];
  labelEl.textContent = selected ? selected.textContent : '';
  trigger.title = selected?.title || select.title || '';

  if (activeMenu && activeMenu.select === select) {
    renderMenuOptions(select, activeMenu.menu);
    positionMenu(trigger, activeMenu.menu);
  }
}

function renderMenuOptions(select, menu) {
  const currentValue = select.value;
  menu.innerHTML = Array.from(select.options)
    .map((opt, idx) => {
      const isSelected = !opt.disabled && opt.value === currentValue;
      const desc = opt.title ? `<span class="custom-select-option-desc">${escapeHtml(opt.title)}</span>` : '';
      return `
        <button
          type="button"
          class="custom-select-option${isSelected ? ' selected' : ''}"
          role="option"
          aria-selected="${isSelected ? 'true' : 'false'}"
          data-index="${idx}"
          ${opt.disabled ? 'disabled' : ''}
        >
          <span class="custom-select-option-main">
            <span class="custom-select-option-label">${escapeHtml(opt.textContent)}</span>
            ${desc}
          </span>
          ${isSelected ? getCheckSVG() : ''}
        </button>
      `;
    })
    .join('');

  menu.querySelectorAll('.custom-select-option').forEach((btn) => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      const idx = Number(btn.dataset.index);
      const opt = select.options[idx];
      if (!opt || opt.disabled) return;
      const changed = select.value !== opt.value;
      select.value = opt.value;
      closeActiveSelectMenu();
      instFocusTrigger(select);
      if (changed) {
        select.dispatchEvent(new Event('change', { bubbles: true }));
      }
    });
  });
}

function instFocusTrigger(select) {
  instances.get(select)?.trigger.focus();
}

function openMenuFor(select) {
  const inst = instances.get(select);
  if (!inst || inst.trigger.disabled) return;

  if (activeMenu && activeMenu.select === select) {
    closeActiveSelectMenu();
    return;
  }

  closeActiveSelectMenu();

  const menu = document.createElement('div');
  menu.className = 'custom-select-menu';
  menu.setAttribute('role', 'listbox');
  if (select.id) menu.dataset.for = select.id;
  renderMenuOptions(select, menu);

  menu.style.visibility = 'hidden';
  document.body.appendChild(menu);
  positionMenu(inst.trigger, menu);
  menu.style.visibility = '';

  inst.wrapper.classList.add('open');
  inst.trigger.setAttribute('aria-expanded', 'true');
  activeMenu = { select, wrapper: inst.wrapper, trigger: inst.trigger, menu };

  requestAnimationFrame(() => {
    menu.classList.add('visible');
    const selectedBtn = menu.querySelector('.custom-select-option.selected');
    selectedBtn?.scrollIntoView({ block: 'nearest' });
  });
}

function bindGlobalsOnce() {
  if (globalsBound) return;
  globalsBound = true;

  document.addEventListener('pointerdown', (e) => {
    if (!activeMenu) return;
    if (activeMenu.wrapper.contains(e.target) || activeMenu.menu.contains(e.target)) return;
    closeActiveSelectMenu();
  });

  document.addEventListener('keydown', (e) => {
    if (!activeMenu) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      const sel = activeMenu.select;
      closeActiveSelectMenu();
      instFocusTrigger(sel);
      return;
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const items = Array.from(activeMenu.menu.querySelectorAll('.custom-select-option:not(:disabled)'));
      if (items.length === 0) return;
      const curIdx = items.indexOf(document.activeElement);
      const nextIdx = e.key === 'ArrowDown'
        ? (curIdx < 0 ? 0 : (curIdx + 1) % items.length)
        : (curIdx <= 0 ? items.length - 1 : curIdx - 1);
      items[nextIdx].focus();
    }
  });

  window.addEventListener('resize', closeActiveSelectMenu);
  document.addEventListener('scroll', (e) => {
    if (activeMenu && !activeMenu.menu.contains(e.target)) {
      closeActiveSelectMenu();
    }
  }, true);
}

function enhanceSelect(select) {
  if (instances.has(select)) {
    syncSelect(select);
    return;
  }

  const wrapper = document.createElement('div');
  wrapper.className = 'custom-select';
  if (select.classList.contains('org-select')) wrapper.classList.add('org-select');

  const trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'custom-select-trigger';
  trigger.setAttribute('aria-haspopup', 'listbox');
  trigger.setAttribute('aria-expanded', 'false');

  const labelEl = document.createElement('span');
  labelEl.className = 'custom-select-label truncate';
  trigger.appendChild(labelEl);
  trigger.insertAdjacentHTML('beforeend', getChevronSVG());

  select.parentNode.insertBefore(wrapper, select);
  wrapper.appendChild(trigger);
  wrapper.appendChild(select);
  select.classList.add('native-select-hidden');
  select.tabIndex = -1;
  select.setAttribute('aria-hidden', 'true');

  trigger.addEventListener('click', (e) => {
    e.stopPropagation();
    openMenuFor(select);
  });

  trigger.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === ' ' || e.key === 'Enter') {
      if (!activeMenu || activeMenu.select !== select) {
        e.preventDefault();
        openMenuFor(select);
      }
    }
  });

  const protoDesc = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value');
  if (protoDesc) {
    Object.defineProperty(select, 'value', {
      get() {
        return protoDesc.get.call(this);
      },
      set(val) {
        protoDesc.set.call(this, val);
        syncSelect(this);
      },
      configurable: true,
    });
  }

  const observer = new MutationObserver(() => syncSelect(select));
  observer.observe(select, { childList: true, attributes: true, attributeFilter: ['class', 'disabled', 'title'] });

  instances.set(select, { wrapper, trigger, labelEl, observer });
  syncSelect(select);
}

export function initCustomSelects() {
  bindGlobalsOnce();
  document.querySelectorAll('select.mini-select').forEach(enhanceSelect);
}
