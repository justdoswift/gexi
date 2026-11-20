package main

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The stage rail is zoom's other half. When one pane fills the session, the
// panes it hides float over the stage on the right, as Stage Manager keeps
// the windows behind the one in front: tilted back in perspective, each
// showing a live, scaled mirror of its terminal. A click brings one forward
// and sends the pane it replaces back to the rail.
const (
	stageCardW       float32 = 200
	stageCardH       float32 = 140
	stageCardGap     float32 = 18
	stageRailInset   float32 = 18
	stageRailMinW    float32 = 520
	stageTiltY       float32 = -18 // degrees around the card's vertical axis
	stagePerspective float32 = 520 // DIPs from the viewer
	stageMirrorFPS           = 12
	stageCardRadius  float32 = 10
)

// stageMirror is one off-stage pane's preview: a sink terminal that shows
// the frames of a Mirror of the pane's own terminal.
type stageMirror struct {
	sink   *terminal.Terminal
	conn   *mirrorConn
	mirror *terminal.Mirror
	stop   chan struct{}
	// live says the card is on screen. A mirror whose card is hidden — the
	// window is in the background, the rail is off because the stage is too
	// narrow, the drawer covers that edge — keeps its sink but stops
	// copying the screen, and catches up in one frame when it comes back.
	live atomic.Bool
}

// mirrorConn carries mirror frames to the sink terminal. Frames are deltas,
// so they queue in order rather than replace one another.
type mirrorConn struct {
	mu     sync.Mutex
	cond   *sync.Cond
	frames [][]byte
	closed bool
}

func newMirrorConn() *mirrorConn {
	c := &mirrorConn{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *mirrorConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	for len(c.frames) == 0 && !c.closed {
		c.cond.Wait()
	}
	if len(c.frames) == 0 {
		c.mu.Unlock()
		return 0, io.EOF
	}
	frame := c.frames[0]
	n := copy(p, frame)
	if n == len(frame) {
		c.frames = c.frames[1:]
	} else {
		c.frames[0] = frame[n:]
	}
	c.mu.Unlock()
	return n, nil
}

// Write collects what the sink's program would send back; a mirror has no
// program to hear it.
func (c *mirrorConn) Write(p []byte) (int, error) { return len(p), nil }

// Resize takes the size the sink's view fits; the screen stays the size the
// pane's terminal has, so the frames and the sink agree.
func (c *mirrorConn) Resize(cols, rows int) error { return nil }

func (c *mirrorConn) push(frame []byte) {
	c.mu.Lock()
	if !c.closed {
		c.frames = append(c.frames, frame)
	}
	c.mu.Unlock()
	c.cond.Signal()
}

func (c *mirrorConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	return nil
}

// stageRailPanes are the panes the rail shows: every pane of the session
// except the one on stage, in the split tree's order, so a card keeps its
// place as the stage moves.
func (w *workspace) stageRailPanes(s *session) []*pane {
	if s == nil || s.Root == nil || s.zoomedPane == "" {
		return nil
	}
	stage := s.Root.paneByID(s.zoomedPane)
	if stage == nil {
		return nil
	}
	var out []*pane
	for _, p := range s.Root.panes() {
		if p.ID != stage.ID {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stageRailVisible reports whether the rail belongs on screen: a pane is
// zoomed, it hides another pane, and the drawer is not covering that edge.
func (w *workspace) stageRailVisible(s *session) bool {
	if w.drawer.open || w.drawer.closing {
		return false
	}
	if w.stageW > 0 && w.stageW < stageRailMinW {
		return false
	}
	return len(w.stageRailPanes(s)) > 0
}

// stagePane brings p to the stage: it fills the session, takes the focus, and
// the pane it replaces joins the rail.
func (w *workspace) stagePane(p *pane) {
	if p == nil || !w.showPane(p) {
		return
	}
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
	w.scheduleSave()
}

// showPane hands the stage to p: the pane on it shrinks back into the rail and
// p grows out of the card it had there. It reports whether the stage moved. A
// pane already on the stage is left as it is, and so is one in a session that
// is not zoomed, where every pane already shows.
func (w *workspace) showPane(p *pane) bool {
	if p == nil {
		return false
	}
	s := w.sessionOf(p)
	if s == nil || s.Root == nil || s.zoomedPane == p.ID {
		return false
	}
	// Only a zoomed session has a stage to hand over: without a zoom every
	// pane already shows, and a call to bring one forward changes nothing.
	if s.zoomedPane == "" {
		return false
	}
	if cur := s.Root.paneByID(s.zoomedPane); cur != nil {
		// The pane that leaves the stage shrinks into the rail.
		cur.stageCardShown = false
		cur.stageCardShrink = true
	}
	s.zoomedPane = p.ID
	// A pane that has been drawn grows out of its card; one that has not is
	// fading in as the stage anyway, with no card to start from.
	if p.shown {
		p.stageEnter = true
	}
	w.syncStageMirrors()
	return true
}

// syncStageMirrors starts a live preview for every pane in the rail and
// stops the ones no card shows, including the pane that has just come on
// stage: its own view draws it now, and a mirror would take the frame's
// changes before the view saw them.
func (w *workspace) syncStageMirrors() {
	keep := map[string]bool{}
	if s := w.selected(); s != nil && w.stageRailVisible(s) {
		for _, p := range w.stageRailPanes(s) {
			keep[p.ID] = true
		}
	}
	for _, pr := range w.allProjects() {
		for _, s := range pr.Sessions {
			if s.Root == nil {
				continue
			}
			for _, p := range s.Root.panes() {
				if keep[p.ID] {
					w.startStageMirror(p)
				} else {
					w.stopStageMirror(p)
					p.stageCardShown = false
				}
			}
		}
	}
}

func (w *workspace) startStageMirror(p *pane) {
	if p == nil || p.term == nil || p.stageMirror != nil {
		return
	}
	cols, rows := p.term.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	conn := newMirrorConn()
	lightTheme, darkTheme := w.terminalThemes()
	sink, err := terminal.New(terminal.Options{
		Conn:         conn,
		ResizeByConn: true,
		Scrollback:   -1,
		NoBlink:      true,
		Transparent:  true,
		Theme:        lightTheme,
		DarkTheme:    darkTheme,
		Font:         w.terminalTextFont(),
		Padding:      terminal.Padding{X: 12, Y: 10},
	})
	if err != nil {
		return
	}
	// The frames draw at the pane terminal's size; the sink's screen takes
	// that size too, and its view scales it into the card.
	sink.Resize(cols, rows)
	m, err := p.term.NewMirror()
	if err != nil {
		_ = sink.Close()
		return
	}
	sm := &stageMirror{sink: sink, conn: conn, mirror: m, stop: make(chan struct{})}
	// A mirror starts live: it was made because a card wants it. The rail
	// clears the flag on the next frame for any card that does not draw.
	sm.live.Store(true)
	p.stageMirror = sm
	startStageClock()
	go sm.pump()
}

// startStageClock runs the rail's one timer while any card is on it.
func startStageClock() {
	stageTickOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second / stageMirrorFPS)
			defer ticker.Stop()
			for t := range ticker.C {
				// A tick nobody took is a tick that would have drawn the same
				// frame twice: drop it rather than queue behind the rail.
				select {
				case stageTick <- t:
				default:
				}
			}
		}()
	})
}

