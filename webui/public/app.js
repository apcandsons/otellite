// Live updates: one SSE connection per page. Elements tagged data-path get
// their status class, value and sparkline updated in place; badges are
// recounted from the DOM; the history chart is refetched on new samples.
(function () {
  "use strict";
  var SPARK_W = 120, SPARK_H = 24, SPARK_N = 60;
  // Mount point when served behind a reverse proxy ("" at the root).
  var BASE = document.body.dataset.base || "";
  var SEV = ["red", "amber", "green", "unknown"];

  function fmtBytes(n) {
    if (Math.abs(n) < 1024) return Math.round(n) + " B";
    var units = ["KB", "MB", "GB", "TB"], v = n / 1024, i = 0;
    while (i < units.length - 1 && Math.abs(v) >= 1024) { v /= 1024; i++; }
    return v.toFixed(1) + " " + units[i];
  }

  function fmtValue(raw, unit) {
    var v = Number(raw);
    if ((unit === "By" || unit === "Bytes") && isFinite(v)) return fmtBytes(v);
    var text = raw;
    if (isFinite(v) && !Number.isInteger(v)) {
      text = Math.abs(v) >= 1000 ? String(Math.round(v)) : String(Number(v.toPrecision(4)));
    }
    return unit && unit !== "1" ? text + " " + unit : text; // "1" is OTel's dimensionless unit
  }

  function sparkPoints(values) {
    var vs = values.filter(function (v) { return isFinite(v); });
    if (vs.length < 2) return "";
    var min = Math.min.apply(null, vs), max = Math.max.apply(null, vs), span = max - min;
    return vs.map(function (v, i) {
      var x = (i / (vs.length - 1)) * SPARK_W;
      var y = span === 0 ? SPARK_H / 2 : SPARK_H - ((v - min) / span) * SPARK_H;
      return x.toFixed(1) + "," + y.toFixed(1);
    }).join(" ");
  }

  function applyStatus(el, u) {
    SEV.forEach(function (s) { el.classList.remove(s); });
    el.classList.add(u.severity);
    el.querySelectorAll(".value, .kval").forEach(function (v) { v.textContent = fmtValue(u.raw, u.unit); });
    if (u.alert) return; // alert events repeat the sample that triggered them
    var svg = el.querySelector("svg.spark");
    if (svg) {
      var values = [];
      try { values = JSON.parse(svg.dataset.values || "[]"); } catch (e) {}
      if (isFinite(u.value)) values.push(u.value);
      if (values.length > SPARK_N) values.splice(0, values.length - SPARK_N);
      svg.dataset.values = JSON.stringify(values);
      svg.querySelector("polyline").setAttribute("points", sparkPoints(values));
    }
  }

  function recountBadges() {
    document.querySelectorAll(".badges[data-scope]").forEach(function (badges) {
      var container = badges.closest("details, section");
      if (!container) return;
      var items = container.querySelectorAll("[data-path]");
      var red = 0, amber = 0;
      items.forEach(function (it) {
        if (it.classList.contains("red")) red++;
        if (it.classList.contains("amber")) amber++;
      });
      set(badges, ".badge.red", red);
      set(badges, ".badge.amber", amber);
    });
  }

  function set(root, sel, n) {
    var b = root.querySelector(sel);
    if (!b) return;
    b.textContent = String(n);
    b.hidden = n === 0;
  }

  // Which KPI detail is showing: the history page itself, or the modal.
  var detail = document.body.dataset.history || null;

  // The history chart's right edge is "now", so keep it moving even when
  // no samples arrive (that is exactly when the gap matters).
  setInterval(function () { if (detail) refreshChart(); }, 10000);

  var chartTimer = null;
  function refreshChart() {
    if (chartTimer || !detail) return;
    chartTimer = setTimeout(function () {
      chartTimer = null;
      var chart = document.getElementById("chart");
      if (!chart || !detail) return;
      fetch(BASE + "/_/history" + pagePath(detail))
        .then(function (r) { return r.ok ? r.text() : null; })
        .then(function (html) { if (html !== null && detail) chart.innerHTML = html; })
        .catch(function () {});
    }, 2000);
  }

  // "/iam/iam-api/metrics/x.dat" -> "/iam/iam-api/x"
  function pagePath(streamPath) {
    var m = /^\/([^/]+)\/([^/]+)\/metrics\/(.+)\.dat$/.exec(streamPath);
    return m ? "/" + m[1] + "/" + m[2] + "/" + m[3] : streamPath;
  }

  // ---- modal: KPI links open the detail in place; the page stays behind.
  var modal = document.getElementById("modal");
  var modalBody = modal && modal.querySelector(".body");
  var modalOpenPage = modal && modal.querySelector(".open-page");

  // href is the page link (under BASE); rel is the app-relative part.
  function openModal(href) {
    if (!modal) return;
    var rel = href.slice(BASE.length);
    var path = rel.replace(/^\/history\/([^/]+)\/([^/]+)\/(.+)$/, "/$1/$2/metrics/$3.dat");
    detail = path;
    modalBody.innerHTML = "";
    modalOpenPage.href = href;
    modal.hidden = false;
    document.body.style.overflow = "hidden";
    fetch(BASE + "/_/detail" + rel.slice("/history".length))
      .then(function (r) { return r.ok ? r.text() : "<p class=\"empty\">not found</p>"; })
      .then(function (html) { if (detail === path) modalBody.innerHTML = html; })
      .catch(function () { modalBody.innerHTML = "<p class=\"empty\">could not load</p>"; });
    if (location.pathname !== href) history.pushState({ modal: href }, "", href);
  }

  function closeModal(viaHistory) {
    if (!modal || modal.hidden) return;
    modal.hidden = true;
    modalBody.innerHTML = "";
    document.body.style.overflow = "";
    detail = document.body.dataset.history || null;
    if (!viaHistory && history.state && history.state.modal) history.back();
  }

  if (modal) {
    document.addEventListener("click", function (e) {
      var a = e.target.closest && e.target.closest('a[href^="' + BASE + '/history/"]');
      if (!a || a.classList.contains("open-page")) return;
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return; // let new-tab clicks through
      e.preventDefault();
      openModal(a.getAttribute("href"));
    });
    modal.querySelector(".backdrop").addEventListener("click", function () { closeModal(false); });
    modal.querySelector(".close").addEventListener("click", function () { closeModal(false); });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeModal(false); });
    window.addEventListener("popstate", function () {
      if (modal.hidden) return;
      closeModal(true);
    });
    var base = location.pathname;
    history.replaceState({ base: base }, "", base);
  }

  function handle(u) {
    document.querySelectorAll('[data-path="' + CSS.escape(u.path) + '"]').forEach(function (el) { applyStatus(el, u); });
    recountBadges();
    if (detail === u.path && !u.alert) refreshChart();
  }

  var conn = document.getElementById("conn");
  var es = new EventSource(BASE + "/events");
  es.addEventListener("update", function (e) {
    try { handle(JSON.parse(e.data)); } catch (err) { console.error(err); }
  });
  es.onopen = function () { conn.textContent = "live"; conn.classList.add("live"); };
  es.onerror = function () { conn.textContent = "reconnecting"; conn.classList.remove("live"); };
})();
