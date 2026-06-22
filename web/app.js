"use strict";

let REPOS = [];
let SELECTED = new Set();
let FILTER = "all";
let QUERY = "";
let CONFIG = { collections: [], active: "" };
let DRAWER = null;          // { name, view:'commit'|'pull', info, files, ... }
let REVIEW = null;          // { mode:'commit'|'pull', queue:[], index }

const $ = (s) => document.querySelector(s);
const el = (tag, cls, html) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (html != null) e.innerHTML = html;
  return e;
};
const enc = encodeURIComponent;

// ---------- API ----------
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  const text = await res.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch { data = { raw: text }; }
  if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
  return data;
}

// ---------- Load ----------
async function load() {
  setRefreshing(true);
  try {
    const data = await api("GET", "/api/repos");
    REPOS = data.repos || [];
    SELECTED = new Set([...SELECTED].filter((n) => REPOS.some((r) => r.name === n)));
    render();
  } catch (e) {
    toast("err", "Failed to load", e.message);
  } finally {
    setRefreshing(false);
  }
}

async function loadCollections() {
  try {
    const data = await api("GET", "/api/config");
    CONFIG = { collections: data.collections || [], active: data.active || "" };
    renderCollection();
  } catch { /* ignore */ }
}

function setRefreshing(on) {
  const b = $("#refreshBtn");
  b.disabled = on;
  b.querySelector(".ic").textContent = on ? "…" : "⟳";
}

// ---------- Collections ----------
function renderCollection() {
  const active = CONFIG.collections.find((c) => c.path === CONFIG.active);
  $("#collectionName").textContent = active ? active.name : "(no folder)";

  const menu = $("#collectionMenu");
  menu.innerHTML = "";
  if (!CONFIG.collections.length) {
    menu.appendChild(el("div", "collection-empty", "No folders tracked yet."));
  }
  for (const c of CONFIG.collections) {
    const isActive = c.path === CONFIG.active;
    const row = el("div", "collection-row" + (isActive ? " active" : ""));
    const pick = el("button", "collection-pick");
    pick.innerHTML =
      `<span class="cr-dot">${isActive ? "●" : ""}</span>` +
      `<span class="cr-body">` +
      `<span class="cr-top"><span class="cr-name">${esc(c.name)}</span>` +
      `<span class="cr-meta ${c.exists ? "" : "missing"}">${c.exists ? c.repoCount + " repos" : "⚠ missing"}</span></span>` +
      `<span class="cr-path">${esc(c.path)}</span></span>`;
    pick.onclick = (e) => { e.stopPropagation(); switchCollection(c.path); };
    row.appendChild(pick);
    const rm = el("button", "cr-remove", "✕");
    rm.title = "Remove from list (the folder itself is not deleted)";
    rm.onclick = (e) => { e.stopPropagation(); removeCollection(c.path); };
    row.appendChild(rm);
    menu.appendChild(row);
  }
  const add = el("button", "collection-add", "＋ Add a folder…");
  add.onclick = (e) => { e.stopPropagation(); addCollection(); };
  menu.appendChild(add);
}

async function switchCollection(path) {
  $("#collectionMenu").hidden = true;
  if (path === CONFIG.active) return;
  try {
    const data = await api("POST", "/api/collections", { action: "switch", path });
    CONFIG = { collections: data.collections, active: data.active };
    SELECTED.clear();
    renderCollection();
    await load();
  } catch (e) { toast("err", "Switch failed", e.message); }
}

async function addCollection() {
  $("#collectionMenu").hidden = true;
  let path;
  try {
    const res = await api("POST", "/api/pick-folder", {});
    if (res.error) {
      // Non-macOS fallback: ask for a path with a styled dialog.
      path = await promptDialog("Add a collection", {
        message: "Paste the full path to a folder that holds your project repositories.",
        label: "Folder path", placeholder: "/path/to/Projects", okLabel: "Add folder",
      });
    } else {
      path = res.path; // empty if the user cancelled the Finder dialog
    }
  } catch (e) { toast("err", "Folder picker failed", e.message); return; }
  if (!path) return;
  try {
    const data = await api("POST", "/api/collections", { action: "add", path });
    CONFIG = { collections: data.collections, active: data.active };
    SELECTED.clear();
    renderCollection();
    await load();
  } catch (e) { toast("err", "Could not add folder", e.message); }
}

async function removeCollection(path) {
  $("#collectionMenu").hidden = true;
  if (!(await confirmDialog("Remove collection",
    "Remove this folder from ChitHub's list?\nThe folder and its repos are NOT deleted.",
    { okLabel: "Remove", danger: true }))) return;
  try {
    const data = await api("POST", "/api/collections", { action: "remove", path });
    CONFIG = { collections: data.collections, active: data.active };
    renderCollection();
    await load();
  } catch (e) { toast("err", "Remove failed", e.message); }
}

// ---------- Repo list ----------
function passesFilter(r) {
  if (QUERY && !r.name.toLowerCase().includes(QUERY)) return false;
  switch (FILTER) {
    case "behind": return r.behind > 0;
    case "ahead": return r.ahead > 0;
    case "dirty": return r.dirty;
    case "attention":
      return r.dirty || r.behind > 0 || r.ahead > 0 ||
        r.state === "diverged" || r.state === "no-upstream" ||
        r.state === "detached" || !!r.error;
    default: return true;
  }
}

function render() {
  renderStats();
  const list = $("#repoList");
  list.innerHTML = "";
  const visible = REPOS.filter(passesFilter);
  $("#emptyState").hidden = REPOS.length !== 0;
  list.hidden = REPOS.length === 0;
  for (const r of visible) list.appendChild(renderRepo(r));

  const allVisibleSel = visible.length > 0 && visible.every((r) => SELECTED.has(r.name));
  $("#selectAll").checked = allVisibleSel;
  const n = SELECTED.size;
  $("#selectCount").textContent = n ? `${n} selected` : t("Select all");
  renderBulkBar();
}

function renderStats() {
  const total = REPOS.length;
  const behind = REPOS.filter((r) => r.behind > 0).length;
  const ahead = REPOS.filter((r) => r.ahead > 0).length;
  const dirty = REPOS.filter((r) => r.dirty).length;
  $("#stats").innerHTML = `
    <div class="stat"><div class="num">${total}</div><div class="lbl">repos</div></div>
    <div class="stat behind"><div class="num">${behind}</div><div class="lbl">behind</div></div>
    <div class="stat ahead"><div class="num">${ahead}</div><div class="lbl">ahead</div></div>
    <div class="stat dirty"><div class="num">${dirty}</div><div class="lbl">dirty</div></div>`;
}

function renderRepo(r) {
  const row = el("div", "repo" + (SELECTED.has(r.name) ? " sel" : ""));
  row.dataset.name = r.name;

  const pick = el("label", "pick");
  const cb = el("input");
  cb.type = "checkbox";
  cb.checked = SELECTED.has(r.name);
  cb.onchange = () => toggleSelect(r.name, cb.checked);
  pick.appendChild(cb);

  const main = el("div", "repo-main");
  const title = el("div", "repo-title");
  const nameBtn = el("span", "repo-name", esc(r.name));
  nameBtn.style.cursor = "pointer";
  nameBtn.onclick = () => openDrawer(r.name, { commit: true });
  title.appendChild(nameBtn);
  if (!r.detached && r.branch) title.appendChild(el("span", "branch", esc(r.branch)));
  title.appendChild(badges(r));
  main.appendChild(title);

  const meta = el("div", "repo-meta");
  if (r.error) {
    meta.appendChild(el("span", "", "⚠ " + esc(r.error.split("\n")[0])));
  } else {
    if (r.lastCommit) {
      meta.appendChild(el("span", "commit-subj", `“${esc(r.lastCommit.subject)}”`));
      meta.appendChild(el("span", "", relTime(r.lastCommit.time)));
    }
    meta.appendChild(el("span", "", "fetched " + (r.lastFetch ? relTime(r.lastFetch) : "never")));
  }
  main.appendChild(meta);

  const actions = el("div", "repo-actions");
  const sync = actBtn(t("sync"), "ghost", () => doSync(r.name));
  sync.title = "Fetch, then pull and push in one step";
  actions.appendChild(sync);
  actions.appendChild(splitButton(t("pull"), "primary", () => doPull(r.name, "ff"), [
    { label: "Pull (fast-forward only)", fn: () => doPull(r.name, "ff") },
    { label: "Pull --rebase", fn: () => doPull(r.name, "rebase") },
    { label: "Pull (merge)", fn: () => doPull(r.name, "merge") },
  ]));
  if (r.state === "no-upstream") {
    actions.appendChild(actBtn(t("publish"), "", () => doPublish(r.name)));
  } else {
    actions.appendChild(splitButton(t("push"), "", () => doPush(r.name, false), [
      { label: "Push", fn: () => doPush(r.name, false) },
      { label: "Force push (--force-with-lease)", fn: () => doPush(r.name, true), danger: true },
    ]));
  }
  if (r.dirty) {
    const c = actBtn(t("commit") + "…", "ghost", () => openDrawer(r.name, { commit: true }));
    c.style.color = "var(--orange)";
    actions.appendChild(c);
  }
  const more = actBtn("⋯", "ghost more", null);
  actions.appendChild(attachMenu(more, [
    { label: "Open details", fn: () => openDrawer(r.name, { commit: true }) },
    { label: "History…", fn: () => openHistory(r.name) },
    { label: "New branch…", fn: () => newBranch(r.name) },
    { label: "—", sep: true },
    { label: "Open on web", fn: () => doOpen(r.name, "web") },
    { label: "Open in editor", fn: () => doOpen(r.name, "editor") },
    { label: "Open in terminal", fn: () => doOpen(r.name, "terminal") },
    { label: "Reveal in Finder", fn: () => doReveal(r.name) },
    ...(r.dirty ? [
      { label: "—", sep: true },
      { label: "Stash changes", fn: () => doStash(r.name, "push") },
      { label: "Discard all changes…", fn: () => doDiscardAll(r.name), danger: true },
    ] : []),
  ]));

  row.appendChild(pick);
  row.appendChild(main);
  row.appendChild(actions);
  return row;
}

function badges(r) {
  const wrap = el("div", "badges");
  if (r.error) { wrap.appendChild(badge("b-error", "⚠ error")); return wrap; }
  switch (r.state) {
    case "synced": wrap.appendChild(badge("b-synced", "✓ up to date")); break;
    case "ahead": wrap.appendChild(badge("b-ahead", `↑ ${r.ahead} to push`)); break;
    case "behind": wrap.appendChild(badge("b-behind", `↓ ${r.behind} to pull`)); break;
    case "diverged": wrap.appendChild(badge("b-diverged", `⇅ diverged ↑${r.ahead} ↓${r.behind}`)); break;
    case "no-upstream": wrap.appendChild(badge("b-noupstream", "no upstream")); break;
    case "detached": wrap.appendChild(badge("b-detached", "detached HEAD")); break;
  }
  if (r.dirty) {
    const n = r.staged + r.unstaged + r.untracked + r.conflicts;
    let label = `● ${n} change${n === 1 ? "" : "s"}`;
    if (r.conflicts) label += ` · ${r.conflicts} conflict`;
    wrap.appendChild(badge("b-dirty", label));
  }
  return wrap;
}

const badge = (cls, txt) => el("span", "badge " + cls, esc(txt));
const actBtn = (label, extra, fn) => {
  const b = el("button", "btn small " + (extra || ""), esc(label));
  if (fn) b.onclick = fn;
  return b;
};

function splitButton(label, kind, mainFn, items) {
  const wrap = el("div", "split");
  const main = el("button", "btn small " + kind, esc(label));
  main.onclick = mainFn;
  const caret = el("button", "btn small caret " + kind, "▾");
  const menu = el("div", "menu");
  menu.hidden = true;
  for (const it of items) {
    const b = el("button", it.danger ? "danger-item" : "", esc(it.label));
    b.onclick = (e) => { e.stopPropagation(); menu.hidden = true; it.fn(); };
    menu.appendChild(b);
  }
  caret.onclick = (e) => { e.stopPropagation(); closeAllMenus(); menu.hidden = !menu.hidden; };
  wrap.appendChild(main);
  wrap.appendChild(caret);
  wrap.appendChild(menu);
  return wrap;
}

// attachMenu wraps a button + its dropdown menu and returns the wrapper, which
// the caller must insert into the DOM.
function attachMenu(btn, items) {
  const wrap = el("span", "split");
  const menu = el("div", "menu");
  menu.hidden = true;
  for (const it of items) {
    if (it.sep) { menu.appendChild(el("div", "menu-sep")); continue; }
    const b = el("button", it.danger ? "danger-item" : "", esc(it.label));
    b.onclick = (e) => { e.stopPropagation(); menu.hidden = true; it.fn(); };
    menu.appendChild(b);
  }
  btn.onclick = (e) => { e.stopPropagation(); closeAllMenus(); menu.hidden = !menu.hidden; };
  wrap.appendChild(btn);
  wrap.appendChild(menu);
  return wrap;
}

function closeAllMenus() {
  document.querySelectorAll(".menu").forEach((m) => (m.hidden = true));
}

// ---------- Single-repo operations ----------
function setBusy(name, on) {
  const row = document.querySelector(`.repo[data-name="${cssEsc(name)}"]`);
  if (row) row.classList.toggle("busy", on);
}

async function runOp(name, label, fn) {
  const t = toast("run", label, "running…");
  setBusy(name, true);
  try {
    const res = await fn();
    finishToast(t, res);
  } catch (e) {
    finishToast(t, { ok: false, output: e.message });
  } finally {
    setBusy(name, false);
    await refreshOne(name);
    if (DRAWER && DRAWER.name === name) await reloadDrawer();
  }
}

