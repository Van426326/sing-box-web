const state = {
  config: null,
  meta: null,
  activeTab: "overview",
  outboundQuery: "",
  ruleQuery: "",
  editor: null,
  dirty: false,
};

const el = (id) => document.getElementById(id);

document.addEventListener("DOMContentLoaded", () => {
  document.querySelectorAll(".tab").forEach((button) => {
    button.addEventListener("click", () => switchTab(button.dataset.tab));
  });
  el("reloadBtn").addEventListener("click", loadConfig);
  el("syncDaedBtn").addEventListener("click", syncDaed);
  el("saveBtn").addEventListener("click", saveConfig);
  el("closeDialogBtn").addEventListener("click", closeDialog);
  el("cancelDialogBtn").addEventListener("click", closeDialog);
  el("confirmDialogBtn").addEventListener("click", confirmDialog);
  loadConfig();
});

async function loadConfig() {
  showAlert("正在加载配置...", "");
  try {
    const response = await fetch("/api/config");
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || "加载失败");
    state.config = body.config;
    state.meta = body;
    setDirty(false);
    render();
    if (body.fallback) {
      showAlert(`无法读取 ${body.configPath}，当前展示示例配置：${body.source}\n${body.loadError || ""}`, "error");
    } else {
      hideAlert();
    }
  } catch (error) {
    showAlert(error.message, "error");
  }
}

async function saveConfig() {
  if (!state.config) return;
  const jsonText = el("fullJson")?.value;
  if (state.activeTab === "json" && jsonText) {
    try {
      state.config = JSON.parse(jsonText);
    } catch (error) {
      showAlert(`JSON 解析失败：${error.message}`, "error");
      return;
    }
  }
  showAlert("正在运行 sing-box check...", "");
  try {
    const response = await fetch("/api/config/save", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({config: state.config}),
    });
    const body = await response.json();
    if (!response.ok) {
      const detail = body.result ? formatCommandResult(body.result) : "";
      throw new Error(`${body.error || "保存失败"}\n${detail}`);
    }
    showAlert(`保存成功\n备份：${body.backupPath || "无"}\n${formatCommandResult(body)}`, "ok");
    await loadConfig();
  } catch (error) {
    showAlert(error.message, "error");
  }
}

async function syncDaed() {
  if (state.dirty) {
    showAlert("当前配置有未保存修改，请先点击“检查并保存”后再同步到 Daed。", "error");
    return;
  }
  showAlert("正在同步到 Daed...", "");
  try {
    const response = await fetch("/api/daed/sync-route-rules", {method: "POST"});
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.error || "同步到 Daed 失败");
    }
    const added = Array.isArray(body.added) && body.added.length > 0 ? `\n新增 IP:\n${body.added.join("\n")}` : "";
    const routing = body.routingName ? `\nRouting: ${body.routingName}` : "";
    showAlert(`${body.message || "同步完成"}${routing}${added}`, body.changed ? "ok" : "");
  } catch (error) {
    showAlert(error.message, "error");
  }
}

function render() {
  renderOverview();
  renderOutbounds();
  renderRules();
  renderJson();
}

function switchTab(tab) {
  state.activeTab = tab;
  document.querySelectorAll(".tab").forEach((button) => button.classList.toggle("active", button.dataset.tab === tab));
  document.querySelectorAll(".panel").forEach((panel) => panel.classList.toggle("active", panel.id === tab));
  if (tab === "json") renderJson();
}

function renderOverview() {
  const meta = state.meta || {};
  el("overview").innerHTML = `
    <div class="summary-grid">
      ${metric("配置路径", meta.configPath || "")}
      ${metric("当前来源", meta.source || "")}
      ${metric("加载时间", meta.loadedAt ? new Date(meta.loadedAt).toLocaleString() : "")}
      ${metric("Outbounds", String(meta.outboundCount ?? outboundList().length))}
      ${metric("Route Rules", String(meta.routeRuleCount ?? ruleList().length))}
      ${metric("route.final", meta.routeFinal || valueAt(state.config, ["route", "final"]) || "")}
    </div>
  `;
}

