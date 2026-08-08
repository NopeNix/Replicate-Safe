(() => {
  "use strict";

  // ----- state -----
  let entries = [];
  let selectedId = null;
  let filterText = "";
  let nextRefreshAt = 0;

  // ----- elements -----
  const $list = document.getElementById("list");
  const $empty = document.getElementById("empty");
  const $filter = document.getElementById("filter");
  const $refresh = document.getElementById("refresh");
  const $theme = document.getElementById("theme");
  const $preview = document.getElementById("preview");
  const $meta = document.getElementById("meta");
  const $metaContent = document.getElementById("meta-content");
  const $root = document.documentElement;

  // ----- theme -----
  const THEME_KEY = "replicate-safe-theme";
  const storedTheme = localStorage.getItem(THEME_KEY);
  if (storedTheme === "light" || storedTheme === "dark") {
    $root.setAttribute("data-theme", storedTheme);
  } else {
    $root.setAttribute("data-theme", "auto");
  }
  $theme.addEventListener("click", () => {
    const cur = $root.getAttribute("data-theme");
    let next;
    if (cur === "auto") next = "light";
    else if (cur === "light") next = "dark";
    else next = "auto";
    if (next === "auto") localStorage.removeItem(THEME_KEY);
    else localStorage.setItem(THEME_KEY, next);
    $root.setAttribute("data-theme", next);
  });

  // ----- helpers -----
  const fmtSize = (n) => {
    if (!n && n !== 0) return "—";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    return (i === 0 ? n : n.toFixed(n >= 100 ? 0 : n >= 10 ? 1 : 2)) + " " + units[i];
  };

  const fmtDate = (iso) => {
    if (!iso) return "—";
    const d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    const pad = (n) => String(n).padStart(2, "0");
    return (
      d.getFullYear() + "-" +
      pad(d.getMonth() + 1) + "-" +
      pad(d.getDate()) + " " +
      pad(d.getHours()) + ":" +
      pad(d.getMinutes())
    );
  };

  const fmtDuration = (secs) => {
    if (!secs || secs <= 0) return "—";
    if (secs < 1) return (secs * 1000).toFixed(0) + "ms";
    if (secs < 60) return secs.toFixed(secs < 10 ? 2 : 1) + "s";
    const m = Math.floor(secs / 60);
    const s = Math.round(secs % 60);
    return m + "m " + s + "s";
  };

  const statusBadge = (s) => {
    const span = document.createElement("span");
    span.className = "badge " + (s === "succeeded" ? "ok" : (s === "failed" || s === "canceled") ? "fail" : "");
    span.textContent = s || "—";
    return span;
  };

  const matches = (e, q) => {
    if (!q) return true;
    const hay = (
      (e.id || "") + " " +
      (e.model || "") + " " +
      (e.status || "") + " " +
      (e.filename || "") + " " +
      (e.version || "")
    ).toLowerCase();
    return hay.includes(q);
  };

  // ----- list rendering -----
  const renderList = () => {
    $list.innerHTML = "";
    const q = filterText.trim().toLowerCase();
    let count = 0;
    for (const e of entries) {
      if (!matches(e, q)) continue;
      count++;
      const li = document.createElement("li");
      li.dataset.id = e.id;
      if (e.id === selectedId) li.classList.add("selected");

      const cName = document.createElement("span");
      cName.className = "col-name";
      cName.textContent = e.filename;
      cName.title = e.filename;
      li.appendChild(cName);

      const cModel = document.createElement("span");
      cModel.className = "col-model";
      cModel.textContent = e.model || "—";
      cModel.title = e.model;
      li.appendChild(cModel);

      const cStatus = document.createElement("span");
      cStatus.className = "col-status";
      cStatus.appendChild(statusBadge(e.status));
      li.appendChild(cStatus);

      const cTime = document.createElement("span");
      cTime.className = "col-time";
      cTime.textContent = fmtDuration(e.time_to_make);
      cTime.title = "created " + fmtDate(e.created_at);
      li.appendChild(cTime);

      const cSize = document.createElement("span");
      cSize.className = "col-size";
      cSize.textContent = fmtSize(e.size);
      li.appendChild(cSize);

      li.addEventListener("click", () => selectEntry(e.id));
      $list.appendChild(li);
    }
    $empty.hidden = count > 0;
  };

  // ----- preview rendering -----
  const renderPreview = (e) => {
    $preview.classList.remove("empty");
    $preview.innerHTML = "";
    if (!e) {
      $preview.classList.add("empty");
      const p = document.createElement("p");
      p.textContent = "Select a file on the left to preview.";
      $preview.appendChild(p);
      return;
    }
    const url = "/file?id=" + encodeURIComponent(e.id);
    let el;
    switch (e.preview_kind) {
      case "image":
        el = document.createElement("img");
        el.alt = e.filename;
        el.src = url;
        break;
      case "video":
        el = document.createElement("video");
        el.src = url;
        el.controls = true;
        el.preload = "metadata";
        break;
      case "audio":
        el = document.createElement("audio");
        el.src = url;
        el.controls = true;
        break;
      case "text": {
        el = document.createElement("iframe");
        el.src = url;
        el.style.width = "100%";
        el.style.height = "60vh";
        el.style.border = "none";
        el.style.background = "var(--hover)";
        break;
      }
      default: {
        const wrap = document.createElement("div");
        wrap.className = "unsupported";
        const p = document.createElement("p");
        p.textContent = "No inline preview for this file type (" + (e.mime || "unknown") + ").";
        const a = document.createElement("a");
        a.href = url;
        a.download = e.filename;
        a.textContent = "Download " + e.filename;
        wrap.appendChild(p);
        wrap.appendChild(a);
        el = wrap;
      }
    }
    $preview.appendChild(el);
  };

  // ----- metadata -----
  const renderMeta = async (id) => {
    try {
      const res = await fetch("/api/metadata?id=" + encodeURIComponent(id));
      if (res.status === 404) {
        $meta.hidden = true;
        return;
      }
      if (!res.ok) {
        $metaContent.textContent = "Failed to load metadata: " + res.status;
        $meta.hidden = false;
        return;
      }
      const text = await res.text();
      // Pretty-print JSON if it parses.
      let pretty = text;
      try {
        pretty = JSON.stringify(JSON.parse(text), null, 2);
      } catch (_) { /* not json, leave raw */ }
      $metaContent.textContent = pretty;
      $meta.hidden = false;
    } catch (err) {
      $metaContent.textContent = "Error: " + err.message;
      $meta.hidden = false;
    }
  };

  // ----- selection -----
  const selectEntry = (id) => {
    const e = entries.find((x) => x.id === id);
    if (!e) return;
    selectedId = id;
    for (const li of $list.querySelectorAll("li")) {
      li.classList.toggle("selected", li.dataset.id === id);
    }
    renderPreview(e);
    renderMeta(id);
    history.replaceState(null, "", "#" + encodeURIComponent(id));
  };

  // ----- data loading -----
  const loadList = async () => {
    try {
      const res = await fetch("/api/predictions", { cache: "no-store" });
      if (!res.ok) throw new Error("HTTP " + res.status);
      const data = await res.json();
      entries = Array.isArray(data) ? data : [];
      renderList();
      if (selectedId && !entries.find((e) => e.id === selectedId)) {
        selectedId = null;
        renderPreview(null);
        $meta.hidden = true;
      }
      nextRefreshAt = Date.now() + 30_000;
    } catch (err) {
      console.error("load list failed", err);
    }
  };

  // ----- events -----
  $filter.addEventListener("input", (ev) => {
    filterText = ev.target.value;
    renderList();
  });

  $refresh.addEventListener("click", () => { loadList(); });

  document.addEventListener("keydown", (ev) => {
    if (ev.target === $filter) return;
    const visible = Array.from($list.querySelectorAll("li"));
    if (visible.length === 0) return;
    let idx = visible.findIndex((li) => li.classList.contains("selected"));
    if (ev.key === "ArrowDown") {
      idx = Math.min(idx + 1, visible.length - 1);
      ev.preventDefault();
      visible[idx].click();
      visible[idx].scrollIntoView({ block: "nearest" });
    } else if (ev.key === "ArrowUp") {
      idx = Math.max(idx - 1, 0);
      ev.preventDefault();
      visible[idx].click();
      visible[idx].scrollIntoView({ block: "nearest" });
    }
  });

  // ----- bootstrap -----
  loadList().then(() => {
    const hashId = decodeURIComponent((location.hash || "").replace(/^#/, ""));
    if (hashId && entries.find((e) => e.id === hashId)) {
      selectEntry(hashId);
    }
  });
  setInterval(() => {
    if (Date.now() >= nextRefreshAt) loadList();
  }, 5_000);
})();
