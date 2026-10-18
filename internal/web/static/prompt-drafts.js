// Each composer owns a separate key so other tabs cannot overwrite its draft.
window.PromptDrafts = (() => {
  const prefix = 'tuios-deck:prompt:';
  const editors = new WeakMap();
  function attach(input) {
    if (editors.has(input)) return editors.get(input);
    const box = document.createElement('div');
    box.className = 'hint';
    const status = document.createElement('div');
    status.setAttribute('role', 'status');
    const recover = document.createElement('button');
    recover.type = 'button'; recover.textContent = 'Recover saved prompt…';
    const panel = document.createElement('div'); panel.hidden = true;
    const select = document.createElement('select'); select.setAttribute('aria-label', 'Saved prompts');
    const preview = document.createElement('textarea'); preview.readOnly = true;
    preview.setAttribute('aria-label', 'Saved prompt preview');
    const restore = document.createElement('button'); restore.type = 'button'; restore.textContent = 'Restore prompt';
    panel.append(select, preview, restore); box.append(status, recover, panel);
    input.closest('label').after(box);
    let key = '', saved = '';
    const editor = {
      save() {
        if (input.value === saved) return true;
        if (!input.value.trim()) { key = ''; saved = input.value; return true; }
        try {
          const nextKey = key || prefix + crypto.getRandomValues(new Uint32Array(4)).join('-');
          localStorage.setItem(nextKey, JSON.stringify({text: input.value, saved: Date.now()}));
          key = nextKey; saved = input.value;
          status.textContent = 'Saved in this browser'; status.classList.remove('err');
          return true;
        } catch (e) {
          status.textContent = 'Prompt NOT saved — keep this editor open: ' + e.message;
          status.classList.add('err');
          return false;
        }
      },
      begin() {
        key = ''; saved = ''; panel.hidden = true;
        status.textContent = 'Prompts save automatically in this browser';
        editor.save();
      },
    };
    input.addEventListener('input', editor.save);

    recover.onclick = () => {
      if (!editor.save()) return;
      try {
        const entries = Object.keys(localStorage).filter(k => k.startsWith(prefix))
          .map(k => JSON.parse(localStorage.getItem(k))).sort((a, b) => b.saved - a.saved);
        select.replaceChildren();
        const seen = new Set();
        for (const entry of entries) {
          if (seen.has(entry.text)) continue;
          seen.add(entry.text);
          const option = document.createElement('option'); option.value = entry.text;
          option.textContent = new Date(entry.saved).toLocaleString() + ' · ' + entry.text.replace(/\s+/g, ' ').slice(0, 80);
          select.append(option);
        }
        preview.value = select.value; restore.disabled = !select.options.length;
        panel.hidden = false;
        if (!select.options.length) status.textContent = 'No saved prompts in this browser';
      } catch (e) { status.textContent = 'Cannot read saved prompts: ' + e.message; status.classList.add('err'); }
    };
    select.onchange = () => { preview.value = select.value; };
    restore.onclick = () => {
      if (!editor.save()) return;
      key = ''; saved = '';
      input.value = select.value;
      input.dispatchEvent(new Event('input', {bubbles: true}));
      panel.hidden = true; input.focus();
    };
    editors.set(input, editor);
    return editor;
  }
  const save = form => [...form.querySelectorAll('[data-compose]')].every(input => attach(input).save());
  document.addEventListener('cancel', e => {
    if (e.target.matches('dialog') && !save(e.target)) e.preventDefault();
  }, true);
  document.addEventListener('close', e => {
    if (e.target.matches('dialog')) save(e.target);
  }, true);
  return {
    begin(form) { for (const input of form.querySelectorAll('[data-compose]')) attach(input).begin(); },
    save,
  };
})();
