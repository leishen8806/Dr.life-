(() => {
  const menu = document.querySelector('.menu-toggle');
  const nav = document.querySelector('#site-nav');
  if (!menu || !nav) return;

  const closeMenu = () => {
    nav.dataset.open = 'false';
    menu.setAttribute('aria-expanded', 'false');
  };
  menu.addEventListener('click', () => {
    const open = nav.dataset.open !== 'true';
    nav.dataset.open = String(open);
    menu.setAttribute('aria-expanded', String(open));
  });
  nav.addEventListener('click', (event) => {
    if (event.target.closest('a')) closeMenu();
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && nav.dataset.open === 'true') {
      closeMenu();
      menu.focus();
    }
  });

  const dialog = document.querySelector('#inquiry');
  const fieldList = document.querySelector('#dialog-fields');
  const brief = document.querySelector('#inquiry-text');
  const status = document.querySelector('#copy-status');
  let opener = null;
  document.querySelectorAll('[data-inquiry]').forEach((button) => button.addEventListener('click', () => {
    if (!dialog || !fieldList || !brief || !status) return;
    const partner = button.closest('.partner, .panel-copy, .market');
    const fields = [...(partner?.querySelectorAll('.partner-fields li') ?? [])].map((field) => field.textContent.trim());
    const title = partner?.querySelector('h3, h2')?.textContent.trim() || button.textContent.trim();
    opener = button;
    document.querySelector('#dialog-title').textContent = button.textContent.trim();
    fieldList.replaceChildren(...fields.map((field) => { const item = document.createElement('li'); item.textContent = field; return item; }));
    brief.value = [`您好，我想咨询 DR.LIFE+ 的「${title}」。`, ...fields.map((field, index) => `${index + 1}、${field}：`)].join('\n');
    status.textContent = '';
    dialog.showModal();
  }));
  dialog?.addEventListener('close', () => opener?.focus());
  dialog?.addEventListener('click', (event) => { if (event.target === dialog) dialog.close(); });
  document.querySelector('#copy-inquiry')?.addEventListener('click', async () => {
    if (!brief || !status) return;
    try {
      if (!navigator.clipboard?.writeText) throw Error();
      await navigator.clipboard.writeText(brief.value);
      status.textContent = '已复制咨询模板。';
    } catch {
      brief.focus();
      brief.select();
      status.textContent = document.execCommand('copy') ? '已复制咨询模板。' : '已选中模板，请复制。';
    }
  });

  const channelStatus = document.querySelector('#channel-status');
  document.querySelectorAll('[data-copy-channel]').forEach((button) => button.addEventListener('click', async () => {
    if (!channelStatus) return;
    let copied = false;
    try {
      if (!navigator.clipboard?.writeText) throw Error();
      await navigator.clipboard.writeText('DR520');
      copied = true;
    } catch {
      const box = document.createElement('textarea');
      box.value = 'DR520';
      box.readOnly = true;
      box.style.position = 'fixed';
      box.style.left = '-9999px';
      document.body.append(box);
      box.select();
      try { copied = document.execCommand('copy'); } catch {}
      box.remove();
      button.focus();
    }
    channelStatus.textContent = copied ? `已复制 ${button.dataset.copyChannel} 账号：DR520。` : '账号：DR520，请手动复制。';
  }));

  window.__homepageInteractionLoaded = true;
})();
