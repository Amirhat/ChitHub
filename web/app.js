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
  $("#selectCount").textContent = n ? `${n} selected` : "Select all";
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
  actions.appendChild(actBtn("Fetch", "ghost", () => doOp(r.name, "fetch")));
  actions.appendChild(splitButton("Pull", "primary", () => doPull(r.name, "ff"), [
    { label: "Pull (fast-forward only)", fn: () => doPull(r.name, "ff") },
    { label: "Pull --rebase", fn: () => doPull(r.name, "rebase") },
    { label: "Pull (merge)", fn: () => doPull(r.name, "merge") },
  ]));
  actions.appendChild(splitButton("Push", "", () => doPush(r.name, false), [
    { label: "Push", fn: () => doPush(r.name, false) },
    { label: "Force push (--force-with-lease)", fn: () => doPush(r.name, true), danger: true },
  ]));
  if (r.dirty) {
    const c = actBtn("Commit…", "ghost", () => openDrawer(r.name, { commit: true }));
    c.style.color = "var(--orange)";
    actions.appendChild(c);
  }
  const more = actBtn("⋯", "ghost more", null);
  attachMenu(more, [
    { label: "Open details", fn: () => openDrawer(r.name, { commit: true }) },
    { label: "Reveal in Finder", fn: () => doReveal(r.name) },
    { label: "New branch…", fn: () => newBranch(r.name) },
    ...(r.dirty ? [
      { label: "Stash changes", fn: () => doStash(r.name, "push") },
      { label: "Discard all changes…", fn: () => doDiscardAll(r.name), danger: true },
    ] : []),
  ]);
  actions.appendChild(more);

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

function attachMenu(btn, items) {
  const wrap = el("span", "split");
  const menu = el("div", "menu");
  menu.hidden = true;
  for (const it of items) {
    const b = el("button", it.danger ? "danger-item" : "", esc(it.label));
    b.onclick = (e) => { e.stopPropagation(); menu.hidden = true; it.fn(); };
    menu.appendChild(b);
  }
  btn.onclick = (e) => { e.stopPropagation(); closeAllMenus(); menu.hidden = !menu.hidden; };
  btn.replaceWith(wrap);
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
  return runOp(name, `push ${name}`, () => api("POST", `/api/repo/${enc(name)}/push`, { force }));
}
const doStash = (name, action) =>
  runOp(name, `stash ${action} · ${name}`, () => api("POST", `/api/repo/${enc(name)}/stash`, { action }));

