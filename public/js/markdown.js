import { escapeHtml } from './state.js';

function renderInlineMarkdown(raw) {
  const codeTokens = [];
  let s = String(raw || '').replace(/`([^`\n]+)`/g, (_, code) => {
    const idx = codeTokens.length;
    codeTokens.push(`<code class="md-inline">${escapeHtml(code)}</code>`);
    return `\x00IC${idx}\x00`;
  });

  s = escapeHtml(s);
  s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_, label, url) => (
    `<a href="${url}" target="_blank" rel="noopener noreferrer">${label}</a>`
  ));
  s = s.replace(/\*\*([\s\S]+?)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/__([\s\S]+?)__/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^\*])\*([^*\n]+?)\*(?!\*)/g, '$1<em>$2</em>');
  s = s.replace(/~~([\s\S]+?)~~/g, '<del>$1</del>');
  s = s.replace(/\x00IC(\d+)\x00/g, (_, i) => codeTokens[Number(i)] || '');
  return s;
}

export function renderMarkdown(md) {
  const src = String(md || '').replace(/\r\n/g, '\n');
  if (!src.trim()) return '';

  const codeBlocks = [];
  const tokenized = src.replace(/```([a-zA-Z0-9_+-]*)\n([\s\S]*?)(?:```|$)/g, (_, lang, code) => {
    const idx = codeBlocks.length;
    const langLabel = lang ? escapeHtml(lang) : 'code';
    const cleanCode = code.replace(/\n$/, '');
    codeBlocks.push(
      `<div class="md-code-wrap">` +
        `<div class="md-code-head"><span>${langLabel}</span><button type="button" class="btn btn-xs btn-subtle md-copy-btn">Copy</button></div>` +
        `<pre class="md-code-block"><code>${escapeHtml(cleanCode)}</code></pre>` +
      `</div>`
    );
    return `\n\x00CB${idx}\x00\n`;
  });

  const lines = tokenized.split('\n');
  const out = [];
  let i = 0;

  while (i < lines.length) {
    const trimmed = lines[i].trim();

    if (!trimmed) {
      i++;
      continue;
    }

    const cbMatch = trimmed.match(/^\x00CB(\d+)\x00$/);
    if (cbMatch) {
      out.push(codeBlocks[Number(cbMatch[1])] || '');
      i++;
      continue;
    }

    if (/^([-*_])\1{2,}$/.test(trimmed)) {
      out.push('<hr>');
      i++;
      continue;
    }

    const hMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (hMatch) {
      const lvl = hMatch[1].length;
      out.push(`<h${lvl}>${renderInlineMarkdown(hMatch[2])}</h${lvl}>`);
      i++;
      continue;
    }

    if (trimmed.startsWith('>')) {
      const bqLines = [];
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        bqLines.push(lines[i].trim().replace(/^>\s?/, ''));
        i++;
      }
      out.push(`<blockquote>${bqLines.map(renderInlineMarkdown).join('<br>')}</blockquote>`);
      continue;
    }

    if (trimmed.includes('|') && i + 1 < lines.length && /^\s*\|?\s*[-:]+[-|\s:]*$/.test(lines[i + 1])) {
      const parseRow = (r) => r.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map((c) => c.trim());
      const headers = parseRow(lines[i]);
      i += 2;
      const rows = [];
      while (i < lines.length && lines[i].includes('|') && lines[i].trim() !== '') {
        rows.push(parseRow(lines[i]));
        i++;
      }
      const thead = `<thead><tr>${headers.map((h) => `<th>${renderInlineMarkdown(h)}</th>`).join('')}</tr></thead>`;
      const tbody = rows.length
        ? `<tbody>${rows.map((r) => `<tr>${r.map((c) => `<td>${renderInlineMarkdown(c)}</td>`).join('')}</tr>`).join('')}</tbody>`
        : '';
      out.push(`<table class="md-table">${thead}${tbody}</table>`);
      continue;
    }

    if (/^[-*+]\s+/.test(trimmed)) {
      const items = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        items.push(`<li>${renderInlineMarkdown(lines[i].replace(/^\s*[-*+]\s+/, ''))}</li>`);
        i++;
      }
      out.push(`<ul>${items.join('')}</ul>`);
      continue;
    }

    if (/^\d+\.\s+/.test(trimmed)) {
      const items = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        items.push(`<li>${renderInlineMarkdown(lines[i].replace(/^\s*\d+\.\s+/, ''))}</li>`);
        i++;
      }
      out.push(`<ol>${items.join('')}</ol>`);
      continue;
    }

    const pLines = [];
    while (
      i < lines.length &&
      lines[i].trim() !== '' &&
      !/^\x00CB\d+\x00$/.test(lines[i].trim()) &&
      !/^#{1,4}\s+/.test(lines[i].trim()) &&
      !/^([-*_])\1{2,}$/.test(lines[i].trim()) &&
      !lines[i].trim().startsWith('>') &&
      !/^\s*[-*+]\s+/.test(lines[i]) &&
      !/^\s*\d+\.\s+/.test(lines[i])
    ) {
      pLines.push(renderInlineMarkdown(lines[i]));
      i++;
    }
    out.push(`<p>${pLines.join('<br>')}</p>`);
  }

  return out.join('');
}
