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
  const $shareTg = document.getElementById("share-telegram");
  const $shareWa = document.getElementById("share-whatsapp");
  const $copyLink = document.getElementById("copy-link");
  const $copyImage = document.getElementById("copy-image");
  const $zoomControls = document.getElementById("zoom-controls");
  const $zoomVal = document.getElementById("zoom-val");
  const $zoomIn = document.getElementById("zoom-in");
  const $zoomOut = document.getElementById("zoom-out");
  const $zoomReset = document.getElementById("zoom-reset");
  const $splitResizer = document.getElementById("split-resizer");
  const $metaHandle = document.getElementById("meta-handle");

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
      $preview.classList.remove("has-media");
      return;
    }
    $toolbar.hidden = false;
    const fileUrl = window.location.origin + "/file?id=" + encodeURIComponent(e.id);
    const url = fileUrl; // local alias for the preview element below
    $download.href = fileUrl;
    $download.setAttribute("download", e.filename);
    $toolbar.classList.toggle("is-image", e.preview_kind === "image");
    $zoomControls.hidden = e.preview_kind !== "image" && e.preview_kind !== "video";
    $metaHandle.hidden = false;
    // Grab cursor + pan support are only relevant for zoomable content.
    $preview.classList.toggle("has-media", e.preview_kind === "image" || e.preview_kind === "video");
    // Share URLs (built lazily so the latest origin is used)
    const shareText = e.filename;
    $shareTg.href = "https://t.me/share/url?url=" + encodeURIComponent(fileUrl) + "&text=" + encodeURIComponent(shareText);
    $shareWa.href = "https://wa.me/?text=" + encodeURIComponent(fileUrl + "\n" + shareText);
    // Stash the URL on the copy buttons so handlers can grab it
    $copyLink.dataset.url = fileUrl;
    $copyImage.dataset.url = fileUrl;
    $copyImage.dataset.id = e.id;
    // reset convert picker whenever a new entry is selected
    if ($convert) $convert.value = "";
    let el;
    switch (e.preview_kind) {
      case "image":
        el = document.createElement("img");
        el.alt = e.filename;
        el.src = url;
        el.style.transform = "scale(" + zoom + ")";
        break;
      case "video":
        el = document.createElement("video");
        el.src = url;
        el.controls = true;
        el.preload = "metadata";
        el.style.transform = "scale(" + zoom + ")";
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

  // ----- shared helpers for the copy / share buttons -----
  function showFeedback(btn, text) {
    if (!btn) return;
    const originalTitle = btn.dataset.origTitle || btn.getAttribute("title") || "";
    btn.dataset.origTitle = originalTitle;
    btn.classList.add("btn-feedback");
    // Swap the icon for a brief text label, then restore.
    const svg = btn.querySelector("svg");
    let label = btn.querySelector(".btn-label");
    if (!label) {
      label = document.createElement("span");
      label.className = "btn-label";
      label.style.marginLeft = "4px";
      btn.appendChild(label);
    }
    if (svg) svg.style.display = "none";
    label.textContent = text;
    label.style.display = "";
    btn.setAttribute("title", text);
    setTimeout(() => {
      btn.classList.remove("btn-feedback");
      if (svg) svg.style.display = "";
      label.style.display = "none";
      btn.setAttribute("title", originalTitle);
    }, 1400);
  }

  async function copyText(btn, text, okMsg, failMsg) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
      } else {
        // Fallback for non-secure contexts (e.g. http://192.168.x.x).
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        const ok = document.execCommand("copy");
        document.body.removeChild(ta);
        if (!ok) throw new Error("execCommand copy failed");
      }
      showFeedback(btn, okMsg);
    } catch (err) {
      console.error("copy failed", err);
      showFeedback(btn, failMsg);
    }
  }

  // ----- copy link -----
  if ($copyLink) {
    $copyLink.addEventListener("click", () => {
      const url = $copyLink.dataset.url;
      if (!url) return;
      copyText($copyLink, url, "Copied!", "Failed");
    });
  }

  // ----- copy image -----
  // Browsers only accept image/png in the clipboard (per spec), so we go
  // through /convert?to=png first so the user gets reliable cross-browser
  // behavior regardless of source format.
  if ($copyImage) {
    $copyImage.addEventListener("click", async () => {
      const id = $copyImage.dataset.id;
      if (!id) return;
      const btn = $copyImage;
      try {
        if (!navigator.clipboard || !window.ClipboardItem) {
          throw new Error("Clipboard API not available");
        }
        const res = await fetch("/convert?id=" + encodeURIComponent(id) + "&to=png");
        if (!res.ok) throw new Error("HTTP " + res.status);
        const blob = await res.blob();
        await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
        showFeedback(btn, "Copied!");
      } catch (err) {
        console.error("copy image failed", err);
        showFeedback(btn, "Failed");
      }
    });
  }

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

  // ============================================================
  // Resizable panels + image zoom
  // ============================================================

  // ---- split divider (file explorer vs preview) ----
  const SPLIT_KEY = "replicate-safe-split";
  const savedSplit = parseInt(getCookie(SPLIT_KEY), 10);
  if (savedSplit > 0) {
    $root.style.setProperty("--split-left", savedSplit + "px");
  }
  if ($splitResizer) {
    let dragging = false;
    let startX = 0;
    let startLeft = 0;
    $splitResizer.addEventListener("mousedown", (e) => {
      dragging = true;
      startX = e.clientX;
      startLeft = $splitResizer.parentElement.firstElementChild.getBoundingClientRect().width;
      document.body.classList.add("resizing");
      $splitResizer.classList.add("dragging");
      e.preventDefault();
    });
    document.addEventListener("mousemove", (e) => {
      if (!dragging) return;
      const dx = e.clientX - startX;
      const min = 280, max = window.innerWidth - 360;
      let next = Math.max(min, Math.min(max, startLeft + dx));
      $root.style.setProperty("--split-left", next + "px");
    });
    document.addEventListener("mouseup", () => {
      if (!dragging) return;
      dragging = false;
      document.body.classList.remove("resizing");
      $splitResizer.classList.remove("dragging");
      const px = parseFloat(getComputedStyle($root).getPropertyValue("--split-left"));
      if (!isNaN(px)) setCookie(SPLIT_KEY, Math.round(px));
    });
  }

  // ---- metadata: height + open/closed state ----
  const META_KEY = "replicate-safe-meta-height";
  const META_OPEN_KEY = "replicate-safe-meta-open";

  // Restore height first (so the closed-state height matches user intent).
  const savedMeta = parseInt(getCookie(META_KEY), 10);
  if (savedMeta > 80 && savedMeta < window.innerHeight) {
    $root.style.setProperty("--meta-height", savedMeta + "px");
  }

  const $metaToggle = document.getElementById("meta-toggle");
  function setMetaOpen(open, persist) {
    $meta.classList.toggle("open", open);
    if ($metaToggle) $metaToggle.setAttribute("aria-expanded", open ? "true" : "false");
    if (persist) setCookie(META_OPEN_KEY, open ? "1" : "0");
  }
  // Restore open/closed state.
  setMetaOpen(getCookie(META_OPEN_KEY) === "1", false);

  if ($metaToggle) {
    $metaToggle.addEventListener("click", () => {
      setMetaOpen(!$meta.classList.contains("open"), true);
    });
  }

  if ($metaHandle) {
    let dragging = false;
    let startY = 0;
    let startH = 0;
    let lastWrite = 0;
    $metaHandle.addEventListener("mousedown", (e) => {
      dragging = true;
      startY = e.clientY;
      startH = $meta.getBoundingClientRect().height;
      document.body.classList.add("resizing-y");
      $metaHandle.classList.add("dragging");
      e.preventDefault();
    });
    document.addEventListener("mousemove", (e) => {
      if (!dragging) return;
      // Dragging UP increases the metadata height (since the handle is at the top).
      const dy = startY - e.clientY;
      const min = 80, max = window.innerHeight - 200;
      const next = Math.max(min, Math.min(max, startH + dy));
      $root.style.setProperty("--meta-height", next + "px");
      // Throttle the cookie write to ~10 Hz so we don't churn storage on drag.
      const now = Date.now();
      if (now - lastWrite > 100) {
        setCookie(META_KEY, Math.round(next));
        lastWrite = now;
      }
    });
    document.addEventListener("mouseup", () => {
      if (!dragging) return;
      dragging = false;
      document.body.classList.remove("resizing-y");
      $metaHandle.classList.remove("dragging");
      const px = parseFloat(getComputedStyle($root).getPropertyValue("--meta-height"));
      if (!isNaN(px)) setCookie(META_KEY, Math.round(px));
    });
  }

  // ---- image zoom ----
  const ZOOM_STEP = 1.25;
  const ZOOM_MIN = 0.25;
  const ZOOM_MAX = 16;
  const ZOOM_KEY = "replicate-safe-zoom";
  let zoom = parseFloat(getCookie(ZOOM_KEY)) || 1;

  function applyZoom() {
    const el = $preview.querySelector("img, video");
    if (el) {
      el.style.transform = "scale(" + zoom + ")";
    }
    $zoomVal.textContent = Math.round(zoom * 100) + "%";
    setCookie(ZOOM_KEY, zoom.toFixed(3));
  }
  function setZoom(z) {
    zoom = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z));
    applyZoom();
  }
  function resetZoom() { setZoom(1); }

  if ($zoomIn)  $zoomIn.addEventListener("click", () => setZoom(zoom * ZOOM_STEP));
  if ($zoomOut) $zoomOut.addEventListener("click", () => setZoom(zoom / ZOOM_STEP));
  if ($zoomReset) $zoomReset.addEventListener("click", resetZoom);

  // Wheel-to-zoom inside the preview area (plain wheel, no modifier).
  // The preview area's overflow:auto lets the user scroll-pan with the
  // wheel for non-zoomable content (audio, text, etc.).
  $preview.addEventListener("wheel", (e) => {
    const target = $preview.querySelector("img, video");
    if (!target) return;
    // Zoom only when the image fills the preview (zoomed in or at least
    // large enough that wheel-zoom feels useful); otherwise let the
    // wheel scroll the preview container naturally.
    if (zoom <= 1 && target.clientWidth * zoom <= $preview.clientWidth - 32
                  && target.clientHeight * zoom <= $preview.clientHeight - 32) {
      return;
    }
    e.preventDefault();
    setZoom(zoom * (e.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP));
  }, { passive: false });

  // Drag-to-pan: when zoomed in past the container bounds, dragging
  // adjusts scrollLeft/scrollTop so the user can see all parts of the
  // scaled image. Works for any element with .has-media on the preview.
  let panning = false;
  let panStartX = 0, panStartY = 0;
  let panStartScrollLeft = 0, panStartScrollTop = 0;
  $preview.addEventListener("mousedown", (e) => {
    if (e.button !== 0) return;
    if (!$preview.classList.contains("has-media")) return;
    // Don't pan when clicking on a button/control inside the preview.
    if (e.target.closest("button, a, select, input, .zoom-controls")) return;
    panning = true;
    panStartX = e.clientX;
    panStartY = e.clientY;
    panStartScrollLeft = $preview.scrollLeft;
    panStartScrollTop = $preview.scrollTop;
    $preview.classList.add("panning");
    document.body.classList.add("resizing");
    e.preventDefault();
  });
  document.addEventListener("mousemove", (e) => {
    if (!panning) return;
    const dx = e.clientX - panStartX;
    const dy = e.clientY - panStartY;
    $preview.scrollLeft = panStartScrollLeft - dx;
    $preview.scrollTop = panStartScrollTop - dy;
  });
  document.addEventListener("mouseup", () => {
    if (!panning) return;
    panning = false;
    $preview.classList.remove("panning");
    document.body.classList.remove("resizing");
  });

  // Keyboard: when the preview has an image/video focused, +/-/0 zoom.
  document.addEventListener("keydown", (e) => {
    if (e.target === $filter) return;
    if (!$preview.querySelector("img, video")) return;
    if (e.key === "+" || e.key === "=") { setZoom(zoom * ZOOM_STEP); e.preventDefault(); }
    else if (e.key === "-" || e.key === "_") { setZoom(zoom / ZOOM_STEP); e.preventDefault(); }
    else if (e.key === "0") { resetZoom(); e.preventDefault(); }
  });

  // Expose so renderPreview can call it after creating an image/video.
  window.__applyZoom = applyZoom;

  // ============================================================
  // Resizable file-explorer columns
  // ============================================================
  // Each column header (except thumb) has a <span class="col-resizer"
  // data-resize="name|created|...">. Dragging it updates the
  // --col-w-<name> CSS variable, which drives the grid-template-columns
  // for both the header and every row in the list. Each width persists
  // in its own cookie.

  const COLS_KEY = "replicate-safe-cols";
  function loadColWidths() {
    try {
      const raw = getCookie(COLS_KEY);
      if (!raw) return null;
      return JSON.parse(decodeURIComponent(raw));
    } catch (_) { return null; }
  }
  function saveColWidths(widths) {
    setCookie(COLS_KEY, encodeURIComponent(JSON.stringify(widths)));
  }

  // Apply persisted widths on boot.
  const savedCols = loadColWidths();
  if (savedCols && typeof savedCols === "object") {
    // Switch the relevant columns from minmax to fixed px so user-resized
    // widths stick even when the pane is wide.
    for (const [name, px] of Object.entries(savedCols)) {
      if (typeof px === "number" && px > 30 && px < 1200) {
        $root.style.setProperty("--col-w-" + name, px + "px");
      }
    }
  }

  for (const handle of document.querySelectorAll(".col-resizer")) {
    let dragging = false;
    let startX = 0;
    let startW = 0;
    let col = "";
    handle.addEventListener("mousedown", (e) => {
      col = handle.getAttribute("data-resize");
      if (!col) return;
      const span = handle.parentElement;
      dragging = true;
      startX = e.clientX;
      startW = span.getBoundingClientRect().width;
      document.body.classList.add("resizing-col");
      handle.classList.add("dragging");
      e.preventDefault();
      e.stopPropagation();
    });
    document.addEventListener("mousemove", (e) => {
      if (!dragging) return;
      const dx = e.clientX - startX;
      let next = Math.max(40, startW + dx);
      // Hard cap so columns don't run off the pane
      const max = window.innerWidth * 0.6;
      next = Math.min(max, next);
      $root.style.setProperty("--col-w-" + col, next + "px");
    });
    document.addEventListener("mouseup", () => {
      if (!dragging) return;
      dragging = false;
      document.body.classList.remove("resizing-col");
      handle.classList.remove("dragging");
      const v = parseFloat(getComputedStyle($root).getPropertyValue("--col-w-" + col));
      if (!isNaN(v)) {
        const all = loadColWidths() || {};
        all[col] = Math.round(v);
        saveColWidths(all);
      }
    });
  }

  // Double-click on a resizer resets that column to its default.
  for (const handle of document.querySelectorAll(".col-resizer")) {
    handle.addEventListener("dblclick", () => {
      const col = handle.getAttribute("data-resize");
      if (!col) return;
      // Remove the override so the default in :root applies again.
      $root.style.removeProperty("--col-w-" + col);
      const all = loadColWidths() || {};
      delete all[col];
      saveColWidths(all);
    });
  }
})();