func (w *workspace) stopStageMirror(p *pane) {
	sm := p.stageMirror
	if sm == nil {
		return
	}
	p.stageMirror = nil
	close(sm.stop)
	sm.mirror.Close()
	_ = sm.conn.Close()
	_ = sm.sink.Close()
}

// pump keeps a preview current at a thumbnail's pace. A pane's output may
// change many times a frame; the rail needs the shape of it, not every byte.
//
// The pace comes from one clock for the whole rail, not one ticker per card.
// A Mirror.Frame() is not cheap: it builds a fresh render state and copies
// every row of the screen, because the changed-row marks go to whichever
// render state reads them first and two mirrors cannot share them. A ticker
// per card therefore cost one whole-screen copy per card per tick, so a
// session with six hidden panes paid six full copies twelve times a second
// for thumbnails nobody was reading closely.
func (sm *stageMirror) pump() {
	for {
		select {
		case <-sm.stop:
			return
		case <-stageTick:
			if !sm.live.Load() {
				// Not on screen: the frame it would copy is a frame nobody
				// sees. The mirror keeps its place, and the first tick after
				// it comes back sends everything that changed meanwhile.
				continue
			}
			if frame, _ := sm.mirror.Frame(); len(frame) > 0 {
				sm.conn.push(frame)
			}
		}
	}
}

// stageTick paces every preview on the rail. It is started once and stopped
// when the last mirror goes, so an app with no rail running has no timer.
var (
	stageTickOnce sync.Once
	stageTick     = make(chan time.Time, 1)
)

