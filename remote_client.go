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
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Panes  []string      `json:"panes"`
	Layout *remoteLayout `json:"layout"`
}

type remotePane struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Cwd      string         `json:"cwd"`
	Focused  bool           `json:"focused"`
	Activity *agentActivity `json:"activity"`
	Agent    *remoteAgent   `json:"agent"`
	Branch   *remoteBranch  `json:"branch"`
}

// remoteAgent is the host's agent detection, as agentJSON sends it: the
// window shows the same mark and name the host's header does.
type remoteAgent struct {
	Kind    string `json:"kind"`
	Session string `json:"session"`
}

func (p *remotePane) agent() *agentDetection {
	if p == nil || p.Agent == nil || p.Agent.Kind == "" {
		return nil
	}
	return &agentDetection{Kind: agentKind(p.Agent.Kind), SessionID: p.Agent.Session}
}

// remoteLayout is a session's split tree as the host has it: a leaf names
// one of its panes, and a branch names its axis and the percent the first
// side takes.
type remoteLayout struct {
	Pane   string        `json:"pane"`
	Axis   string        `json:"axis"`
	Ratio  float32       `json:"ratio"`
	First  *remoteLayout `json:"first"`
	Second *remoteLayout `json:"second"`
}

func (n *remoteLayout) isLeaf() bool { return n != nil && n.Pane != "" }

func (n *remoteLayout) panes() []string {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return []string{n.Pane}
	}
	return append(n.First.panes(), n.Second.panes()...)
}