const doOp = (name, action) =>
  runOp(name, `${action} ${name}`, () => api("POST", `/api/repo/${enc(name)}/${action}`, {}));
const doPull = (name, mode) =>
  runOp(name, `pull ${name}`, () => api("POST", `/api/repo/${enc(name)}/pull`, { mode }));
async function doPush(name, force) {
  if (force && !(await confirmDialog("Force push",
    `Force-push ${name}? This rewrites remote history (uses --force-with-lease).`,
    { okLabel: "Force push", danger: true }))) return;
  const r = REPOS.find((x) => x.name === name);
  const branch = (r && r.branch) || (DRAWER && DRAWER.name === name && DRAWER.branches && DRAWER.branches.current) || "";
  if (SETTINGS.warnMainPush && /^(main|master|trunk|production|release)$/.test(branch) &&
    !(await confirmDialog("Push to " + branch,
      `You're about to push directly to the protected branch “${branch}”. Continue?`,
      { okLabel: "Push to " + branch }))) return;
  return runOp(name, `push ${name}`, () => api("POST", `/api/repo/${enc(name)}/push`, { force }));
}
const doStash = (name, action) =>
  runOp(name, `stash ${action} · ${name}`, () => api("POST", `/api/repo/${enc(name)}/stash`, { action }));

async function doReveal(name) {
  try { await api("POST", `/api/repo/${enc(name)}/reveal`, {}); }
  catch (e) { toast("err", "Reveal failed", e.message); }
}
async function doDiscardAll(name) {
  const toStash = SETTINGS.discardToStash;
  const note = toStash ? "Move ALL local changes in {n} to a stash (recoverable)?" : "Discard ALL local changes in {n}?\nThis can't be undone.";
  if (!(await confirmDialog(toStash ? "Stash all changes" : "Discard all changes",
    note.replace("{n}", name), { okLabel: toStash ? "Stash all" : "Discard all", danger: !toStash }))) return;
  return runOp(name, `discard all · ${name}`, () => api("POST", `/api/repo/${enc(name)}/discard`, { toStash }));
}
async function doUndo(name) {
  if (!(await confirmDialog("Undo last commit",
    `Undo the last commit in ${name}?\nIts changes return to your working tree (soft reset).`,
    { okLabel: "Undo commit" }))) return;
  return runOp(name, `undo · ${name}`, () => api("POST", `/api/repo/${enc(name)}/undo`, {}));
}
async function newBranch(name) {
  const branch = await promptDialog(`New branch in ${name}`,
    { label: "Branch name", placeholder: "feature/my-thing", okLabel: "Create branch" });
  if (!branch) return;
  await runOp(name, `branch ${name}`,
    () => api("POST", `/api/repo/${enc(name)}/checkout`, { branch, create: true }));
  if (DRAWER && DRAWER.name === name) reloadDrawer();
}

async function refreshOne(name) {
  try {
    const data = await api("GET", `/api/repo/${enc(name)}`);
    const idx = REPOS.findIndex((r) => r.name === name);
    if (idx >= 0 && data.info) {
      REPOS[idx] = data.info;
      const old = document.querySelector(`.repo[data-name="${cssEsc(name)}"]`);
      if (old) old.replaceWith(renderRepo(data.info));
      renderStats();
    }
  } catch { /* ignore */ }
}

// ---------- Batch ----------
function selectedOrAll() {
  if (SELECTED.size) return [...SELECTED];
  return REPOS.filter(passesFilter).map((r) => r.name);
}
async function batch(action, extra, quiet) {
  const repos = selectedOrAll();
  if (!repos.length) { if (!quiet) toast("err", "Nothing to do", "No repositories selected."); return; }
  const label = SELECTED.size ? `${repos.length} selected` : "all visible";
  const tn = quiet ? null : toast("run", `${action} → ${label}`, `running on ${repos.length} repos…`);
  repos.forEach((n) => setBusy(n, true));
  try {
    const data = await api("POST", "/api/batch", { action, repos, ...extra });
    const results = data.results || [];
    const ok = results.filter((r) => r.ok).length;
    const fail = results.length - ok;
    const lines = results.filter((r) => !r.ok)
      .map((r) => `✗ ${r.repo}: ${firstLine(r.output)}`).join("\n");
    if (tn) finishToast(tn, { ok: fail === 0, output: `${ok} ok, ${fail} failed${lines ? "\n\n" + lines : ""}` });
  } catch (e) {
    if (tn) finishToast(tn, { ok: false, output: e.message });
  } finally {
    repos.forEach((n) => setBusy(n, false));
    await load();
  }
}

// ---------- Selection ----------
function toggleSelect(name, on) {
  if (on) SELECTED.add(name); else SELECTED.delete(name);
  const row = document.querySelector(`.repo[data-name="${cssEsc(name)}"]`);
  if (row) row.classList.toggle("sel", on);
  const n = SELECTED.size;
  $("#selectCount").textContent = n ? `${n} selected` : t("Select all");
  const visible = REPOS.filter(passesFilter);
  $("#selectAll").checked = visible.length > 0 && visible.every((r) => SELECTED.has(r.name));
  renderBulkBar();
}

// ---------- Review wizard ----------
async function startReview(mode) {
  closeAllMenus();
  let data;
  try { data = await api("GET", "/api/review"); }
  catch (e) { toast("err", "Review failed", e.message); return; }
  const queue = mode === "pull" ? (data.pull || []) : (data.commit || []);
  if (!queue.length) {
    toast("ok", "Nothing to review",
      mode === "pull" ? "No repository is behind its upstream." : "No repository needs a commit or push.");
    return;
  }
  REVIEW = { mode, queue, index: 0 };
  reviewShow();
}
function reviewShow() {
  openDrawer(REVIEW.queue[REVIEW.index], REVIEW.mode === "pull" ? { pull: true } : { commit: true });
}
function reviewAdvance() {
  if (!REVIEW) return;
  if (REVIEW.index >= REVIEW.queue.length - 1) { reviewFinish(); return; }
  REVIEW.index++;
  reviewShow();
}
function reviewPrev() {
  if (!REVIEW || REVIEW.index === 0) return;
  REVIEW.index--;
  reviewShow();
}
function reviewFinish() {
  REVIEW = null;
  closeDrawer();
  load();
  toast("ok", "Review complete", "You stepped through every repo. 🎉");
}
function reviewExit() { REVIEW = null; closeDrawer(); }

function reviewBar() {
  const bar = el("div", "review-bar");
  const left = el("div", "rb-left");
  left.appendChild(el("span", "rb-tag", REVIEW.mode === "pull" ? "PULL REVIEW" : "COMMIT REVIEW"));
  left.appendChild(el("span", "rb-prog", `${REVIEW.index + 1} / ${REVIEW.queue.length}`));
  bar.appendChild(left);
  const nav = el("div", "rb-nav");
  const prev = el("button", "btn small ghost", "← Prev"); prev.disabled = REVIEW.index === 0; prev.onclick = reviewPrev;
  const next = el("button", "btn small ghost", REVIEW.index >= REVIEW.queue.length - 1 ? "Finish ✓" : "Skip →");
  next.onclick = reviewAdvance;
  const exit = el("button", "btn small ghost", "Exit"); exit.onclick = reviewExit;
  nav.appendChild(prev); nav.appendChild(next); nav.appendChild(exit);
  bar.appendChild(nav);
  return bar;
}

// ---------- Drawer ----------
async function openDrawer(name, opts) {
  opts = opts || {};
  $("#drawer").hidden = false;
  $("#drawerPanel").innerHTML = `<div class="sub">Loading ${esc(name)}…</div>`;
  await reloadDrawer(name, opts.pull ? "pull" : "commit");
  if (opts.commit) { const ta = $("#drawerPanel textarea"); if (ta) ta.focus(); }
}

async function reloadDrawer(name, view) {
  name = name || (DRAWER && DRAWER.name);
  view = view || (DRAWER && DRAWER.view) || "commit";
  if (!name) return;
  const prevMsg = DRAWER && DRAWER.name === name ? DRAWER.draftMsg : "";
  try {
    const data = await api("GET", `/api/repo/${enc(name)}`);
    DRAWER = {
      name,
      view,
      info: data.info,
      log: data.log || [],
      incoming: data.incoming || [],
      branches: data.branches || { local: [], current: data.info.branch },
      stashes: data.stashes || [],
      draftMsg: prevMsg || "",
      conflicts: null,
      files: (data.files || []).map((f) => ({
        path: f.path, code: f.code, untracked: f.code.includes("?"),
        sel: "all", expanded: false, loading: false,
      })),
    };
    renderDrawer();
    if (data.info && data.info.conflicts > 0) {
      try {
        const cs = await api("GET", `/api/repo/${enc(name)}/conflicts`);
        if (DRAWER && DRAWER.name === name) { DRAWER.conflicts = cs; renderDrawer(); }
      } catch { /* ignore */ }
    }
  } catch (e) {
    $("#drawerPanel").innerHTML = `<div class="sub">Error: ${esc(e.message)}</div>`;
  }
}

function renderDrawer() {
  const panel = $("#drawerPanel");
  const scroll = panel ? panel.scrollTop : 0;
  if (DRAWER.view === "pull") renderPullReview(); else renderStaging();
  if (panel) panel.scrollTop = scroll;
}

function drawerHead() {
  const d = DRAWER, r = d.info;
  const frag = document.createDocumentFragment();
  if (REVIEW) frag.appendChild(reviewBar());
  const close = el("button", "close", "×"); close.onclick = closeDrawer;
  frag.appendChild(close);
  frag.appendChild(el("h2", null, esc(d.name)));
  frag.appendChild(el("div", "sub",
    (r.upstream ? `→ ${esc(r.upstream)}` : "no upstream") + (r.remote ? `<br>${esc(r.remote)}` : "")));
  frag.appendChild(badges(r));
  return frag;
}

