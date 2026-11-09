/*
  The mechanics map on How it works. Without this script the page lists every job's steps under
  the map; with it, the jobs become tabs and the chosen job plays through the map one step at a
  time: the pieces the job touches stay lit, the two a step passes between are marked, and a dot
  travels from one to the other. Choosing a step shows that step and stops the play. With reduced
  motion it never plays on its own and the dot does not travel.

  Hand-written, no build, nothing fetched: the page reads the same opened from disk.
*/
(function () {
  "use strict";
  var root = document.querySelector("[data-mech]");
  if (!root) return;
  var map = root.querySelector(".mech-map");
  var wire = root.querySelector(".mech-wire");
  var line = wire.querySelector("line");
  var dot = wire.querySelector("circle");
  var jobs = [].slice.call(root.querySelectorAll("[data-job]"));
  var still = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var STEP_MS = 2400;
  var TRAVEL_MS = 700;

  var current = null; // the job shown
  var index = 0; // its step shown
  var timer = null; // the play, while it plays
  var travel = null; // the dot's animation frame

  // The tabs: one button per job, then the play button.
  var bar = document.createElement("div");
  bar.className = "mech-tabs";
  bar.setAttribute("role", "tablist");
  bar.setAttribute("aria-label", "Jobs");
  var tabs = jobs.map(function (job) {
    var b = document.createElement("button");
    b.type = "button";
    b.className = "mech-tab";
    b.setAttribute("role", "tab");
    b.setAttribute("aria-controls", job.id);
    b.textContent = job.querySelector("h3").textContent;
    b.addEventListener("click", function () { show(job, true); });
    bar.appendChild(b);
    return b;
  });
  var play = document.createElement("button");
  play.type = "button";
  play.className = "mech-play";
  play.addEventListener("click", function () {
    if (timer) { stop(); return; }
    if (index >= steps().length - 1) index = -1;
    start();
  });
  bar.appendChild(play);
  root.insertBefore(bar, root.firstChild);
  root.classList.add("mech-ready");

  jobs.forEach(function (job) {
    job.setAttribute("role", "tabpanel");
    [].forEach.call(job.querySelectorAll("li"), function (li, i) {
      li.tabIndex = 0;
      li.addEventListener("click", function () { stop(); at(i); });
      li.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); stop(); at(i); }
      });
    });
  });

  function steps() { return [].slice.call(current.querySelectorAll("li")); }
  function part(id) { return map.querySelector('[data-part="' + id + '"]'); }

  // show makes a job the one on the map: its pieces lit, its first step shown, and, unless motion
  // is reduced or this is the page opening, its steps played through.
  function show(job, byHand) {
    stop();
    current = job;
    jobs.forEach(function (j, i) {
      var on = j === job;
      j.hidden = !on;
      tabs[i].setAttribute("aria-selected", on ? "true" : "false");
      tabs[i].classList.toggle("is-on", on);
    });
    var used = {};
    steps().forEach(function (li) {
      used[li.getAttribute("data-from")] = true;
      used[li.getAttribute("data-to")] = true;
    });
    [].forEach.call(map.querySelectorAll("[data-part]"), function (p) {
      p.classList.toggle("is-used", !!used[p.getAttribute("data-part")]);
    });
    map.classList.add("is-showing");
    at(0);
    if (byHand && !still) start();
  }

  // at shows one step: it is marked in the list, and the dot travels from its first piece to its
  // second.
  function at(i) {
    var list = steps();
    index = Math.max(0, Math.min(i, list.length - 1));
    list.forEach(function (li, n) { li.classList.toggle("is-on", n === index); });
    var from = list[index].getAttribute("data-from");
    var to = list[index].getAttribute("data-to");
    [].forEach.call(map.querySelectorAll("[data-part]"), function (p) {
      var id = p.getAttribute("data-part");
      p.classList.toggle("is-from", id === from);
      p.classList.toggle("is-to", id === to && to !== from);
    });
    route(part(from), part(to));
    label();
  }

  // route draws the step's line between the two pieces and sends the dot along it.
  function route(a, b) {
    if (travel) cancelAnimationFrame(travel);
    if (!a || !b || a === b) { wire.classList.remove("is-on"); return; }
    var box = map.getBoundingClientRect();
    var ra = a.getBoundingClientRect();
    var rb = b.getBoundingClientRect();
    var ca = { x: ra.left + ra.width / 2 - box.left, y: ra.top + ra.height / 2 - box.top };
    var cb = { x: rb.left + rb.width / 2 - box.left, y: rb.top + rb.height / 2 - box.top };
    // The line runs from edge to edge, so it never crosses either piece's own words.
    var pa = edge(ca, cb, ra), pb = edge(cb, ca, rb);
    var x1 = pa.x, y1 = pa.y, x2 = pb.x, y2 = pb.y;
    wire.setAttribute("viewBox", "0 0 " + box.width + " " + box.height);
    line.setAttribute("x1", x1); line.setAttribute("y1", y1);
    line.setAttribute("x2", x2); line.setAttribute("y2", y2);
    wire.classList.add("is-on");
    if (still) { place(x2, y2); return; }
    var began = null;
    function frame(t) {
      if (began === null) began = t;
      var k = Math.min(1, (t - began) / TRAVEL_MS);
      var e = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2;
      place(x1 + (x2 - x1) * e, y1 + (y2 - y1) * e);
      if (k < 1) travel = requestAnimationFrame(frame);
    }
    travel = requestAnimationFrame(frame);
  }
  // edge is where the line from the centre c towards the point to leaves the box r, a few pixels
  // outside it.
  function edge(c, to, r) {
    var dx = to.x - c.x, dy = to.y - c.y;
    var tx = dx === 0 ? Infinity : (r.width / 2 + 4) / Math.abs(dx);
    var ty = dy === 0 ? Infinity : (r.height / 2 + 4) / Math.abs(dy);
    var t = Math.min(tx, ty, 1);
    return { x: c.x + dx * t, y: c.y + dy * t };
  }
  function place(x, y) { dot.setAttribute("cx", x); dot.setAttribute("cy", y); }

  // start plays from the step shown (from the first, after the last was reached), one step every
  // few seconds, and stops at the last.
  function start() {
    stop();
    if (index < 0) at(0);
    timer = setInterval(function () {
      if (index >= steps().length - 1) { stop(); return; }
      at(index + 1);
    }, STEP_MS);
    label();
  }
  function stop() {
    if (timer) clearInterval(timer);
    timer = null;
    label();
  }
  function label() {
    if (!current) return;
    play.textContent = timer ? "Pause" : (index >= steps().length - 1 ? "Play again" : "Play");
  }

  window.addEventListener("resize", function () { if (current) at(index); });
  show(jobs[0], false);
})();
