/* wasctl web UI — minimal vanilla JS (no framework) */
(function () {
  'use strict';

  /* ── HTMX global config (wasctl.js is deferred after htmx.min.js) ────────── */
  if (window.htmx) {
    htmx.config.defaultSwapStyle = 'innerHTML';
    htmx.config.historyCacheSize = 0;
    // Alpine does not auto-bind x-data injected by HTMX swaps (cluster tabs,
    // install wizard steps). Re-init the swapped subtree.
    document.body.addEventListener('htmx:afterSwap', function (evt) {
      if (window.Alpine && evt.detail && evt.detail.elt) {
        Alpine.initTree(evt.detail.elt);
      }
    });
  }

  /* ── Auto-dismiss flash messages ────────────────────────────────────────── */
  function initFlash() {
    document.querySelectorAll('.flash[data-autohide]').forEach(function (el) {
      var ms = parseInt(el.dataset.autohide, 10) || 4000;
      setTimeout(function () {
        el.style.opacity = '0';
        el.style.transition = 'opacity 0.4s';
        setTimeout(function () { el.remove(); }, 450);
      }, ms);
    });
    document.querySelectorAll('.flash__close').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var flash = btn.closest('.flash');
        if (flash) flash.remove();
      });
    });
  }

  /* ── Relative timestamps ─────────────────────────────────────────────────── */
  function initRelTimes() {
    document.querySelectorAll('time[datetime]').forEach(function (el) {
      if (el.dataset.noRelative) return;
      var abs = new Date(el.getAttribute('datetime'));
      if (isNaN(abs)) return;
      var diff = (Date.now() - abs) / 1000;
      var str;
      if (diff < 60)       str = 'just now';
      else if (diff < 3600) str = Math.floor(diff / 60) + 'm ago';
      else if (diff < 86400) str = Math.floor(diff / 3600) + 'h ago';
      else                  str = Math.floor(diff / 86400) + 'd ago';
      el.textContent = str;
      el.title = abs.toUTCString();
    });
  }

  /* ── Log pane auto-scroll (used in Phase 3 SSE view) ────────────────────── */
  window.wasctl = {
    logPane: null,
    pinToBottom: true,
    initLogPane: function (el) {
      this.logPane = el;
      el.addEventListener('scroll', function () {
        var atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 32;
        window.wasctl.pinToBottom = atBottom;
      });
    },
    appendLog: function (line) {
      if (!this.logPane) return;
      var row = document.createElement('div');
      row.className = 'log-line';
      row.textContent = line;
      this.logPane.appendChild(row);
      if (this.pinToBottom) {
        this.logPane.scrollTop = this.logPane.scrollHeight;
      }
    }
  };

  /* ── Log export ──────────────────────────────────────────────────────────── */
  window.wasctl.exportLogs = function () {
    var meta = document.getElementById('stream-meta');
    if (!meta) return;
    var cluster   = meta.dataset.cluster   || 'unknown';
    var cloud     = meta.dataset.cloud     || 'unknown';
    var operation = meta.dataset.operation || 'unknown';

    var now = new Date();
    var pad = function (n) { return String(n).padStart(2, '0'); };
    var ts = now.getUTCFullYear().toString()
      + pad(now.getUTCMonth() + 1)
      + pad(now.getUTCDate())
      + '-'
      + pad(now.getUTCHours())
      + pad(now.getUTCMinutes())
      + pad(now.getUTCSeconds());

    var filename = cluster + '-' + cloud + '-' + operation + '-error-' + ts + '.log';

    var logPane = document.getElementById('log-pane');
    var text = logPane
      ? Array.from(logPane.querySelectorAll('.log-line'))
          .map(function (el) { return el.textContent; })
          .join('\n')
      : '';

    var blob = new Blob([text], { type: 'text/plain' });
    var url  = URL.createObjectURL(blob);
    var a    = document.createElement('a');
    a.href     = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  /* ── Help overlay ────────────────────────────────────────────────────────── */
  window.wasctl.closeHelp = function () {
    var overlay = document.getElementById('help-overlay');
    if (overlay) { overlay.hidden = true; }
    window.wasctl._helpOpen = false;
  };
  window.wasctl.openHelp = function () {
    var overlay = document.getElementById('help-overlay');
    if (overlay) { overlay.hidden = false; }
    window.wasctl._helpOpen = true;
  };
  window.wasctl._helpOpen = false;

  /* ── Keyboard navigation ─────────────────────────────────────────────────── */
  function initKeyboard() {
    var gPending = false;
    var gTimer   = null;

    // Track last-visited cluster URL in sessionStorage for g-c shortcut.
    var clusterMatch = location.pathname.match(/^\/clusters\/([^\/]+)\/?$/);
    if (clusterMatch) {
      sessionStorage.setItem('wasctl.lastCluster', location.pathname);
    }

    document.addEventListener('keydown', function (e) {
      // Skip when focused in any form element.
      var tag = document.activeElement && document.activeElement.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
      if (document.activeElement && document.activeElement.isContentEditable) return;

      if (window.wasctl._helpOpen) {
        if (e.key === 'Escape' || e.key === '?') {
          window.wasctl.closeHelp();
          e.preventDefault();
        }
        return;
      }

      if (e.key === '?') {
        window.wasctl.openHelp();
        e.preventDefault();
        return;
      }

      if (gPending) {
        clearTimeout(gTimer);
        gPending = false;
        if (e.key === 'h') { location.href = '/'; return; }
        if (e.key === 'c') {
          var last = sessionStorage.getItem('wasctl.lastCluster');
          if (last) location.href = last;
          return;
        }
        return;
      }

      if (e.key === 'g' && !e.ctrlKey && !e.metaKey) {
        gPending = true;
        gTimer = setTimeout(function () { gPending = false; }, 1000);
      }
    });
  }

  /* ── Page progress bar (Pattern C) ──────────────────────────────────────── */
  function initProgress() {
    var bar = document.getElementById('page-progress');
    if (!bar) return;
    var hideTimer = null;

    function start() {
      clearTimeout(hideTimer);
      bar.className = 'is-loading';
      document.body.classList.add('wc-busy');
    }
    function finish() {
      bar.className = 'is-complete';
      document.body.classList.remove('wc-busy');
      hideTimer = setTimeout(function () { bar.className = ''; }, 400);
    }

    document.addEventListener('htmx:beforeRequest', start);
    document.addEventListener('htmx:afterRequest',  finish);

    // Regular link clicks — skip hash anchors and javascript: pseudo-URLs.
    document.addEventListener('click', function (e) {
      var a = e.target.closest('a[href]');
      if (!a) return;
      var href = a.getAttribute('href');
      if (!href || href.charAt(0) === '#' || href.startsWith('javascript:')) return;
      start();
    });

    // Plain form submits (non-HTMX) that navigate away.
    document.addEventListener('submit', function (e) {
      var f = e.target;
      if (f.hasAttribute('hx-post') || f.hasAttribute('hx-get') ||
          f.hasAttribute('hx-put') || f.hasAttribute('hx-delete')) return;
      start();
    });
  }

  /* ── "Updated X seconds ago" counter ────────────────────────────────────── */
  function initRefreshAge(contentId, stampId) {
    var contentEl = document.getElementById(contentId);
    var stampEl   = document.getElementById(stampId);
    if (!contentEl || !stampEl) return;

    var last = Date.now();

    var ticker = setInterval(function () {
      var secs = Math.floor((Date.now() - last) / 1000);
      if (secs < 5)        stampEl.textContent = '';
      else if (secs < 60)  stampEl.textContent = secs + 's ago';
      else                 stampEl.textContent = Math.floor(secs / 60) + 'm ago';
    }, 3000);

    contentEl.addEventListener('htmx:afterSettle', function () {
      last = Date.now();
      stampEl.textContent = '';
    });

    window.addEventListener('beforeunload', function () { clearInterval(ticker); });
  }

  function initTabs() {
    function handleHashChange() {
      var hash = location.hash || '#overview';
      var panel = document.querySelector(hash);
      if (panel && window.htmx) {
        htmx.trigger(panel, 'show');
      }
    }
    window.addEventListener('hashchange', handleHashChange);
    // Trigger on initial load
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', handleHashChange);
    } else {
      handleHashChange();
    }
  }

  document.addEventListener('DOMContentLoaded', function () {
    initFlash();
    initRelTimes();
    initKeyboard();
    initProgress();
    initRefreshAge('clusters-content', 'clusters-last-updated');
    initRefreshAge('ops-content',      'ops-last-updated');
    initTabs();
  });
}());