function branchBar() {
  const d = DRAWER;
  const bar = el("div", "branchbar");
  bar.appendChild(el("span", "bb-label", "⑂"));

  // styled branch dropdown
  const split = el("div", "split branch-switch");
  const btn = el("button", "btn small branch-current");
  btn.innerHTML = `<span class="mono">${esc(d.branches.current || "?")}</span> <span class="caret-dim">▾</span>`;
  const menu = el("div", "menu branch-menu");
  menu.hidden = true;
  for (const b of d.branches.local || []) {
    const mb = el("button", b === d.branches.current ? "bm-active" : "");
    mb.innerHTML = `<span class="bm-dot">${b === d.branches.current ? "●" : ""}</span><span class="mono">${esc(b)}</span>`;
    mb.onclick = (e) => { e.stopPropagation(); menu.hidden = true; switchBranch(b); };
    menu.appendChild(mb);
  }
  const remotes = (d.branches.remote || []).filter((r) => {
    const local = r.replace(/^[^/]+\//, "");
    return !(d.branches.local || []).includes(local);
  });
  if (remotes.length) {
    menu.appendChild(el("div", "bm-divider", "remote branches"));
    for (const r of remotes) {
      const local = r.replace(/^[^/]+\//, "");
      const mb = el("button", null,
        `<span class="bm-dot"></span><span class="mono">${esc(r)}</span><span class="bm-track">+ track</span>`);
      mb.onclick = (e) => { e.stopPropagation(); menu.hidden = true; checkoutRemote(local, r); };
      menu.appendChild(mb);
    }
  }
  btn.onclick = (e) => { e.stopPropagation(); closeAllMenus(); menu.hidden = !menu.hidden; };
  split.appendChild(btn);
  split.appendChild(menu);
  bar.appendChild(split);

  bar.appendChild(barBtn("＋ Branch", () => newBranch(d.name)));
  bar.appendChild(barBtn("Stash", () => doStashDrawer("push")));
  if (d.stashes.length) bar.appendChild(barBtn(`Pop (${d.stashes.length})`, () => doStashDrawer("pop")));
  const more = el("button", "btn small ghost", "Branch ⋯");
  bar.appendChild(attachMenu(more, branchMenuItems(d)));
  return bar;
}

// switchBranch mirrors GitHub Desktop: if uncommitted changes would block the
// switch, it offers to stash them (and pop later), instead of just erroring.
async function switchBranch(target) {
  const d = DRAWER;
  if (!d || target === d.branches.current) return;
  const t = toast("run", `switch → ${target}`, "git checkout…");
  setBusy(d.name, true);
  try {
    let res = await api("POST", `/api/repo/${enc(d.name)}/checkout`, { branch: target });
    if (!res.ok && /would be overwritten|stash them before|commit your changes/i.test(res.output || "")) {
      const choice = await dialog({
        title: `Switch to “${target}”`,
        message: `You have uncommitted changes that would be overwritten by switching to “${target}”.\n\nChitHub can stash them, switch, and you can pop them back anytime with the Stash button.`,
        buttons: [
          { label: "Cancel", value: "cancel", kind: "ghost" },
          { label: "Stash & Switch", value: "stash", kind: "primary" },
        ],
      });
      if (choice !== "stash") { finishToast(t, { ok: false, output: "Cancelled — staying on " + d.branches.current }); return; }
      res = await api("POST", `/api/repo/${enc(d.name)}/checkout`, { branch: target, stash: true });
    }
    finishToast(t, res);
  } catch (e) {
    finishToast(t, { ok: false, output: e.message });
  } finally {
    setBusy(d.name, false);
    await refreshOne(d.name);
    if (DRAWER && DRAWER.name === d.name) await reloadDrawer();
  }
}

async function checkoutRemote(local, remoteRef) {
  await runOp(DRAWER.name, `track ${remoteRef}`,
    () => api("POST", `/api/repo/${enc(DRAWER.name)}/checkout`, { branch: local, create: true, startPoint: remoteRef }));
  if (DRAWER) reloadDrawer();
}

function syncRow() {
  const d = DRAWER;
  const row = el("div", "sync-row");
  row.appendChild(actBtn("Sync", "accent", () => doSync(d.name)));
  row.appendChild(actBtn("Fetch", "ghost", () => doOp(d.name, "fetch")));
  row.appendChild(splitButton("Pull", "", () => doPull(d.name, "ff"), [
    { label: "Pull (fast-forward only)", fn: () => doPull(d.name, "ff") },
    { label: "Pull --rebase", fn: () => doPull(d.name, "rebase") },
    { label: "Pull (merge)", fn: () => doPull(d.name, "merge") },
  ]));
  row.appendChild(splitButton("Push", "primary", () => doPush(d.name, false), [
    { label: "Push", fn: () => doPush(d.name, false) },
    { label: "Force push (--force-with-lease)", fn: () => doPush(d.name, true), danger: true },
  ]));
  return row;
}

function renderStaging() {
  const d = DRAWER;
  const panel = $("#drawerPanel");
  panel.innerHTML = "";
  panel.appendChild(drawerHead());
  panel.appendChild(branchBar());
  if (d.conflicts && d.conflicts.files && d.conflicts.files.length) {
    const banner = el("div", "conflict-banner inline");
    banner.appendChild(el("span", null, `⚠ ${d.conflicts.files.length} conflicted file(s) — a ${d.conflicts.inProgress || "merge"} is in progress.`));
    const btn = el("button", "btn small primary", "Resolve…");
    btn.onclick = () => openConflicts(d.name);
    banner.appendChild(btn);
    panel.appendChild(banner);
  }
  panel.appendChild(syncRow());

  const files = d.files;
  panel.appendChild(stagingHeader(files));
  if (files.length) {
    panel.appendChild(commitBox());
    const list = el("div", "files");
    for (const f of files) list.appendChild(fileRow(f));
    panel.appendChild(list);
  } else {
    panel.appendChild(el("div", "clean-note", "✓ Working tree clean — nothing to commit."));
  }

  panel.appendChild(logSection(d));
}

function logSection(d) {
  const frag = document.createDocumentFragment();
  const head = el("div", "staging-head");
  head.appendChild(el("div", "section-title", "Recent commits"));
  if (d.log.length) head.appendChild(linkBtn("Undo last", () => doUndo(d.name)));
  frag.appendChild(head);
  const ll = el("div", "loglist");
  for (const c of d.log) {
    const it = el("div", "logitem");
    it.style.cursor = "pointer";
    it.title = "View this commit's changes";
    it.onclick = () => openShow(d.name, c);
    it.appendChild(el("div", "subj", esc(c.subject)));
    it.appendChild(el("div", "lmeta", `${esc(c.short)} · ${esc(c.author)} · ${relTime(c.time)}`));
    ll.appendChild(it);
  }
  frag.appendChild(ll);
  return frag;
}

function stagingHeader(files) {
  const selCount = files.filter((f) => f.sel !== "none").length;
  const head = el("div", "staging-head");
  head.appendChild(el("div", "section-title", `Changes — ${selCount}/${files.length} selected`));
  if (files.length) {
    const tools = el("div", "sel-tools");
    tools.appendChild(linkBtn("All", () => { files.forEach((f) => setFileSel(f, "all")); renderDrawer(); }));
    tools.appendChild(linkBtn("None", () => { files.forEach((f) => setFileSel(f, "none")); renderDrawer(); }));
    head.appendChild(tools);
  }
  return head;
}

function commitBox() {
  const d = DRAWER;
  const box = el("div", "commit-box");
  const ta = el("textarea");
  ta.placeholder = "Commit message…  (⌘/Ctrl+Enter to commit)";
  ta.value = d.draftMsg || "";
  ta.oninput = () => { d.draftMsg = ta.value; };
  ta.onkeydown = (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); doCommit(false); }
  };
  box.appendChild(ta);

  const row = el("div", "commit-row");
  const n = d.files.filter((f) => f.sel !== "none").length;
  const commitBtn = el("button", "btn primary", `Commit ${n} file${n === 1 ? "" : "s"}`);
  commitBtn.disabled = n === 0;
  commitBtn.onclick = () => doCommit(false);
  const pushBtn = el("button", "btn", "Commit & Push");
  pushBtn.disabled = n === 0;
  pushBtn.onclick = () => doCommit(true);
  row.appendChild(commitBtn);
  row.appendChild(pushBtn);
  if (d.log.length) row.appendChild(linkBtn("Amend last…", () => doAmend()));
  box.appendChild(row);
  return box;
}

function fileRow(f) {
  const wrap = el("div", "file-wrap");
  const head = el("div", "file " + fileCls(f));
  const cb = el("input", "fcb");
  cb.type = "checkbox";
  cb.checked = f.sel !== "none";
  cb.indeterminate = f.sel === "partial";
  cb.onclick = (e) => { e.stopPropagation(); setFileSel(f, cb.checked ? "all" : "none"); renderDrawer(); };
  head.appendChild(cb);
  head.appendChild(el("span", "code mono", esc(f.code.replace(/ /g, "·"))));
  const p = el("span", "fpath", esc(f.path));
  p.onclick = () => toggleFile(f);
  head.appendChild(p);

  const tools = el("span", "file-tools");
  const caret = el("button", "icbtn", f.expanded ? "▾" : "▸");
  caret.title = "Show diff";
  caret.onclick = () => toggleFile(f);
  tools.appendChild(caret);
  const disc = el("button", "icbtn danger", "↩");
  disc.title = "Discard this file";
  disc.onclick = async (e) => {
    e.stopPropagation();
    const toStash = SETTINGS.discardToStash;
    if (!(await confirmDialog(toStash ? "Stash changes" : "Discard changes",
      toStash ? `Move changes in ${f.path} to a stash (recoverable)?` : `Discard all changes in ${f.path}?\nThis can't be undone.`,
      { okLabel: toStash ? "Stash" : "Discard", danger: !toStash }))) return;
    await runOp(DRAWER.name, `discard ${f.path}`,
      () => api("POST", `/api/repo/${enc(DRAWER.name)}/discard`, { paths: [f.path], toStash }));
  };
  tools.appendChild(disc);
  head.appendChild(tools);
  wrap.appendChild(head);

  if (f.expanded) {
    const body = el("div", "diff");
    if (f.loading) body.appendChild(el("div", "diff-msg", "Loading diff…"));
    else if (!f.diff) body.appendChild(el("div", "diff-msg", "No diff."));
    else if (f.diff.binary) body.appendChild(el("div", "diff-msg", "Binary file."));
    else if (f.diff.tooLarge) body.appendChild(el("div", "diff-msg", "Diff too large to display."));
    else if (diffLineCount(f.diff.hunks) > 4000) body.appendChild(el("div", "diff-msg", `Large diff — ${diffLineCount(f.diff.hunks)} lines. Stage the whole file, or open it in your editor.`));
    else { const lang = langForPath(f.path); for (const h of f.diff.hunks) body.appendChild(hunkView(f, h, f.untracked, lang)); }
    wrap.appendChild(body);
  }
  return wrap;
}

// hunkSelState reports whether a hunk's changed lines are all/some/none selected.
function hunkSelState(h) {
  let total = 0, on = 0;
  for (const ln of h.lines) if (ln.t !== " ") { total++; if (ln.sel) on++; }
  if (total === 0 || on === total) return "all";
  return on === 0 ? "none" : "partial";
}

function hunkView(f, h, readonly, lang) {
  const block = el("div", "hunk");
  const hh = el("div", "hunk-head");
  if (!readonly) {
    const cb = el("input", "hcb");
    cb.type = "checkbox";
    const st = hunkSelState(h);
    cb.checked = st !== "none";
    cb.indeterminate = st === "partial";
    cb.title = "Stage / unstage this whole hunk";
    cb.onclick = (e) => {
      e.stopPropagation();
      const on = cb.checked;
      for (const ln of h.lines) if (ln.t !== " ") ln.sel = on;
      recomputeSel(f);
      renderDrawer();
    };
    hh.appendChild(cb);
  }
  hh.appendChild(el("span", "hunk-header mono", esc(h.header)));
  // Expand more context above/below this hunk.
  if (f && !f.untracked && !readonly) {
    const tools = el("span", "hunk-tools");
    const exp = el("button", "icbtn", "↕");
    exp.title = "Show more surrounding lines";
    exp.onclick = (e) => { e.stopPropagation(); expandContext(f); };
    tools.appendChild(exp);
    const disc = el("button", "icbtn danger", "↩");
    disc.title = "Discard this hunk";
    disc.onclick = (e) => { e.stopPropagation(); discardHunk(f, h); };
    tools.appendChild(disc);
    hh.appendChild(tools);
  }
  block.appendChild(hh);
  block.appendChild(diffLines(f, h, readonly, lang));
  return block;
}

// computeWordDiff annotates paired -/+ lines in a hunk with the [start,end)
// char range that actually changed, so the renderer can highlight just that
// span (word-level diff) instead of the whole line.
function computeWordDiff(h) {
  const L = h.lines;
  let i = 0;
  while (i < L.length) {
    if (L[i].t !== "-") { i++; continue; }
    let dStart = i;
    while (i < L.length && L[i].t === "-") i++;
    let aStart = i;
    while (i < L.length && L[i].t === "+") i++;
    const dels = L.slice(dStart, aStart), adds = L.slice(aStart, i);
    const n = Math.min(dels.length, adds.length);
    for (let k = 0; k < n; k++) {
      const r = intraLineDiff(dels[k].c, adds[k].c);
      if (r) { dels[k]._wd = r.a; adds[k]._wd = r.b; }
    }
  }
}

