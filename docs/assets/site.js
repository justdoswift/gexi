(() => {
  const root = document.documentElement;
  const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

  // Hero lines rise once the page is ready.
  const ready = () => root.classList.add('ready');
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', ready, { once: true });
  } else {
    ready();
  }

  // Sections arrive as they are met.
  const revealables = document.querySelectorAll('[data-reveal]');
  if ('IntersectionObserver' in window && !reduce.matches) {
    const io = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add('in');
          io.unobserve(entry.target);
        }
      }
    }, { rootMargin: '0px 0px -8% 0px', threshold: 0.12 });
    revealables.forEach((el) => io.observe(el));
  } else {
    revealables.forEach((el) => el.classList.add('in'));
  }

  // Copy buttons.
  document.querySelectorAll('[data-copy]').forEach((button) => {
    button.addEventListener('click', async () => {
      const code = button.parentElement.querySelector('code');
      if (!code) return;
      const text = code.textContent.trim();
      try {
        await navigator.clipboard.writeText(text);
        button.dataset.copied = 'true';
        window.setTimeout(() => { delete button.dataset.copied; }, 1600);
      } catch {
        // Clipboard unavailable: leave the command selected for a manual copy.
        const range = document.createRange();
        range.selectNodeContents(code);
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
      }
    });
  });

  // The live window: split, close, focus.
  function setupPanes(app) {
    const panes = app.querySelector('[data-panes]');
    if (!panes) return;
    const status = app.querySelector('[data-pane-status]');
    const template = document.getElementById('pane-template');

    const sync = () => {
      const count = panes.querySelectorAll(':scope > .pane').length;
      panes.dataset.count = String(count);
      if (status) status.textContent = count + (count === 1 ? ' pane' : ' panes');
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
        // The new pane settles in rather than popping.
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
  document.querySelectorAll('.app').forEach(setupPanes);

  // A session row can be picked, as it can in the sidebar.
  document.querySelectorAll('.app-side').forEach((side) => {
    side.addEventListener('click', (event) => {
      const row = event.target.closest('.session');
      if (!row) return;
      side.querySelectorAll('.session').forEach((s) => s.classList.toggle('active', s === row));
    });
  });

  // The hero window drifts a little under the page, as a window on a desk.
  const heroWindow = document.querySelector('.hero-window');
  if (heroWindow && !reduce.matches) {
    let frame = 0;
    const draw = () => {
      frame = 0;
      const y = Math.min(window.scrollY * 0.028, 16);
      heroWindow.style.transform = y > 0.2 ? 'translateY(' + y.toFixed(2) + 'px)' : '';
    };
    window.addEventListener('scroll', () => {
      if (frame) return;
      frame = window.requestAnimationFrame(draw);
    }, { passive: true });
  }
})();
