package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// The other half of the LAN remote: this Mac's gexi reaching another one.
// A host is an address and a token pairing gave us; a window shows the
// host's sessions and one session's terminal, as the terminal itself draws
// it: the pane's output is streamed and fed to a terminal of the same size,
// so colors, the cursor, full-screen programs and the keys typed work as
// they do on the host.

var remoteHTTP = &http.Client{Timeout: 15 * time.Second}

var errRemoteNotPaired = errors.New("this Mac is no longer paired; add it again")

type remoteClient struct {
	Name    string
	Address string
	Token   string
}

type remoteSession struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Panes []string `json:"panes"`
}

type remotePane struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Cwd      string         `json:"cwd"`
	Focused  bool           `json:"focused"`
	Activity *agentActivity `json:"activity"`
}

// remoteStreamView is what one poll of a pane's mirror brings: the output
// after the offset the client has, or a snapshot of the whole screen when
// the client starts over, with the size and the view the pane has.
type remoteStreamView struct {
	Pane     string  `json:"pane"`
	Title    string  `json:"title"`
	Cwd      string  `json:"cwd"`
	Offset   int64   `json:"offset"`
	Data     string  `json:"data"`
	Snapshot string  `json:"snapshot"`
	Reset    bool    `json:"reset"`
	Cols     int     `json:"cols"`
	Rows     int     `json:"rows"`
	ViewW    float32 `json:"viewW"`
	ViewH    float32 `json:"viewH"`
}

