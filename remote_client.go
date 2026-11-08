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

// MARK: the remote window

type remoteWindow struct {
	ws     *workspace
	client remoteClient
	win    *mygo.Window

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

func (w *workspace) openRemoteHost(host remoteHost) {
	if existing := w.remoteWindows[host.Address]; existing != nil {
		existing.win.Show()
		existing.win.Focus()
		return
	}
	rw := &remoteWindow{
		ws:      w,
		client:  remoteClient{Name: host.Name, Address: host.Address, Token: host.Token},
		loading: true,
		stop:    make(chan struct{}),
	}
	rw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "gexi — " + host.Name,
		Width:           860,
		Height:          620,
		MinWidth:        520,
		MinHeight:       380,
		TitleBarStyle:   mygo.TitleBarHidden,
		BackgroundColor: w.chromeBackground(),
		Content:         ui.View(rw.view),
	})
	w.remoteWindows[host.Address] = rw
	rw.win.OnShow(func() { rw.win.Invalidate() })
	rw.win.OnClosed(func() {
		rw.close()
		if w.remoteWindows[host.Address] == rw {
			delete(w.remoteWindows, host.Address)
		}
	})
	go rw.refreshLoop()
}

func (rw *remoteWindow) close() {
	select {
	case <-rw.stop:
	default:
		close(rw.stop)
	}
}

func (rw *remoteWindow) stopped() bool {
	select {
	case <-rw.stop:
		return true
	default:
		return false
	}
}

func (rw *remoteWindow) target() (sessionID, paneID string) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.sessionID, rw.paneID
}

func (rw *remoteWindow) refreshLoop() {
	rw.refresh()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-rw.stop:
			return
		case <-ticker.C:
			rw.refresh()
		}
	}
}

func (rw *remoteWindow) refresh() {
	if rw.stopped() || rw.ws.quitting {
		return
	}
	sessionID, paneID := rw.target()
	if sessionID == "" {
		var sessions []remoteSession
		if err := rw.client.call("session.list", nil, &sessions); err != nil {
			rw.fail(err)
			return
		}
		var panes []remotePane
		_ = rw.client.call("pane.list", map[string]any{"all": true}, &panes)
		rw.update(func() {
			rw.loading = false
			rw.err = ""
			rw.sessions = sessions
			rw.panes = panes
		})
		return
	}
	if paneID == "" {
		return
	}
	var view remoteTextView
	err := rw.client.call("pane.text", map[string]any{
		"pane":  paneID,
		"limit": 64_000,
	}, &view)
	if err != nil {
		rw.fail(err)
		return
	}
	rw.update(func() {
		rw.loading = false
		rw.err = ""
		rw.text = view.Text
		if view.Title != "" {
			rw.title = view.Title
		}
		rw.cwd = view.Cwd
	})
}

func (rw *remoteWindow) update(fn func()) {
	if rw.stopped() {
		return
	}
	// The remote window's own Update keeps the main window out of the
	// polling: it changes only what this window shows.
	rw.win.Update(func() {
		if rw.stopped() {
			return
		}
		fn()
	})
}

func (rw *remoteWindow) fail(err error) {
	if errors.Is(err, errRemoteNotPaired) {
		err = errors.New(locales.RemoteNotPaired())
	}
	rw.update(func() {
		rw.loading = false
		rw.err = err.Error()
	})
}

