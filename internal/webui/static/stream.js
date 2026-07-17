/* wasctl SSE stream controller — shared by install and destroy stream pages */
(function () {
  'use strict';

  /**
   * initStream wires all SSE event listeners for a wasctl operation stream page.
   * Call once inside DOMContentLoaded.
   *
   * opts:
   *   sseUrl          {string}       EventSource URL
   *   clusterName     {string}       display name used in outcome messages
   *   operation       {string}       'install' | 'destroy'
   *   totalStages     {number}
   *   elapsedEl       {Element}      span that shows "1:23" elapsed time
   *   progressEl      {Element}      the fill div inside the progress bar
   *   progressLabelEl {Element|null} label beside the bar (null on destroy page)
   *   logPaneEl       {Element}
   *   outcomeEl       {Element}      hidden initially; shown on completion
   */
  window.wasctl.initStream = function (opts) {
    var completed = 0;
    var total = opts.totalStages;

    window.wasctl.initLogPane(opts.logPaneEl);

    function stageEl(name) {
      return document.getElementById('stage-' + name);
    }

    var src = new EventSource(opts.sseUrl);

    /* Server-driven elapsed counter — avoids client/server clock skew */
    src.addEventListener('tick', function (e) {
      var d = JSON.parse(e.data);
      opts.elapsedEl.textContent = d.totalElapsed;
    });

    src.addEventListener('stage-start', function (e) {
      var d = JSON.parse(e.data);
      if (opts.progressLabelEl) {
        opts.progressLabelEl.textContent =
          'Stage ' + d.num + ' of ' + total + ' — ' + d.label;
      }
      var el = stageEl(d.name);
      if (el) el.className = 'stream-stage stream-stage--active';
    });

    src.addEventListener('substep-start', function (e) {
      var d = JSON.parse(e.data);
      window.wasctl.appendLog('  » ' + d.name);
    });

    src.addEventListener('stage-done', function (e) {
      var d = JSON.parse(e.data);
      completed++;
      var el = stageEl(d.name);
      if (el) {
        el.className = 'stream-stage stream-stage--done';
        var etaEl = el.querySelector('.stream-stage__eta');
        var elapsedEl = el.querySelector('.stream-stage__elapsed');
        if (etaEl) etaEl.style.display = 'none';
        if (elapsedEl) { elapsedEl.textContent = d.elapsed; elapsedEl.style.display = ''; }
      }
      if (total > 0) {
        opts.progressEl.style.width = Math.round(completed / total * 100) + '%';
      }
    });

    src.addEventListener('stage-fail', function (e) {
      var d = JSON.parse(e.data);
      var el = stageEl(d.name);
      if (el) el.className = 'stream-stage stream-stage--failed';
      window.wasctl.appendLog('✗ Stage failed: ' + (d.error || 'unknown error'));
    });

    src.addEventListener('log', function (e) {
      var d = JSON.parse(e.data);
      window.wasctl.appendLog(d.line || '');
    });

    src.addEventListener('install-complete', function (e) {
      var d = JSON.parse(e.data);
      src.close();
      var out = opts.outcomeEl;
      out.hidden = false;
      if (d.error) {
        var marker  = opts.operation === 'destroy' ? 'DestroyFailed' : 'StageFailed';
        var title   = opts.operation === 'destroy' ? 'Destroy failed' : 'Installation failed';
        var retryBtn = opts.operation === 'destroy'
          ? '<a href="/clusters/' + opts.clusterName + '/destroy" class="btn btn--primary">Try again</a>'
          : '<a href="/install/retry/' + (opts.sessionID || '') + '" class="btn btn--primary">Try again</a>';
        out.innerHTML =
          '<div class="error-state">' +
          '<div class="brand-marker">\\[' + marker + ']</div>' +
          '<p class="error-state__title">' + title + '</p>' +
          '<p class="error-state__detail">' + d.error + '</p>' +
          '<div class="stream-failure-actions">' +
          '<button class="btn btn--secondary" onclick="window.wasctl.exportLogs()">Export logs</button>' +
          retryBtn +
          '</div>' +
          '</div>';
      } else {
        opts.progressEl.style.width = '100%';
        if (opts.progressLabelEl) opts.progressLabelEl.textContent = 'Complete';
        if (opts.operation === 'destroy') {
          out.innerHTML =
            '<div class="empty-state">' +
            '<div class="brand-marker">\\[DestroyComplete]</div>' +
            '<p class="empty-state__title">Cluster destroyed</p>' +
            '<p class="empty-state__sub"><strong>' + opts.clusterName + '</strong> has been fully deprovisioned.</p>' +
            '<a href="/" class="btn btn--primary" style="margin-top:16px">Back to clusters</a>' +
            '</div>';
        } else {
          out.innerHTML =
            '<div class="empty-state">' +
            '<div class="brand-marker">\\[InstallComplete]</div>' +
            '<p class="empty-state__title">Installation complete</p>' +
            '<p class="empty-state__sub">' + opts.clusterName + ' is ready.</p>' +
            '<a href="' + d.clusterUrl + '" class="btn btn--primary">View cluster →</a>' +
            '</div>';
        }
      }
    });

    /* Close on error so the browser stops auto-reconnecting (SSE default). */
    src.onerror = function () {
      window.wasctl.appendLog(
        '[connection lost — ' + opts.operation +
        ' continues server-side; reload to reconnect]'
      );
      src.close();
    };
  };
}());
