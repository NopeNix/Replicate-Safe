(() => {
  "use strict";

  // ----- state -----
  let entries = [];
  let selectedId = null;
  let nextRefreshAt = 0;
  let searchSeq = 0;
  let sort = { field: "created_at", dir: "desc" }; // default: newest first

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
  const $size = document.getElementById("size");
  const $sizeVal = document.getElementById("size-val");
  const $listHeader = document.getElementById("list-header");
  const $toolbar = document.getElementById("toolbar");
  const $download = document.getElementById("download-btn");
  const $convert = document.getElementById("convert-to");

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

  // ----- thumbnail size slider (cookie-persisted) -----
  // Drives the --thumb-size CSS variable on the file-explorer thumbnail
  // column. The big preview on the right is unaffected.
  const THUMB_COOKIE = "replicate-safe-thumb";
  const THUMB_DEFAULT = 48; // px
  const THUMB_MIN = 20;
  const THUMB_MAX = 100;
  function setThumbSize(px) {
    px = Math.max(THUMB_MIN, Math.min(THUMB_MAX, Math.round(px)));
    $size.value = px;
    $sizeVal.textContent = px + "px";
    document.documentElement.style.setProperty("--thumb-size", px + "px");
    setCookie(THUMB_COOKIE, px);
  }
  setThumbSize(parseInt(getCookie(THUMB_COOKIE), 10) || THUMB_DEFAULT);
  $size.addEventListener("input", (e) => setThumbSize(parseInt(e.target.value, 10)));

  // ----- cookie helpers -----
  function setCookie(name, value) {
    const d = new Date();
    d.setTime(d.getTime() + 365 * 86400 * 1000);
    document.cookie = name + "=" + value + ";expires=" + d.toUTCString() + ";path=/;SameSite=Lax";
  }
  function getCookie(name) {
    const m = document.cookie.match(new RegExp("(?:^|;\\s*)" + name + "\\s*=\\s*([^;]+)"));
    return m ? m[1] : "";
  }

  // ----- helpers -----
  const fmtSize = (n) => {
    if (n == null) return "—";
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
    return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate())
      + " " + pad(d.getHours()) + ":" + pad(d.getMinutes());
  };

  const fmtDateCompact = (iso) => {
    if (!iso) return "—";
    const d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    const pad = (n) => String(n).padStart(2, "0");
    return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate())
      + " " + pad(d.getHours()) + ":" + pad(d.getMinutes());
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

  // ----- sort -----
  const sortEntries = () => {
    const f = sort.field;
    const asc = sort.dir === "asc";
    const get = (e) => {
      switch (f) {
        case "filename": return (e.filename || "").toLowerCase();
        case "model": return (e.model || "").toLowerCase();
        case "status": return (e.status || "").toLowerCase();
        case "time_to_make": return e.time_to_make || 0;
        case "size": return e.size || 0;
        case "created_at": {
          const t = e.created_at ? new Date(e.created_at).getTime() : NaN;
          return isNaN(t) ? null : t;
        }
      }
      return null;
    };
    entries.sort((a, b) => {
      const av = get(a), bv = get(b);
      // nulls/missing always at the bottom regardless of direction
      if (av == null && bv == null) return 0;
      if (av == null) return 1;
      if (bv == null) return -1;
      if (av < bv) return asc ? -1 : 1;
      if (av > bv) return asc ? 1 : -1;
      return 0;
    });
    updateSortHeader();
  };

  const updateSortHeader = () => {
    for (const span of $listHeader.querySelectorAll("span[data-sort]")) {
      const f = span.getAttribute("data-sort");
      const isActive = f === sort.field;
      span.classList.toggle("active", isActive);
      let arrow = span.querySelector(".sort-arrow");
      if (!isActive) {
        if (arrow) arrow.remove();
        continue;
      }
      if (!arrow) {
        arrow = document.createElement("span");
        arrow.className = "sort-arrow";
        span.appendChild(arrow);
      }
      arrow.textContent = sort.dir === "asc" ? "▲" : "▼";
    }
  };

  $listHeader.addEventListener("click", (e) => {
    const span = e.target.closest("span[data-sort]");
    if (!span) return;
    const field = span.getAttribute("data-sort");
    if (sort.field === field) {
      sort.dir = sort.dir === "asc" ? "desc" : "asc";
    } else {
      sort.field = field;
      sort.dir = "asc";
    }
    sortEntries();
    renderList();
  });

  // ----- thumbnail rendering -----
  const renderThumb = (e) => {
    const wrap = document.createElement("div");
    wrap.className = "col-thumb";
    if (e.preview_kind === "image") {
      const img = document.createElement("img");
      img.src = "/thumb?id=" + encodeURIComponent(e.id);
      img.alt = "";
      img.loading = "lazy";
      img.decoding = "async";
      wrap.appendChild(img);
    } else {
      // inline SVG icon, no server roundtrip
      wrap.innerHTML = ICON_BY_KIND[e.preview_kind] || ICON_BY_KIND.other;
    }
    return wrap;
  };

  const ICON_BY_KIND = {
    video: '<svg viewBox="0 0 64 64" width="32" height="32"><rect width="64" height="64" fill="#1f1f1f" rx="8"/><polygon points="24,18 24,46 48,32" fill="#f3f3f3"/></svg>',
    audio: '<svg viewBox="0 0 64 64" width="32" height="32"><rect width="64" height="64" fill="#1f1f1f" rx="8"/><g fill="#f3f3f3"><rect x="30" y="14" width="4" height="20"/><rect x="22" y="22" width="4" height="14"/><rect x="38" y="22" width="4" height="14"/><rect x="14" y="28" width="4" height="10"/><rect x="46" y="28" width="4" height="10"/></g></svg>',
    text:  '<svg viewBox="0 0 64 64" width="32" height="32"><rect width="64" height="64" fill="#1f1f1f" rx="8"/><g fill="#f3f3f3"><rect x="16" y="16" width="32" height="3"/><rect x="16" y="24" width="32" height="3"/><rect x="16" y="32" width="32" height="3"/><rect x="16" y="40" width="22" height="3"/></g></svg>',
    other: '<svg viewBox="0 0 64 64" width="32" height="32"><rect width="64" height="64" fill="#1f1f1f" rx="8"/><text x="32" y="42" text-anchor="middle" font-family="monospace" font-size="14" fill="#f3f3f3">FILE</text></svg>',
  };

  // ----- list rendering -----
  const renderList = () => {
    $list.innerHTML = "";
    let count = 0;
    for (const e of entries) {
      count++;
      const li = document.createElement("li");
      li.dataset.id = e.id;
      if (e.id === selectedId) li.classList.add("selected");

      li.appendChild(renderThumb(e));

      const cName = document.createElement("span");
      cName.className = "col-name";
      cName.textContent = e.filename;
      cName.title = e.filename;
      li.appendChild(cName);

      const cCreated = document.createElement("span");
      cCreated.className = "col-created";
      cCreated.textContent = fmtDateCompact(e.created_at);
      cCreated.title = e.created_at || "";
      li.appendChild(cCreated);

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
    $preview.classList.remove("idle");
    $preview.innerHTML = "";
    if (!e) {
      $preview.classList.add("idle");
      const p = document.createElement("p");
      p.textContent = "Select a file on the left to preview.";
      $preview.appendChild(p);
      $toolbar.hidden = true;
      $toolbar.classList.remove("is-image");
      $download.removeAttribute("href");
      return;
    }
    $toolbar.hidden = false;
    const url = "/file?id=" + encodeURIComponent(e.id);
    $download.href = url;
    $download.setAttribute("download", e.filename);
    $toolbar.classList.toggle("is-image", e.preview_kind === "image");
    // reset convert picker whenever a new entry is selected
    if ($convert) $convert.value = "";
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
        el.style.background = "var(--bg)";
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
      if (res.status === 404) { $meta.hidden = true; return; }
      if (!res.ok) { $metaContent.textContent = "Failed to load metadata: " + res.status; $meta.hidden = false; return; }
      const text = await res.text();
      let pretty = text;
      try { pretty = JSON.stringify(JSON.parse(text), null, 2); } catch (_) { /* not json */ }
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
    const seq = ++searchSeq;
    const q = $filter.value.trim();
    try {
      const url = "/api/predictions" + (q ? "?q=" + encodeURIComponent(q) : "");
      const res = await fetch(url, { cache: "no-store" });
      if (!res.ok) throw new Error("HTTP " + res.status);
      const data = await res.json();
      if (seq !== searchSeq) return; // a newer request superseded us
      entries = Array.isArray(data) ? data : [];
      sortEntries();
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
  let filterTimer = null;
  $filter.addEventListener("input", () => {
    clearTimeout(filterTimer);
    filterTimer = setTimeout(loadList, 200);
  });

  $refresh.addEventListener("click", () => loadList());

  // ----- convert dropdown -----
  if ($convert) {
    $convert.addEventListener("change", () => {
      const to = $convert.value;
      if (!to || !selectedId) return;
      const url = "/convert?id=" + encodeURIComponent(selectedId) + "&to=" + encodeURIComponent(to);
      // Trigger a download. A temporary <a> with download attribute is the
      // most reliable cross-browser way; the server sends Content-Disposition
      // too, but this gives us the right filename even without it.
      const a = document.createElement("a");
      a.href = url;
      a.download = selectedId + "." + to;
      a.style.display = "none";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      // Reset the select so re-picking the same format still fires.
      $convert.value = "";
    });
  }

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
  updateSortHeader();
  loadList().then(() => {
    const hashId = decodeURIComponent((location.hash || "").replace(/^#/, ""));
    if (hashId && entries.find((e) => e.id === hashId)) selectEntry(hashId);
  });
  setInterval(() => { if (Date.now() >= nextRefreshAt) loadList(); }, 5_000);
})();
