'use strict';
(() => {
 const $ = id => document.getElementById(id);
 let state, draft, csrf = '', editing = -1, busy = false, online = false, paused = false, logController, logLines = [], tab = 'services';
 const clone = value => JSON.parse(JSON.stringify(value));
 const dirty = () => !!state && JSON.stringify(draft) !== JSON.stringify(state.saved.config);
 function notice(message) { $('notice').textContent = message; $('notice').hidden = !message; }
 async function api(path, body, signal) {
  const response = await fetch('/api/v1/' + path, {method: body ? 'POST' : 'GET', credentials:'same-origin', cache:'no-store', headers:body ? {'Content-Type':'application/json','X-Raymond-CSRF':csrf} : {}, body:body ? JSON.stringify(body) : undefined, signal});
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || 'Raymond could not complete this request.');
  return data;
 }
 function connection(connected) { online = connected; $('connection').textContent = connected ? 'Connected locally' : 'Disconnected'; $('connection').classList.toggle('online', connected); $('add-service').disabled = !state || busy; $('save').disabled = !connected || busy; document.querySelectorAll('[data-action],#stop').forEach(b => b.disabled = !connected || busy); }
 function draftStatus() {
  $('draft-bar').hidden = !dirty();
  if (!state) return;
  const old = state.saved.config.routes || [], routes = draft.routes || [];
  const added = routes.filter(r => !old.some(o => o.domain === r.domain)).length;
  const removed = old.filter(r => !routes.some(o => o.domain === r.domain)).length;
  const edited = routes.filter(r => old.some(o => o.domain === r.domain && o.target !== r.target)).length;
  const summary = [];
  if (added) summary.push(`${added} added`); if (edited) summary.push(`${edited} edited`); if (removed) summary.push(`${removed} removed`);
  if (JSON.stringify(draft.dns) !== JSON.stringify(state.saved.config.dns) || JSON.stringify(draft.proxy) !== JSON.stringify(state.saved.config.proxy)) summary.push('settings changed');
  $('change-summary').textContent = summary.join(' · ');
  $('route-count').textContent = routes.length;
 }
 function renderServices() {
  const list = $('service-list'); list.replaceChildren();
  const search = $('service-search').value.toLowerCase();
  const routes = (draft?.routes || []).filter(r => `${r.domain} ${r.target}`.toLowerCase().includes(search));
  if (!routes.length) { const empty = document.createElement('div'); empty.className = 'empty'; empty.textContent = !state ? 'Connect to Raymond to see your services.' : search ? 'No services match your search.' : 'A fresh workspace. Add your first local service to get started.'; list.append(empty); }
  for (const route of routes) {
   const row = document.createElement('div'); row.className = 'service';
   const icon = document.createElement('div'); icon.className = 'service-icon'; icon.textContent = route.domain[0].toUpperCase(); icon.setAttribute('aria-hidden','true');
   const name = document.createElement('div'); name.className = 'service-name';
   const link = document.createElement('a'); link.textContent = route.domain; link.setAttribute('aria-label', route.domain + ' ↗'); const arrow = document.createElementNS('http://www.w3.org/2000/svg','svg'); arrow.setAttribute('viewBox','0 0 16 16'); arrow.setAttribute('aria-hidden','true'); const arrowPath = document.createElementNS('http://www.w3.org/2000/svg','path'); arrowPath.setAttribute('d','M4 12L12 4M4 4h8v8'); arrowPath.setAttribute('fill','none'); arrowPath.setAttribute('stroke','currentColor'); arrow.append(arrowPath); link.append(arrow);
   if (/^(\*\.)?[a-z0-9.-]+$/i.test(route.domain)) { link.href = 'http://' + route.domain.replace(/^\*\./,'app.'); link.target = '_blank'; link.rel = 'noopener noreferrer'; }
   const backend = document.createElement('p'); backend.textContent = '→ ' + route.target; name.append(link,backend);
   const label = document.createElement('span'); label.className = 'route-label'; label.textContent = route.domain.startsWith('*.') ? 'WILDCARD' : 'LOCAL BACKEND';
   const edit = document.createElement('button'); edit.textContent = 'Edit'; edit.setAttribute('aria-label','Edit ' + route.domain); edit.onclick = () => openService(draft.routes.indexOf(route));
   const remove = document.createElement('button'); remove.textContent = 'Remove'; remove.setAttribute('aria-label','Remove ' + route.domain); remove.onclick = () => confirmAction('Remove service?', `${route.domain} will be removed when you Save & Apply.`, () => { draft.routes.splice(draft.routes.indexOf(route),1); renderServices(); draftStatus(); });
   row.append(icon,name,label,edit,remove); list.append(row);
  }
  $('service-total').textContent = state ? draft.routes.length : '—';
  draftStatus();
 }
 function renderSettings() { if (!draft) return; $('zones').value = draft.dns.zones.join(', '); $('dns-listen').value = draft.dns.listen; $('proxy-listen').value = draft.proxy.listen; $('coexist').checked = !!draft.proxy.allow_wildcard_coexistence; }
 function render() {
  renderServices(); renderSettings(); if (!state) return;
  $('zone-total').textContent = draft.dns.zones.length; $('running').textContent = state.running ? 'Listening' : 'Stopped'; $('proxy-address').textContent = state.active.config.proxy.listen; $('version').textContent = 'Raymond ' + state.version; $('pending').hidden = !state.pending;
  const dl = $('system-info'); dl.replaceChildren();
  for (const [key,value] of [['Version',state.version],['Platform',state.platform],['Service',state.running ? 'Running' : 'Stopped'],['Saved revision',state.saved.revision.slice(0,12)],['Active revision',state.active.revision.slice(0,12)]]) { const dt = document.createElement('dt'), dd = document.createElement('dd'); dt.textContent = key; dd.textContent = value; dl.append(dt,dd); }
 }
 async function refresh() {
  try { const next = await api('state'); if (!next.saved?.config || !next.active?.config) throw new Error('Raymond returned an incomplete state.'); const preserve = dirty(); state = next; if (!preserve) draft = clone(state.saved.config); connection(true); render(); return true; }
  catch (error) { connection(false); notice(error.message + ' Open an authorized browser with raymond dashboard.'); return false; }
 }
 function openService(index = -1) { if (!draft) return; editing = index; $('service-form').reset(); $('form-error').textContent = ''; $('form-title').textContent = index < 0 ? 'Add a service' : 'Edit service'; if (index >= 0) { const route = draft.routes[index]; $('domain').value = route.domain; const [ip,port] = route.target.split(':'); $('backend-ip').value = ip; $('backend-port').value = port; } $('service-dialog').showModal(); $('domain').focus(); }
 $('add-service').onclick = () => openService();
 for (const id of ['close-dialog','cancel-dialog']) $(id).onclick = () => $('service-dialog').close();
 $('service-form').onsubmit = event => {
  event.preventDefault(); const domain = $('domain').value.trim().toLowerCase().replace(/\.$/,''); const ip = $('backend-ip').value.trim(), port = Number($('backend-port').value);
  const labels = domain.replace(/^\*\./,'').split('.');
  if (domain.length > 253 || labels.some(s => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(s))) { $('form-error').textContent = 'Use a valid ASCII domain, such as studio.test.'; return; }
  if (!draft.dns.zones.some(zone => domain === zone || domain.endsWith('.' + zone))) { $('form-error').textContent = 'The domain must belong to one of your DNS zones.'; return; }
  if (draft.routes.some((r,i) => i !== editing && r.domain === domain)) { $('form-error').textContent = 'This domain is already registered.'; return; }
  if (!/^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(ip) || ip.split('.').some(s => Number(s) > 255 || String(Number(s)) !== s) || !Number.isInteger(port) || port < 1 || port > 65535) { $('form-error').textContent = 'Use a loopback IPv4 address and a port from 1 to 65535.'; return; }
  const route = {domain,target:ip + ':' + port}; if (editing < 0) draft.routes.push(route); else draft.routes[editing] = route;
  $('service-dialog').close(); renderServices();
 };
 $('service-search').oninput = renderServices;
 $('stage-settings').onclick = () => { if (!draft) return; const zones = $('zones').value.split(',').map(s => s.trim().toLowerCase().replace(/\.$/,'')).filter(Boolean); if (!zones.length || zones.some(s => !/^[a-z0-9.-]+$/.test(s))) { notice('Enter at least one valid DNS zone.'); return; } draft.dns = {zones,listen:$('dns-listen').value.trim()}; draft.proxy = {listen:$('proxy-listen').value.trim(),allow_wildcard_coexistence:$('coexist').checked}; draftStatus(); $('zone-total').textContent = zones.length; notice('Settings kept in your browser draft. Save & Apply validates the complete configuration.'); };
 $('discard-draft').onclick = () => { draft = clone(state.saved.config); render(); notice('Browser draft discarded.'); };
 function confirmAction(title,message,callback) { $('confirm-title').textContent = title; $('confirm-message').textContent = message; $('confirm-dialog').showModal(); $('confirm-cancel').onclick = () => $('confirm-dialog').close(); $('confirm-ok').onclick = () => { $('confirm-dialog').close(); callback(); }; }
 async function submit(action, extra = {}) {
  if (busy || !online) return;
  if (dirty() && action !== 'save') { notice('Save & Apply or discard your browser draft before running a system action.'); return; }
  busy = true; connection(online); notice('Raymond is working…');
  try {
   let op = await api('operations',{action,...extra});
   const deadline = Date.now() + 5 * 60 * 1000;
   while (op.status === 'queued' || op.status === 'running') { if (Date.now() > deadline) throw new Error('This operation is still pending. Reconnect to check its status.'); notice(op.phase || 'Applying changes. Raymond may briefly reconnect…'); await new Promise(resolve => setTimeout(resolve,750)); try { op = await api('operations/' + encodeURIComponent(op.id)); } catch { connection(false); } }
   if (op.status !== 'succeeded') throw new Error(op.error || 'The operation failed. Your saved changes may still be pending.');
   if (action === 'save') { state = undefined; draft = undefined; }
   if (action === 'stop') { connection(false); notice('Raymond is stopped. Run raymond start to bring your workspace back.'); return; }
   if (op.result !== undefined) { $('operation-result').hidden = false; $('operation-result').textContent = JSON.stringify(op.result,null,2); }
   await refresh(); notice(action === 'save' ? 'Your workspace is up to date. Changes saved and applied.' : 'Operation completed.');
  } catch(error) { notice(error.message); if (state) { try { const next = await api('state'); if (action === 'save') { state.pending = next.pending; state.active = next.active; } else state = next; render(); } catch { connection(false); } } }
  finally { busy = false; connection(online); }
 }
 $('save').onclick = () => submit('save',{config:clone(draft),revision:state.saved.revision});
 document.querySelectorAll('[data-action]').forEach(button => button.onclick = () => { const action = button.dataset.action; if (['restart','update','discard'].includes(action)) confirmAction(button.textContent + '?', action === 'update' ? 'Download the latest stable release from the fixed Raymond release source and update the local installation. The dashboard will briefly disconnect.' : action === 'discard' ? 'Restore the active configuration as the saved configuration. This discards saved pending edits.' : 'Raymond will restart and this dashboard will briefly disconnect.', () => submit(action)); else submit(action); });
 $('stop').onclick = () => confirmAction('Stop Raymond?', 'Your local addresses and this dashboard will stop responding. Run raymond start in your terminal to bring them back.', () => submit('stop'));
 function showTab(name) { tab = name; document.querySelectorAll('.view').forEach(view => view.hidden = (view.dataset.view || view.id) !== name); document.querySelectorAll('[data-tab]').forEach(button => { if (button.dataset.tab === name) button.setAttribute('aria-current','page'); else button.removeAttribute('aria-current'); }); $('breadcrumb').textContent = {services:'Services',dns:'DNS & settings',logs:'Live logs',system:'System'}[name]; if (name === 'logs' && !paused) connectLogs(); else logController?.abort(); }
 document.querySelectorAll('[data-tab]').forEach(button => button.onclick = () => showTab(button.dataset.tab));
 function renderLogs() { const search = $('log-search').value.toLowerCase(), host = $('log-host').value.toLowerCase(), status = $('log-status').value; const lines = logLines.filter(line => { let entry; try { entry = JSON.parse(line); } catch { const fields = {}; for (const match of line.matchAll(/(?:^|\s)(host|status)=(?:"([^"]*)"|([^\s]+))/g)) fields[match[1]] = match[2] || match[3]; entry = fields; } return line.toLowerCase().includes(search) && (!host || String(entry.host || '').toLowerCase().includes(host)) && (!status || String(entry.status || '').startsWith(status)); }); const stick = $('logs').scrollHeight - $('logs').scrollTop - $('logs').clientHeight < 50; $('logs').textContent = lines.join('\n') || (logLines.length ? 'No requests match your filters.' : 'Waiting for requests…'); if (stick) $('logs').scrollTop = $('logs').scrollHeight; $('log-count').textContent = `${lines.length} / ${logLines.length} entries`; }
 async function connectLogs() {
  logController?.abort(); if (!online || paused || tab !== 'logs') return;
  const controller = new AbortController(); logController = controller;
  try { const res = await fetch('/api/v1/logs',{credentials:'same-origin',signal:controller.signal}); if (!res.ok) throw new Error('Activity is unavailable.'); $('log-state').textContent = 'Live activity · last 500 entries · query strings, cookies and payloads excluded'; const reader = res.body.getReader(); const decoder = new TextDecoder(); let pending = ''; while (!controller.signal.aborted) { const {done,value} = await reader.read(); if (done) break; pending += decoder.decode(value,{stream:true}); const lines = pending.split('\n'); pending = lines.pop().slice(-8192); logLines.push(...lines.map(line => line.slice(0,8192))); logLines = logLines.slice(-500); renderLogs(); } if (!controller.signal.aborted) throw new Error('Activity disconnected. Reconnecting…'); }
  catch(error) { if (!controller.signal.aborted) { $('log-state').textContent = error.message; setTimeout(() => { if (tab === 'logs' && !paused) connectLogs(); },3000); } }
 }
 $('pause-logs').onclick = () => { paused = !paused; $('pause-logs').textContent = paused ? 'Resume' : 'Pause'; if (paused) { logController?.abort(); $('log-state').textContent = 'Paused. New entries are not being collected.'; } else connectLogs(); };
 $('clear-logs').onclick = () => { logLines = []; renderLogs(); };
 for (const id of ['log-search','log-host','log-status']) $(id).oninput = renderLogs;
 addEventListener('beforeunload',event => { if (dirty()) { event.preventDefault(); event.returnValue = ''; } });
 async function boot() { connection(false); renderServices(); try { const token = new URLSearchParams(location.hash.slice(1)).get('token'); if (token) history.replaceState(null,'',location.pathname); const session = await api('session',token ? {token} : undefined); csrf = session.csrf; await refresh(); } catch(error) { notice(error.message + ' Open this page with raymond dashboard to connect securely.'); } }
 boot(); setInterval(() => { if (!online && !busy) boot(); },10000);
})();
