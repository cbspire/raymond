// Run with PLAYWRIGHT_MODULE pointing to a Playwright installation and
// CHROMIUM_PATH pointing to a Chromium executable. Neither is a runtime dependency.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../../internal/proxy');
const output = process.env.SCREENSHOT_DIR || '/tmp/raymond-dashboard-screenshots';
fs.mkdirSync(output, {recursive:true});
let logsConnections = 0;
const server = http.createServer((req,res) => {
 if (req.url === '/api/v1/logs') {
  logsConnections++;
  res.writeHead(200,{'Content-Type':'application/x-ndjson','Cache-Control':'no-store'});
  res.write('{"method":"GET","host":"studio.test","path":"/","status":200}\n{"method":"GET","host":"docs.test","path":"/missing","status":404}\n');
  let count = 0; const timer = setInterval(() => res.write(`time=2026-10-30T18:00:00Z level=INFO msg=request method=GET host=api.test path=/health status=200 sequence=${++count}\n`),300);
  req.on('close',() => clearInterval(timer)); return;
 }
 const filename = {'/theme.js':'theme.js','/':'dashboard.html','/dashboard.js':'dashboard.js','/dashboard.css':'dashboard.css'}[req.url];
 if (!filename) { res.writeHead(404); res.end(); return; }
 res.setHeader('Content-Type', filename.endsWith('.js') ? 'text/javascript' : filename.endsWith('.css') ? 'text/css' : 'text/html');
 res.setHeader('Content-Security-Policy', "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src data:; base-uri 'none'; frame-ancestors 'none'; form-action 'none'");
 res.end(fs.readFileSync(path.join(root, filename)));
});
(async () => {
 await new Promise(resolve => server.listen(0,'127.0.0.1',resolve));
 const origin = `http://127.0.0.1:${server.address().port}`;
 const browser = await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:['--no-sandbox']});
 try {
 const context = await browser.newContext({viewport:{width:1440,height:1000}});
 const page = await context.newPage();
 const errors = [];
 page.on('pageerror',error => errors.push(error.message));
 let config = {version:1,dns:{zones:['test','localhost'],listen:'127.0.0.2:53'},proxy:{listen:'127.0.0.3:80'},routes:[{domain:'studio.test',target:'127.0.0.1:3000'},{domain:'api.test',target:'127.0.0.1:8080'},{domain:'docs.test',target:'127.0.0.1:4321'},{domain:'*.sites.test',target:'127.0.0.1:8025'}]};
 let revision = 'a'.repeat(64), operations = [], conflict = false, disconnected = false, pending = false;
 await page.route('**/api/v1/**',async route => {
  const request = route.request(), endpoint = new URL(request.url()).pathname.slice(8);
  let data, status = 200;
  if (disconnected) { await route.fulfill({status:503,json:{error:'Management unavailable.'}}); return; }
  if (endpoint === 'session') data = {csrf:'test-csrf'};
  else if (endpoint === 'state') data = {version:'0.1.2-preview',platform:'linux/amd64',running:true,saved:{config,revision},active:{config,revision},pending};
  else if (endpoint === 'operations' && request.method() === 'POST') {
   assert.equal(request.headers()['x-raymond-csrf'],'test-csrf');
   const body = request.postDataJSON(); operations.push(body);
   if (body.action === 'save') {
    if (conflict || body.revision !== revision) { status = 409; data = {error:'Configuration changed externally; reload before saving.'}; }
    else { config = body.config; revision = 'b'.repeat(64); }
   }
   if (!data) data = {id:'fixture-operation',action:body.action,status:'queued'};
  } else if (endpoint.startsWith('operations/')) data = {id:'fixture-operation',status:'succeeded',result:{service:'healthy',config:'valid'}};
  else if (endpoint === 'logs') { await route.continue(); return; }
  else { status = 404; data = {error:'Unknown fixture endpoint'}; }
  await route.fulfill({status,json:data});
 });
 await page.goto(origin);
 await page.getByText('Connected locally',{exact:true}).waitFor();
 await page.screenshot({path:path.join(output,'dashboard-desktop.png'),fullPage:true});
 await page.getByRole('button',{name:'+ Add service',exact:true}).click();
 await page.getByLabel('Domain',{exact:true}).fill('preview.test');
 await page.getByLabel('Port',{exact:true}).fill('5173');
 await page.screenshot({path:path.join(output,'dashboard-add-service.png'),fullPage:true});
 await page.getByRole('button',{name:'Keep in draft',exact:true}).click();
 await page.getByRole('link',{name:'preview.test ↗',exact:true}).waitFor();
 assert.equal(operations.length,0,'draft must not write to backend');
 await page.getByRole('button',{name:'Save & Apply',exact:true}).click();
 await page.getByText('Your workspace is up to date. Changes saved and applied.',{exact:true}).waitFor();
 assert.equal(config.routes.length,5);
 await page.getByRole('button',{name:'+ Add service',exact:true}).click();
 await page.getByLabel('Domain',{exact:true}).fill('*.preview.test');
 await page.getByRole('button',{name:'Keep in draft',exact:true}).click();
 await page.getByRole('link',{name:'*.preview.test ↗',exact:true}).waitFor();
 await page.getByRole('button',{name:'Discard',exact:true}).click();
 await page.getByRole('button',{name:'Edit preview.test',exact:true}).click();
 await page.getByLabel('Port',{exact:true}).fill('5174');
 await page.getByRole('button',{name:'Keep in draft',exact:true}).click();
 await page.getByRole('button',{name:'Discard',exact:true}).click();
 assert.equal(config.routes[4].target,'127.0.0.1:5173');
 await page.getByRole('button',{name:'Remove preview.test',exact:true}).click();
 await page.getByRole('button',{name:'Continue',exact:true}).click();
 assert.equal(config.routes.length,5,'remove stays in draft');
 await page.getByRole('button',{name:'Save & Apply',exact:true}).click();
 await page.getByText('Your workspace is up to date. Changes saved and applied.',{exact:true}).waitFor();
 assert.equal(config.routes.length,4);
 await page.getByRole('searchbox',{name:'Search services'}).fill('studio');
 assert.equal(await page.locator('.service').count(),1);
 await page.getByRole('searchbox',{name:'Search services'}).fill('');
 await page.getByRole('button',{name:'DNS & settings',exact:true}).click();
 await page.getByText('Advanced settings',{exact:true}).click();
 await page.screenshot({path:path.join(output,'dashboard-settings.png'),fullPage:true});
 await page.getByLabel('Zones, separated by commas').fill('test, localhost, internal');
 await page.getByRole('button',{name:'Keep changes in draft'}).click();
 await page.getByRole('button',{name:'Discard',exact:true}).click();
 await page.getByRole('button',{name:'Live logs',exact:true}).click();
 await page.locator('#logs').filter({hasText:'studio.test'}).waitFor();
 await page.locator('#logs').filter({hasText:'sequence=1'}).waitFor();
 await page.getByRole('button',{name:'Pause',exact:true}).click();
 const pausedLogs = await page.locator('#logs').textContent(); await page.waitForTimeout(400); assert.equal(await page.locator('#logs').textContent(),pausedLogs);
 await page.getByLabel('Filter status').selectOption('4');
 assert.match(await page.locator('#logs').textContent(),/404/);
 assert.doesNotMatch(await page.locator('#logs').textContent(),/200/);
 await page.getByLabel('Filter status').selectOption('');
 await page.screenshot({path:path.join(output,'dashboard-logs.png'),fullPage:true});
 await page.getByRole('button',{name:'Resume',exact:true}).click(); await page.waitForFunction(() => document.getElementById('log-state').textContent.startsWith('Live activity')); assert.ok(logsConnections >= 2);
 await page.getByLabel('Filter host').fill('api.test'); assert.doesNotMatch(await page.locator('#logs').textContent(),/studio.test/); await page.getByLabel('Filter host').fill('');
 await page.getByRole('button',{name:'System',exact:true}).click();
 await page.getByRole('button',{name:'Run diagnostics'}).click();
 await page.getByText('Operation completed.',{exact:true}).waitFor();
 await page.screenshot({path:path.join(output,'dashboard-system.png'),fullPage:true});
 await page.getByRole('button',{name:'Stop Raymond',exact:true}).click();
 await page.getByText(/Run raymond start in your terminal/).waitFor();
 await page.getByRole('button',{name:'Cancel',exact:true}).click();
 assert.ok(!operations.some(o => o.action === 'stop'),'cancel must not stop service');
 await page.getByRole('button',{name:/Services/,exact:false}).first().click();
 await page.getByRole('button',{name:'+ Add service',exact:true}).click();
 await page.getByLabel('Domain',{exact:true}).fill('studio.test');
 await page.getByRole('button',{name:'Keep in draft',exact:true}).click();
 await page.getByText('This domain is already registered.').waitFor();
 await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'Edit studio.test',exact:true}).click();
 await page.getByLabel('Port',{exact:true}).fill('3001');
 await page.getByRole('button',{name:'Keep in draft',exact:true}).click();
 conflict = true;
 await page.getByRole('button',{name:'Save & Apply',exact:true}).click();
 await page.getByText(/Configuration changed externally/).waitFor();
 assert.equal(await page.locator('#draft-bar').isVisible(),true,'failed save keeps draft');
 await page.screenshot({path:path.join(output,'dashboard-conflict.png'),fullPage:true});
 await page.getByRole('button',{name:'Discard',exact:true}).click(); conflict = false;
 // Hostile saved values must render as text and never become executable links.
 config.routes.push({domain:'<img src=x onerror="window.pwned=true">',target:'<script>bad()</script>'});
 await page.reload(); await page.getByText('Connected locally',{exact:true}).waitFor();
 assert.equal(await page.locator('#service-list img,#service-list script').count(),0);
 assert.equal(await page.evaluate(() => window.pwned),undefined);
 config.routes.pop();
 await page.setViewportSize({width:390,height:844});
 await page.reload(); await page.getByText('Connected locally',{exact:true}).waitFor();
 assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),false,'mobile must not overflow');
 await page.screenshot({path:path.join(output,'dashboard-mobile.png'),fullPage:true});
 await page.getByRole('button',{name:'Toggle dark mode'}).click(); assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');
 await page.reload(); await page.getByText('Connected locally',{exact:true}).waitFor(); assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');
 await page.screenshot({path:path.join(output,'dashboard-mobile-dark.png'),fullPage:true});
 await page.setViewportSize({width:1440,height:1000});
 await page.screenshot({path:path.join(output,'dashboard-desktop-dark.png'),fullPage:true});
 await page.getByRole('button',{name:'Live logs',exact:true}).click(); await page.locator('#logs').filter({hasText:'sequence=1'}).waitFor();
 await page.screenshot({path:path.join(output,'dashboard-logs-dark.png'),fullPage:true});
 await page.getByRole('button',{name:'Services',exact:true}).click();
 await page.getByRole('button',{name:'Toggle dark mode'}).click();
 await page.getByRole('button',{name:'+ Add service',exact:true}).click();
 assert.equal(await page.getByLabel('Domain',{exact:true}).evaluate(el => el === document.activeElement),true);
 await page.keyboard.press('Escape');
 pending = true; await page.reload(); await page.getByText('Saved changes are waiting to be applied.').waitFor();
 disconnected = true; await page.reload(); await page.getByText(/Management unavailable/).waitFor();
 assert.equal(await page.locator('#stop').isEnabled(),false);
 await page.screenshot({path:path.join(output,'dashboard-disconnected.png'),fullPage:true});
 assert.deepEqual(errors,[]);
 console.log('PASS: wildcard service drafts, live streaming/append/pause/resume/cancellation, persisted dark mode, service CRUD, browser drafts, save/CSRF, search, settings, logs/filter/pause, diagnostics, stop cancellation, duplicate domains, stale saves, XSS, mobile overflow, focus, pending and disconnected states.');
 console.log('Screenshots: ' + output + ' (controlled API fixture; not native lifecycle acceptance)');
 await context.close();
 } finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
})().catch(error => { console.error(error); process.exitCode = 1; server.close(); });