function renderOutbounds() {
  const rows = outboundList()
    .map((item, index) => ({item, index}))
    .filter(({item}) => JSON.stringify(item).toLowerCase().includes(state.outboundQuery.toLowerCase()));
  if (!el("outboundRows")) {
    el("outbounds").innerHTML = `
      <div class="toolbar">
        <input class="search" id="outboundSearch" placeholder="搜索 tag、server、type" />
        <button type="button" id="addOutboundBtn" class="primary">新增 outbound</button>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>#</th><th>tag</th><th>type</th><th>server</th><th>port</th><th>network</th><th>操作</th></tr></thead>
          <tbody id="outboundRows"></tbody>
        </table>
      </div>
    `;
    el("outboundSearch").addEventListener("input", (event) => {
      state.outboundQuery = event.target.value;
      renderOutbounds();
    });
    el("addOutboundBtn").addEventListener("click", () => openObjectEditor("新增 outbound", {type: "socks", tag: "", server: "", server_port: 1080, version: "5", network: "tcp"}, (value) => {
      ensureArray(["outbounds"]).push(value);
      markDirty();
      render();
    }));
  }
  if (el("outboundSearch") !== document.activeElement) {
    el("outboundSearch").value = state.outboundQuery;
  }
  el("outboundRows").innerHTML = rows.map(({item, index}) => `
            <tr>
              <td>${index + 1}</td>
              <td>${escapeHtml(item.tag || "")}</td>
              <td>${escapeHtml(item.type || "")}</td>
              <td>${escapeHtml(item.server || "")}</td>
              <td>${escapeHtml(item.server_port ?? "")}</td>
              <td>${escapeHtml(item.network || "")}</td>
              <td><div class="row-actions">
                <button type="button" data-action="edit-outbound" data-index="${index}">编辑</button>
                <button type="button" data-action="dup-outbound" data-index="${index}">复制</button>
                <button type="button" class="danger" data-action="del-outbound" data-index="${index}">删除</button>
              </div></td>
            </tr>
          `).join("");
  bindTableActions(el("outboundRows"));
}

function renderRules() {
  const rows = ruleList()
    .map((item, index) => ({item, index}))
    .filter(({item}) => JSON.stringify(item).toLowerCase().includes(state.ruleQuery.toLowerCase()));
  if (!el("ruleRows")) {
    el("rules").innerHTML = `
      <div class="toolbar">
        <input class="search" id="ruleSearch" placeholder="搜索 action、outbound、ip_cidr、domain" />
        <button type="button" id="addRuleBtn" class="primary">新增 rule</button>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>#</th><th>action</th><th>outbound</th><th>protocol</th><th>match</th><th>操作</th></tr></thead>
          <tbody id="ruleRows"></tbody>
        </table>
      </div>
    `;
    el("ruleSearch").addEventListener("input", (event) => {
      state.ruleQuery = event.target.value;
      renderRules();
    });
    el("addRuleBtn").addEventListener("click", () => openObjectEditor("新增 rule", {ip_cidr: [], outbound: ""}, (value) => {
      ensureArray(["route", "rules"]).push(value);
      markDirty();
      render();
    }));
  }
  if (el("ruleSearch") !== document.activeElement) {
    el("ruleSearch").value = state.ruleQuery;
  }
  el("ruleRows").innerHTML = rows.map(({item, index}) => `
            <tr>
              <td>${index + 1}</td>
              <td>${escapeHtml(item.action || "")}</td>
              <td>${escapeHtml(item.outbound || "")}</td>
              <td>${escapeHtml(item.protocol || "")}</td>
              <td>${escapeHtml(matchSummary(item))}</td>
              <td><div class="row-actions">
                <button type="button" data-action="up-rule" data-index="${index}">上移</button>
                <button type="button" data-action="down-rule" data-index="${index}">下移</button>
                <button type="button" data-action="edit-rule" data-index="${index}">编辑</button>
                <button type="button" data-action="dup-rule" data-index="${index}">复制</button>
                <button type="button" class="danger" data-action="del-rule" data-index="${index}">删除</button>
              </div></td>
            </tr>
          `).join("");
  bindTableActions(el("ruleRows"));
}

function renderJson() {
  el("json").innerHTML = `
    <textarea id="fullJson" class="json-editor" spellcheck="false">${escapeHtml(JSON.stringify(state.config, null, 2))}</textarea>
  `;
  el("fullJson").addEventListener("change", (event) => {
    try {
      state.config = JSON.parse(event.target.value);
      markDirty();
      renderOverview();
      renderOutbounds();
      renderRules();
      hideAlert();
    } catch (error) {
      showAlert(`JSON 解析失败：${error.message}`, "error");
    }
  });
}