// intraLineDiff returns the differing middle range of two strings by trimming
// the common prefix and suffix. Returns null when they're identical or the
// change spans almost the whole line (where a word highlight adds no clarity).
function intraLineDiff(a, b) {
  if (a === b) return null;
  let p = 0;
  const max = Math.min(a.length, b.length);
  while (p < max && a[p] === b[p]) p++;
  let s = 0;
  while (s < max - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
  const aMid = a.length - p - s, bMid = b.length - p - s;
  // Skip when nearly everything changed (no useful signal).
  if (aMid > a.length * 0.8 && bMid > b.length * 0.8 && a.length > 6) return null;
  return { a: [p, a.length - s], b: [p, b.length - s] };
}

function diffLines(f, h, readonly, lang) {
  computeWordDiff(h);
  const lines = el("div", "hunk-lines");
  const m = (h.header || "").match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
  let oldN = m ? parseInt(m[1], 10) : 0;
  let newN = m ? parseInt(m[2], 10) : 0;
  for (const ln of h.lines) {
    const changed = ln.t === "+" || ln.t === "-";
    const cls = ln.t === "+" ? "add" : ln.t === "-" ? "del" : "ctx";
    const selectable = changed && !readonly;
    // `.off` (excluded-from-commit) only applies to selectable diffs — a readonly
    // commit-view diff must render every line at full contrast.
    const row = el("div", "dl " + cls + (selectable ? " selectable" : "") + (selectable && !ln.sel ? " off" : ""));

    const gutter = el("span", "dl-gutter");
    if (selectable) {
      gutter.classList.add("on");
      // No per-line checkbox: a selected line is shown by a blue gutter (GitHub
      // Desktop style). The whole changed line is clickable to toggle.
      row.title = ln.sel ? "Click to exclude this line from the commit" : "Click to include this line";
      row.onclick = () => { ln.sel = !ln.sel; recomputeSel(f); renderDrawer(); };
    }
    row.appendChild(gutter);

    let oldStr = "", newStr = "";
    if (ln.t === " ") { oldStr = oldN++; newStr = newN++; }
    else if (ln.t === "-") { oldStr = oldN++; }
    else if (ln.t === "+") { newStr = newN++; }
    row.appendChild(el("span", "dl-ln", String(oldStr)));
    row.appendChild(el("span", "dl-ln", String(newStr)));
    row.appendChild(el("span", "dl-sign", ln.t === "+" ? "+" : ln.t === "-" ? "−" : ""));

    const text = el("span", "dl-text");
    text.innerHTML = renderCode(ln.c, lang, ln._wd) || "&nbsp;";
    if (ln.noNL) text.appendChild(noNLBadge());
    row.appendChild(text);
    lines.appendChild(row);
  }
  return lines;
}

function noNLBadge() {
  const b = el("span", "nonl");
  b.textContent = "↵";
  b.title = "No newline at end of file";
  return b;
}

function diffLineCount(hunks) {
  let n = 0;
  for (const h of (hunks || [])) n += h.lines.length;
  return n;
}

// buildHunkPatch builds a unified diff containing exactly one hunk's changes,
// used to discard that hunk (the patch is reverse-applied to the working tree).
function buildHunkPatch(f, hunk) {
  if (!f.diff || !f.diff.preamble) return null;
  let oldc = 0, newc = 0, has = false;
  const lines = [];
  const push = (s, ln) => { lines.push(s); if (ln.noNL) lines.push("\\ No newline at end of file"); };
  for (const ln of hunk.lines) {
    if (ln.t === " ") { push(" " + ln.c, ln); oldc++; newc++; }
    else if (ln.t === "+") { push("+" + ln.c, ln); newc++; has = true; }
    else if (ln.t === "-") { push("-" + ln.c, ln); oldc++; has = true; }
  }
  if (!has) return null;
  const m = hunk.header.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
  const oldStart = m ? m[1] : "1", newStart = m ? m[2] : "1";
  let pre = f.diff.preamble;
  if (!pre.endsWith("\n")) pre += "\n";
  return pre + `@@ -${oldStart},${oldc} +${newStart},${newc} @@\n` + lines.join("\n") + "\n";
}

async function discardHunk(f, hunk) {
  if (!(await confirmDialog("Discard hunk",
    `Discard this hunk's changes in ${f.path}?\nThis can't be undone.`, { okLabel: "Discard hunk", danger: true }))) return;
  const patch = buildHunkPatch(f, hunk);
  if (!patch) { toast("err", "Discard failed", "Could not build the patch."); return; }
  await runOp(DRAWER.name, `discard hunk · ${f.path}`,
    () => api("POST", `/api/repo/${enc(DRAWER.name)}/discard-patch`, { patch }));
}

async function expandContext(f) {
  f.context = (f.context || 3) + 25;
  try {
    const fresh = await api("GET", `/api/repo/${enc(DRAWER.name)}/diff?path=${enc(f.path)}&context=${f.context}`);
    // Preserve per-line selection POSITIONALLY: expanding context only adds
    // context lines, so the ordered sequence of +/- lines is stable. (A content
    // hash would wrongly collapse duplicate identical changed lines.)
    const sels = [];
    for (const h of (f.diff.hunks || [])) for (const ln of h.lines)
      if (ln.t === "+" || ln.t === "-") sels.push(ln.sel);
    f.diff = fresh;
    let i = 0;
    for (const h of (f.diff.hunks || [])) for (const ln of h.lines)
      if (ln.t === "+" || ln.t === "-") { ln.sel = i < sels.length ? sels[i] : true; i++; }
    recomputeSel(f);
    renderDrawer();
  } catch (e) { toast("err", "Expand failed", e.message); }
}

// ---------- Syntax highlighting + word-level diff ----------
const KW = ("if else elif endif for foreach while do done then fi return func function fn def end " +
  "var let const class struct interface enum type typedef union namespace module mod package import " +
  "export from as use using require include new delete this self super public private protected internal " +
  "static final abstract virtual override extends implements throws throw try catch finally except raise " +
  "switch case default break continue goto async await yield defer go chan select range map make panic recover " +
  "void int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 float float32 float64 double bool boolean " +
  "string str char byte rune any object number symbol bigint null nil none true false undefined NaN " +
  "and or not in is of with lambda pass print echo def fn impl trait mut pub where match when unless begin " +
  "let const readonly typeof instanceof keyof infer declare extends implements")
  .split(/\s+/).reduce((s, w) => (w && s.add(w), s), new Set());

const CSTYLE = { line: ["//"], block: ["/*", "*/"], strings: ["\"", "'", "`"], kw: KW };
const HASH = { line: ["#"], block: null, strings: ["\"", "'"], kw: KW };
const SQL = { line: ["--"], block: ["/*", "*/"], strings: ["'", "\""], kw: KW };
const JSONC = { line: [], block: null, strings: ["\""], kw: new Set(["true", "false", "null"]) };
const PLAIN = { line: [], block: null, strings: [], kw: new Set() };

const EXT_LANG = {
  go: CSTYLE, js: CSTYLE, jsx: CSTYLE, ts: CSTYLE, tsx: CSTYLE, mjs: CSTYLE, cjs: CSTYLE,
  c: CSTYLE, h: CSTYLE, cpp: CSTYLE, cc: CSTYLE, hpp: CSTYLE, cs: CSTYLE, java: CSTYLE,
  kt: CSTYLE, kts: CSTYLE, swift: CSTYLE, rs: CSTYLE, scala: CSTYLE, dart: CSTYLE, php: CSTYLE,
  proto: CSTYLE, css: CSTYLE, scss: CSTYLE, less: CSTYLE, m: CSTYLE, mm: CSTYLE, groovy: CSTYLE,
  py: HASH, rb: HASH, sh: HASH, bash: HASH, zsh: HASH, fish: HASH, yaml: HASH, yml: HASH,
  toml: HASH, ini: HASH, conf: HASH, cfg: HASH, env: HASH, dockerfile: HASH, makefile: HASH,
  pl: HASH, r: HASH, tf: HASH, hcl: HASH, properties: HASH, gitignore: HASH,
  sql: SQL, json: JSONC, json5: JSONC,
  md: PLAIN, markdown: PLAIN, txt: PLAIN, html: PLAIN, xml: PLAIN, svg: PLAIN, csv: PLAIN, lock: PLAIN,
};

function langForPath(path) {
  if (!path) return CSTYLE;
  const base = path.split("/").pop().toLowerCase();
  if (base === "dockerfile") return HASH;
  if (base === "makefile") return HASH;
  const dot = base.lastIndexOf(".");
  const ext = dot >= 0 ? base.slice(dot + 1) : base;
  return EXT_LANG[ext] || CSTYLE;
}

// tokenize splits code into [{s,e,cls}] segments. cls ∈ '', 'str','com','num','kw'.
function tokenize(code, cfg) {
  const seg = [];
  const n = code.length;
  let i = 0, plainStart = 0;
  const flushPlain = (end) => { if (end > plainStart) seg.push({ s: plainStart, e: end, cls: "" }); };
  const isIdent = (c) => /[A-Za-z0-9_$]/.test(c);
  while (i < n) {
    const c = code[i];
    // block comment
    if (cfg.block && code.startsWith(cfg.block[0], i)) {
      flushPlain(i);
      let j = code.indexOf(cfg.block[1], i + cfg.block[0].length);
      j = j < 0 ? n : j + cfg.block[1].length;
      seg.push({ s: i, e: j, cls: "com" }); i = plainStart = j; continue;
    }
    // line comment
    let lc = null;
    for (const m of cfg.line) if (code.startsWith(m, i)) { lc = m; break; }
    if (lc) { flushPlain(i); seg.push({ s: i, e: n, cls: "com" }); i = plainStart = n; break; }
    // string
    if (cfg.strings.includes(c)) {
      flushPlain(i);
      let j = i + 1;
      while (j < n) { if (code[j] === "\\") { j += 2; continue; } if (code[j] === c) { j++; break; } j++; }
      seg.push({ s: i, e: j, cls: "str" }); i = plainStart = j; continue;
    }
    // number
    if (/[0-9]/.test(c) && (i === 0 || !isIdent(code[i - 1]))) {
      flushPlain(i);
      let j = i;
      while (j < n && /[0-9a-fA-FxX._]/.test(code[j])) j++;
      seg.push({ s: i, e: j, cls: "num" }); i = plainStart = j; continue;
    }
    // identifier / keyword
    if (isIdent(c) && !/[0-9]/.test(c)) {
      let j = i;
      while (j < n && isIdent(code[j])) j++;
      const word = code.slice(i, j);
      if (cfg.kw.has(word)) { flushPlain(i); seg.push({ s: i, e: j, cls: "kw" }); plainStart = j; }
      i = j; continue;
    }
    i++;
  }
  flushPlain(n);
  return seg;
}

// renderCode returns highlighted, escaped HTML for one line of code. `mark`, if
// given as [start,end), wraps that char range in a word-diff highlight.
function renderCode(code, lang, mark) {
  if (code === "") return "";
  const cfg = lang || CSTYLE;
  let segs;
  try { segs = tokenize(code, cfg); } catch { segs = [{ s: 0, e: code.length, cls: "" }]; }
  let html = "";
  for (const sg of segs) {
    let a = sg.s;
    while (a < sg.e) {
      let b = sg.e, marked = false;
      if (mark && a < mark[1] && sg.e > mark[0]) {
        if (a < mark[0]) { b = Math.min(sg.e, mark[0]); }
        else { marked = true; b = Math.min(sg.e, mark[1]); }
      }
      const piece = esc(code.slice(a, b));
      const cls = (sg.cls ? "t-" + sg.cls : "") + (marked ? (sg.cls ? " " : "") + "wd" : "");
      html += cls ? `<span class="${cls}">${piece}</span>` : piece;
      a = b;
    }
  }
  return html;
}

async function toggleFile(f) {
  f.expanded = !f.expanded;
  if (f.expanded && !f.diff && !f.loading) {
    f.loading = true;
    renderDrawer();
    try {
      f.diff = await api("GET", `/api/repo/${enc(DRAWER.name)}/diff?path=${enc(f.path)}`);
      annotateLines(f);
    } catch (e) {
      toast("err", "Diff failed", e.message);
    } finally {
      f.loading = false;
    }
  }
  renderDrawer();
}

// annotateLines gives every changed line a `sel` flag, seeded from the file's
// current checkbox state (default: included).
function annotateLines(f) {
  const on = f.sel !== "none";
  for (const h of (f.diff.hunks || [])) {
    for (const ln of h.lines) {
      if ((ln.t === "+" || ln.t === "-") && ln.sel === undefined) ln.sel = on;
    }
  }
}

function setFileSel(f, sel) {
  f.sel = sel;
  if (f.diff && !f.untracked) {
    const on = sel === "all";
    for (const h of (f.diff.hunks || [])) for (const ln of h.lines) if (ln.t !== " ") ln.sel = on;
  }
}

function recomputeSel(f) {
  if (!f.diff) return;
  let total = 0, on = 0;
  for (const h of (f.diff.hunks || [])) for (const ln of h.lines) if (ln.t !== " ") { total++; if (ln.sel) on++; }
  f.sel = total === 0 ? "all" : on === 0 ? "none" : on === total ? "all" : "partial";
}
function fileCls(f) {
  if (f.untracked) return "untracked";
  const x = f.code ? f.code[0] : " ";
  return x !== " " && x !== "?" ? "staged" : "unstaged";
}

function buildCommitFiles() {
  const out = [];
  for (const f of DRAWER.files) {
    if (f.sel === "none") continue;
    if (f.untracked || f.sel === "all" || !f.diff) { out.push({ path: f.path, mode: "all" }); continue; }
    const patch = buildFilePatch(f);
    if (patch) out.push({ path: f.path, mode: "patch", patch });
  }
  return out;
}

// buildFilePatch reconstructs a unified diff containing only the selected lines:
// unselected additions are dropped, unselected deletions become context. Counts
// are recomputed; `git apply --recount` tidies the rest.
function buildFilePatch(f) {
  if (!f.diff || !f.diff.preamble) return null;
  let body = "";
  let finalLn = null; // last line emitted across hunks = the file's EOF line
  for (const h of f.diff.hunks) {
    let oldc = 0, newc = 0, has = false, last = null;
    const lines = [];
    const push = (s, ln) => { lines.push(s); last = ln; };
    for (const ln of h.lines) {
      if (ln.t === " ") { push(" " + ln.c, ln); oldc++; newc++; }
      else if (ln.t === "+") {
        if (ln.sel) { push("+" + ln.c, ln); newc++; has = true; }
      } else if (ln.t === "-") {
        if (ln.sel) { push("-" + ln.c, ln); oldc++; has = true; }
        else { push(" " + ln.c, ln); oldc++; newc++; }
      }
    }
    if (!has) continue;
    const m = h.header.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    const oldStart = m ? m[1] : "1";
    const newStart = m ? m[2] : "1";
    body += `@@ -${oldStart},${oldc} +${newStart},${newc} @@\n` + lines.join("\n") + "\n";
    finalLn = last;
  }
  if (!body) return null;
  // The "\ No newline at end of file" marker only applies to the file's final
  // line, so emit it once after the last hunk's last line — never inline after a
  // mid-hunk line (which would corrupt the patch when a deletion is deselected).
  if (finalLn && finalLn.noNL) body += "\\ No newline at end of file\n";
  let pre = f.diff.preamble;
  if (!pre.endsWith("\n")) pre += "\n";
  return pre + body;
}

async function doCommit(push) {
  const d = DRAWER;
  const msg = (d.draftMsg || "").trim();
  if (!msg) { const ta = $("#drawerPanel textarea"); if (ta) ta.focus(); return; }
  const files = buildCommitFiles();
  if (!files.length) { toast("err", "Nothing selected", "Select at least one file or hunk."); return; }
  const t = toast("run", `commit ${d.name}`, "git commit…");
  try {
    const res = await api("POST", `/api/repo/${enc(d.name)}/commit`, { message: msg, push, files });
    finishToast(t, res);
    if (res.ok) {
      d.draftMsg = "";
      await refreshOne(d.name);
      if (REVIEW && REVIEW.mode === "commit") reviewAdvance(); else reloadDrawer();
    }
  } catch (e) {
    finishToast(t, { ok: false, output: e.message });
  }
}

async function doAmend() {
  const d = DRAWER;
  const files = buildCommitFiles();
  const msg = (d.draftMsg || "").trim();
  const note = files.length ? `fold ${files.length} selected file(s) into` : "reword";
  if (!(await confirmDialog("Amend last commit",
    `${note.charAt(0).toUpperCase() + note.slice(1)} the last commit?\nThis rewrites it.`,
    { okLabel: "Amend" }))) return;
  const t = toast("run", `amend ${d.name}`, "git commit --amend…");
  try {
    const res = await api("POST", `/api/repo/${enc(d.name)}/amend`, { message: msg, files });
    finishToast(t, res);
    if (res.ok) { d.draftMsg = ""; await refreshOne(d.name); reloadDrawer(); }
  } catch (e) {
    finishToast(t, { ok: false, output: e.message });
  }
}

async function doStashDrawer(action) {
  await runOp(DRAWER.name, `stash ${action} · ${DRAWER.name}`,
    () => api("POST", `/api/repo/${enc(DRAWER.name)}/stash`, { action }));
}

// ---------- Pull review view ----------
function renderPullReview() {
  const d = DRAWER, r = d.info;
  const panel = $("#drawerPanel");
  panel.innerHTML = "";
  panel.appendChild(drawerHead());
  panel.appendChild(branchBar());

  const stat = el("div", "pull-stat " + (r.behind > 0 ? "warn" : "ok"));
  stat.textContent = r.behind > 0
    ? `↓ ${r.behind} commit${r.behind === 1 ? "" : "s"} to pull` + (r.ahead > 0 ? ` · ↑ ${r.ahead} ahead (diverged)` : "")
    : "Up to date with upstream.";
  panel.appendChild(stat);

  const row = el("div", "sync-row");
  row.appendChild(actBtn("Fetch", "ghost", () => doOp(d.name, "fetch")));
  const pullAndNext = (mode) => async () => {
    await runOp(d.name, `pull ${mode} · ${d.name}`, () => api("POST", `/api/repo/${enc(d.name)}/pull`, { mode }));
    if (REVIEW && REVIEW.mode === "pull") reviewAdvance();
  };
  row.appendChild(splitButton("Pull", "primary", pullAndNext("ff"), [
    { label: "Pull (fast-forward only)", fn: pullAndNext("ff") },
    { label: "Pull --rebase", fn: pullAndNext("rebase") },
    { label: "Pull (merge)", fn: pullAndNext("merge") },
  ]));
  panel.appendChild(row);

  panel.appendChild(el("div", "section-title", `Incoming commits (${d.incoming.length})`));
  const ll = el("div", "loglist");
  if (!d.incoming.length) ll.appendChild(el("div", "sub", "Nothing to pull."));
  for (const c of d.incoming) {
    const it = el("div", "logitem");
    it.style.cursor = "pointer";
    it.onclick = () => openShow(d.name, c);
    it.appendChild(el("div", "subj", esc(c.subject)));
    it.appendChild(el("div", "lmeta", `${esc(c.short)} · ${esc(c.author)} · ${relTime(c.time)}`));
    ll.appendChild(it);
  }
  panel.appendChild(ll);
}

function closeDrawer() { $("#drawer").hidden = true; DRAWER = null; }

const barBtn = (label, fn) => { const b = el("button", "btn small ghost", esc(label)); b.onclick = fn; return b; };
const linkBtn = (label, fn) => { const b = el("button", "linkbtn", esc(label)); b.onclick = fn; return b; };

// ---------- Commit diff modal ----------
async function openShow(name, commit) {
  const modal = $("#showModal");
  const card = $("#showCard");
  modal.hidden = false;
  card.innerHTML = `<div class="sub">Loading ${esc(commit.short)}…</div>`;
  try {
    const data = await api("GET", `/api/repo/${enc(name)}/show?hash=${enc(commit.hash)}`);
    card.innerHTML = "";
    const close = el("button", "close", "×"); close.onclick = closeShow;
    card.appendChild(close);
    card.appendChild(el("h3", null, esc(commit.subject)));
    card.appendChild(el("div", "sub", `${esc(commit.short)} · ${esc(commit.author)} · ${relTime(commit.time)}`));
    const files = data.files || [];
    card.appendChild(el("div", "section-title", `${files.length} file${files.length === 1 ? "" : "s"} changed`));
    if (!files.length) {
      card.appendChild(el("div", "clean-note", "No file changes to show (e.g. a merge with no conflicts, or an empty commit)."));
    }
    for (const fd of files) {
      const wrap = el("div", "show-file");
      wrap.appendChild(el("div", "show-fpath mono", esc(fd.path)));
      const body = el("div", "diff");
      if (fd.binary) body.appendChild(el("div", "diff-msg", "Binary file."));
      else if (fd.tooLarge) body.appendChild(el("div", "diff-msg", "Diff too large to display."));
      else if (!fd.hunks || !fd.hunks.length) body.appendChild(el("div", "diff-msg", "No textual changes (mode, rename, or empty)."));
      else if (diffLineCount(fd.hunks) > 4000) body.appendChild(el("div", "diff-msg", `Large diff — ${diffLineCount(fd.hunks)} lines. Open the repo in your editor to view it.`));
      else { const lang = langForPath(fd.path); for (const h of fd.hunks) body.appendChild(hunkView(null, h, true, lang)); }
      wrap.appendChild(body);
      card.appendChild(wrap);
    }
  } catch (e) {
    card.innerHTML = `<button class="close" onclick="closeShow()">×</button><div class="sub">Error: ${esc(e.message)}</div>`;
  }
}
function closeShow() { $("#showModal").hidden = true; }

// ---------- Clone ----------
function openClone() {
  $("#cloneModal").hidden = false;
  $("#cloneUrl").value = ""; $("#cloneName").value = "";
  $("#cloneUrl").focus();
}
function closeClone() { $("#cloneModal").hidden = true; }
async function doClone() {
  const url = $("#cloneUrl").value.trim();
  if (!url) { $("#cloneUrl").focus(); return; }
  const name = $("#cloneName").value.trim();
  closeClone();
  const t = toast("run", "clone", `cloning ${url}…`);
  try {
    const res = await api("POST", "/api/clone", { url, name });
    finishToast(t, res);
    await load();
  } catch (e) { finishToast(t, { ok: false, output: e.message }); }
}

// ---------- Toasts ----------
let toastId = 0;
function toast(kind, title, body) {
  const t = el("div", "toast " + kind);
  t.id = "toast-" + ++toastId;
  t.innerHTML = `<div class="t-head"><span class="t-title"></span><button class="dismiss">×</button></div>`;
  t.querySelector(".t-title").textContent = title;
  t.querySelector(".dismiss").onclick = () => t.remove();
  if (body) { const p = el("pre"); p.textContent = body; t.appendChild(p); }
  $("#toasts").appendChild(t);
  return t;
}
function finishToast(t, res) {
  t.className = "toast " + (res.ok ? "ok" : "err");
  let pre = t.querySelector("pre");
  if (!pre) { pre = el("pre"); t.appendChild(pre); }
  pre.textContent = res.output || (res.ok ? "done" : "failed");
  if (res.ok) setTimeout(() => t.remove(), 4500);
}

// ---------- Styled dialogs (replace browser alert/confirm/prompt) ----------
function dialog(opts) {
  return new Promise((resolve) => {
    const modal = el("div", "modal dialog-modal");
    const backdrop = el("div", "modal-backdrop");
    const card = el("div", "modal-card dialog-card");
    if (opts.title) card.appendChild(el("h3", null, esc(opts.title)));
    if (opts.message) {
      const m = el("div", "dialog-msg");
      m.innerHTML = esc(opts.message).replace(/\n/g, "<br>");
      card.appendChild(m);
    }
    let input = null;
    if (opts.input) {
      const field = el("label", "field");
      if (opts.input.label) field.appendChild(el("span", null, esc(opts.input.label)));
      input = el("input");
      input.type = "text";
      input.value = opts.input.value || "";
      input.placeholder = opts.input.placeholder || "";
      input.spellcheck = false;
      field.appendChild(input);
      card.appendChild(field);
    }
    const actions = el("div", "modal-actions");
    const buttons = opts.buttons || [
      { label: "Cancel", value: false, kind: "ghost" },
      { label: "OK", value: true, kind: "primary" },
    ];
    const cancelVal = opts.input ? false : false;
    const finish = (val) => { modal.remove(); document.removeEventListener("keydown", onKey, true); resolve(val); };
    const valueOf = (b) => (input && b.value !== false && b.value !== "cancel") ? input.value.trim() : b.value;
    for (const b of buttons) {
      const btn = el("button", "btn " + (b.kind || ""), esc(b.label));
      btn.onclick = () => finish(valueOf(b));
      actions.appendChild(btn);
    }
    card.appendChild(actions);
    backdrop.onclick = () => finish(cancelVal);
    const onKey = (e) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); finish(cancelVal); }
      else if (e.key === "Enter") {
        const primary = buttons.find((b) => b.kind === "primary") || buttons[buttons.length - 1];
        e.preventDefault(); e.stopPropagation(); finish(valueOf(primary));
      }
    };
    document.addEventListener("keydown", onKey, true);
    modal.appendChild(backdrop);
    modal.appendChild(card);
    document.body.appendChild(modal);
    setTimeout(() => { (input || card.querySelector(".btn.primary") || card.querySelector(".btn")).focus(); }, 20);
  });
}