func (c remoteClient) call(method string, params map[string]any, out any) error {
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"id":     newID(),
		"method": method,
		"params": params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+c.Address+"/api/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := remoteHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return errRemoteNotPaired
	case http.StatusOK:
	default:
		return fmt.Errorf("remote: %s", resp.Status)
	}
	var env controlResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if !env.OK {
		if env.Error == "" {
			env.Error = "request failed"
		}
		return errors.New(env.Error)
	}
	if out == nil {
		return nil
	}
	data, err := json.Marshal(env.Result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// pairRemoteHost trades the code for a token and keeps the host's own name.
func pairRemoteHost(address, code, name string) (remoteHost, error) {
	body, err := json.Marshal(map[string]string{
		"code":   code,
		"device": "gexi on " + deviceName(),
	})
	if err != nil {
		return remoteHost{}, err
	}
	resp, err := remoteHTTP.Post("http://"+address+"/api/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return remoteHost{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
		Host  string `json:"host"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return remoteHost{}, err
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error == "" {
			out.Error = resp.Status
		}
		return remoteHost{}, errors.New(out.Error)
	}
	if out.Token == "" {
		return remoteHost{}, errors.New("the other Mac did not return a token")
	}
	if strings.TrimSpace(name) == "" {
		name = out.Host
	}
	if name == "" {
		name = address
	}
	return remoteHost{Name: name, Address: address, Token: out.Token}, nil
}

// normalizeRemoteAddress takes what the user typed and makes an address the
// client can dial: host:port, with the remote port when they left it out.
func normalizeRemoteAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if u.Port() != "" {
		return net.JoinHostPort(u.Hostname(), u.Port())
	}
	return net.JoinHostPort(u.Hostname(), strconv.Itoa(remotePort))
}

// MARK: the add-host dialog

func (w *workspace) openHostDialog() {
	w.hostOpen = true
	w.hostErr = ""
	w.hostBusy = false
	w.hostName = ""
	w.hostAddr = ""
	// The usual path is copy the code on the other Mac and walk over: take
	// it from the clipboard when it already looks like one.
	code := strings.TrimSpace(readClipboard())
	if isPairingCode(code) {
		w.hostCode = code
	} else {
		w.hostCode = ""
	}
	w.invalidate()
}

func isPairingCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (w *workspace) hostDialog(c *ui.Context, tk tokens) {
	ui.Column(c).Width(440).Padding(20).Gap(8).Children(func() {
		ui.Text(c, locales.RemoteAddTitle()).FontSize(14).FontWeight(600).
			TextColor(tk.Foreground)
		ui.Text(c, locales.RemoteAddBody()).FontSize(11).TextColor(tk.MutedForeground)

		ui.Text(c, locales.RemoteAddName()).FontSize(11).
			TextColor(tk.MutedForeground).Padding(6, 0, 0, 0)
		ui.TextInput(c, &w.hostName).Height(28).FontSize(12).
			Placeholder(locales.RemoteAddNamePlaceholder())

		ui.Text(c, locales.RemoteAddAddress()).FontSize(11).
			TextColor(tk.MutedForeground).Padding(6, 0, 0, 0)
		ui.TextInput(c, &w.hostAddr).Height(28).FontSize(12).
			Placeholder("192.168.1.23:" + strconv.Itoa(remotePort))

		ui.Text(c, locales.RemoteAddCode()).FontSize(11).
			TextColor(tk.MutedForeground).Padding(6, 0, 0, 0)
		ui.TextInput(c, &w.hostCode).Height(28).FontSize(12).Placeholder("000000")

		if w.hostErr != "" {
			ui.Text(c, w.hostErr).FontSize(11).TextColor(tk.Waiting)
		}
		ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			ui.Button(c, locales.Cancel()).OnClick(func() {
				w.hostOpen = false
				w.invalidate()
			})
			label := locales.RemoteConnect()
			if w.hostBusy {
				label = locales.RemoteConnecting()
			}
			ui.Button(c, label).OnClick(func() { w.submitHost() })
		})
	})
}

// submitHost pairs off the UI thread, then keeps the host and opens it.
func (w *workspace) submitHost() {
	if w.hostBusy {
		return
	}
	address := normalizeRemoteAddress(w.hostAddr)
	code := strings.TrimSpace(w.hostCode)
	name := strings.TrimSpace(w.hostName)
	if address == "" {
		w.hostErr = locales.RemoteAddAddressMissing()
		w.invalidate()
		return
	}
	if !isPairingCode(code) {
		w.hostErr = locales.RemoteAddCodeMissing()
		w.invalidate()
		return
	}
	w.hostBusy = true
	w.hostErr = ""
	w.invalidate()
	go func() {
		host, err := pairRemoteHost(address, code, name)
		w.onMain(func() {
			w.hostBusy = false
			if err != nil {
				w.hostErr = err.Error()
				w.invalidate()
				return
			}
			w.addRemoteHost(host)
			w.hostOpen = false
			w.invalidate()
			w.invalidateSettings()
			w.openRemoteHost(host)
		})
	}()
}

// remoteMenu is the sidebar's server menu: the Macs already added, and the
// door to add one.
func (w *workspace) remoteMenu(m *ui.Menu) {
	if len(w.settings.RemoteHosts) == 0 {
		m.Item(locales.SettingsRemoteNoHosts()).Disabled(true)
	}
	for _, entry := range w.settings.RemoteHosts {
		host := entry
		if m.Item(host.Name + "  " + host.Address).Chosen() {
			w.openRemoteHost(host)
		}
	}
	m.Separator()
	if m.Item(locales.RemoteAddTitle()).Chosen() {
		w.openHostDialog()
	}
}

// MARK: the sidebar's remote hosts

// remoteSection lists the paired Macs under the projects, each row opening
// the host and its live sessions. It keeps out of the way while the search
// box filters local projects.
func (w *workspace) remoteSection(c *ui.Context, tk tokens) {
	if len(w.settings.RemoteHosts) == 0 || strings.TrimSpace(w.searchQuery) != "" {
		return
	}
	for _, host := range w.settings.RemoteHosts {
		w.remoteHostRow(c, tk, host)
	}
}

func (w *workspace) remoteHostRow(c *ui.Context, tk tokens, host remoteHost) {
	rc := w.remoteConns[host.Address]
	open := w.remoteOpen[host.Address]
	selected := w.remoteActive == host.Address
	row := ui.Row(c).Height(rowHeightPx).Padding(0, 8, 0, 8).Gap(6).
		Radius(chromeRadius).MarginX(2)
	hovered := row.Hovered()
	color := tk.MutedForeground
	if selected || hovered {
		color = tk.Foreground
	}
	switch {
	case selected:
		row = controlWash(c, row, tk, 0.09, tk.Active)
	case hovered:
		row = controlWash(c, row, tk, 0.045, tk.Active)
	}
	pick := func() { w.openRemoteHost(host) }
	toggle := func() {
		w.remoteOpen[host.Address] = !open
		w.invalidate()
	}
	row.OnClick(pick).Children(func() {
		chevron := "chevron-right"
		if open {
			chevron = "chevron-down"
		}
		if src := iconSource(chevron, colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(15).Height(15).OnClick(toggle)
		}
		if src := iconSource("monitor", colorHex(color)); src != nil {
			ui.Image(c, src).Width(14).Height(14).OnClick(pick)
		}
		ui.Text(c, host.Name).FontSize(12).FontWeight(500).SingleLine().
			TextColor(color).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
		if rc != nil && rc.err != "" {
			ui.Box(c).Width(7).Height(7).Radius(4).Background(tk.Waiting).
				Tooltip(rc.err).OnClick(pick)
		}
	})
	if !open || rc == nil {
		return
	}
	for _, s := range rc.sessions {
		w.remoteSessionRow(c, tk, rc, s)
	}
}

func (w *workspace) remoteSessionRow(c *ui.Context, tk tokens, rc *remoteConn, s remoteSession) {
	sel := w.remoteActive == rc.host.Address && rc.session != nil && rc.session.ID == s.ID
	row := ui.Row(c).Height(rowHeightPx).Padding(0, 8, 0, 8).Gap(8).
		Radius(chromeRadius).MarginX(2)
	hovered := row.Hovered()
	color := tk.MutedForeground
	if sel || hovered {
		color = tk.Foreground
	}
	switch {
	case sel:
		row = controlWash(c, row, tk, 0.09, tk.Active)
	case hovered:
		row = controlWash(c, row, tk, 0.045, tk.Active)
	}
	panes := rc.panesOf(s)
	state := ""
	for _, pane := range panes {
		if pane.Activity == nil {
			continue
		}
		if pane.Activity.State == activityWaiting {
			state = activityWaiting
			break
		}
		if pane.Activity.State == activityWorking {
			state = activityWorking
		}
	}
	label := s.Title
	if label == "" && len(panes) > 0 {
		label = panes[0].Cwd
	}
	pick := func() { rc.openSession(s) }
	row.OnClick(pick).Children(func() {
		if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(16).Height(16).OnClick(pick)
		}
		labelColor := color
		if state == activityWaiting {
			labelColor = tk.Foreground
		}
		ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine().
			TextColor(labelColor).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
		if state != "" {
			dot := tk.Working
			if state == activityWaiting {
				dot = tk.Waiting
			}
			ui.Box(c).Width(7).Height(7).Radius(4).Background(dot).OnClick(pick)
		}
	})
}

// MARK: the remote host, inside the main window

// remoteConn is this Mac's link to one paired host. It keeps the host's
// sessions and, while a session is on the canvas, the terminal showing one
// of its panes: the pane's output is polled as a mirror and fed to a sink
// terminal here. The sidebar lists the sessions under the host's row and
// the canvas shows the terminal in the same window as the local projects.
type remoteConn struct {
	ws     *workspace
	host   remoteHost
	client remoteClient

	// mu guards the target a poll reads while the main thread changes it.
	mu        sync.Mutex
	sessionID string
	paneID    string

	loading  bool
	err      string
	sessions []remoteSession
	panes    []remotePane
	session  *remoteSession

	title string
	cwd   string

	// The mirror: term is the sink the frames feed, sink its connection,
	// and streamPane the pane it shows. Main thread only.
	term       *terminal.Terminal
	sink       *remoteSink
	streamPane string
	streamStop chan struct{}
	cols, rows int
	viewW      float32
	viewH      float32
	// areaW and areaH are the room the terminal view had last frame, which
	// scales the sink into it. focus asks the next frame to focus it.
	areaW, areaH float32
	focus        bool

	stop chan struct{}
}

// startRemoteClients starts a poll loop for every host already paired, so
// the sidebar can list their sessions as soon as the window opens.
func (w *workspace) startRemoteClients() {
	for _, host := range w.settings.RemoteHosts {
		w.remoteOpen[host.Address] = true
		w.remoteConn(host)
	}
}

// stopRemoteClients ends every poll loop. Called on quit.
func (w *workspace) stopRemoteClients() {
	for address, rc := range w.remoteConns {
		rc.close()
		delete(w.remoteConns, address)
	}
}

// remoteConn returns the link to host, starting it when it is the first
// time the app reaches it. Main thread.
func (w *workspace) remoteConn(host remoteHost) *remoteConn {
	if rc := w.remoteConns[host.Address]; rc != nil {
		return rc
	}
	rc := &remoteConn{
		ws:      w,
		host:    host,
		client:  remoteClient{Name: host.Name, Address: host.Address, Token: host.Token},
		loading: true,
		stop:    make(chan struct{}),
	}
	w.remoteConns[host.Address] = rc
	go rc.refreshLoop()
	return rc
}

// openRemoteHost brings a host to the front of the window: its row in the
// sidebar opens, and the canvas shows the host's session list.
func (w *workspace) openRemoteHost(host remoteHost) {
	w.remoteConn(host)
	w.remoteActive = host.Address
	w.remoteOpen[host.Address] = true
	if w.settings.SidebarCollapsed {
		w.settings.SidebarCollapsed = false
		w.scheduleSave()
	}
	w.invalidate()
}

// leaveRemote puts the canvas back on the local work. Main thread.
func (w *workspace) leaveRemote() {
	if w.remoteActive == "" {
		return
	}
	w.remoteActive = ""
	w.invalidate()
}

// activeRemote is the host the canvas shows, or nil while it shows local
// work. Main thread.
func (w *workspace) activeRemote() *remoteConn {
	if w.remoteActive == "" {
		return nil
	}
	return w.remoteConns[w.remoteActive]
}

func (rc *remoteConn) close() {
	select {
	case <-rc.stop:
	default:
		close(rc.stop)
	}
	// stopRemoteClients runs on the main thread, where the mirror lives.
	rc.stopStream()
}

func (rc *remoteConn) stopped() bool {
	select {
	case <-rc.stop:
		return true
	default:
		return false
	}
}

func (rc *remoteConn) target() (sessionID, paneID string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.sessionID, rc.paneID
}

func (rc *remoteConn) refreshLoop() {
	rc.refresh()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-rc.stop:
			return
		case <-ticker.C:
			rc.refresh()
		}
	}
}

func (rc *remoteConn) refresh() {
	if rc.stopped() || rc.ws.quitting {
		return
	}
	// The sidebar keeps the host's sessions and their activity current even
	// while one of them fills the canvas.
	var sessions []remoteSession
	if err := rc.client.call("session.list", nil, &sessions); err != nil {
		rc.fail(err)
		return
	}
	var panes []remotePane
	_ = rc.client.call("pane.list", map[string]any{"all": true}, &panes)
	rc.update(func() {
		rc.loading = false
		rc.err = ""
		rc.sessions = sessions
		rc.panes = panes
		if rc.session != nil {
			for i := range sessions {
				if sessions[i].ID == rc.session.ID {
					rc.session = &sessions[i]
					break
				}
			}
			// A session opened before its panes were listed picks the
			// focused one now, and the mirror starts with it.
			if rc.currentPaneID() == "" {
				if ps := rc.panesOf(*rc.session); len(ps) > 0 {
					rc.mu.Lock()
					rc.paneID = ps[0].ID
					for _, p := range ps {
						if p.Focused {
							rc.paneID = p.ID
							break
						}
					}
					rc.mu.Unlock()
				}
			}
		}
		// Runs on the main thread, where the mirror's terminal is made.
		rc.startStreamIfNeeded()
	})
	// The mirror of the session on the canvas runs on its own; the poll
	// only keeps the sidebar current.
}

// viewingRemote says whether the canvas shows address. Polled from a
// goroutine, so it reads the selection on the main thread.
func (w *workspace) viewingRemote(address string) bool {
	var on bool
	mygo.RunOnMain(func() { on = w.remoteActive == address })
	return on
}

func (rc *remoteConn) update(fn func()) {
	if rc.stopped() {
		return
	}
	if rc.ws.win == nil {
		fn()
		return
	}
	// The main window rebuilds its content: the sidebar's host row and the
	// canvas both read what the poll brings back.
	rc.ws.win.Update(func() {
		if rc.stopped() {
			return
		}
		fn()
	})
}

func (rc *remoteConn) fail(err error) {
	if errors.Is(err, errRemoteNotPaired) {
		err = errors.New(locales.RemoteNotPaired())
	}
	rc.update(func() {
		rc.loading = false
		rc.err = err.Error()
	})
}

func (rc *remoteConn) panesOf(s remoteSession) []remotePane {
	var out []remotePane
	for _, pane := range rc.panes {
		if includesString(s.Panes, pane.ID) {
			out = append(out, pane)
		}
	}
	return out
}

func includesString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// openSession shows one remote session's terminal in the canvas. Main
// thread.
func (rc *remoteConn) openSession(s remoteSession) {
	panes := rc.panesOf(s)
	paneID := ""
	for _, pane := range panes {
		if pane.Focused {
			paneID = pane.ID
			break
		}
	}
	if paneID == "" && len(panes) > 0 {
		paneID = panes[0].ID
	}
	rc.mu.Lock()
	rc.sessionID, rc.paneID = s.ID, paneID
	rc.mu.Unlock()
	rc.session = &s
	rc.title = s.Title
	rc.cwd = ""
	rc.loading = true
	rc.ws.remoteActive = rc.host.Address
	rc.startStreamIfNeeded()
	rc.ws.invalidate()
}

// back leaves the session and returns the canvas to the host's session
// list. Main thread.
func (rc *remoteConn) back() {
	rc.mu.Lock()
	rc.sessionID, rc.paneID = "", ""
	rc.mu.Unlock()
	rc.session = nil
	rc.loading = true
	rc.stopStream()
	rc.ws.invalidate()
}

func (rc *remoteConn) selectPane(paneID string) {
	rc.mu.Lock()
	rc.paneID = paneID
	rc.mu.Unlock()
	rc.startStreamIfNeeded()
	rc.ws.invalidate()
}

// remoteStreamInterval paces the mirror's polls: a frame of the display,
// the delay a keystroke or a program's output takes to show.
const remoteStreamInterval = 16 * time.Millisecond

// remoteSink is the sink terminal's connection to the pane on the host: the
// frames the mirror's poll brings, which the terminal reads, and the keys
// typed into it, which go back as pane.send. Writes keep their order: one
// goroutine sends them as they were typed.
type remoteSink struct {
	rc   *remoteConn
	mu   sync.Mutex
	cond *sync.Cond
	buf  [][]byte
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func newRemoteSink(rc *remoteConn) *remoteSink {
	c := &remoteSink{
		rc:   rc,
		out:  make(chan []byte, 64),
		done: make(chan struct{}),
	}
	c.cond = sync.NewCond(&c.mu)
	go c.sendLoop()
	return c
}

func (c *remoteSink) Read(p []byte) (int, error) {
	c.mu.Lock()
	for len(c.buf) == 0 {
		select {
		case <-c.done:
			c.mu.Unlock()
			return 0, io.EOF
		default:
		}
		c.cond.Wait()
	}
	frame := c.buf[0]
	n := copy(p, frame)
	if n == len(frame) {
		c.buf = c.buf[1:]
	} else {
		c.buf[0] = frame[n:]
	}
	c.mu.Unlock()
	return n, nil
}

func (c *remoteSink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	select {
	case c.out <- append([]byte(nil), p...):
	case <-c.done:
	}
	return len(p), nil
}

func (c *remoteSink) push(b []byte) {
	if len(b) == 0 {
		return
	}
	c.mu.Lock()
	select {
	case <-c.done:
	default:
		c.buf = append(c.buf, b)
	}
	c.mu.Unlock()
	c.cond.Signal()
}

func (c *remoteSink) Close() error {
	c.once.Do(func() {
		close(c.done)
		c.mu.Lock()
		c.cond.Broadcast()
		c.mu.Unlock()
	})
	return nil
}

// sendLoop sends what was typed, in order, to the pane the mirror shows.
func (c *remoteSink) sendLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.out:
			_, paneID := c.rc.target()
			if paneID == "" {
				continue
			}
			_ = c.rc.client.call("pane.send", map[string]any{
				"pane": paneID,
				"text": string(b),
			}, nil)
		}
	}
}

// startStreamIfNeeded brings up the mirror of the pane the canvas shows,
// or takes down the one of a pane it no longer does. Main thread.
func (rc *remoteConn) startStreamIfNeeded() {
	paneID := rc.currentPaneID()
	if paneID == "" {
		rc.stopStream()
		return
	}
	if rc.streamPane == paneID && rc.term != nil {
		return
	}
	rc.startStream(paneID)
}

// startStream makes a terminal for the pane's mirror and polls it. Main
// thread.
func (rc *remoteConn) startStream(paneID string) {
	rc.stopStream()
	sink := newRemoteSink(rc)
	lightTheme, darkTheme := rc.ws.terminalThemes()
	term, err := terminal.New(terminal.Options{
		Conn:         sink,
		ResizeByConn: true,
		Scrollback:   4 << 20,
		Transparent:  true,
		Theme:        lightTheme,
		DarkTheme:    darkTheme,
		Font: terminal.Font{
			Family:     terminalFont + ", SF Mono, Menlo, monospace",
			Size:       12,
			Weight:     400,
			LineHeight: 1.2,
		},
		Padding: terminal.Padding{X: 12, Y: 10},
	})
	if err != nil {
		_ = sink.Close()
		rc.fail(err)
		return
	}
	stop := make(chan struct{})
	rc.sink = sink
	rc.term = term
	rc.streamPane = paneID
	rc.streamStop = stop
	rc.focus = true
	go rc.streamLoop(paneID, sink, term, stop)
}

// stopStream takes the mirror down. Main thread.
func (rc *remoteConn) stopStream() {
	if rc.streamStop != nil {
		close(rc.streamStop)
		rc.streamStop = nil
	}
	if rc.sink != nil {
		_ = rc.sink.Close()
		rc.sink = nil
	}
	if rc.term != nil {
		_ = rc.term.Close()
		rc.term = nil
	}
	rc.streamPane = ""
	rc.cols, rc.rows = 0, 0
	rc.viewW, rc.viewH = 0, 0
}

// streamLoop polls the pane's mirror and feeds the sink terminal what came
// back. It runs until the pane changes or the app quits.
func (rc *remoteConn) streamLoop(paneID string, sink *remoteSink, term *terminal.Terminal, stop chan struct{}) {
	offset := int64(0)
	snapshot := true
	cols, rows := 0, 0
	first := true
	var title, cwd string
	var vw, vh float32
	var ucols, urows int
	for {
		select {
		case <-stop:
			return
		case <-rc.stop:
			return
		default:
		}
		// Nothing to show while the canvas is on local work: the log keeps
		// the output until the next poll starts over from a snapshot.
		if !rc.ws.viewingRemote(rc.host.Address) {
			time.Sleep(250 * time.Millisecond)
			snapshot = false
			continue
		}
		var view remoteStreamView
		err := rc.client.call("pane.stream", map[string]any{
			"pane":     paneID,
			"offset":   offset,
			"snapshot": snapshot,
		}, &view)
		if err != nil {
			if errors.Is(err, errRemoteNotPaired) {
				rc.fail(err)
				return
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		snapshot = false
		if view.Reset || view.Cols != cols || view.Rows != rows {
			cols, rows = view.Cols, view.Rows
			if cols > 0 && rows > 0 {
				term.Resize(cols, rows)
			}
		}
		if view.Reset {
			if snap, err := base64.StdEncoding.DecodeString(view.Snapshot); err == nil {
				sink.push(snap)
			}
		}
		if data, err := base64.StdEncoding.DecodeString(view.Data); err == nil {
			sink.push(data)
		}
		offset = view.Offset
		// The sink draws itself as it is fed; the header and the size the
		// canvas lays the view out at change with the poll, not every frame.
		if first || view.Title != title || view.Cwd != cwd || view.Cols != ucols ||
			view.Rows != urows || view.ViewW != vw || view.ViewH != vh {
			first = false
			title, cwd, ucols, urows, vw, vh = view.Title, view.Cwd, view.Cols, view.Rows, view.ViewW, view.ViewH
			rc.update(func() {
				rc.loading = false
				rc.err = ""
				rc.cols, rc.rows = view.Cols, view.Rows
				rc.viewW, rc.viewH = view.ViewW, view.ViewH
				if view.Title != "" {
					rc.title = view.Title
				}
				rc.cwd = view.Cwd
			})
		}
		time.Sleep(remoteStreamInterval)
	}
}

func (rc *remoteConn) view(c *ui.Context) {
	tk := rc.ws.tokens(c)
	outer := ui.Column(c).Fill()
	if rc.ws.settings.PaneStyle == paneComfortable {
		outer = outer.Padding(6)
	}
	outer.Children(func() {
		// The host's sessions take the same card the local panes do, so the
		// remote work sits in the window rather than in a window of its own.
		col := ui.Column(c).Fill()
		glassCard := liquid(c) && rc.ws.settings.PaneStyle == paneComfortable
		if rc.ws.settings.PaneStyle == paneComfortable {
			col = col.Radius(windowRadius).Clip()
			if glassCard {
				col = col.Border(1, rimTint(tk)).Background(cardTint(tk))
			} else {
				col = col.Border(1, tk.Border).Background(tk.Background)
			}
			col = col.Shadow(0, 1, 20, 0, paneShadow(tk))
		}
		col.Children(func() {
			headerBg := tk.Background
			if glassCard {
				headerBg = ui.Transparent
			}
			header := ui.Row(c).Height(headerHeight).Padding(0, 8, 0, 10).Gap(8).
				Background(headerBg).DragWindow()
			header.Children(func() {
				if rc.session != nil {
					textButton(c, tk, "‹", rc.back, locales.RemoteBack())
				}
				ui.Column(c).Gap(1).Children(func() {
					title := rc.client.Name
					if rc.session != nil {
						title = rc.title
						if title == "" {
							title = rc.session.Title
						}
					}
					if title == "" {
						title = rc.client.Address
					}
					ui.Text(c, title).FontSize(12).FontWeight(500).
						TextColor(tk.Foreground).SingleLine()
					subtitle := rc.client.Address
					if rc.session != nil && rc.cwd != "" {
						subtitle = rc.cwd
					}
					ui.Text(c, subtitle).FontSize(10).TextColor(tk.MutedForeground).SingleLine()
				})
				ui.Spacer(c)
				textButton(c, tk, "⟳", func() { go rc.refresh() }, locales.RemoteRefresh())
			})
			if rc.err != "" {
				ui.Row(c).Padding(8, 12, 8, 12).Background(tk.Waiting.Alpha(0.12)).
					Children(func() {
						ui.Text(c, rc.err).FontSize(11).TextColor(tk.Waiting)
					})
			}
			if rc.session == nil {
				rc.listView(c, tk)
				return
			}
			rc.terminalView(c, tk)
		})
	})
}

func (rc *remoteConn) listView(c *ui.Context, tk tokens) {
	if rc.loading && len(rc.sessions) == 0 {
		ui.Spacer(c)
		ui.Text(c, locales.RemoteConnecting()).FontSize(12).
			TextColor(tk.MutedForeground)
		ui.Spacer(c)
		return
	}
	if len(rc.sessions) == 0 {
		ui.Spacer(c)
		ui.Text(c, locales.RemoteNoSessions()).FontSize(12).
			TextColor(tk.MutedForeground)
		ui.Spacer(c)
		return
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(12, 14, 16, 14).Gap(2).Children(func() {
			for _, s := range rc.sessions {
				rc.sessionRow(c, tk, s)
			}
		})
	})
}

func (rc *remoteConn) sessionRow(c *ui.Context, tk tokens, s remoteSession) {
	session := s
	panes := rc.panesOf(s)
	state := ""
	for _, pane := range panes {
		if pane.Activity == nil {
			continue
		}
		if pane.Activity.State == activityWaiting {
			state = activityWaiting
			break
		}
		if pane.Activity.State == activityWorking {
			state = activityWorking
		}
	}
	path := ""
	if len(panes) > 0 {
		path = panes[0].Cwd
	}
	row := ui.Row(c).Height(46).Padding(0, 10, 0, 10).Gap(8).
		Radius(chromeRadius).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row = row.Background(tk.Active)
	}
	row.OnClick(func() { rc.openSession(session) }).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, session.Title).FontSize(13).TextColor(tk.Foreground).
				SingleLine().OnClick(func() { rc.openSession(session) })
			ui.Text(c, path).FontSize(11).TextColor(tk.MutedForeground).
				SingleLine().OnClick(func() { rc.openSession(session) })
		})
		ui.Spacer(c).OnClick(func() { rc.openSession(session) })
		if state != "" {
			color := tk.Working
			if state == activityWaiting {
				color = tk.Waiting
			}
			ui.Box(c).Width(7).Height(7).Radius(4).Background(color)
		}
	})
}

func (rc *remoteConn) terminalView(c *ui.Context, tk tokens) {
	panes := rc.panesOf(*rc.session)
	if len(panes) > 1 {
		ui.Row(c).Height(32).Padding(0, 10, 0, 10).Gap(4).Children(func() {
			for _, pane := range panes {
				p := pane
				label := p.Title
				if label == "" {
					label = p.Cwd
				}
				selected := p.ID == rc.currentPaneID()
				chip := ui.Row(c).Height(22).Padding(0, 8, 0, 8).Radius(5).
					Border(1, tk.Border).Cursor(ui.CursorPointer)
				if selected {
					chip = chip.BorderColor(tk.Foreground)
				} else if chip.Hovered() {
					chip = chip.Background(tk.Active)
				}
				chip.OnClick(func() { rc.selectPane(p.ID) }).Children(func() {
					ui.Text(c, label).FontSize(11).TextColor(tk.MutedForeground).
						SingleLine().OnClick(func() { rc.selectPane(p.ID) })
				})
			}
		})
	}
	// The pane's terminal as the host draws it: the mirror's frames feed a
	// sink of the host's size, scaled into the room the canvas leaves.
	if rc.term == nil {
		ui.Spacer(c)
		ui.Text(c, locales.RemoteConnecting()).FontSize(12).
			TextColor(tk.MutedForeground)
		ui.Spacer(c)
		return
	}
	natW, natH := rc.naturalSize()
	area := ui.Box(c).Fill()
	area.Children(func() {
		availW, availH := rc.areaW, rc.areaH
		if availW <= 0 || availH <= 0 {
			availW, availH = natW, natH
		}
		scale := min(availW/natW, availH/natH, 1)
		w, h := natW*scale, natH*scale
		// The card centers the grid; the view inside it is the host's size,
		// scaled to the room the window leaves.
		ui.Column(c).Fill().Center().Children(func() {
			ui.Box(c).Width(w).Height(h).Clip().Children(func() {
				el := terminal.View(c, rc.term).
					Width(natW).Height(natH).Absolute().
					Left(0).Top(0).TransformOrigin(0, 0).
					Scale(scale, scale)
				if rc.focus {
					el = el.Focus()
					rc.focus = false
				}
			})
		})
	})
	if b := area.LayoutBounds(); b.W > 0 && b.H > 0 {
		rc.areaW, rc.areaH = b.W, b.H
	}
}

// naturalSize is the size the sink's view was laid out at on the host, or
// an estimate from the grid when the pane has not been drawn there.
func (rc *remoteConn) naturalSize() (float32, float32) {
	w, h := rc.viewW, rc.viewH
	if w <= 0 || h <= 0 {
		// SF Mono at 12 DIPs is about 7.2 by 14.4 a cell, with the grid's
		// padding around it.
		w = float32(max(rc.cols, 80))*7.2 + 24
		h = float32(max(rc.rows, 24))*14.4 + 20
	}
	return w, h
}

func (rc *remoteConn) currentPaneID() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.paneID
}

// textButton is a quiet glyph or short word button for a window header,
// where the icon set has no mark.
func textButton(c *ui.Context, tk tokens, label string, action func(), hint string) {
	el := ui.Row(c).Height(22).Padding(0, 6, 0, 6).Radius(5).
		Center().Cursor(ui.CursorPointer)
	if el.Hovered() {
		el = el.Background(tk.Active)
	}
	if hint != "" {
		el = el.Tooltip(hint)
	}
	el.OnClick(action).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(tk.MutedForeground).OnClick(action)
	})
}