async function doReveal(name) {
  try { await api("POST", `/api/repo/${enc(name)}/reveal`, {}); }
  catch (e) { toast("err", "Reveal failed", e.message); }
}
async function doDiscardAll(name) {
  if (!(await confirmDialog("Discard all changes",
    `Discard ALL local changes in ${name}?\nThis can't be undone.`,
    { okLabel: "Discard all", danger: true }))) return;
  return runOp(name, `discard all · ${name}`, () => api("POST", `/api/repo/${enc(name)}/discard`, {}));
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
async function batch(action, extra) {
  const repos = selectedOrAll();
  if (!repos.length) { toast("err", "Nothing to do", "No repositories selected."); return; }
  const label = SELECTED.size ? `${repos.length} selected` : "all visible";
  const t = toast("run", `${action} → ${label}`, `running on ${repos.length} repos…`);
  repos.forEach((n) => setBusy(n, true));
  try {
    const data = await api("POST", "/api/batch", { action, repos, ...extra });
    const results = data.results || [];
    const ok = results.filter((r) => r.ok).length;
    const fail = results.length - ok;
    const lines = results.filter((r) => !r.ok)
      .map((r) => `✗ ${r.repo}: ${firstLine(r.output)}`).join("\n");
    finishToast(t, { ok: fail === 0, output: `${ok} ok, ${fail} failed${lines ? "\n\n" + lines : ""}` });
  } catch (e) {
    finishToast(t, { ok: false, output: e.message });
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
  $("#selectCount").textContent = n ? `${n} selected` : "Select all";
  const visible = REPOS.filter(passesFilter);
  $("#selectAll").checked = visible.length > 0 && visible.every((r) => SELECTED.has(r.name));
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
      files: (data.files || []).map((f) => ({
        path: f.path, code: f.code, untracked: f.code.includes("?"),
        sel: "all", expanded: false, loading: false,
      })),
    };
    renderDrawer();
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
  bar.appendChild(barBtn("Reveal", () => doReveal(d.name)));
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
    if (!(await confirmDialog("Discard changes", `Discard all changes in ${f.path}?\nThis can't be undone.`, { okLabel: "Discard", danger: true }))) return;
    await runOp(DRAWER.name, `discard ${f.path}`,
      () => api("POST", `/api/repo/${enc(DRAWER.name)}/discard`, { paths: [f.path] }));
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
    else for (const h of f.diff.hunks) body.appendChild(hunkView(f, h, f.untracked));
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

function hunkView(f, h, readonly) {
  const block = el("div", "hunk");
  const hh = el("div", "hunk-head");
  if (!readonly) {
    const cb = el("input", "hcb");
    cb.type = "checkbox";
    const st = hunkSelState(h);
    cb.checked = st !== "none";
    cb.indeterminate = st === "partial";
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
  block.appendChild(hh);
  block.appendChild(diffLines(f, h, readonly));
  return block;
}

function diffLines(f, h, readonly) {
  const lines = el("div", "hunk-lines mono");
  for (const ln of h.lines) {
    const changed = ln.t === "+" || ln.t === "-";
    const cls = ln.t === "+" ? "add" : ln.t === "-" ? "del" : "ctx";
    const selectable = changed && !readonly;
    const row = el("div", "dl " + cls + (selectable ? " selectable" : "") + (changed && !ln.sel ? " off" : ""));
    if (selectable) {
      row.title = ln.sel ? "Click to exclude this line from the commit" : "Click to include this line";
      row.onclick = () => { ln.sel = !ln.sel; recomputeSel(f); renderDrawer(); };
      row.appendChild(el("span", "dl-pick", ln.sel ? "✓" : ""));
    } else {
      row.appendChild(el("span", "dl-pick"));
    }
    row.appendChild(el("span", "dl-sign", ln.t === " " ? "" : ln.t));
    row.appendChild(el("span", "dl-text", esc(ln.c) || "&nbsp;"));
    lines.appendChild(row);
  }
  return lines;
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
  for (const h of f.diff.hunks) {
    let oldc = 0, newc = 0, has = false;
    const lines = [];
    for (const ln of h.lines) {
      if (ln.t === " ") { lines.push(" " + ln.c); oldc++; newc++; }
      else if (ln.t === "+") {
        if (ln.sel) { lines.push("+" + ln.c); newc++; has = true; }
      } else if (ln.t === "-") {
        if (ln.sel) { lines.push("-" + ln.c); oldc++; has = true; }
        else { lines.push(" " + ln.c); oldc++; newc++; }
      }
    }
    if (!has) continue;
    const m = h.header.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    const oldStart = m ? m[1] : "1";
    const newStart = m ? m[2] : "1";
    body += `@@ -${oldStart},${oldc} +${newStart},${newc} @@\n` + lines.join("\n") + "\n";
  }
  if (!body) return null;
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
    for (const fd of files) {
      const wrap = el("div", "show-file");
      wrap.appendChild(el("div", "show-fpath mono", esc(fd.path)));
      const body = el("div", "diff");
      if (fd.binary) body.appendChild(el("div", "diff-msg", "Binary file."));
      else if (fd.tooLarge) body.appendChild(el("div", "diff-msg", "Diff too large."));
      else for (const h of fd.hunks) {
        const block = el("div", "hunk");
        block.appendChild(el("div", "hunk-head", `<span class="hunk-header mono">${esc(h.header)}</span>`));
        block.appendChild(diffLines(null, h, true));
        body.appendChild(block);
      }
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

  document.addEventListener("click", closeAllMenus);
  document.addEventListener("keydown", (e) => {
    if (document.querySelector(".dialog-modal")) return; // a styled dialog handles its own keys
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

  loadCollections();
  load();
  setInterval(() => {
    if (document.visibilityState === "visible" && !DRAWER && !REVIEW &&
        $("#cloneModal").hidden && $("#showModal").hidden && !document.querySelector(".dialog-modal")) load();
  }, 60000);
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

init();
