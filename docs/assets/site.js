(() => {
  const root = document.documentElement;
  const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');
  const zh = (document.documentElement.lang || '').toLowerCase().indexOf('zh') === 0;
  const t = (en, cn) => (zh ? cn : en);

  /* ---------- copy buttons ---------- */

  document.querySelectorAll('[data-copy]').forEach((button) => {
    button.addEventListener('click', async () => {
      const row = button.closest('.cmd-row') || button.parentElement;
      const code = row ? row.querySelector('code') : null;
      if (!code) return;
      // The "$" prompt is decoration: copy the command, not the prompt.
      const clone = code.cloneNode(true);
      clone.querySelectorAll('.prompt').forEach((el) => el.remove());
      const text = clone.textContent.trim();
      try {
        await navigator.clipboard.writeText(text);
        button.dataset.copied = 'true';
        window.setTimeout(() => { delete button.dataset.copied; }, 1600);
      } catch {
        const range = document.createRange();
        range.selectNodeContents(code);
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
      }
    });
  });

  /* ---------- the live window: split, close, focus ---------- */

  function setupPanes(app) {
    const panes = app.matches('[data-panes]') ? app : app.querySelector('[data-panes]');
    if (!panes) return;
    const status = app.querySelector('[data-pane-status]');
    const template = document.getElementById('pane-template');

    const sync = () => {
      const count = panes.querySelectorAll(':scope > .pane').length;
      panes.dataset.count = String(count);
      if (status) status.textContent = t(count + (count === 1 ? ' pane' : ' panes'), count + ' 个窗格');
      const full = count >= 4;
      panes.querySelectorAll('[data-act="split"]').forEach((b) => {
        b.disabled = full;
        b.setAttribute('aria-disabled', String(full));
      });
    };

    const focusPane = (pane) => {
      panes.querySelectorAll(':scope > .pane').forEach((p) => p.classList.toggle('focused', p === pane));
    };

    panes.addEventListener('click', (event) => {
      const button = event.target.closest('.pane-btn');
      const pane = event.target.closest('.pane');
      if (!pane) return;

      if (!button) {
        focusPane(pane);
        return;
      }

      const act = button.dataset.act;
      if (act === 'split') {
        if (panes.querySelectorAll(':scope > .pane').length >= 4 || !template) return;
        const next = template.content.firstElementChild.cloneNode(true);
        panes.appendChild(next);
        focusPane(next);
        sync();
        next.animate(
          [{ opacity: 0, transform: 'scale(.985)' }, { opacity: 1, transform: 'none' }],
          { duration: 240, easing: 'cubic-bezier(.2,.8,.2,1)' }
        );
        button.blur();
      } else if (act === 'close') {
        const all = panes.querySelectorAll(':scope > .pane');
        if (all.length <= 1) return;
        const wasFocused = pane.classList.contains('focused');
        pane.remove();
        if (wasFocused) focusPane(panes.querySelector(':scope > .pane'));
        sync();
      }
    });

    sync();
  }
  document.querySelectorAll('.win, .band').forEach(setupPanes);

  /* ---------- session rows ---------- */

  document.querySelectorAll('.win-side').forEach((side) => {
    side.addEventListener('click', (event) => {
      const row = event.target.closest('.sess');
      if (!row) return;
      side.querySelectorAll('.sess').forEach((s) => s.classList.toggle('active', s === row));
    });
  });

  document.querySelectorAll('.session-stack').forEach((stack) => {
    stack.addEventListener('click', (event) => {
      const row = event.target.closest('.stack-row');
      if (!row) return;
      stack.querySelectorAll('.stack-row').forEach((r) => { r.dataset.on = String(r === row); });
    });
  });

  /* ---------- motion ---------- */

  const gsap = window.gsap;
  const ScrollTrigger = window.ScrollTrigger;
  if (!gsap || !ScrollTrigger || reduce.matches) return;

  gsap.registerPlugin(ScrollTrigger);
  root.classList.add('anim');

  let lenis = null;
  if (window.Lenis) {
    lenis = new window.Lenis({ duration: 0.9, smoothWheel: true });
    lenis.on('scroll', ScrollTrigger.update);
    gsap.ticker.add((time) => { lenis.raf(time * 1000); });
    gsap.ticker.lagSmoothing(0);
  }

  /* anchors: keep them clear of the fixed title bar */
  document.querySelectorAll('a[href^="#"]').forEach((a) => {
    a.addEventListener('click', (event) => {
      const id = a.getAttribute('href').slice(1);
      const el = document.getElementById(id);
      if (!el) return;
      event.preventDefault();
      if (id === 'top') {
        if (lenis) lenis.scrollTo(0, { duration: 1 });
        else window.scrollTo({ top: 0, behavior: 'smooth' });
        return;
      }
      if (lenis) lenis.scrollTo(el, { offset: -64, duration: 1.1 });
      else el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
  });

  /* rail follows the canvas */
  const railItems = Array.from(document.querySelectorAll('[data-rail]'));
  const setActive = (id) => {
    railItems.forEach((item) => {
      const on = item.dataset.rail === id;
      if (on) item.setAttribute('aria-current', 'true');
      else item.removeAttribute('aria-current');
    });
  };

  const sections = ['top', 'canvas', 'sidebar', 'cli', 'agents', 'worktrees', 'install']
    .map((id) => document.getElementById(id))
    .filter(Boolean);

  sections.forEach((section, index) => {
    ScrollTrigger.create({
      trigger: section,
      start: 'top 45%',
      end: 'bottom 45%',
      onToggle: (self) => { if (self.isActive) setActive(section.id); },
      onEnter: () => setActive(section.id),
      onEnterBack: () => setActive(section.id),
    });
    if (index === 0) setActive(section.id);
  });

  /* first paint: title rises, the window settles, the rail fills in */
  const intro = gsap.timeline({ defaults: { ease: 'expo.out' } });
  intro
    .from('.hero-head .eyebrow', { opacity: 0, y: 10, duration: 0.6 }, 0)
    .from('.hero-line > span', { yPercent: 112, duration: 1.15, stagger: 0.09 }, 0.05)
    .from('.hero-sub > *', { opacity: 0, y: 16, duration: 0.8, stagger: 0.08 }, 0.3)
    .from('[data-intro]', { opacity: 0, y: 30, scale: 0.992, duration: 1, ease: 'power3.out' }, 0.34)
    .from('.rail-item', { opacity: 0, x: -8, duration: 0.5, stagger: 0.045, ease: 'power2.out' }, 0.1);

  /* sections arrive once, each in the weight of its own material.
     Content is never left invisible: if a trigger is already past (deep link,
     anchor jump), it plays on creation. */
  const reveal = (targets) => {
    gsap.to(targets, {
      opacity: 1,
      y: 0,
      duration: 0.75,
      stagger: 0.06,
      ease: 'power3.out',
      overwrite: true,
    });
  };

  gsap.utils.toArray('[data-reveal]').forEach((el) => {
    const rows = el.querySelectorAll('tbody tr');
    const panes = el.classList.contains('band') ? el.querySelectorAll('.pane') : null;
    const targets = rows.length ? rows : panes;
    const list = targets && targets.length ? targets : [el];
    gsap.set(list, { opacity: 0, y: 24 });
    ScrollTrigger.create({
      trigger: el,
      start: 'top bottom',
      once: true,
      onEnter: () => reveal(list),
      onLeave: () => reveal(list),
      onRefresh: (self) => { if (self.progress > 0) reveal(list); },
    });
  });

  /* section headings lead, their body follows */
  gsap.utils.toArray('.section-head').forEach((head) => {
    if (head.closest('[data-reveal]')) return;
    const list = Array.from(head.children);
    gsap.set(list, { opacity: 0, y: 22 });
    ScrollTrigger.create({
      trigger: head,
      start: 'top bottom',
      once: true,
      onEnter: () => reveal(list),
      onLeave: () => reveal(list),
      onRefresh: (self) => { if (self.progress > 0) reveal(list); },
    });
  });

  /* CLI: the sticky terminal runs the step in view */
  const termLines = Array.from(document.querySelectorAll('.cli-term .term-line'));
  const cliSteps = Array.from(document.querySelectorAll('.cli-step'));
  const narrow = window.matchMedia('(max-width: 900px)');
  if (termLines.length && cliSteps.length) {
    let activeStep = 0;
    const paint = (n) => {
      termLines.forEach((line) => {
        const step = Number(line.dataset.step);
        line.dataset.on = String(narrow.matches ? step === n : step <= n);
      });
      cliSteps.forEach((step) => {
        step.dataset.active = String(Number(step.dataset.step) === n);
      });
    };
    const activate = (n) => {
      if (n === activeStep) return;
      activeStep = n;
      paint(n);
    };
    activate(1);
    narrow.addEventListener('change', () => paint(activeStep));
    cliSteps.forEach((step) => {
      ScrollTrigger.create({
        trigger: step,
        start: 'top 62%',
        end: 'bottom 42%',
        onEnter: () => activate(Number(step.dataset.step)),
        onEnterBack: () => activate(Number(step.dataset.step)),
      });
    });
  }

  /* worktree track: the branch walks to the merged PR */
  const track = document.querySelector('[data-track]');
  if (track) {
    const fill = track.querySelector('[data-track-fill]');
    const stops = Array.from(track.querySelectorAll('.stop'));
    ScrollTrigger.create({
      trigger: track,
      start: 'top 74%',
      end: 'bottom 66%',
      scrub: true,
      onUpdate: (self) => {
        const p = self.progress;
        if (fill) fill.style.width = (p * 100).toFixed(2) + '%';
        stops.forEach((stop, i) => {
          const reached = p >= i / 5 - 0.02;
          const done = p >= (i + 1) / 5 - 0.02;
          if (!reached) stop.removeAttribute('data-on');
          else stop.dataset.on = done ? 'done' : 'true';
        });
      },
    });
  }

  /* positions settle after the webfont lands */
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(() => ScrollTrigger.refresh());
  }
})();
