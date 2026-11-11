package main

import (
	"io"
	"sync"
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
)

// stageMirror is one off-stage pane's preview: a sink terminal that shows
// the frames of a Mirror of the pane's own terminal.
type stageMirror struct {
	sink   *terminal.Terminal
	conn   *mirrorConn
	mirror *terminal.Mirror
	stop   chan struct{}
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
	if p == nil {
		return
	}
	s := w.sessionOf(p)
	if s == nil || s.Root == nil || s.zoomedPane == p.ID {
		return
	}
	if cur := s.Root.paneByID(s.zoomedPane); cur != nil && cur != p {
		// The pane that leaves the stage shrinks into the rail.
		cur.stageCardShown = false
		cur.stageCardShrink = true
	}
	s.zoomedPane = p.ID
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	p.stageEnter = true
	w.syncStageMirrors()
	w.invalidate()
	w.scheduleSave()
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
	p.stageMirror = sm
	go sm.pump()
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
func (sm *stageMirror) pump() {
	ticker := time.NewTicker(time.Second / stageMirrorFPS)
	defer ticker.Stop()
	for {
		select {
		case <-sm.stop:
			return
		case <-ticker.C:
			if frame, _ := sm.mirror.Frame(); len(frame) > 0 {
				sm.conn.push(frame)
			}
		}
	}
}

// stageOverlay draws the rail over the stage: cards floating at the right
// edge, vertically centered, each one the mirror of a pane it hides.
func (w *workspace) stageOverlay(c *ui.Context, tk tokens, s *session, stageH float32) {
	panes := w.stageRailPanes(s)
	if len(panes) == 0 {
		return
	}
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
		panel := ui.Box(c).Fill().Clip().Radius(8).
			Border(1, tk.Foreground.Alpha(0.14)).
			Background(tk.Background).
			Shadow(0, 16, 48, 2, stageShadow(tk)).
			RotateY(stageTiltY).Perspective(stagePerspective).
			Rotate(-3.5)
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
	natW, natH := p.viewW, p.viewH
	if natW <= 0 || natH <= 0 {
		natW, natH = stageCardW, stageCardH
	}
	scale := max(stageCardW/natW, stageCardH/natH)
	terminal.View(c, p.stageMirror.sink).Width(natW).Height(natH).Absolute().
		Left(0).Top(0).TransformOrigin(0, 0).Scale(scale, scale).
		IgnoreInput(true)
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