function bindTableActions(root = document) {
  root.querySelectorAll("[data-action]").forEach((button) => {
    button.addEventListener("click", () => {
      const index = Number(button.dataset.index);
      const action = button.dataset.action;
      if (action.endsWith("outbound")) handleOutboundAction(action, index);
      if (action.endsWith("rule")) handleRuleAction(action, index);
    });
  });
}

function handleOutboundAction(action, index) {
  const list = outboundList();
  if (action === "edit-outbound") {
    openObjectEditor("编辑 outbound", list[index], (value) => {
      list[index] = value;
      markDirty();
      render();
    });
  }
  if (action === "dup-outbound") {
    list.splice(index + 1, 0, structuredClone(list[index]));
    markDirty();
    render();
  }
  if (action === "del-outbound" && confirm("删除这个 outbound？")) {
    list.splice(index, 1);
    markDirty();
    render();
  }
}

function handleRuleAction(action, index) {
  const list = ruleList();
  if (action === "edit-rule") {
    openObjectEditor("编辑 rule", list[index], (value) => {
      list[index] = value;
      markDirty();
      render();
    });
  }
  if (action === "dup-rule") {
    list.splice(index + 1, 0, structuredClone(list[index]));
    markDirty();
    render();
  }
  if (action === "del-rule" && confirm("删除这个 rule？")) {
    list.splice(index, 1);
    markDirty();
    render();
  }
  if (action === "up-rule" && index > 0) {
    [list[index - 1], list[index]] = [list[index], list[index - 1]];
    markDirty();
    render();
  }
  if (action === "down-rule" && index < list.length - 1) {
    [list[index + 1], list[index]] = [list[index], list[index + 1]];
    markDirty();
    render();
  }
}

function openObjectEditor(title, value, onSave) {
  state.editor = {onSave};
  el("dialogTitle").textContent = title;
  el("dialogBody").innerHTML = `<textarea id="objectJson" class="object-editor" spellcheck="false">${escapeHtml(JSON.stringify(value, null, 2))}</textarea>`;
  el("editorDialog").showModal();
}

function confirmDialog() {
  try {
    const value = JSON.parse(el("objectJson").value);
    state.editor.onSave(value);
    closeDialog();
  } catch (error) {
    showAlert(`对象 JSON 解析失败：${error.message}`, "error");
  }
}

function closeDialog() {
  el("editorDialog").close();
  state.editor = null;
}

function outboundList() {
  return Array.isArray(state.config?.outbounds) ? state.config.outbounds : [];
}

function ruleList() {
  return Array.isArray(state.config?.route?.rules) ? state.config.route.rules : [];
}

function ensureArray(path) {
  let cursor = state.config;
  for (let i = 0; i < path.length - 1; i += 1) {
    const key = path[i];
    if (!cursor[key] || typeof cursor[key] !== "object") cursor[key] = {};
    cursor = cursor[key];
  }
  const last = path[path.length - 1];
  if (!Array.isArray(cursor[last])) cursor[last] = [];
  return cursor[last];
}

function valueAt(root, path) {
  return path.reduce((cursor, key) => cursor && cursor[key], root);
}

function matchSummary(item) {
  for (const key of ["ip_cidr", "domain", "domain_suffix", "domain_keyword", "rule_set"]) {
    if (item[key]) return `${key}: ${Array.isArray(item[key]) ? item[key].join(", ") : item[key]}`;
  }
  return JSON.stringify(item);
}

function metric(label, value) {
  return `<div class="metric"><span>${escapeHtml(label)}</span><strong>${escapeHtml(value)}</strong></div>`;
}

function showAlert(message, kind) {
  const alert = el("alert");
  alert.textContent = message;
  alert.className = `alert ${kind || ""}`;
}

function hideAlert() {
  el("alert").className = "alert hidden";
}

function markDirty() {
  setDirty(true);
}

function setDirty(value) {
  state.dirty = value;
  document.body.classList.toggle("is-dirty", value);
}

function formatCommandResult(result) {
  const parts = [];
  if (result.check) parts.push(`check stdout:\n${result.check.stdout || ""}\ncheck stderr:\n${result.check.stderr || ""}`);
  if (result.reload) parts.push(`reload stdout:\n${result.reload.stdout || ""}\nreload stderr:\n${result.reload.stderr || ""}`);
  return parts.join("\n");
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
