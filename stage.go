package main

import (
	"io"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The stage rail is zoom's other half. When one pane fills the session, the
// panes it hides float beside it on the right, as Stage Manager keeps the
// windows behind the one in front: upright thumbnails of the same size, each
// with the pane's mark and name above a live, scaled mirror of its terminal.
// A click brings one forward and sends the pane it replaces back to the rail.
//
// They are drawn flat on purpose. A tilted card distorts the text it is
// showing and reads as decoration; a thumbnail standing square, with the
// window's own hairline, a lit top edge and shadows that fall further the
// higher it sits, reads as the window it stands for, which is what Apple's
// stage does.
const (
	stageCardW       float32 = 208
	stageCardH       float32 = 132
	stageCardGap     float32 = 12
	stageRailInset   float32 = 20
	stageRailMinW    float32 = 520
	stageCardRadius  float32 = 12
	stageCardHeaderH float32 = 26
	stageMirrorFPS           = 12
	// stageHoldFor is how long the rail's previews stand still after the
	// stage moves, covering the longest of the two moves (a pane growing
	// out of its card, the one it replaces shrinking back).
	stageHoldFor = 280 * time.Millisecond
)

// stageCardSpring is how a card takes the pointer and how one arrives: the
// short spring SwiftUI uses for what the hand touches, small enough that a
// terminal's thumbnail never looks like a toy.
var stageCardSpring = ui.Animation{Spring: ui.SpringOptions{
	Duration: 220 * time.Millisecond,
	Bounce:   0.08,
}}

// stageMirror is one off-stage pane's preview: a sink terminal that shows
// the frames of a Mirror of the pane's own terminal.
type stageMirror struct {
	sink   *terminal.Terminal
	conn   *mirrorConn
	mirror *terminal.Mirror
	stop   chan struct{}
	// w is the workspace whose stage hold pauses this preview, and held
	// tells that the last tick was one, so the first tick after it renders
	// the pane's screen even when its output did not move meanwhile.
	w    *workspace
	held bool
	// pane is the mirrored pane, and lastOut/lastCols/lastRows what the
	// last frame saw of it: a frame is rendered only when the output byte
	// count or the size moved, and a quiet pane costs the pump nothing.
	pane               *pane
	lastOut            int64
	lastCols, lastRows int
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
	w.holdStagePreviews()
	w.syncStageMirrors()
	return true
}

// holdStagePreviews keeps the rail's previews from feeding their terminals
// while the stage moves. A preview that pushes a frame has its card paint
// the whole terminal grid on that frame, and a card doing that mid-move
// costs the animation the frames it needs to stay smooth. The previews
// stand still for stageHoldFor and then show the pane's current screen.
func (w *workspace) holdStagePreviews() {
	w.stageHoldUntil.Store(time.Now().Add(stageHoldFor).UnixNano())
}

// stagePreviewsHeld reports whether the rail's previews should stand still
// now.
func (w *workspace) stagePreviewsHeld() bool {
	until := w.stageHoldUntil.Load()
	return until != 0 && time.Now().UnixNano() < until
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
	if p == nil || p.term == nil {
		return
	}
	cols, rows := p.term.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	// A mirror frames the screen at the size it was made for; a pane the
	// stage's grid moved takes a fresh one.
	if sm := p.stageMirror; sm != nil {
		if sc, sr := sm.sink.Size(); sc == cols && sr == rows {
			return
		}
		w.stopStageMirror(p)
	}
	conn := newMirrorConn()
	lightTheme, darkTheme := w.terminalThemes()
	sink, err := terminal.New(terminal.Options{
		Conn:         conn,
		ResizeByConn: true,
		Scrollback:   -1,
		ImageLimit:   32 << 20,
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
	sm := &stageMirror{sink: sink, conn: conn, mirror: m, stop: make(chan struct{}), pane: p, w: w}
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
	// The card starts with the pane's screen rather than the folder name: a
	// pane comes back to the rail while it is still being looked at, and
	// the stage hold that follows does not take this frame away.
	sm.render(true)
	ticker := time.NewTicker(time.Second / stageMirrorFPS)
	defer ticker.Stop()
	for {
		select {
		case <-sm.stop:
			return
		case <-ticker.C:
			// While the stage moves, a preview's frame would have its card
			// paint the whole terminal grid on that frame, which the motion
			// cannot afford: the preview stands still and catches up after.
			if sm.w != nil && sm.w.stagePreviewsHeld() {
				sm.held = true
				continue
			}
			// The first tick after a hold renders even a quiet pane, whose
			// screen may have changed into what the last frame showed.
			force := sm.held
			sm.held = false
			sm.render(force)
		}
	}
}

// render draws the pane's screen into the preview: one frame of the mirror,
// pushed to the sink. force asks for it even when the pane's output has not
// moved since the last frame.
func (sm *stageMirror) render(force bool) {
	// Frames walk the whole screen, so skip the ones nothing would change:
	// remote panes report no byte count and render every tick as before.
	out, busy := int64(-1), false
	if p := sm.pane; p != nil && p.conn != nil {
		out, busy = p.conn.outputState()
	}
	cols, rows := 0, 0
	if p := sm.pane; p != nil && p.term != nil {
		cols, rows = p.term.Size()
	}
	if !force && out >= 0 && !busy && out == sm.lastOut && cols == sm.lastCols && rows == sm.lastRows {
		return
	}
	if frame, _ := sm.mirror.Frame(); len(frame) > 0 {
		sm.conn.push(frame)
	}
	// A count the terminal has not caught up with is no mark of what the
	// frame showed: leaving lastOut alone renders the chunk again next
	// tick, once the screen carries it.
	if !busy {
		sm.lastOut, sm.lastCols, sm.lastRows = out, cols, rows
	}
}

// stageGrid is what a pane's terminal is given: its columns and rows, and a
// cell's size in DIPs.
type stageGrid [4]int

// stageFitState is what the stage's grid was last, when it was first seen at
// that value, and whether the rail's panes already have it.
type stageFitState struct {
	grid stageGrid
	at   time.Time
	done bool
}

// stageFitWait is how long the stage's grid must hold still before the rail's
// panes are moved to it: a zoom finds the stage's size over a frame or two,
// and a window being dragged changes it every frame.
const stageFitWait = 150 * time.Millisecond

// fitRailToStage gives the panes the rail hides the stage's grid. A pane that
// comes forward is then already the size the stage lays it out at, and the
// program in it neither reflows nor redraws its whole screen at the moment
// its card moves — which is what a full-screen agent caught mid-frame.
func (w *workspace) fitRailToStage(c *ui.Context, s *session, stage *pane) {
	if s == nil || s.Root == nil || stage == nil || stage.term == nil || stage.viewW <= 0 {
		return
	}
	cols, rows := stage.term.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	g := stageGrid{cols, rows, int(w.paneCellW), int(w.paneCellH)}
	// A stage still finding its size is not one to move the rail to.
	if g != w.stageFit.grid {
		w.stageFit.grid, w.stageFit.at, w.stageFit.done = g, time.Now(), false
		c.After(stageFitWait)
		return
	}
	if w.stageFit.done || w.stageFit.at.IsZero() || time.Since(w.stageFit.at) < stageFitWait {
		return
	}
	w.stageFit.done = true
	moved := false
	for _, p := range s.Root.panes() {
		if p == stage || p.term == nil || p.remote != nil {
			continue
		}
		if pc, pr := p.term.Size(); pc == cols && pr == rows {
			continue
		}
		p.term.ResizePixels(cols, rows, g[2], g[3])
		moved = true
	}
	if moved {
		// A mirror draws at its pane's size, so the ones that moved get a
		// mirror made again.
		w.syncStageMirrors()
	}
}

// stageOverlay draws the rail in the band beside the stage: cards standing at
// the right edge, vertically centered, each one the mirror of a pane it hides.
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
		pose = ui.Pose{X: 16, Fade: 1}
		c.After(time.Millisecond)
	}
	rail := ui.Box(c).Width(stageCardW).Height(stackH).Absolute().
		Right(stageRailInset).Top(top).
		AnimatePose(pose, stageCardSpring)
	rail.Children(func() {
		for i, p := range panes {
			w.stageCardAt(c, tk, p, float32(i)*step, i, len(panes))
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

// stageCardAt is one pane off stage: a thumbnail of its window, its mark and
// name in a title strip, its terminal mirrored below. Every card is the same
// size; top is where it sits in the stack, which overlap shortens when the
// rail is full. index is its place in the rail of count: cards further back
// sit deeper, and a rail coming in lands one card after the next.
func (w *workspace) stageCardAt(c *ui.Context, tk tokens, p *pane, top float32, index, count int) {
	wrap := ui.Box(c).Width(stageCardW).Height(stageCardH).Absolute().Left(0).Top(top)
	// A card that has just joined the rail comes in from the edge; one that
	// was already there stays put while the stage changes behind it.
	pose := ui.Pose{}
	anim := stageCardSpring
	if !p.stageCardShown {
		p.stageCardShown = true
		if p.stageCardShrink {
			// The pane that was on stage comes back small: it starts where
			// the stage was, and shrinks into its card.
			p.stageCardShrink = false
			pose = ui.Pose{Scale: 2.4, X: -w.stageCardOffset()}
		} else {
			pose = ui.Pose{X: 16, Fade: 1, Scale: 0.98}
			d := stageCardSpring
			d.Delay = time.Duration(index) * 24 * time.Millisecond
			anim = d
		}
		c.After(time.Millisecond)
	}
	wrap = wrap.AnimatePose(pose, anim)
	pick := func() { w.stagePane(p) }
	wrap.OnClick(pick).Cursor(ui.CursorPointer).Tooltip(p.title()).Children(func() {
		w.stageCardPanel(c, tk, p, pick, count-1-index)
	})
}

// stageCardPanel is the window itself: the pane's own paper, a hairline and a
// lit top edge, the name strip, and the live mirror under it. The pointer
// lifts it a hundredth of itself and takes the rim from the hairline to the
// text's own tone, as a window under the pointer lifts in the Finder.
func (w *workspace) stageCardPanel(c *ui.Context, tk tokens, p *pane, pick func(), depth int) {
	ink := ui.RGB(0x11, 0x11, 0x1a)
	// The screen is the pane's own paper, so the mirror inside sits on the
	// colour it was drawn on and the thumbnail reads as that terminal; the
	// name strip above it takes the floating surface, one step off the paper,
	// as a window's title bar does.
	panel := ui.Column(c).Fill().Clip().Radius(stageCardRadius).
		Background(tk.Background).
		Transition(ui.ElementTransition{
			Duration: hoverMotion,
			Ease:     ui.EaseOut,
			Colors:   true,
			Shadows:  true,
		})
	hover := panel.Hovered()
	rim := tk.Border
	if hover {
		rim = tk.Foreground.Alpha(0.22)
		panel = panel.AnimatePose(ui.Pose{Scale: 1.012}, stageCardSpring)
	}
	// The elevation, as the app's raised cards have it: a contact shadow and
	// two that fall further and softer. Cards further back sit lower and
	// cast less, and the pointer's own card casts more, so the stack reads as
	// depth rather than as a column of equal tiles. The count and order of
	// the shadows never change, which is what lets the transition animate
	// them one by one.
	rest := 1 - float32(min(depth, 3))*0.07
	deep := float32(1)
	if hover {
		deep = 1.5
	}
	if tk.Dark {
		panel = panel.
			InsetShadow(0, 1, 2, 0, ui.RGB(0xff, 0xff, 0xff).Alpha(0.06)).
			Shadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.34*rest)).
			Shadow(0, 6, 16, -4, ui.RGB(0, 0, 0).Alpha(0.40*rest*deep)).
			Shadow(0, 16, 32, -10, ui.RGB(0, 0, 0).Alpha(0.48*rest*deep))
	} else {
		panel = panel.
			Shadow(0, 1, 2, 0, ink.Alpha(0.06*rest)).
			Shadow(0, 6, 16, -4, ink.Alpha(0.09*rest*deep)).
			Shadow(0, 16, 32, -10, ink.Alpha(0.11*rest*deep))
	}
	panel = panel.Border(1, rim).OnClick(pick)
	panel.Children(func() {
		// The name strip stands where a window's title bar does, so a rail of
		// terminals can be told apart before any of the text is readable.
		ui.Row(c).Height(stageCardHeaderH).Padding(0, 10, 0, 10).Gap(6).
			Center().Background(floatingFill(tk)).Children(func() {
			if src := paneMark(p, tk); src != nil {
				ui.Image(c, src).Width(13).Height(13).IgnoreInput(true)
			}
			name := tk.MutedForeground
			if hover {
				name = tk.Foreground
			}
			ui.Text(c, p.title()).FontSize(11).FontWeight(600).
				TextColor(name).SingleLine()
			ui.Spacer(c)
		})
		ui.Box(c).Height(1).Background(tk.Border)
		ui.Box(c).Fill().Clip().Children(func() {
			w.stageMirrorView(c, tk, p)
		})
	})
}

// stageMirrorView draws the live miniature: the pane's terminal scaled down
// whole, the way a window is thumbnailed. Keeping its own proportions keeps
// the text readable as a shape; filling the card by cropping turns a screen
// of text into a blur of glyphs.
func (w *workspace) stageMirrorView(c *ui.Context, tk tokens, p *pane) {
	bodyH := stageCardH - stageCardHeaderH - 1
	if p.stageMirror == nil {
		// A terminal that has not opened its preview yet still says where
		// the pane is.
		ui.Column(c).Fill().Center().Padding(12).Children(func() {
			ui.Text(c, abbreviate(p.dir())).FontSize(9.5).SingleLine().
				TextColor(tk.MutedForeground)
		})
		return
	}
	natW, natH := w.stageMirrorSize(p)
	scale := min(stageCardW/natW, bodyH/natH)
	left := (stageCardW - natW*scale) / 2
	top := (bodyH - natH*scale) / 2
	terminal.View(c, p.stageMirror.sink).Width(natW).Height(natH).Absolute().
		Left(left).Top(top).TransformOrigin(0, 0).Scale(scale, scale).
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
	if p.term == nil {
		return stageCardW, stageCardH
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