function confirmDialog(title, message, opts = {}) {
  return dialog({
    title, message,
    buttons: [
      { label: opts.cancelLabel || "Cancel", value: false, kind: "ghost" },
      { label: opts.okLabel || "OK", value: true, kind: opts.danger ? "danger" : "primary" },
    ],
  });
}
function promptDialog(title, opts = {}) {
  return dialog({
    title, message: opts.message,
    input: { label: opts.label, placeholder: opts.placeholder, value: opts.value || "" },
    buttons: [
      { label: "Cancel", value: false, kind: "ghost" },
      { label: opts.okLabel || "OK", value: true, kind: "primary" },
    ],
  });
}

// ---------- Helpers ----------
function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}
function cssEsc(s) { return String(s).replace(/["\\]/g, "\\$&"); }
function firstLine(s) { return String(s || "").split("\n").find((l) => l.trim()) || ""; }
function relTime(unix) {
  if (!unix) return "never";
  const diff = Math.floor(Date.now() / 1000) - unix;
  if (diff < 60) return "just now";
  const units = [["y", 31536000], ["mo", 2592000], ["d", 86400], ["h", 3600], ["m", 60]];
  for (const [u, s] of units) { const v = Math.floor(diff / s); if (v >= 1) return `${v}${u} ago`; }
  return "just now";
}

// ---------- Wire up ----------
function init() {
  $("#refreshBtn").onclick = load;
  $("#cloneBtn").onclick = openClone;
  $("#cloneCancel").onclick = closeClone;
  $("#cloneGo").onclick = doClone;
  $("#cloneModal").querySelector(".modal-backdrop").onclick = closeClone;

  const cBtn = $("#collectionBtn"), cMenu = $("#collectionMenu");
  cBtn.onclick = (e) => { e.stopPropagation(); closeAllMenus(); cMenu.hidden = !cMenu.hidden; };

  $("#reviewBtn").onclick = () => startReview("commit");
  bindMenu("#reviewMenuBtn", "#reviewMenu", (b) => startReview(b.dataset.mode));

  $("#fetchAllBtn").onclick = () => batch("fetch", {});
  $("#pullAllBtn").onclick = () => batch("pull", { mode: "ff" });
  $("#pushAllBtn").onclick = () => batch("push", { force: false });
  bindMenu("#pullAllMenuBtn", "#pullAllMenu", (b) => batch("pull", { mode: b.dataset.mode }));
  bindMenu("#pushAllMenuBtn", "#pushAllMenu", (b) => batch("push", { force: b.dataset.force === "true" }));

  $("#selectAll").onchange = (e) => {
    const visible = REPOS.filter(passesFilter);
    if (e.target.checked) visible.forEach((r) => SELECTED.add(r.name));
    else visible.forEach((r) => SELECTED.delete(r.name));
    render();
  };
  $("#filters").addEventListener("click", (e) => {
    const chip = e.target.closest(".chip");
    if (!chip) return;
    document.querySelectorAll(".chip").forEach((c) => c.classList.remove("active"));
    chip.classList.add("active");
    FILTER = chip.dataset.filter;
    render();
  });
  $("#search").addEventListener("input", (e) => { QUERY = e.target.value.trim().toLowerCase(); render(); });

  $("#settingsBtn").onclick = openSettings;
  $("#paletteBtn").onclick = openPalette;

  document.addEventListener("click", closeAllMenus);
  document.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
      e.preventDefault(); openPalette(); return;
    }
    if (document.querySelector(".dialog-modal") || document.querySelector(".palette")) return;
    if (e.key === "Escape") {
      if (!$("#showModal").hidden) closeShow();
      else if (!$("#cloneModal").hidden) closeClone();
      else if (REVIEW) reviewExit();
      else { closeDrawer(); closeAllMenus(); }
    }
    if (e.key === "r" && !e.metaKey && !e.ctrlKey &&
        document.activeElement.tagName !== "INPUT" &&
        document.activeElement.tagName !== "TEXTAREA") load();
  });

  loadSettings().then(() => { applySettings(); restartAutoFetch(); }).finally(() => {
    loadCollections();
    load();
  });
  connectEvents();
  checkForUpdate();

  // Safety-net poll for raw working-tree edits (the SSE watcher covers all git
  // operations instantly; this catches files edited in an external editor).
  setInterval(() => {
    if (document.visibilityState === "visible" && !DRAWER && !REVIEW && !busyModalOpen()) load();
  }, 15000);
}

function busyModalOpen() {
  return !$("#cloneModal").hidden || !$("#showModal").hidden ||
    document.querySelector(".dialog-modal") || document.querySelector(".palette") ||
    document.querySelector(".sheet");
}

function bindMenu(btnSel, menuSel, onPick) {
  const btn = $(btnSel), menu = $(menuSel);
  btn.onclick = (e) => { e.stopPropagation(); closeAllMenus(); menu.hidden = !menu.hidden; };
  menu.addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    menu.hidden = true;
    onPick(b);
  });
}

// =========================================================================
// Settings · i18n · theming
// =========================================================================
let SETTINGS = {
  theme: "dark", lang: "en", defaultPull: "ff", autoFetchMin: 0,
  fontSize: 14, warnMainPush: true, discardToStash: true,
};
// The Language/RTL option is hidden in this release (set true to re-enable).
const SHOW_LANGUAGE = false;

const I18N = {
  fa: {
    refresh: "تازه‌سازی", palette: "فرمان‌ها", sync: "همگام‌سازی", pull: "دریافت",
    push: "ارسال", publish: "انتشار شاخه", commit: "ثبت", clone: "کلون", review: "مرور",
    fetch: "واکشی", settings: "تنظیمات", history: "تاریخچه", search: "جستجو",
    "Select all": "انتخاب همه", All: "همه", "Needs attention": "نیازمند توجه",
    Behind: "عقب", Ahead: "جلو", Dirty: "تغییر‌یافته", "Filter by name…": "فیلتر بر اساس نام…",
    Branch: "شاخه", Stash: "ذخیره موقت", Reveal: "نمایش در فایندر",
  },
};