// stageOverlay draws the rail over the stage: cards floating at the right
// edge, vertically centered, each one the mirror of a pane it hides.
func (w *workspace) stageOverlay(c *ui.Context, tk tokens, s *session, stageH float32) {
	panes := w.stageRailPanes(s)
	if len(panes) == 0 {
		return
	}
	// Every mirror starts this frame hidden; the cards that draw below mark
	// themselves live. A rail that is not drawn at all leaves them all
	// hidden, which is what stops the copying while nothing shows.
	w.markStageMirrorsIdle()
	// A full stack when there is room; otherwise the cards overlap, as
	// Stage Manager piles the windows behind the one in front, so every
	// pane keeps its top strip and its mark reachable.
	step := stageCardH + stageCardGap
	stackH := float32(len(panes))*stageCardH + float32(len(panes)-1)*stageCardGap
	avail := stageH - 2*stageRailInset
	if stackH > avail && len(panes) > 1 && avail > stageCardH {
		step = (avail - stageCardH) / float32(len(panes)-1)
		if least := stageCardH / 3; step < least {
			step = least
		}
		stackH = stageCardH + float32(len(panes)-1)*step
	}
	top := (stageH - stackH) / 2
	if top < stageRailInset {
		top = stageRailInset
	}
	pose := ui.Pose{}
	if !w.stageRailShown {
		w.stageRailShown = true
		pose = ui.Pose{X: 24, Fade: 1}
		c.After(time.Millisecond)
	}
	rail := ui.Box(c).Width(stageCardW).Height(stackH).Absolute().
		Right(stageRailInset).Top(top).
		AnimatePose(pose, ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut})
	rail.Children(func() {
		for i, p := range panes {
			w.stageCardAt(c, tk, p, float32(i)*step)
		}
	})
	// Only the cards drawn this frame are worth copying the screen for. The
	// rail is rebuilt every frame, so this is where "on screen" is known.
	for _, p := range panes {
		if p.stageMirror != nil {
			p.stageMirror.live.Store(true)
		}
	}
}

// markStageMirrorsIdle clears the live flag on every preview, so a card that
// does not draw this frame is not copied for.
//
// A window nobody is looking at is the largest version of this: a minimised
// or hidden window still runs its frame loop, and its rail would go on
// copying every hidden pane's screen for a picture on no display. The
// mirrors keep their sinks and catch up in one frame when it comes back.
func (w *workspace) markStageMirrorsIdle() {
	if w.win != nil && !w.win.IsVisible() {
		w.stageMirrorsOff()
		return
	}
	for _, pr := range w.allProjects() {
		for _, s := range pr.Sessions {
			if s.Root == nil {
				continue
			}
			for _, p := range s.Root.panes() {
				if p.stageMirror != nil {
					p.stageMirror.live.Store(false)
				}
			}
		}
	}
}

// stageMirrorsOff clears every preview's live flag in one pass.
func (w *workspace) stageMirrorsOff() {
	for _, pr := range w.allProjects() {
		for _, s := range pr.Sessions {
			if s.Root == nil {
				continue
			}
			for _, p := range s.Root.panes() {
				if p.stageMirror != nil {
					p.stageMirror.live.Store(false)
				}
			}
		}
	}
}

// stageCardOffset is how far the rail's card centers sit from the stage's
// center, which is where a stage swap starts and ends.
func (w *workspace) stageCardOffset() float32 {
	if w.stageW <= 0 {
		return 0
	}
	return w.stageW/2 - (stageRailInset + stageCardW/2)
}