func (rw *remoteWindow) panesOf(s remoteSession) []remotePane {
	var out []remotePane
	for _, pane := range rw.panes {
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

func (rw *remoteWindow) openSession(s remoteSession) {
	panes := rw.panesOf(s)
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
	rw.mu.Lock()
	rw.sessionID, rw.paneID = s.ID, paneID
	rw.mu.Unlock()
	rw.session = &s
	rw.text = ""
	rw.title = s.Title
	rw.cwd = ""
	rw.loading = true
	rw.scroll = ui.ScrollState{}
	rw.win.Invalidate()
}

func (rw *remoteWindow) back() {
	rw.mu.Lock()
	rw.sessionID, rw.paneID = "", ""
	rw.mu.Unlock()
	rw.session = nil
	rw.text = ""
	rw.loading = true
	rw.win.Invalidate()
}

func (rw *remoteWindow) selectPane(paneID string) {
	rw.mu.Lock()
	rw.paneID = paneID
	rw.mu.Unlock()
	rw.text = ""
	rw.scroll = ui.ScrollState{}
	rw.win.Invalidate()
}

func (rw *remoteWindow) sendInput() {
	if rw.input == "" {
		return
	}
	_, paneID := rw.target()
	text := rw.input + "\r"
	rw.input = ""
	if paneID == "" {
		return
	}
	rw.win.Invalidate()
	go func() { _ = rw.client.call("pane.send", map[string]any{"pane": paneID, "text": text}, nil) }()
}

func (rw *remoteWindow) sendKey(seq string) {
	_, paneID := rw.target()
	if paneID == "" {
		return
	}
	go func() { _ = rw.client.call("pane.send", map[string]any{"pane": paneID, "text": seq}, nil) }()
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

func (rw *remoteWindow) view(c *ui.Context) {
	tk := rw.ws.tokens(c)
	bar := c.TitleBar()
	root := ui.Column(c).Fill().Background(tk.Background)
	root.Children(func() {
		header := ui.Row(c).Height(36).Padding(0, 8, 0, bar.Left+6+iconSize+8).
			Background(tk.Background).DragWindow()
		header.Children(func() {
			if rw.session != nil {
				textButton(c, tk, "‹", rw.back, locales.RemoteBack())
			}
			ui.Column(c).Gap(0).Children(func() {
				title := rw.client.Name
				if rw.session != nil {
					title = rw.title
					if title == "" {
						title = rw.session.Title
					}
				}
				if title == "" {
					title = rw.client.Address
				}
				ui.Text(c, title).FontSize(12).FontWeight(500).
					TextColor(tk.Foreground).SingleLine()
				subtitle := rw.client.Address
				if rw.session != nil && rw.cwd != "" {
					subtitle = rw.cwd
				}
				ui.Text(c, subtitle).FontSize(10).TextColor(tk.MutedForeground).SingleLine()
			})
			ui.Spacer(c)
			textButton(c, tk, "⟳", func() { go rw.refresh() }, locales.RemoteRefresh())
		})
		if rw.err != "" {
			ui.Row(c).Padding(8, 12, 8, 12).Background(tk.Waiting.Alpha(0.12)).
				Children(func() {
					ui.Text(c, rw.err).FontSize(11).TextColor(tk.Waiting)
				})
		}
		if rw.session == nil {
			rw.listView(c, tk)
			return
		}
		rw.terminalView(c, tk)
	})
}

func (rw *remoteWindow) listView(c *ui.Context, tk tokens) {
	if rw.loading && len(rw.sessions) == 0 {
		ui.Spacer(c)
		ui.Text(c, locales.RemoteConnecting()).FontSize(12).
			TextColor(tk.MutedForeground)
		ui.Spacer(c)
		return
	}
	if len(rw.sessions) == 0 {
		ui.Spacer(c)
		ui.Text(c, locales.RemoteNoSessions()).FontSize(12).
			TextColor(tk.MutedForeground)
		ui.Spacer(c)
		return
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(12, 14, 16, 14).Gap(2).Children(func() {
			for _, s := range rw.sessions {
				rw.sessionRow(c, tk, s)
			}
		})
	})
}

func (rw *remoteWindow) sessionRow(c *ui.Context, tk tokens, s remoteSession) {
	session := s
	panes := rw.panesOf(s)
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
	row.OnClick(func() { rw.openSession(session) }).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, session.Title).FontSize(13).TextColor(tk.Foreground).
				SingleLine().OnClick(func() { rw.openSession(session) })
			ui.Text(c, path).FontSize(11).TextColor(tk.MutedForeground).
				SingleLine().OnClick(func() { rw.openSession(session) })
		})
		ui.Spacer(c).OnClick(func() { rw.openSession(session) })
		if state != "" {
			color := tk.Working
			if state == activityWaiting {
				color = tk.Waiting
			}
			ui.Box(c).Width(7).Height(7).Radius(4).Background(color)
		}
	})
}

func (rw *remoteWindow) terminalView(c *ui.Context, tk tokens) {
	panes := rw.panesOf(*rw.session)
	if len(panes) > 1 {
		ui.Row(c).Height(32).Padding(0, 10, 0, 10).Gap(4).Children(func() {
			for _, pane := range panes {
				p := pane
				label := p.Title
				if label == "" {
					label = p.Cwd
				}
				selected := p.ID == rw.currentPaneID()
				chip := ui.Row(c).Height(22).Padding(0, 8, 0, 8).Radius(5).
					Border(1, tk.Border).Cursor(ui.CursorPointer)
				if selected {
					chip = chip.BorderColor(tk.Foreground)
				} else if chip.Hovered() {
					chip = chip.Background(tk.Active)
				}
				chip.OnClick(func() { rw.selectPane(p.ID) }).Children(func() {
					ui.Text(c, label).FontSize(11).TextColor(tk.MutedForeground).
						SingleLine().OnClick(func() { rw.selectPane(p.ID) })
				})
			}
		})
	}
	// Keep the view at the end of the output unless the user scrolled up.
	if rw.scroll.Y >= rw.scroll.MaxY {
		rw.scroll.Y = math.MaxFloat32
	}
	ui.Scroll(c).Fill().TrackScroll(&rw.scroll).Children(func() {
		ui.Text(c, rw.text).Font("SF Mono, Menlo, monospace").FontSize(12).
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
			key.OnClick(func() { rw.sendKey(seq) }).Children(func() {
				ui.Text(c, label).FontSize(11).TextColor(tk.MutedForeground).
					OnClick(func() { rw.sendKey(seq) })
			})
		}
	})
	ui.Row(c).Height(40).Padding(8, 10, 8, 10).Gap(6).Children(func() {
		input := ui.TextInput(c, &rw.input).
			Placeholder(locales.RemoteInputPlaceholder()).Height(28).FontSize(12).
			FillWidth()
		if input.Submitted() {
			rw.sendInput()
		}
		ui.Button(c, locales.RemoteSend()).OnClick(func() { rw.sendInput() })
	})
}

func (rw *remoteWindow) currentPaneID() string {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.paneID
}

// settingsHostName is the window's title fallback before a session is open.
func (w *workspace) settingsHostName() string {
	return deviceName()
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