function t(key) {
  const lang = effLang();
  if (lang && lang !== "en" && I18N[lang] && I18N[lang][key]) return I18N[lang][key];
  // English fallback: dictionary keys are short lowercase tokens (sync, pull, …);
  // present them Title-cased so dynamic labels match the rest of the UI.
  return key ? key[0].toUpperCase() + key.slice(1) : key;
}

async function loadSettings() {
  try {
    const s = await api("GET", "/api/settings");
    SETTINGS = Object.assign(SETTINGS, s);
  } catch { /* defaults */ }
}

function applySettings() {
  const root = document.documentElement;
  const lang = SHOW_LANGUAGE ? (SETTINGS.lang || "en") : "en";
  root.setAttribute("data-theme", SETTINGS.theme || "dark");
  root.style.fontSize = (SETTINGS.fontSize || 14) + "px";
  root.lang = lang;
  root.dir = lang === "fa" ? "rtl" : "ltr";
  applyI18n();
}

function effLang() { return SHOW_LANGUAGE ? (SETTINGS.lang || "en") : "en"; }

function applyI18n() {
  // For English we keep the properly-capitalized labels authored in index.html;
  // only a non-English language swaps them through the dictionary.
  const en = effLang() === "en";
  if (!en) {
    document.querySelectorAll("[data-i18n]").forEach((e) => {
      e.textContent = t(e.getAttribute("data-i18n"));
    });
    const s = $("#search"); if (s) s.placeholder = t("Filter by name…");
  }
  if (REPOS.length || $("#repoList").children.length) render();
}

async function saveSettings() {
  try {
    const s = await api("POST", "/api/settings", SETTINGS);
    SETTINGS = Object.assign(SETTINGS, s);
  } catch (e) { toast("err", "Could not save settings", e.message); }
}

function openSettings() {
  const s = sheet({ title: SHOW_LANGUAGE ? t("settings") : "Settings" });
  const f = s.body;
  const row = (label, control) => {
    const r = el("div", "set-row");
    r.appendChild(el("div", "set-label", label));
    r.appendChild(control);
    f.appendChild(r);
  };
  // theme
  const theme = segmented(["dark", "light"], SETTINGS.theme, (v) => { SETTINGS.theme = v; applySettings(); saveSettings(); });
  row("Theme", theme);
  // language (hidden for this release — the i18n/RTL code stays in place)
  if (SHOW_LANGUAGE) {
    const lang = segmented([["en", "English"], ["fa", "فارسی (RTL)"]], SETTINGS.lang, (v) => { SETTINGS.lang = v; applySettings(); saveSettings(); });
    row("Language", lang);
  }
  // default pull
  const pull = segmented([["ff", "Fast-forward"], ["rebase", "Rebase"], ["merge", "Merge"]], SETTINGS.defaultPull, (v) => { SETTINGS.defaultPull = v; saveSettings(); });
  row("Default pull", pull);
  // auto-fetch
  const af = el("select", "set-select");
  for (const [v, l] of [[0, "Off"], [5, "Every 5 min"], [10, "Every 10 min"], [30, "Every 30 min"]]) {
    const o = el("option", null, l); o.value = v; if (SETTINGS.autoFetchMin === v) o.selected = true; af.appendChild(o);
  }
  af.onchange = () => { SETTINGS.autoFetchMin = parseInt(af.value, 10); saveSettings(); restartAutoFetch(); };
  row("Background fetch", af);
  // font size
  const fs = el("input", "set-range"); fs.type = "range"; fs.min = 12; fs.max = 18; fs.value = SETTINGS.fontSize;
  const fsv = el("span", "set-val", SETTINGS.fontSize + "px");
  fs.oninput = () => { SETTINGS.fontSize = parseInt(fs.value, 10); fsv.textContent = SETTINGS.fontSize + "px"; applySettings(); };
  fs.onchange = saveSettings;
  const fsWrap = el("div", "set-inline"); fsWrap.appendChild(fs); fsWrap.appendChild(fsv);
  row("Font size", fsWrap);
  // toggles
  row("Warn before pushing to main", toggle(SETTINGS.warnMainPush, (v) => { SETTINGS.warnMainPush = v; saveSettings(); }));
  row("Discard moves changes to a stash (recoverable)", toggle(SETTINGS.discardToStash, (v) => { SETTINGS.discardToStash = v; saveSettings(); }));
}

function segmented(options, value, onPick) {
  const wrap = el("div", "segmented");
  for (const opt of options) {
    const v = Array.isArray(opt) ? opt[0] : opt;
    const label = Array.isArray(opt) ? opt[1] : opt;
    const b = el("button", "seg" + (v === value ? " active" : ""), esc(label));
    b.onclick = () => { wrap.querySelectorAll(".seg").forEach((x) => x.classList.remove("active")); b.classList.add("active"); onPick(v); };
    wrap.appendChild(b);
  }
  return wrap;
}

function toggle(on, onChange) {
  const t = el("button", "switch" + (on ? " on" : ""));
  t.innerHTML = "<i></i>";
  t.onclick = () => { on = !on; t.classList.toggle("on", on); onChange(on); };
  return t;
}

// =========================================================================
// Live updates (SSE) + auto-fetch + update check
// =========================================================================
let evtSource = null;
let refreshDebounce = null;
function connectEvents() {
  try {
    evtSource = new EventSource("/api/events");
    evtSource.onmessage = (e) => { if (e.data === "refresh") scheduleLiveRefresh(); };
    evtSource.onerror = () => { /* EventSource auto-reconnects */ };
  } catch { /* SSE unsupported */ }
}

// Tell the server the window is closing so it can quit promptly (in app mode)
// rather than waiting out the longer SSE-drop grace. Harmless in dev/browser
// mode, where the server ignores it. `pagehide` is the reliable unload hook.
window.addEventListener("pagehide", () => {
  try { navigator.sendBeacon("/api/window-closed"); } catch { /* ignore */ }
});
function scheduleLiveRefresh() {
  clearTimeout(refreshDebounce);
  refreshDebounce = setTimeout(() => {
    if (busyModalOpen()) return;
    if (DRAWER && !REVIEW) { reloadDrawer(); }
    load();
  }, 350);
}

let autoFetchTimer = null;
function restartAutoFetch() {
  clearInterval(autoFetchTimer);
  if (SETTINGS.autoFetchMin > 0) {
    autoFetchTimer = setInterval(() => {
      if (document.visibilityState === "visible") batch("fetch", {}, true);
    }, SETTINGS.autoFetchMin * 60000);
  }
}

async function checkForUpdate() {
  try {
    const u = await api("GET", "/api/update-check");
    if (u.hasUpdate) {
      const t = toast("run", "Update available", `ChitHub ${u.latest} is out (you have ${u.current}).`);
      t.className = "toast ok";
      const pre = t.querySelector("pre") || t.appendChild(el("pre"));
      pre.textContent = "Click to open the release page.";
      t.style.cursor = "pointer";
      t.onclick = () => window.open(u.url, "_blank");
    }
  } catch { /* offline — ignore */ }
}

// =========================================================================
// Per-repo: sync · publish · open · push-to-main guard
// =========================================================================
function doSync(name) {
  return runOp(name, `sync ${name}`, () => api("POST", `/api/repo/${enc(name)}/sync`, { mode: SETTINGS.defaultPull }));
}
function doPublish(name) {
  return runOp(name, `publish ${name}`, () => api("POST", `/api/repo/${enc(name)}/publish`, {}));
}
async function doOpen(name, target) {
  try {
    const res = await api("POST", `/api/repo/${enc(name)}/open`, { target });
    if (!res.ok) toast("err", "Open failed", res.output);
  } catch (e) { toast("err", "Open failed", e.message); }
}

// =========================================================================
// Branch management
// =========================================================================
function branchMenuItems(d) {
  const cur = d.branches.current;
  return [
    { label: "Publish branch", fn: () => doPublish(d.name) },
    { label: "Rename branch…", fn: () => renameBranchUI(d.name, cur) },
    { label: "Delete a branch…", fn: () => deleteBranchUI(d) },
    { label: "—", sep: true },
    { label: "Merge a branch into " + cur + "…", fn: () => mergeUI(d) },
    { label: "Rebase " + cur + " onto…", fn: () => rebaseUI(d) },
    { label: "—", sep: true },
    { label: "Tags…", fn: () => openTags(d.name) },
  ];
}
async function renameBranchUI(name, cur) {
  const to = await promptDialog("Rename branch", { label: "New name for “" + cur + "”", value: cur, okLabel: "Rename" });
  if (!to || to === cur) return;
  await runOp(name, `rename → ${to}`, () => api("POST", `/api/repo/${enc(name)}/branch-rename`, { from: cur, to }));
  if (DRAWER && DRAWER.name === name) reloadDrawer();
}
async function deleteBranchUI(d) {
  const others = (d.branches.local || []).filter((b) => b !== d.branches.current);
  if (!others.length) { toast("err", "No other branches", "There are no other local branches to delete."); return; }
  const branch = await pickDialog("Delete a branch", "Choose a local branch to delete:", others);
  if (!branch) return;
  const remote = await confirmDialog("Delete branch", `Also delete the remote branch origin/${branch}?`, { okLabel: "Local + remote", cancelLabel: "Local only" });
  await runOp(d.name, `delete ${branch}`, async () => {
    const r1 = await api("POST", `/api/repo/${enc(d.name)}/branch-delete`, { branch, force: true });
    if (remote) await api("POST", `/api/repo/${enc(d.name)}/branch-delete`, { branch, remote: true }).catch(() => {});
    return r1;
  });
  if (DRAWER && DRAWER.name === d.name) reloadDrawer();
}
async function mergeUI(d) {
  const others = (d.branches.local || []).concat(d.branches.remote || []).filter((b) => b !== d.branches.current);
  const branch = await pickDialog("Merge branch", `Merge which branch into ${d.branches.current}?`, others);
  if (!branch) return;
  await runOp(d.name, `merge ${branch}`, () => api("POST", `/api/repo/${enc(d.name)}/merge`, { branch }));
  if (DRAWER && DRAWER.name === d.name) reloadDrawer();
}
async function rebaseUI(d) {
  const others = (d.branches.local || []).concat(d.branches.remote || []).filter((b) => b !== d.branches.current);
  const branch = await pickDialog("Rebase", `Rebase ${d.branches.current} onto which branch?`, others);
  if (!branch) return;
  await runOp(d.name, `rebase onto ${branch}`, () => api("POST", `/api/repo/${enc(d.name)}/rebase`, { branch }));
  if (DRAWER && DRAWER.name === d.name) reloadDrawer();
}

// =========================================================================
// Tags
// =========================================================================
async function openTags(name) {
  const s = sheet({ title: "Tags — " + name });
  const add = el("button", "btn small primary", "＋ New tag");
  s.head.insertBefore(add, s.head.querySelector(".close"));
  const list = el("div", "tag-list");
  s.body.appendChild(list);
  async function refresh() {
    list.innerHTML = "Loading…";
    const data = await api("GET", `/api/repo/${enc(name)}/tags`);
    list.innerHTML = "";
    const tags = data.tags || [];
    if (!tags.length) list.appendChild(el("div", "sub", "No tags yet."));
    for (const tg of tags) {
      const it = el("div", "tag-item");
      it.appendChild(el("div", "tag-name mono", esc(tg.name)));
      it.appendChild(el("div", "tag-subj", esc(tg.subject || tg.short)));
      const del = el("button", "icbtn danger", "🗑");
      del.title = "Delete tag";
      del.onclick = async () => {
        if (!(await confirmDialog("Delete tag", `Delete tag ${tg.name}?`, { okLabel: "Delete", danger: true }))) return;
        const push = await confirmDialog("Delete tag", "Also delete it on origin?", { okLabel: "Local + remote", cancelLabel: "Local only" });
        await api("POST", `/api/repo/${enc(name)}/tag`, { action: "delete", name: tg.name, push });
        refresh();
      };
      it.appendChild(del);
      list.appendChild(it);
    }
  }
  add.onclick = async () => {
    const tag = await promptDialog("New tag", { label: "Tag name", placeholder: "v1.0.0", okLabel: "Next" });
    if (!tag) return;
    const msg = await promptDialog("New tag", { label: "Message (optional, for an annotated tag)", placeholder: "Release 1.0.0", okLabel: "Create" });
    const push = await confirmDialog("New tag", "Push the tag to origin?", { okLabel: "Create + push", cancelLabel: "Create locally" });
    const res = await api("POST", `/api/repo/${enc(name)}/tag`, { action: "create", name: tag, message: msg || "", push });
    if (!res.ok) toast("err", "Tag failed", res.output); else refresh();
  };
  refresh();
}

// =========================================================================
// History view + graph
// =========================================================================
async function openHistory(name) {
  const s = sheet({ title: "History — " + name, wide: true });
  const search = el("input", "sheet-search");
  search.type = "search"; search.placeholder = "Search messages…";
  s.head.insertBefore(search, s.head.querySelector(".close"));
  const list = el("div", "history-list");
  s.body.appendChild(list);
  const state = { skip: 0, q: "", done: false, loading: false, all: [] };
  async function loadMore(reset) {
    if (state.loading || (state.done && !reset)) return;
    state.loading = true;
    if (reset) { state.skip = 0; state.done = false; state.all = []; list.innerHTML = ""; }
    try {
      const data = await api("GET", `/api/repo/${enc(name)}/history?skip=${state.skip}&limit=80&q=${enc(state.q)}`);
      const commits = data.commits || [];
      if (commits.length < 80) state.done = true;
      state.skip += commits.length;
      state.all.push(...commits);
      if (state.all.length >= 600) state.done = true; // cap infinite scroll
      list.innerHTML = "";
      renderHistory(name, list, state.all);
    } catch (e) { list.innerHTML = ""; list.appendChild(el("div", "sub", "Error: " + esc(e.message))); }
    state.loading = false;
  }
  search.oninput = debounce(() => { state.q = search.value.trim(); loadMore(true); }, 250);
  s.body.onscroll = () => {
    if (!state.done && !state.loading && s.body.scrollTop + s.body.clientHeight > s.body.scrollHeight - 240) loadMore(false);
  };
  loadMore(true);
}