// stageCardAt is one pane off stage: a floating window, leaning back, showing
// its terminal mirrored inside, with the pane's mark at its lower left. Every
// card is the same size; the mirror fills it like a thumbnail. top is where
// the card sits in the stack, which overlap shortens when the rail is full.
func (w *workspace) stageCardAt(c *ui.Context, tk tokens, p *pane, top float32) {
	wrap := ui.Box(c).Width(stageCardW).Height(stageCardH).Absolute().Left(0).Top(top)
	// A card that has just joined the rail slides in from the edge; one that
	// was already there stays put while the stage changes behind it.
	pose := ui.Pose{}
	if !p.stageCardShown {
		p.stageCardShown = true
		if p.stageCardShrink {
			// The pane that was on stage comes back small: it starts where
			// the stage was, and shrinks into its card.
			p.stageCardShrink = false
			pose = ui.Pose{Scale: 2.6, X: -w.stageCardOffset()}
		} else {
			pose = ui.Pose{X: 24, Fade: 1}
		}
		c.After(time.Millisecond)
	}
	wrap = wrap.AnimatePose(pose, ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut})
	pick := func() { w.stagePane(p) }
	wrap.OnClick(pick).Cursor(ui.CursorPointer).Tooltip(p.title()).Children(func() {
		// The card is a sheet of the system's material, as a notification
		// banner is, not the theme's paper with a hairline round it. It
		// floats over the stage and hides a pane behind it, which is exactly
		// what the material is for: it frosts what is under it and lights its
		// own rim. A flat fill here was the one opaque surface in a window
		// made of glass, and it read as a screenshot pasted on the stage.
		panel := ui.Box(c).Fill().Clip().Radius(stageCardRadius).
			RotateY(stageTiltY).Perspective(stagePerspective).
			Rotate(-3.5)
		if liquid(c) {
			// The native material paints a hole, so a drawn hairline and a
			// cast shadow would land on it and vanish; the material lights
			// its own rim. The shadow goes on the wrapper, outside the hole.
			panel = panel.Material(noticeGlass(tk))
			wrap = wrap.Shadow(0, 18, 44, 2, stageShadow(tk))
		} else {
			panel = panel.Border(1, tk.Foreground.Alpha(0.14)).
				Background(tk.Background).
				Shadow(0, 16, 48, 2, stageShadow(tk))
		}
		if panel.Hovered() {
			panel = panel.AnimatePose(ui.Pose{Scale: 1.035},
				ui.Animation{Duration: 140 * time.Millisecond, Ease: ui.EaseOut})
		}
		panel.OnClick(pick)
		panel.Children(func() {
			w.stageMirrorView(c, tk, p)
			// The mark sits inside the window's lower left, as an app's icon
			// does on its window in Stage Manager, not on the desktop beside it.
			if src := paneMark(p, tk); src != nil {
				badge := ui.Box(c).Width(22).Height(22).Absolute().
					Left(10).Top(stageCardH-32).Radius(6).
					Background(tk.Raised).Border(1, tk.Border).Center().OnClick(pick)
				badge.Children(func() {
					ui.Image(c, src).Width(14).Height(14).IgnoreInput(true)
				})
			}
		})
	})
}

// stageMirrorView draws the live miniature. The sink's view is laid out at
// the size the pane's own view had, then scaled to fill the card — its
// thumbnail is cropped, never letterboxed, so every card reads the same.
func (w *workspace) stageMirrorView(c *ui.Context, tk tokens, p *pane) {
	if p.stageMirror == nil {
		// A terminal that has not opened its preview yet still says which
		// pane it is.
		ui.Column(c).Fill().Center().Padding(12).Gap(4).Children(func() {
			ui.Text(c, p.title()).FontSize(11).SingleLine().TextColor(tk.MutedForeground)
			ui.Text(c, abbreviate(p.dir())).FontSize(9.5).SingleLine().
				TextColor(tk.MutedForeground)
		})
		return
	}
	natW, natH := w.stageMirrorSize(p)
	scale := max(stageCardW/natW, stageCardH/natH)
	terminal.View(c, p.stageMirror.sink).Width(natW).Height(natH).Absolute().
		Left(0).Top(0).TransformOrigin(0, 0).Scale(scale, scale).
		IgnoreInput(true)
}

// stageMirrorSize is the size a mirror draws at before its card scales it:
// the size the pane's own view last had. A pane a zoomed session brings back
// straight into the rail has none, because no view has laid it out since it
// was built; its grid then stands in at the cell size the last laid-out pane
// measured, which every pane shares, as they all draw one terminal font.
func (w *workspace) stageMirrorSize(p *pane) (float32, float32) {
	if p.viewW > 0 && p.viewH > 0 {
		return p.viewW, p.viewH
	}
	cols, rows := p.term.Size()
	if cols <= 0 || rows <= 0 || w.paneCellW <= 0 || w.paneCellH <= 0 {
		return stageCardW, stageCardH
	}
	return 24 + w.paneCellW*float32(cols), 20 + w.paneCellH*float32(rows)
}

// paneMark is the mark a pane wears: its agent's logo, or a terminal.
func paneMark(p *pane, tk tokens) ui.ImageSource {
	if p.agent != nil {
		return agentLogoSource(p.agent.Kind, tk)
	}
	return iconSource("terminal", colorHex(tk.MutedForeground))
}

func stageShadow(tk tokens) ui.Color {
	alpha := float32(0.55)
	if !tk.Dark { // a light window takes a lighter shadow
		alpha = 0.22
	}
	return ui.RGB(0, 0, 0).Alpha(alpha)
}
