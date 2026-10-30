// Apply the saved preference before rendering the page; no inline script needed.
(() => {
 let saved;
 try { saved = localStorage.getItem('raymond-theme'); } catch {}
 const system = matchMedia('(prefers-color-scheme: dark)');
 const apply = dark => { document.documentElement.dataset.theme = dark ? 'dark' : 'light'; const button = document.getElementById('theme-toggle'); if (button) { button.textContent = dark ? 'Light mode' : 'Dark mode'; button.setAttribute('aria-pressed',String(dark)); } };
 apply(saved === 'dark' || (saved !== 'light' && system.matches));
 system.addEventListener('change',event => { if (!saved) apply(event.matches); });
 addEventListener('DOMContentLoaded',() => {
  apply(document.documentElement.dataset.theme === 'dark');
  document.getElementById('theme-toggle').onclick = () => { saved = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'; try { localStorage.setItem('raymond-theme',saved); } catch {} apply(saved === 'dark'); };
 });
})();