const LANE_COLORS = ["#2f81f7", "#3fb950", "#a371f7", "#db8c3a", "#f85149", "#d29922", "#39c5cf", "#ec6cb9"];
function renderHistory(name, list, commits) {
  const rows = computeGraph(commits);
  const laneW = 14;
  for (const r of rows) {
    const c = r.commit;
    const it = el("div", "hrow");
    it.appendChild(graphCell(r, laneW));
    const body = el("div", "hbody");
    const top = el("div", "htop");
    top.appendChild(el("span", "hsubj", esc(c.subject)));
    for (const ref of (c.refs || [])) {
      const isTag = ref.startsWith("tag: ");
      body && top.appendChild(el("span", "ref" + (isTag ? " tag" : ""), esc(isTag ? ref.slice(5) : ref)));
    }
    body.appendChild(top);
    body.appendChild(el("div", "hmeta", `${esc(c.short)} · ${esc(c.author)} · ${relTime(c.time)}` + ((c.parents || []).length > 1 ? " · merge" : "")));
    it.appendChild(body);
    const more = el("button", "btn small ghost more", "⋯");
    it.appendChild(attachMenu(more, [
      { label: "View changes", fn: () => openShow(name, c) },
      { label: "Copy hash", fn: () => navigator.clipboard && navigator.clipboard.writeText(c.hash) },
      { label: "—", sep: true },
      { label: "Revert this commit", fn: () => commitAction(name, "revert", c) },
      { label: "Cherry-pick onto current", fn: () => commitAction(name, "cherry-pick", c) },
      { label: "—", sep: true },
      { label: "Reset branch to here…", fn: () => resetUI(name, c) },
      { label: "—", sep: true },
      { label: "Tag this commit…", fn: () => tagCommit(name, c) },
    ]));
    it.onclick = (e) => { if (!e.target.closest(".split")) openShow(name, c); };
    list.appendChild(it);
  }
}

// computeGraph assigns each commit a lane and records the lanes passing through
// before/after, so we can draw a railroad-style graph.
function computeGraph(commits) {
  const lanes = []; // lane -> hash it's waiting for
  const rows = [];
  for (const c of commits) {
    let lane = lanes.indexOf(c.hash);
    if (lane === -1) {
      lane = lanes.indexOf(null);
      if (lane === -1) { lane = lanes.length; lanes.push(null); }
      lanes[lane] = c.hash;
    }
    const before = lanes.slice();
    const parents = c.parents || [];
    // clear any other lanes also waiting for this hash (they merge in here)
    for (let i = 0; i < lanes.length; i++) if (i !== lane && lanes[i] === c.hash) lanes[i] = null;
    if (parents.length === 0) {
      lanes[lane] = null;
    } else {
      lanes[lane] = parents[0];
      for (let k = 1; k < parents.length; k++) {
        let pl = lanes.indexOf(parents[k]);
        if (pl === -1) { pl = lanes.indexOf(null); if (pl === -1) { pl = lanes.length; lanes.push(null); } lanes[pl] = parents[k]; }
      }
    }
    rows.push({ commit: c, lane, before, after: lanes.slice() });
  }
  return rows;
}

function graphCell(r, laneW) {
  const width = Math.max(r.before.length, r.after.length, r.lane + 1) * laneW + laneW;
  const h = 46, mid = h / 2;
  const color = (i) => LANE_COLORS[i % LANE_COLORS.length];
  const x = (i) => i * laneW + laneW / 2;
  let svg = `<svg width="${width}" height="${h}" class="graph">`;
  // top half: lanes coming in
  for (let i = 0; i < r.before.length; i++) {
    if (!r.before[i]) continue;
    const target = r.before[i] === r.commit.hash ? r.lane : i;
    svg += `<path d="M${x(i)} 0 L${x(target)} ${mid}" stroke="${color(target)}" />`;
  }
  // bottom half: lanes going out
  for (let i = 0; i < r.after.length; i++) {
    if (!r.after[i]) continue;
    let from = r.lane;
    if (r.before[i] && r.before[i] !== r.commit.hash && r.after[i] === r.before[i]) from = i;
    svg += `<path d="M${x(from)} ${mid} L${x(i)} ${h}" stroke="${color(i)}" />`;
  }
  const merge = (r.commit.parents || []).length > 1;
  svg += `<circle cx="${x(r.lane)}" cy="${mid}" r="${merge ? 5 : 4}" fill="${color(r.lane)}" stroke="var(--diff-bg)" stroke-width="1.5"/>`;
  svg += `</svg>`;
  const cell = el("div", "graph-cell");
  cell.innerHTML = svg;
  return cell;
}

async function commitAction(name, action, c) {
  const verb = action === "revert" ? "Revert" : "Cherry-pick";
  if (!(await confirmDialog(verb, `${verb} ${c.short} — “${c.subject}”?`, { okLabel: verb }))) return;
  await runOp(name, `${action} ${c.short}`, () => api("POST", `/api/repo/${enc(name)}/${action}`, { hash: c.hash }));
}
async function resetUI(name, c) {
  const opts = await resetDialog(name, c);
  if (!opts) return;
  await runOp(name, `reset ${opts.mode} ${c.short}`,
    () => api("POST", `/api/repo/${enc(name)}/reset`, { hash: c.hash, mode: opts.mode, backup: opts.backup }));
}

// A guided reset dialog: pick soft / mixed / hard, each spelled out in plain
// language, with an optional safety backup branch so a hard reset never loses
// work irrecoverably. Resolves to { mode, backup } or null if cancelled.
function resetDialog(name, c) {
  return new Promise((resolve) => {
    const MODES = [
      { id: "soft", title: "Soft", tag: "keeps your work · staged",
        desc: "Move the branch to this commit but keep every later change staged and ready to re-commit. Nothing is lost." },
      { id: "mixed", title: "Mixed", tag: "keeps your work · unstaged",
        desc: "Move the branch to this commit and keep every later change in your working tree, unstaged. Nothing is lost." },
      { id: "hard", title: "Hard", tag: "discards your work", danger: true,
        desc: "Move the branch to this commit and permanently discard every change and commit that came after it." },
    ];
    let mode = "mixed";
    let backup = false;

    const modal = el("div", "modal dialog-modal");
    const backdrop = el("div", "modal-backdrop");
    const card = el("div", "modal-card dialog-card reset-card");
    card.appendChild(el("h3", null, `Reset “${esc(name)}”`));
    card.appendChild(el("div", "dialog-msg",
      "Move this branch to the commit below. Choose what happens to the work that comes after it."));
    card.appendChild(el("div", "reset-target",
      `<span class="mono reset-target-hash">${esc(c.short)}</span><span class="reset-target-subj">${esc(c.subject)}</span>`));

    const modes = el("div", "reset-modes");
    const cards = {};
    for (const m of MODES) {
      const mc = el("button", "reset-mode" + (m.danger ? " danger" : ""),
        `<span class="reset-mode-head"><b>${esc(m.title)}</b><span class="reset-mode-tag">${esc(m.tag)}</span></span>
         <span class="reset-mode-desc">${esc(m.desc)}</span>`);
      mc.type = "button";
      mc.onclick = () => { mode = m.id; render(); };
      cards[m.id] = mc;
      modes.appendChild(mc);
    }
    card.appendChild(modes);

    const warn = el("div", "reset-warn",
      "⚠ A hard reset deletes uncommitted changes for good. Keep the safety branch on below to stay recoverable.");
    card.appendChild(warn);

    const safety = el("label", "reset-safety");
    const sw = el("span", "switch"); sw.appendChild(el("i"));
    safety.appendChild(sw);
    safety.appendChild(el("span", "reset-safety-text",
      `<b>Create a safety backup branch first</b><span>A <code>chithub/backup-…</code> branch will mark your current position so you can always come back.</span>`));
    safety.onclick = (e) => { e.preventDefault(); backup = !backup; sw.classList.toggle("on", backup); };
    card.appendChild(safety);

    const actions = el("div", "modal-actions");
    const cancel = el("button", "btn ghost", "Cancel");
    const ok = el("button", "btn primary", "Reset");
    actions.appendChild(cancel); actions.appendChild(ok);
    card.appendChild(actions);

    function render() {
      for (const m of MODES) cards[m.id].classList.toggle("sel", m.id === mode);
      const danger = mode === "hard";
      warn.classList.toggle("show", danger);
      ok.className = "btn " + (danger ? "danger" : "primary");
      ok.textContent = "Reset (" + mode + ")";
      if (danger && !backup) { backup = true; sw.classList.add("on"); } // default-safe on hard
    }

    const finish = (val) => { modal.remove(); document.removeEventListener("keydown", onKey, true); resolve(val); };
    cancel.onclick = () => finish(null);
    ok.onclick = () => finish({ mode, backup });
    backdrop.onclick = () => finish(null);
    const onKey = (e) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); finish(null); }
      else if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); finish({ mode, backup }); }
    };
    document.addEventListener("keydown", onKey, true);

    modal.appendChild(backdrop); modal.appendChild(card);
    document.body.appendChild(modal);
    render();
    setTimeout(() => ok.focus(), 20);
  });
}
async function tagCommit(name, c) {
  const tag = await promptDialog("Tag commit", { label: `Tag name for ${c.short}`, placeholder: "v1.0.0", okLabel: "Next" });
  if (!tag) return;
  const push = await confirmDialog("Tag commit", "Push the tag to origin?", { okLabel: "Create + push", cancelLabel: "Create locally" });
  const res = await api("POST", `/api/repo/${enc(name)}/tag`, { action: "create", name: tag, ref: c.hash, push });
  if (!res.ok) toast("err", "Tag failed", res.output); else toast("ok", "Tag created", `${tag} → ${c.short}`);
}

// =========================================================================
// Conflict resolution
// =========================================================================
async function openConflicts(name) {
  const cs = await api("GET", `/api/repo/${enc(name)}/conflicts`);
  if (!cs.files || !cs.files.length) { toast("ok", "No conflicts", "Nothing to resolve."); if (DRAWER) reloadDrawer(); return; }
  const s = sheet({ title: `Resolve conflicts — ${name}`, wide: true });
  const op = cs.inProgress || "merge";
  const banner = el("div", "conflict-banner");
  banner.textContent = `A ${op} is in progress with ${cs.files.length} conflicted file(s).`;
  s.body.appendChild(banner);
  const list = el("div", "conflict-files");
  s.body.appendChild(list);
  for (const path of cs.files) {
    const it = el("div", "cf-row");
    it.appendChild(el("span", "fpath mono", esc(path)));
    const tools = el("span", "cf-tools");
    tools.appendChild(smallBtn("Use ours", () => resolve(path, "ours")));
    tools.appendChild(smallBtn("Use theirs", () => resolve(path, "theirs")));
    tools.appendChild(smallBtn("Edit…", () => editConflict(name, path, refresh)));
    tools.appendChild(smallBtn("Mark resolved", () => resolve(path, "mark")));
    it.appendChild(tools);
    list.appendChild(it);
  }
  const actions = el("div", "conflict-actions");
  actions.appendChild(actBtn("Continue " + op, "primary", () => seq("continue")));
  actions.appendChild(actBtn("Abort " + op, "danger", () => seq("abort")));
  if (op === "rebase" || op === "cherry-pick") actions.appendChild(actBtn("Skip", "ghost", () => seq("skip")));
  s.body.appendChild(actions);

  async function resolve(path, side) {
    await api("POST", `/api/repo/${enc(name)}/resolve`, { path, side });
    refresh();
  }
  async function seq(action) {
    const res = await api("POST", `/api/repo/${enc(name)}/sequencer`, { op, action });
    toast(res.ok ? "ok" : "err", `${op} ${action}`, res.output || "done");
    s.close(); if (DRAWER) reloadDrawer(); load();
  }
  function refresh() { s.close(); openConflicts(name); }
}

async function editConflict(name, path, after) {
  const cf = await api("GET", `/api/repo/${enc(name)}/conflict?path=${enc(path)}`);
  const s = sheet({ title: "Edit — " + path, wide: true });
  const ta = el("textarea", "conflict-edit");
  ta.value = cf.merged || "";
  s.body.appendChild(el("div", "sub", "Remove the <<<<<<< ======= >>>>>>> markers, keep what you want, then save."));
  s.body.appendChild(ta);
  const save = actBtn("Save & mark resolved", "primary", async () => {
    await api("POST", `/api/repo/${enc(name)}/resolve`, { path, side: "content", content: ta.value });
    s.close(); after && after();
  });
  const bar = el("div", "conflict-actions"); bar.appendChild(save);
  s.body.appendChild(bar);
}