// remoteBranch is the pane's branch, as the host's own header pill shows it.
type remoteBranch struct {
	Name    string `json:"name"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Changed bool   `json:"changed"`
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

// MARK: the hosts as projects

// A paired Mac is not a view of its own: it is a project in the sidebar, its
// sessions are sessions, and its panes are panes, so the window draws them
// with the same code it draws local work with. Only the transport is remote:
// a pane's terminal is fed by the host's stream, and the buttons its header
// shows call back to the host.

type remoteProjectLink struct {
	conn *remoteConn
	host remoteHost
}

type remoteSessionLink struct {
	conn  *remoteConn
	id    string
	title string
	// panes are the window's panes for the host's, keyed by the host's pane
	// id: a pane that stays across polls keeps its terminal and screen.
	panes map[string]*pane
}

type remotePaneLink struct {
	conn   *remoteConn
	id     string
	title  string
	branch *remoteBranch
	sink   *remoteSink
	// stream is the pane's poll loop while the canvas shows it, nil while it
	// is off stage.
	stream chan struct{}
}

// remoteConn is this Mac's link to one paired host: the transport under the
// project and panes it keeps in the window. Its poll brings the host's
// sessions and panes, and apply turns them into the model the window draws.
type remoteConn struct {
	ws      *workspace
	host    remoteHost
	client  remoteClient
	project *project

	loading  bool
	err      string
	sessions []remoteSession
	panes    []remotePane

	stop chan struct{}
}

// remoteProjectID names the project a host takes in the sidebar, and the
// session and pane ids build on it so nothing collides with local work.
func remoteProjectID(address string) string { return "remote:" + address }

func remoteSessionID(address, id string) string { return "remote:" + address + ":session:" + id }

func remotePaneID(address, id string) string { return "remote:" + address + ":pane:" + id }

// startRemoteClients picks up the Macs already paired, so their projects are
// in the sidebar as soon as the window opens.
func (w *workspace) startRemoteClients() {
	for _, host := range w.settings.RemoteHosts {
		w.remoteConn(host)
	}
	w.syncRemoteProjects()
}

// stopRemoteClients ends every poll loop. Called on quit.
func (w *workspace) stopRemoteClients() {
	for address, rc := range w.remoteConns {
		rc.close()
		delete(w.remoteConns, address)
	}
	w.remoteProjects = nil
}

// syncRemoteProjects puts the projects in the order the settings keep the
// hosts. Main thread.
func (w *workspace) syncRemoteProjects() {
	w.remoteProjects = w.remoteProjects[:0]
	for _, host := range w.settings.RemoteHosts {
		if rc := w.remoteConns[host.Address]; rc != nil && rc.project != nil {
			w.remoteProjects = append(w.remoteProjects, rc.project)
		}
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
	pr := &project{
		ID:     remoteProjectID(host.Address),
		Label:  host.Name,
		remote: &remoteProjectLink{conn: rc, host: host},
	}
	rc.project = pr
	w.remoteConns[host.Address] = rc
	w.syncRemoteProjects()
	go rc.refreshLoop()
	return rc
}

// openRemoteHost brings a host to the front: its project opens, and its
// first session takes the canvas when it has one.
func (w *workspace) openRemoteHost(host remoteHost) {
	rc := w.remoteConn(host)
	w.syncRemoteProjects()
	pr := rc.project
	delete(w.collapsed, pr.ID)
	if len(pr.Sessions) > 0 {
		w.selectSession(pr.Sessions[0].ID)
	}
	w.invalidate()
}

func (rc *remoteConn) close() {
	select {
	case <-rc.stop:
	default:
		close(rc.stop)
	}
	// stopRemoteClients runs on the main thread, where the panes live.
	for _, s := range rc.project.Sessions {
		rc.dropSession(s)
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
		rc.apply()
	})
}

func (rc *remoteConn) update(fn func()) {
	if rc.stopped() {
		return
	}
	if rc.ws.win == nil {
		fn()
		return
	}
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
		rc.ws.invalidate()
	})
}

func (rc *remoteConn) paneByID(id string) *remotePane {
	for i := range rc.panes {
		if rc.panes[i].ID == id {
			return &rc.panes[i]
		}
	}
	return nil
}

// apply turns what the poll brought into the window's model: the host's
// sessions keep the objects they had, so the panes keep their terminals and
// the screen they show, and only what changed on the host changes here.
// Main thread.
func (rc *remoteConn) apply() {
	pr := rc.project
	existing := map[string]*session{}
	for _, s := range pr.Sessions {
		if s.remote != nil {
			existing[s.remote.id] = s
		}
	}
	sessions := make([]*session, 0, len(rc.sessions))
	for _, rs := range rc.sessions {
		s := existing[rs.ID]
		if s == nil {
			s = &session{
				ID: remoteSessionID(rc.host.Address, rs.ID),
				remote: &remoteSessionLink{
					conn:  rc,
					id:    rs.ID,
					panes: map[string]*pane{},
				},
			}
		}
		s.remote.title = rs.Title
		s.Root = rc.tree(s, rs)
		rc.prune(s, rs)
		sessions = append(sessions, s)
		delete(existing, rs.ID)
	}
	for _, gone := range existing {
		rc.dropSession(gone)
	}
	pr.Sessions = sessions
	rc.ws.syncRemoteProjects()
	rc.ws.syncRemoteMirrors()
	rc.ws.invalidate()
}

// tree builds the host's split tree for a session, over the window's panes.
func (rc *remoteConn) tree(s *session, rs remoteSession) *splitNode {
	if rs.Layout != nil {
		return rc.node(s, rs.Layout)
	}
	// A host from before the layout was sent still names its panes; the
	// first one stands for the session, as it always did.
	if len(rs.Panes) == 0 {
		return nil
	}
	return rc.leaf(s, rs.Panes[0])
}

func (rc *remoteConn) node(s *session, n *remoteLayout) *splitNode {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return rc.leaf(s, n.Pane)
	}
	ax := axisHorizontal
	if n.Axis == "vertical" {
		ax = axisVertical
	}
	return &splitNode{
		axis:   ax,
		ratio:  n.Ratio,
		first:  rc.node(s, n.First),
		second: rc.node(s, n.Second),
	}
}

// leaf is the window's pane for one of the host's, made the first time it
// is seen. Its terminal is the sink the host's stream feeds.
func (rc *remoteConn) leaf(s *session, paneID string) *splitNode {
	p := s.remote.panes[paneID]
	if p == nil {
		p = rc.newPane(s, paneID)
	}
	if p == nil {
		return nil
	}
	if hp := rc.paneByID(paneID); hp != nil {
		p.initialDir = hp.Cwd
		p.agent = hp.agent()
		p.activity = hp.Activity
		p.remote.branch = hp.Branch
		p.remote.title = hp.Title
	}
	return leaf(p)
}

func (rc *remoteConn) newPane(s *session, paneID string) *pane {
	p := &pane{
		ID:     remotePaneID(rc.host.Address, paneID),
		ws:     rc.ws,
		remote: &remotePaneLink{conn: rc, id: paneID},
	}
	sink := newRemoteSink(rc, paneID)
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
		rc.err = err.Error()
		return nil
	}
	p.term = term
	p.remote.sink = sink
	s.remote.panes[paneID] = p
	return p
}

// prune lets go of the panes a session no longer has. The layout is what
// the window draws, so it decides; a host from before it is drawn lists its
// panes alone.
func (rc *remoteConn) prune(s *session, rs remoteSession) {
	keep := rs.Panes
	if rs.Layout != nil {
		keep = rs.Layout.panes()
	}
	for id, p := range s.remote.panes {
		if includesString(keep, id) {
			continue
		}
		rc.stopStream(p)
		if p.term != nil {
			_ = p.term.Close()
		}
		delete(s.remote.panes, id)
	}
}

// dropSession takes a session the host no longer has out of the window.
func (rc *remoteConn) dropSession(s *session) {
	if s == nil || s.remote == nil {
		return
	}
	for id, p := range s.remote.panes {
		rc.stopStream(p)
		if p.term != nil {
			_ = p.term.Close()
		}
		delete(s.remote.panes, id)
	}
}

// includesString reports whether a list holds want.
func includesString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// MARK: the streams

// remoteStreamInterval paces the mirror's polls: a frame of the display,
// the delay a keystroke or a program's output takes to show.
const remoteStreamInterval = 16 * time.Millisecond

// startStream polls the pane's mirror while the canvas shows it. The
// terminal keeps the size the host drew it at, so the text stays the size it
// is there; the card only decides how much of the screen shows.
func (rc *remoteConn) startStream(p *pane) {
	if p == nil || p.remote == nil || p.term == nil || p.remote.stream != nil {
		return
	}
	stop := make(chan struct{})
	p.remote.stream = stop
	go rc.streamLoop(p, stop)
}

func (rc *remoteConn) stopStream(p *pane) {
	if p == nil || p.remote == nil || p.remote.stream == nil {
		return
	}
	close(p.remote.stream)
	p.remote.stream = nil
}

func (rc *remoteConn) streamLoop(p *pane, stop chan struct{}) {
	sink := p.remote.sink
	offset := int64(0)
	snapshot := true
	cols, rows := 0, 0
	for {
		select {
		case <-stop:
			return
		case <-rc.stop:
			return
		default:
		}
		var view remoteStreamView
		err := rc.client.call("pane.stream", map[string]any{
			"pane":     p.remote.id,
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
				p.term.Resize(cols, rows)
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
		time.Sleep(remoteStreamInterval)
	}
}

// syncRemoteMirrors runs the streams of the pane the canvas shows and rests
// the ones it does not: a hidden pane costs the host nothing. Main thread.
func (w *workspace) syncRemoteMirrors() {
	shown := map[*pane]bool{}
	// Every pane of the session on the canvas keeps its stream: a zoomed
	// one hides the others, but their rail cards still show them live.
	if s := w.selected(); s != nil && s.remote != nil && s.Root != nil {
		for _, p := range s.Root.panes() {
			shown[p] = true
		}
	}
	for _, rc := range w.remoteConns {
		for _, s := range rc.project.Sessions {
			if s.Root == nil {
				continue
			}
			for _, p := range s.Root.panes() {
				if p.remote == nil {
					continue
				}
				if shown[p] {
					rc.startStream(p)
				} else {
					rc.stopStream(p)
				}
			}
		}
	}
}

// MARK: the pane's buttons, on the host

// splitPane splits the host's pane toward the side asked for; the poll
// brings the new layout and the new pane's stream.
func (rc *remoteConn) splitPane(paneID string, ax axis) {
	axis := "horizontal"
	if ax == axisVertical {
		axis = "vertical"
	}
	go func() {
		if err := rc.client.call("pane.split", map[string]any{
			"pane": paneID,
			"axis": axis,
		}, nil); err != nil {
			rc.fail(err)
			return
		}
		rc.refresh()
	}()
}

// closePane closes one of the host's panes.
func (rc *remoteConn) closePane(paneID string) {
	go func() {
		if err := rc.client.call("pane.close", map[string]any{"pane": paneID}, nil); err != nil {
			rc.fail(err)
			return
		}
		rc.refresh()
	}()
}

// closeSession ends a session on the host.
func (rc *remoteConn) closeSession(sessionID string) {
	go func() {
		if err := rc.client.call("session.close", map[string]any{"session": sessionID}, nil); err != nil {
			rc.fail(err)
			return
		}
		rc.refresh()
	}()
}

// focusPane tells the host which of its panes has the keyboard here.
func (rc *remoteConn) focusPane(paneID string) {
	go func() {
		_ = rc.client.call("pane.focus", map[string]any{"pane": paneID}, nil)
	}()
}

// MARK: the remote pane's place in the window

// remoteHost drops the pairing, its project, and the streams under it.
func (w *workspace) removeRemoteHost(address string) {
	kept := w.settings.RemoteHosts[:0]
	for _, h := range w.settings.RemoteHosts {
		if h.Address != address {
			kept = append(kept, h)
		}
	}
	w.settings.RemoteHosts = kept
	w.settings.save()
	if rc := w.remoteConns[address]; rc != nil {
		rc.close()
		delete(w.remoteConns, address)
	}
	for i, pr := range w.remoteProjects {
		if pr.ID == remoteProjectID(address) {
			w.remoteProjects = append(w.remoteProjects[:i], w.remoteProjects[i+1:]...)
			break
		}
	}
	if s := w.sessionByID(w.SelectedSession); s == nil || s.remote != nil {
		w.SelectedSession = ""
		if first := w.firstSession(); first != nil {
			w.SelectedSession = first.ID
		}
	}
	w.invalidate()
	w.invalidateSettings()
}

// MARK: the stream's connection to a pane

// remoteSink is one sink terminal's connection to the pane on the host: the
// frames the mirror's poll brings, which the terminal reads, and the keys
// typed into it, which go back to that pane as pane.send. Writes keep their
// order: one goroutine sends them as they were typed.
type remoteSink struct {
	rc     *remoteConn
	paneID string
	mu     sync.Mutex
	cond   *sync.Cond
	buf    [][]byte
	out    chan []byte
	done   chan struct{}
	once   sync.Once
}

func newRemoteSink(rc *remoteConn, paneID string) *remoteSink {
	c := &remoteSink{
		rc:     rc,
		paneID: paneID,
		out:    make(chan []byte, 64),
		done:   make(chan struct{}),
	}
	c.cond = sync.NewCond(&c.mu)
	go c.sendLoop()
	return c
}

// Resize makes the sink a sized connection: the terminal keeps the size the
// host gave it, and the view tells the host's grid, not its own, where the
// cells go.
func (c *remoteSink) Resize(cols, rows int) error { return nil }

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

// sendLoop sends what was typed, in order, to the pane the sink shows.
func (c *remoteSink) sendLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.out:
			_ = c.rc.client.call("pane.send", map[string]any{
				"pane": c.paneID,
				"text": string(b),
			}, nil)
		}
	}
}
