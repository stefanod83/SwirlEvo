// Swirl deploy-agent progress / recovery UI — vanilla JS.
// Polls /status.json and /logs every 3s, refreshes the DOM in place,
// and adapts its own chrome (Retry/Rollback buttons, banners) to the
// current phase. This page is embedded as an iframe inside the main
// Swirl UI during a deploy; on success it signals the parent with
// postMessage so the parent can close the modal without waiting for
// its own polling loop.
(function () {
  "use strict";

  var POLL_MS = 3000;
  var IN_PROGRESS = {
    pending: 1, stopping: 1, pulling: 1, starting: 1, health_check: 1
  };
  var RECOVERABLE = {
    failed: 1, recovery: 1, rolled_back: 1
  };

  var successNotified = false;

  function el(id) { return document.getElementById(id); }

  function setBadge(phase) {
    var badge = el("phase-badge");
    if (!badge) return;
    badge.className = "badge badge-" + (phase || "unknown");
    badge.textContent = phase || "unknown";
    // Replace any prior spinner before adding a new one.
    var existing = badge.parentNode.querySelector(".spinner");
    if (existing) existing.remove();
    if (IN_PROGRESS[phase]) {
      var s = document.createElement("span");
      s.className = "spinner";
      s.setAttribute("aria-hidden", "true");
      badge.parentNode.appendChild(s);
    }
  }

  function togglePanels(phase) {
    var actions = el("actions-panel");
    var progressNote = el("progress-note");
    var successNote = el("success-note");

    if (RECOVERABLE[phase]) {
      if (actions) actions.hidden = false;
      if (progressNote) progressNote.hidden = true;
      if (successNote) successNote.hidden = true;
    } else if (phase === "success") {
      if (actions) actions.hidden = true;
      if (progressNote) progressNote.hidden = true;
      if (successNote) successNote.hidden = false;
      // Notify parent window (the main Swirl UI) that the deploy
      // succeeded so it can close the iframe modal immediately
      // rather than waiting for its /api/system/mode polling to
      // observe the new container. Fire once per page load.
      if (!successNotified) {
        successNotified = true;
        setTimeout(function () {
          try {
            if (window.parent && window.parent !== window) {
              window.parent.postMessage(
                { type: "swirl.self-deploy", phase: "success" },
                "*"
              );
            }
          } catch (_) { /* silent */ }
        }, 2000);
      }
    } else {
      // In-progress phases: hide actions, show progress note.
      if (actions) actions.hidden = true;
      if (progressNote) progressNote.hidden = false;
      if (successNote) successNote.hidden = true;
    }
  }

  function formatTime(iso) {
    if (!iso) return "—";
    try {
      var d = new Date(iso);
      if (isNaN(d.getTime())) return iso;
      return d.toLocaleString();
    } catch (_) {
      return iso;
    }
  }

  function pollStatus() {
    fetch("/status.json", { cache: "no-store" })
      .then(function (r) {
        if (!r.ok) throw new Error("status " + r.status);
        return r.json();
      })
      .then(function (s) {
        setBadge(s.phase);
        togglePanels(s.phase);
        if (s.startedAt) el("started-at").textContent = formatTime(s.startedAt);
        if (s.finishedAt && s.finishedAt !== "0001-01-01T00:00:00Z") {
          el("finished-at").textContent = formatTime(s.finishedAt);
        } else {
          el("finished-at").textContent = "—";
        }
        var errBox = el("status-error");
        if (s.error) {
          errBox.textContent = s.error;
          errBox.hidden = false;
        } else {
          errBox.textContent = "";
          errBox.hidden = true;
        }
      })
      .catch(function (err) {
        // Backend likely gone — the sidekick probably shut down after
        // a successful deploy. Surface a subtle note but keep polling
        // so a transient blip recovers naturally.
        var errBox = el("status-error");
        if (errBox) {
          errBox.textContent = "Status unavailable (" + err.message + "). Retrying…";
          errBox.hidden = false;
        }
      });
  }

  function pollLogs() {
    fetch("/logs", { cache: "no-store" })
      .then(function (r) { return r.text(); })
      .then(function (txt) {
        var pre = el("logs");
        if (!pre) return;
        // Only scroll to bottom if the user was already near the bottom.
        var atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 20;
        pre.textContent = txt;
        if (atBottom) pre.scrollTop = pre.scrollHeight;
      })
      .catch(function () { /* silent — status poll will surface the outage */ });
  }

  function wireForm(formId, btnId) {
    var form = document.getElementById(formId);
    var btn = document.getElementById(btnId);
    if (!form || !btn) return;
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      if (btn.disabled) return;
      btn.disabled = true;
      var feedback = el("action-feedback");
      feedback.className = "feedback";
      feedback.textContent = "Dispatching " + btn.textContent.trim() + "…";
      feedback.hidden = false;
      var fd = new FormData(form);
      fetch(form.action, {
        method: "POST",
        body: fd,
        headers: { "X-CSRF-Token": window.__CSRF_TOKEN__ || "" }
      })
        .then(function (r) {
          return r.text().then(function (body) {
            if (!r.ok) throw new Error(body || ("HTTP " + r.status));
            return body;
          });
        })
        .then(function (body) {
          feedback.textContent = body || "Action accepted; watch the status.";
          // Re-enable after a beat so the operator can retry if needed.
          setTimeout(function () { btn.disabled = false; }, 1500);
        })
        .catch(function (err) {
          feedback.className = "feedback error";
          feedback.textContent = "Action failed: " + err.message;
          btn.disabled = false;
        });
    });
  }

  function init() {
    wireForm("retry-form", "retry-btn");
    wireForm("rollback-form", "rollback-btn");
    pollStatus();
    pollLogs();
    setInterval(pollStatus, POLL_MS);
    setInterval(pollLogs, POLL_MS);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