// =========================================================================
// Multi-repo bulk operations
// =========================================================================
function renderBulkBar() {
  let bar = $("#bulkBar");
  const n = SELECTED.size;
  if (!n) { if (bar) bar.remove(); return; }
  if (!bar) {
    bar = el("div", "bulk-bar"); bar.id = "bulkBar";
    document.body.appendChild(bar);
  }
  bar.innerHTML = "";
  bar.appendChild(el("span", "bulk-count", `${n} selected`));
  const acts = el("div", "bulk-acts");
  acts.appendChild(actBtn("Sync", "ghost", () => bulkOp("sync")));
  acts.appendChild(actBtn("Commit…", "primary", bulkCommitUI));
  acts.appendChild(actBtn("Switch branch…", "ghost", bulkCheckoutUI));
  acts.appendChild(actBtn("Run…", "ghost", runCommandUI));
  acts.appendChild(actBtn("Clear", "ghost", () => { SELECTED.clear(); render(); }));
  bar.appendChild(acts);
}
async function bulkOp(action) {
  if (action === "sync") {
    const repos = [...SELECTED];
    const tt = toast("run", `sync → ${repos.length} repos`, "running…");
    repos.forEach((n) => setBusy(n, true));
    const results = await Promise.all(repos.map((n) => api("POST", `/api/repo/${enc(n)}/sync`, { mode: SETTINGS.defaultPull }).catch((e) => ({ ok: false, repo: n, output: e.message }))));
    repos.forEach((n) => setBusy(n, false));
    const ok = results.filter((r) => r.ok).length;
    finishToast(tt, { ok: ok === results.length, output: `${ok}/${results.length} synced` });
    load();
  }
}
async function bulkCommitUI() {
  const repos = [...SELECTED];
  const dirty = REPOS.filter((r) => repos.includes(r.name) && r.dirty).map((r) => r.name);
  if (!dirty.length) { toast("err", "Nothing to commit", "None of the selected repos have changes."); return; }
  const msg = await promptDialog("Commit across repos", {
    message: `Stage all changes and commit in ${dirty.length} repo(s) with one message:`,
    label: "Commit message", placeholder: "Apply the same change everywhere", okLabel: "Commit",
  });
  if (!msg) return;
  const push = await confirmDialog("Commit across repos", "Push after committing?", { okLabel: "Commit + push", cancelLabel: "Commit only" });
  const tt = toast("run", `commit → ${dirty.length} repos`, "running…");
  dirty.forEach((n) => setBusy(n, true));
  try {
    const data = await api("POST", "/api/bulk/commit", { repos: dirty, message: msg, push });
    const ok = (data.results || []).filter((r) => r.ok).length;
    finishToast(tt, { ok: ok === data.results.length, output: bulkOutput(data.results) });
  } catch (e) { finishToast(tt, { ok: false, output: e.message }); }
  finally { dirty.forEach((n) => setBusy(n, false)); load(); }
}
async function bulkCheckoutUI() {
  const repos = [...SELECTED];
  const branch = await promptDialog("Switch branch across repos", {
    message: `Checkout the same branch in ${repos.length} repo(s).`,
    label: "Branch name", placeholder: "main", okLabel: "Switch",
  });
  if (!branch) return;
  const create = await confirmDialog("Switch branch", `If “${branch}” doesn't exist in a repo, create it there?`, { okLabel: "Create if missing", cancelLabel: "Only if it exists" });
  const tt = toast("run", `checkout ${branch} → ${repos.length} repos`, "running…");
  repos.forEach((n) => setBusy(n, true));
  try {
    const data = await api("POST", "/api/bulk/checkout", { repos, branch, create });
    const ok = (data.results || []).filter((r) => r.ok).length;
    finishToast(tt, { ok: ok === data.results.length, output: bulkOutput(data.results) });
  } catch (e) { finishToast(tt, { ok: false, output: e.message }); }
  finally { repos.forEach((n) => setBusy(n, false)); load(); }
}
async function runCommandUI() {
  const repos = SELECTED.size ? [...SELECTED] : REPOS.filter(passesFilter).map((r) => r.name);
  const cmd = await promptDialog("Run a command", {
    message: `Run a shell command in ${repos.length} repo(s). Each runs in its own folder.`,
    label: "Command", placeholder: "git status -s", okLabel: "Run",
  });
  if (!cmd) return;
  const s = sheet({ title: `Run: ${cmd}`, wide: true });
  s.body.appendChild(el("div", "sub", `Running in ${repos.length} repos…`));
  const grid = el("div", "run-grid"); s.body.appendChild(grid);
  try {
    const data = await api("POST", "/api/run", { repos, cmd });
    grid.innerHTML = "";
    for (const r of (data.results || [])) {
      const card = el("div", "run-card" + (r.ok ? "" : " fail"));
      card.appendChild(el("div", "run-repo", esc(r.repo) + (r.ok ? " ✓" : " ✗")));
      const pre = el("pre"); pre.textContent = r.output || "(no output)"; card.appendChild(pre);
      grid.appendChild(card);
    }
  } catch (e) { grid.innerHTML = ""; grid.appendChild(el("div", "sub", "Error: " + esc(e.message))); }
}
function bulkOutput(results) {
  return (results || []).map((r) => `${r.ok ? "✓" : "✗"} ${r.repo}: ${firstLine(r.output)}`).join("\n");
}

// Cross-repo search
async function openSearch(initial) {
  const s = sheet({ title: "Search across repos", wide: true });
  const input = el("input", "sheet-search big");
  input.type = "search"; input.placeholder = "Search code in every repo…"; input.value = initial || "";
  s.head.insertBefore(input, s.head.querySelector(".close"));
  const results = el("div", "search-results"); s.body.appendChild(results);
  const run = debounce(async () => {
    const q = input.value.trim();
    if (q.length < 2) { results.innerHTML = ""; return; }
    results.innerHTML = "<div class='sub'>Searching…</div>";
    try {
      const data = await api("GET", "/api/search?q=" + enc(q));
      const hits = data.hits || [];
      results.innerHTML = "";
      if (!hits.length) { results.appendChild(el("div", "sub", "No matches.")); return; }
      const byRepo = {};
      for (const h of hits) (byRepo[h.repo] = byRepo[h.repo] || []).push(h);
      results.appendChild(el("div", "sub", `${hits.length} matches in ${Object.keys(byRepo).length} repos`));
      for (const repo of Object.keys(byRepo).sort()) {
        results.appendChild(el("div", "search-repo", esc(repo)));
        for (const h of byRepo[repo].slice(0, 50)) {
          const it = el("div", "search-hit");
          it.innerHTML = `<span class="sh-loc mono">${esc(h.path)}:${h.line}</span><span class="sh-text mono">${esc(h.text.slice(0, 200))}</span>`;
          it.onclick = () => doOpen(repo, "editor");
          it.appendChild(el("span", ""));
          results.appendChild(it);
        }
      }
    } catch (e) { results.innerHTML = ""; results.appendChild(el("div", "sub", "Error: " + esc(e.message))); }
  }, 280);
  input.oninput = run;
  setTimeout(() => input.focus(), 30);
  if (initial) run();
}

// Workspace snapshots
async function snapshotSave() {
  const data = await api("GET", "/api/snapshot");
  const entries = data.entries || [];
  const json = JSON.stringify(entries);
  localStorage.setItem("chithub:snapshot", json);
  toast("ok", "Snapshot saved", `Recorded the branch of ${entries.length} repos. Restore it anytime.`);
}
async function snapshotRestore() {
  const json = localStorage.getItem("chithub:snapshot");
  if (!json) { toast("err", "No snapshot", "Save a snapshot first."); return; }
  let entries; try { entries = JSON.parse(json); } catch { toast("err", "Bad snapshot", "Could not read it."); return; }
  if (!(await confirmDialog("Restore snapshot", `Switch ${entries.length} repos back to their saved branches?`, { okLabel: "Restore" }))) return;
  const tt = toast("run", "restore snapshot", "switching branches…");
  try {
    const res = await api("POST", "/api/snapshot/restore", { entries });
    const ok = (res.results || []).filter((r) => r.ok).length;
    finishToast(tt, { ok: ok === res.results.length, output: bulkOutput(res.results) });
  } catch (e) { finishToast(tt, { ok: false, output: e.message }); }
  load();
}

// =========================================================================
// Command palette
// =========================================================================
function paletteCommands() {
  const cmds = [
    { name: "Refresh", run: load },
    { name: "Clone repository…", run: openClone },
    { name: "Settings…", run: openSettings },
    { name: "Toggle theme (dark/light)", run: () => { SETTINGS.theme = SETTINGS.theme === "dark" ? "light" : "dark"; applySettings(); saveSettings(); } },
    { name: "Search across repos…", run: () => openSearch("") },
    { name: "Run command in repos…", run: runCommandUI },
    { name: "Save workspace snapshot", run: snapshotSave },
    { name: "Restore workspace snapshot", run: snapshotRestore },
    { name: "Review commits & pushes", run: () => startReview("commit") },
    { name: "Review pulls", run: () => startReview("pull") },
    { name: "Fetch all", run: () => batch("fetch", {}) },
    { name: "Pull all", run: () => batch("pull", { mode: SETTINGS.defaultPull }) },
    { name: "Push all", run: () => batch("push", { force: false }) },
  ];
  for (const r of REPOS) {
    cmds.push({ name: "Open " + r.name, hint: "repo", run: () => openDrawer(r.name, { commit: true }) });
    cmds.push({ name: "History " + r.name, hint: "repo", run: () => openHistory(r.name) });
  }
  return cmds;
}
function openPalette() {
  if (document.querySelector(".palette")) return;
  closeAllMenus();
  const cmds = paletteCommands();
  const modal = el("div", "palette");
  const backdrop = el("div", "modal-backdrop");
  const card = el("div", "palette-card");
  const input = el("input", "palette-input");
  input.type = "text"; input.placeholder = "Type a command or repo…  (↑↓ to move, Enter to run)";
  const listEl = el("div", "palette-list");
  card.appendChild(input); card.appendChild(listEl);
  modal.appendChild(backdrop); modal.appendChild(card);
  document.body.appendChild(modal);
  let filtered = cmds, sel = 0;
  const close = () => { modal.remove(); document.removeEventListener("keydown", onKey, true); };
  function fuzzy(q, s) {
    q = q.toLowerCase(); s = s.toLowerCase();
    if (!q) return true;
    let i = 0; for (const ch of s) { if (ch === q[i]) i++; if (i === q.length) return true; }
    return s.includes(q);
  }
  function renderList() {
    listEl.innerHTML = "";
    filtered.slice(0, 60).forEach((c, i) => {
      const it = el("div", "palette-item" + (i === sel ? " sel" : ""));
      it.appendChild(el("span", "pi-name", esc(c.name)));
      if (c.hint) it.appendChild(el("span", "pi-hint", esc(c.hint)));
      it.onmouseenter = () => { sel = i; markSel(); };
      it.onclick = () => { close(); c.run(); };
      listEl.appendChild(it);
    });
  }
  function markSel() {
    [...listEl.children].forEach((c, i) => c.classList.toggle("sel", i === sel));
    const cur = listEl.children[sel]; if (cur) cur.scrollIntoView({ block: "nearest" });
  }
  input.oninput = () => { const q = input.value.trim(); filtered = cmds.filter((c) => fuzzy(q, c.name)); sel = 0; renderList(); };
  const onKey = (e) => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); }
    else if (e.key === "ArrowDown") { e.preventDefault(); sel = Math.min(sel + 1, Math.min(filtered.length, 60) - 1); markSel(); }
    else if (e.key === "ArrowUp") { e.preventDefault(); sel = Math.max(sel - 1, 0); markSel(); }
    else if (e.key === "Enter") { e.preventDefault(); const c = filtered[sel]; if (c) { close(); c.run(); } }
  };
  document.addEventListener("keydown", onKey, true);
  backdrop.onclick = close;
  renderList();
  setTimeout(() => input.focus(), 20);
}

// =========================================================================
// Reusable full-screen sheet + small helpers
// =========================================================================
function sheet(opts) {
  opts = opts || {};
  const modal = el("div", "modal sheet-modal");
  const backdrop = el("div", "modal-backdrop");
  const card = el("div", "modal-card sheet" + (opts.wide ? " sheet-wide" : ""));
  const head = el("div", "sheet-head");
  head.appendChild(el("h3", null, esc(opts.title || "")));
  const close = el("button", "close", "×");
  const finish = () => { modal.remove(); document.removeEventListener("keydown", onKey, true); opts.onClose && opts.onClose(); };
  close.onclick = finish;
  head.appendChild(close);
  card.appendChild(head);
  const body = el("div", "sheet-body");
  card.appendChild(body);
  backdrop.onclick = finish;
  // Don't close the sheet on Escape while a dialog, the palette, or a commit-view
  // modal is open on top of it — let that close first.
  const onKey = (e) => {
    if (e.key !== "Escape") return;
    if (document.querySelector(".dialog-modal") || document.querySelector(".palette") || !$("#showModal").hidden) return;
    e.preventDefault(); e.stopPropagation(); finish();
  };
  document.addEventListener("keydown", onKey, true);
  modal.appendChild(backdrop); modal.appendChild(card);
  document.body.appendChild(modal);
  return { modal, body, head, close: finish };
}

const smallBtn = (label, fn) => { const b = el("button", "btn small ghost", esc(label)); b.onclick = fn; return b; };

// pickDialog: choose one value from a list via a styled dialog.
function pickDialog(title, message, options) {
  return new Promise((resolve) => {
    let settled = false;
    const s = sheet({ title, onClose: () => { if (!settled) { settled = true; resolve(null); } } });
    if (message) s.body.appendChild(el("div", "sub", message));
    const list = el("div", "pick-list");
    for (const opt of options) {
      const b = el("button", "pick-item mono", esc(opt));
      b.onclick = () => { settled = true; resolve(opt); s.close(); };
      list.appendChild(b);
    }
    s.body.appendChild(list);
  });
}

function debounce(fn, ms) {
  let h = null;
  return (...a) => { clearTimeout(h); h = setTimeout(() => fn(...a), ms); };
}

init();
