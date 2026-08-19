package server

// dashboardHTML — embedded single-page dashboard (GitHub-dark style).
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Qoder2API</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0d1117; color: #c9d1d9; min-height: 100vh; padding: 24px; }
.header { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
.header h1 { font-size: 20px; font-weight: 600; color: #e6edf3; }
.header .sub { font-size: 13px; color: #8b949e; }
.dot { width: 10px; height: 10px; border-radius: 50%; background: #56d364; box-shadow: 0 0 8px #56d364; animation: pulse 2s infinite; }
@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.5} }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 16px; }
.card { background: #161b22; border: 1px solid #21262d; border-radius: 8px; padding: 18px; }
.card-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: #8b949e; margin-bottom: 14px; }
.row { display: flex; justify-content: space-between; align-items: center; padding: 6px 0; border-bottom: 1px solid #21262d; gap: 8px; }
.row:last-child { border-bottom: none; }
.row-label { font-size: 13px; color: #8b949e; }
.row-value { font-size: 13px; color: #e6edf3; font-weight: 500; text-align: right; word-break: break-all; }
.badge { padding: 2px 8px; border-radius: 12px; font-size: 11px; font-weight: 600; user-select: none; }
.badge-green { background: #1a3a1a; color: #56d364; border: 1px solid #238636; }
.badge-red   { background: #3a1a1a; color: #f85149; border: 1px solid #da3633; }
.badge-gray  { background: #21262d; color: #8b949e; border: 1px solid #30363d; }
.badge-blue  { background: #1a2a3a; color: #79c0ff; border: 1px solid #1f6feb; }
button { background: #21262d; color: #c9d1d9; border: 1px solid #30363d; border-radius: 6px; padding: 4px 10px; font-size: 12px; cursor: pointer; }
button:hover { background: #30363d; }
button.primary { background: #1f6feb; border-color: #1f6feb; color: #fff; }
select { background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; padding: 4px 6px; font-size: 12px; }
input { background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; padding: 6px 8px; font-size: 12px; width: 100%; }
.endpoint { font-family: monospace; font-size: 12px; color: #56d364; }
.method { display: inline-block; padding: 2px 6px; border-radius: 4px; font-size: 10px; font-weight: 600; margin-right: 6px; }
.method-post { background: #1a2a3a; color: #79c0ff; border: 1px solid #1f6feb; }
.method-get  { background: #1a3a1a; color: #56d364; border: 1px solid #238636; }
.path { font-family: monospace; color: #e6edf3; font-size: 13px; }
.desc { font-size: 12px; color: #8b949e; margin-top: 2px; }
.ep-row { padding: 8px 0; border-bottom: 1px solid #21262d; }
.ep-row:last-child { border-bottom: none; }
.loading { color: #484f58; font-style: italic; font-size: 13px; }
.model { padding: 6px 8px; border-radius: 6px; cursor: pointer; display: flex; justify-content: space-between; align-items: center; }
.model:hover { background: #21262d; }
.model.selected { background: #1a2a3a; border: 1px solid #1f6feb; }
.account { border: 1px solid #21262d; border-radius: 6px; padding: 10px; margin-bottom: 10px; }
.account.active { border-color: #238636; }
.account .top { display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; gap: 6px; flex-wrap: wrap; }
.muted { color: #8b949e; font-size: 12px; }
.actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
#modelsList { max-height: 340px; overflow-y: auto; scrollbar-width: thin; scrollbar-color: #30363d transparent; }
#modelsList::-webkit-scrollbar { width: 8px; }
#modelsList::-webkit-scrollbar-track { background: transparent; }
#modelsList::-webkit-scrollbar-thumb { background: #30363d; border-radius: 4px; }
#modelsList::-webkit-scrollbar-thumb:hover { background: #484f58; }
</style>
</head>
<body>

<div class="header">
  <div class="dot" id="statusDot"></div>
  <div>
    <h1>QoderAPI</h1>
    <div class="sub" id="modeSub">OpenAI/Anthropic bridge · loading…</div>
  </div>
</div>

<div class="grid">

  <div class="card">
    <div class="card-title">Status</div>
    <div class="row"><span class="row-label">Proxy</span><span class="badge badge-green" id="proxyBadge">running</span></div>
    <div class="row"><span class="row-label">Endpoint</span><span class="endpoint" id="endpointVal">—</span></div>
    <div class="row"><span class="row-label">Accounts</span><span class="row-value" id="accCountVal">—</span></div>
    <div class="row"><span class="row-label">Active PAT</span><span class="row-value" id="activePatVal">—</span></div>
    <div class="row"><span class="row-label">User</span><span class="row-value" id="userVal">—</span></div>
    <div class="row"><span class="row-label">Default model</span><span class="row-value" id="defModelVal">—</span></div>
    <div class="row"><span class="row-label">Context</span>
      <select id="ctxSel" onchange="setRuntime()"></select></div>
    <div class="row"><span class="row-label">Thinking</span>
      <select id="thinkSel" onchange="setRuntime()"></select></div>
  </div>

  <div class="card">
    <div class="card-title">Usage <span class="muted">(active account)</span>
      <button style="float:right" onclick="refreshQuota()">Refresh</button>
    </div>
    <div class="row"><span class="row-label">Plan</span><span class="row-value" id="planVal">—</span></div>
    <div class="row"><span class="row-label">Credits</span><span class="row-value" id="creditsVal">not fetched</span></div>
    <div class="row"><span class="row-label">Expires</span><span class="row-value" id="expiresVal">—</span></div>
    <div class="row"><span class="row-label">Promo</span></div>
    <div id="promoList" class="muted">—</div>
  </div>

  <div class="card">
    <div class="card-title">Models <span class="muted">(live · click = default model)</span></div>
    <div id="modelsList"><span class="loading">loading…</span></div>
  </div>

  <div class="card" style="grid-column: 1 / -1;">
    <div class="card-title">Accounts</div>
    <div id="accountsList"><span class="loading">loading…</span></div>
    <div class="row" style="border-top:1px solid #21262d; padding-top:10px">
      <input id="newPat" placeholder="pt-…" style="flex:1">
      <button class="primary" onclick="addPat()">Add PAT</button>
    </div>
  </div>

  <div class="card" style="grid-column: 1 / -1;">
    <div class="card-title">API Endpoints</div>
    <div class="ep-row"><span class="method method-post">POST</span><span class="path">/v1/chat/completions</span><div class="desc">OpenAI-compatible chat (stream / non-stream, tool calling); PAT failover on agent limit</div></div>
    <div class="ep-row"><span class="method method-post">POST</span><span class="path">/v1/messages</span><div class="desc">Anthropic-compatible messages (stream / non-stream, tool use)</div></div>
    <div class="ep-row"><span class="method method-get">GET</span><span class="path">/v1/models</span><div class="desc">Live Qoder model list (with thinking/context configs)</div></div>
    <div class="ep-row"><span class="method method-get">GET</span><span class="path">/v1/usage</span><div class="desc">Raw user status (credits)</div></div>
  </div>

</div>

<script>
async function j(url, opts) {
  var res = await fetch(url, opts);
  return res.json();
}
function post(url, body) {
  return j(url, { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body || {}) });
}

var lastAccounts = null;

async function refreshStatus() {
  try {
    var d = await j('/api/status');
    document.getElementById('modeSub').textContent = 'OpenAI/Anthropic bridge · port ' + d.port;
    document.getElementById('endpointVal').textContent = location.origin + '/v1';
    document.getElementById('accCountVal').textContent = d.accounts;
    document.getElementById('activePatVal').textContent = d.active_pat;
    document.getElementById('userVal').textContent = d.active_user || '—';
    document.getElementById('defModelVal').textContent = d.default_model || '—';
    lastStatus = d;
    currentDefault = d.default_model || '';
    applySelects(false);
  } catch(e) {
    document.getElementById('proxyBadge').className = 'badge badge-red';
    document.getElementById('proxyBadge').textContent = 'offline';
  }
}

function setRuntime() {
  post('/api/settings', {
    model: currentDefault,
    context: document.getElementById('ctxSel').value,
    thinking: document.getElementById('thinkSel').value
  }).then(refreshStatus);
}

// fmtDate renders epoch seconds/millis or ISO string as YYYY-MM-DD.
function fmtDate(v) {
  if (v == null || v === '') return '—';
  var n = Number(v);
  if (!isNaN(n) && n > 0) {
    if (n < 1e11) n *= 1000;
    return new Date(n).toISOString().slice(0, 10);
  }
  return String(v).slice(0, 10);
}

var modelCfgById = {};
var currentDefault = '';
var lastStatus = null;
var lastSelectSig = '';

// ctxOptions/thinkOptions read live per-model configs from /v1/models.
function ctxOptions(cfg) {
  if (!cfg || typeof cfg !== 'object') return [];
  return Object.keys(cfg).map(function(k) {
    var v = cfg[k] || {};
    return { name: k, tokens: v.token_count || 0 };
  }).sort(function(a, b) { return a.tokens - b.tokens; });
}

function thinkOptions(cfg) {
  if (!cfg || !cfg.enabled) return [];
  var eff = (cfg.enabled || {}).efforts;
  if (!eff) return ['on'];
  return Object.keys(eff);
}

// applySelects rebuilds Context/Thinking options for the selected default model.
function applySelects(force) {
  var cfg = modelCfgById[currentDefault] || {};
  var ctx = (lastStatus && lastStatus.context) || '';
  var th = (lastStatus && lastStatus.thinking) || '';
  var sig = currentDefault + '|' + ctx + '|' + th + '|' + JSON.stringify(cfg);
  if (!force && sig === lastSelectSig) return;
  lastSelectSig = sig;

  var html = '<option value="">default</option>';
  ctxOptions(cfg.ctx).forEach(function(o) { html += '<option value="' + o.name + '">' + o.name + '</option>'; });
  var ctxSel = document.getElementById('ctxSel');
  ctxSel.innerHTML = html;
  ctxSel.value = ctx;

  var thHtml = '<option value="">model default</option><option value="off">off</option>';
  thinkOptions(cfg.think).forEach(function(l) { thHtml += '<option value="' + l + '">' + l + '</option>'; });
  var thSel = document.getElementById('thinkSel');
  thSel.innerHTML = thHtml;
  thSel.value = th;
}

function quotaParts(acc) {
  var data = (acc.quota || {}).data || {};
  var uq = data.userQuota || null;
  if (uq && (uq.total != null || uq.remaining != null)) {
    var used = (uq.used != null) ? uq.used : ((uq.total || 0) - (uq.remaining || 0));
    return { used: used, total: uq.total, expires: data.expiresAt };
  }
  return null;
}

function renderUsage() {
  if (!lastAccounts) return;
  var acc = (lastAccounts.accounts || []).filter(function(a){ return a.active; })[0];
  if (!acc) return;
  var plan = acc.plan || {};
  document.getElementById('planVal').textContent = plan.plan_tier_name || plan.user_type || '—';
  var q = quotaParts(acc);
  document.getElementById('creditsVal').textContent = q ? (q.used + '/' + q.total) : ((acc.quota && acc.quota.source) || 'not fetched');
  document.getElementById('expiresVal').textContent = q ? fmtDate(q.expires) : '—';
}

function promoLine(a) {
  var ct = a.claimText || {};
  var name = ct.en || ct.zh || a.activityId || 'activity';
  var fq = a.free_quota || null;
  var st;
  if (a.claimed) {
    st = (fq && fq.limit != null) ? (fq.used + '/' + fq.limit) : 'claimed';
  } else {
    st = a.canClaim ? 'available' : 'not claimed';
  }
  return { name: name, value: st };
}

async function loadPromo() {
  var el = document.getElementById('promoList');
  try {
    var d = await j('/api/promo');
    var acts = d.activities || [];
    if (!acts.length) { el.textContent = 'none'; return; }
    el.innerHTML = '';
    acts.forEach(function(a) {
      var line = promoLine(a);
      var div = document.createElement('div');
      div.className = 'row';
      div.innerHTML = '<span class="row-label">' + line.name + '</span><span class="row-value">' + line.value + '</span>';
      el.appendChild(div);
    });
  } catch(e) { el.textContent = '—'; }
}

async function refreshAccounts() {
  var d = await j('/api/accounts');
  lastAccounts = d;
  renderUsage();
  var el = document.getElementById('accountsList');
  el.innerHTML = '';
  (d.accounts || []).forEach(function(acc) {
    var div = document.createElement('div');
    div.className = 'account' + (acc.active ? ' active' : '');
    var badges = acc.active ? '<span class="badge badge-green">active</span>' : '<span class="badge badge-gray">standby</span>';
    if (acc.exhausted_until) badges += ' <span class="badge badge-red">limit until ' + acc.exhausted_until.slice(11,16) + '</span>';
    var q = quotaParts(acc);
    var quotaLine = q ? ('quota: ' + q.used + '/' + q.total) : ('quota: ' + (((acc.quota || {}).source) || 'not fetched'));
    var freeLines = freeQuotaLines(acc);
    div.innerHTML = '<div class="top"><div><b>' + acc.pat + '</b> <span class="muted">' + (acc.user || '') + '</span></div>' +
      '<div class="actions">' + badges +
      (acc.active ? '' : '<button onclick="selectAcc(' + acc.index + ')">Use</button>') +
      '<button onclick="removeAcc(' + acc.index + ')">Remove</button></div></div>' +
      '<div class="muted">' + quotaLine + '</div>' + freeLines;
    el.appendChild(div);
  });
}

// freeQuotaLines renders promo MODEL_FREE_QUOTA lines as "used/limit" (v3pro style).
function freeQuotaLines(acc) {
  var fq = acc.free_quotas || {};
  var html = '';
  Object.keys(fq).forEach(function(id) {
    var f = fq[id] || {};
    var name = cleanName(f.model_name || id);
    var rem = f.remaining, lim = f.limit, used = f.used;
    var txt = name + ': ';
    if (used != null && lim != null) txt += used + '/' + lim;
    else if (rem != null && lim != null) txt += rem + '/' + lim + ' left';
    html += '<div class="muted">' + txt + '</div>';
  });
  return html;
}

// cleanName drops non-ASCII (e.g. Chinese "免费额度") and trims.
function cleanName(s) {
  return s.replace(/[^\x20-\x7E]+/g, '').replace(/\s+/g, ' ').trim();
}

async function refreshModels() {
  var el = document.getElementById('modelsList');
  try {
    var st = await j('/api/status');
    var d = await j('/v1/models');
    el.innerHTML = '';
    (d.data || []).forEach(function(m) {
      modelCfgById[m.id] = { ctx: m.context_config, think: m.thinking_config };
      var div = document.createElement('div');
      div.className = 'model' + (m.id === st.default_model ? ' selected' : '');
      var label = m.display_name || m.name || m.id;
      div.innerHTML = '<span>' + label + '</span>' + (label !== m.id ? '<span class="muted">' + m.id + '</span>' : '');
      div.onclick = function() {
        currentDefault = m.id;
        post('/api/settings', { default_model: m.id }).then(function() { refreshModels(); refreshStatus(); });
        applySelects(true);
      };
      el.appendChild(div);
    });
    currentDefault = st.default_model || currentDefault;
    applySelects(false);
  } catch(e) { el.innerHTML = '<span class="loading">failed to load</span>'; }
}

function refreshQuota() { post('/api/quota/refresh').then(function(){ refreshAccounts(); loadPromo(); }); }
function addPat() {
  var v = document.getElementById('newPat').value.trim();
  if (!v) return;
  post('/api/accounts/add', { pat: v }).then(function(d) {
    if (d.error) { alert(d.error); return; }
    document.getElementById('newPat').value = '';
    refreshAccounts(); refreshStatus(); loadPromo();
  });
}
function removeAcc(i) { if (confirm('Remove this PAT?')) post('/api/accounts/remove', { index: i }).then(function(d){ if (d.error) alert(d.error); refreshAccounts(); refreshStatus(); loadPromo(); }); }
function selectAcc(i) { post('/api/accounts/select', { index: i }).then(function(d){ if (d.error) alert(d.error); refreshAccounts(); refreshStatus(); loadPromo(); }); }

refreshStatus(); refreshAccounts(); refreshModels(); loadPromo();
setInterval(refreshStatus, 5000);
</script>
</body>
</html>`
