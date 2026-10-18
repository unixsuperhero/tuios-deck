function createReviewPage(api, setStatus, isActive) {
  const $ = (id) => document.getElementById(id);
  let items = [], selected = '', history = false, opened = null, busy = false;
  const visible = () => items.filter((r) => Boolean(r.reviewed_at) === history);
  const completed = (r) => new Date(r.completed_at / 1e6).toLocaleString();

  function highlight(id) {
    selected = id;
    for (const row of $('review-list').children) {
      row.classList.toggle('selected', row.dataset.id === id);
      if (row.dataset.id) row.setAttribute('aria-pressed', String(row.dataset.id === id));
    }
    $('review-open').disabled = !id;
  }

  function render() {
    const rows = visible(), list = $('review-list');
    const scroll = list.scrollTop;
    const focused = list.contains(document.activeElement) ? document.activeElement.dataset.id : '';
    if (!rows.some((r) => r.id === selected)) selected = rows[0]?.id || '';
    list.replaceChildren();
    $('review-count').textContent = `${history ? 'Reviewed history' : 'Review queue'} (${rows.length})`;
    if (!rows.length) {
      const empty = document.createElement('div'); empty.className = 'empty';
      empty.textContent = history ? 'No reviewed prompts yet.' : 'No completed prompts awaiting review.';
      list.append(empty);
    }
    for (const r of rows) {
      const row = document.createElement('button'); row.className = 'caprow'; row.dataset.id = r.id;
      row.style.cssText = 'width:100%;text-align:left;border-radius:0';
      const title = document.createElement('span'); title.className = 'cname'; title.textContent = r.prompt.replace(/\s+/g, ' ');
      const meta = document.createElement('span'); meta.className = 'cmeta'; meta.textContent = `${completed(r)} · ${r.session} › ${r.pane}`;
      row.append(title, meta);
      row.onclick = () => highlight(r.id);
      row.onfocus = () => highlight(r.id);
      list.append(row);
    }
    highlight(selected);
    if (focused) [...list.children].find((row) => row.dataset.id === focused)?.focus({ preventScroll: true });
    list.scrollTop = scroll;
  }

  async function load(refresh = false) {
    if (busy) return;
    busy = true;
    try {
      const result = await api('/api/reviews' + (refresh ? '?refresh=1' : ''));
      const next = result.reviews || [];
      if (JSON.stringify(next) !== JSON.stringify(items)) { items = next; render(); }
      if (result.warning) setStatus('review: ' + result.warning, true);
    } catch (e) { setStatus('review: ' + e.message, true); }
    finally { busy = false; }
  }

  async function open() {
    const id = selected;
    if (!id) return;
    $('review-open').disabled = true;
    try {
      const { review: r } = await api(`/api/reviews/${encodeURIComponent(id)}/open`, { method: 'POST' });
      const changed = opened?.id !== r.id;
      opened = r;
      items = items.map((item) => item.id === r.id ? r : item);
      $('review-who').textContent = `${r.session} › ${r.pane}`;
      const text = `${r.harness} · Completed ${completed(r)}\nReviewed ${new Date(r.reviewed_at).toLocaleString()}\nSource: agent activity (may be truncated by the harness)\n\nPROMPT\n${r.prompt}\n\nRESPONSE\n${r.response || 'No response text was reported by this harness.'}${r.output ? '\n\nPANE OUTPUT AT COLLECTION (may include earlier turns)\n' + r.output : ''}`;
      if ($('review-text').textContent !== text) $('review-text').textContent = text;
      if (changed) $('review-text').scrollTop = 0;
      render();
    } catch (e) { setStatus('review: ' + e.message, true); $('review-open').disabled = !selected; }
  }

  function key(e) {
    if (e.key === 'Enter' && (e.target === document.body || e.target.closest('#review-list'))) {
      e.preventDefault(); open();
    } else if (['j', 'k', 'ArrowDown', 'ArrowUp'].includes(e.key)) {
      e.preventDefault();
      const rows = visible(), dir = ['j', 'ArrowDown'].includes(e.key) ? 1 : -1;
      const i = rows.findIndex((r) => r.id === selected) + dir;
      if (rows[i]) { highlight(rows[i].id); [...$('review-list').children].find((row) => row.dataset.id === selected)?.focus({ preventScroll: true }); }
    }
  }
  function reset() {
    history = false; $('review-filter').value = 'pending';
    opened = null;
    $('review-who').textContent = '';
    $('review-text').textContent = 'Highlight a prompt, then Enter or Open to read it and mark it reviewed.';
    render();
  }
  $('review-filter').onchange = () => { history = $('review-filter').value === 'reviewed'; selected = ''; render(); };
  $('review-refresh').onclick = () => load(true);
  $('review-open').onclick = open;
  render();
  setInterval(() => { if (isActive() && !document.querySelector('dialog[open]')) load(); }, 2000);
  return { load, key, reset };
}
