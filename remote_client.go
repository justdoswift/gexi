package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// The other half of the LAN remote: this Mac's gexi reaching another one.
// A host is an address and the token pairing gave us; a window shows the
// host's sessions and one session's terminal, polled once a second.

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

type remoteTextView struct {
	Pane  string `json:"pane"`
	Title string `json:"title"`
	Cwd   string `json:"cwd"`
	Text  string `json:"text"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
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
// sessions and the text of the session being watched, polled once a second
// while the app runs. The sidebar lists the sessions under the host's row
// and the canvas shows one session's terminal, in the same window as the
// local projects.
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

	text   string
	title  string
	cwd    string
	input  string
	scroll ui.ScrollState

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
	sessionID, paneID := rc.target()
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
		}
	})
	if sessionID == "" || paneID == "" {
		return
	}
	// Only the session on screen pulls its text; the others stay listed
	// until the user opens one of them.
	if !rc.ws.viewingRemote(rc.host.Address) {
		return
	}
	var view remoteTextView
	err := rc.client.call("pane.text", map[string]any{
		"pane":  paneID,
		"limit": 64_000,
	}, &view)
	if err != nil {
		rc.fail(err)
		return
	}
	rc.update(func() {
		rc.loading = false
		rc.err = ""
		rc.text = view.Text
		if view.Title != "" {
			rc.title = view.Title
		}
		rc.cwd = view.Cwd
	})
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
	rc.text = ""
	rc.title = s.Title
	rc.cwd = ""
	rc.loading = true
	rc.scroll = ui.ScrollState{}
	rc.ws.remoteActive = rc.host.Address
	rc.ws.invalidate()
}

// back leaves the session and returns the canvas to the host's session
// list. Main thread.
func (rc *remoteConn) back() {
	rc.mu.Lock()
	rc.sessionID, rc.paneID = "", ""
	rc.mu.Unlock()
	rc.session = nil
	rc.text = ""
	rc.loading = true
	rc.ws.invalidate()
}

func (rc *remoteConn) selectPane(paneID string) {
	rc.mu.Lock()
	rc.paneID = paneID
	rc.mu.Unlock()
	rc.text = ""
	rc.scroll = ui.ScrollState{}
	rc.ws.invalidate()
}

func (rc *remoteConn) sendInput() {
	if rc.input == "" {
		return
	}
	_, paneID := rc.target()
	text := rc.input + "\r"
	rc.input = ""
	if paneID == "" {
		return
	}
	rc.ws.invalidate()
	go func() { _ = rc.client.call("pane.send", map[string]any{"pane": paneID, "text": text}, nil) }()
}

func (rc *remoteConn) sendKey(seq string) {
	_, paneID := rc.target()
	if paneID == "" {
		return
	}
	go func() { _ = rc.client.call("pane.send", map[string]any{"pane": paneID, "text": seq}, nil) }()
}

// remoteKeys are the keys a phone or a small window cannot type around.
var remoteKeys = [][2]string{
	{"esc", "\x1b"},
	{"^C", "\x03"},
	{"tab", "\t"},
	{"←", "\x1b[D"},
	{"↑", "\x1b[A"},
	{"↓", "\x1b[B"},
	{"→", "\x1b[C"},
	{"⏎", "\r"},
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
	// Keep the view at the end of the output unless the user scrolled up.
	if rc.scroll.Y >= rc.scroll.MaxY {
		rc.scroll.Y = math.MaxFloat32
	}
	ui.Scroll(c).Fill().TrackScroll(&rc.scroll).Children(func() {
		ui.Text(c, rc.text).Font("SF Mono, Menlo, monospace").FontSize(12).
			FixedLineHeight(15).TextColor(tk.Foreground).
			Padding(10, 14, 10, 14).FillWidth()
	})
	ui.Row(c).Height(28).Padding(0, 10, 0, 10).Gap(4).Children(func() {
		for _, entry := range remoteKeys {
			label, seq := entry[0], entry[1]
			key := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(5).
				Border(1, tk.Border).Cursor(ui.CursorPointer)
			if key.Hovered() {
				key = key.Background(tk.Active)
			}
			key.OnClick(func() { rc.sendKey(seq) }).Children(func() {
				ui.Text(c, label).FontSize(11).TextColor(tk.MutedForeground).
					OnClick(func() { rc.sendKey(seq) })
			})
		}
	})
	ui.Row(c).Height(40).Padding(8, 10, 8, 10).Gap(6).Children(func() {
		input := ui.TextInput(c, &rc.input).
			Placeholder(locales.RemoteInputPlaceholder()).Height(28).FontSize(12).
			FillWidth()
		if input.Submitted() {
			rc.sendInput()
		}
		ui.Button(c, locales.RemoteSend()).OnClick(func() { rc.sendInput() })
	})
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
