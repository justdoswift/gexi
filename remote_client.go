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
	"sync/atomic"
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

var (
	remoteHTTP = &http.Client{Timeout: 15 * time.Second}
	// remoteStreamHTTP waits out a pane.stream hold, which runs longer than
	// an ordinary call and must not be cut off mid-quiet.
	remoteStreamHTTP = &http.Client{Timeout: 60 * time.Second}
)

var errRemoteNotPaired = errors.New("this Mac is no longer paired; add it again")

type remoteClient struct {
	Name    string
	Address string
	Token   string
	// Secure dials the host over TLS: a self-hosted relay or any other
	// address the user reached with https://.
	Secure bool
}

type remoteSession struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Project string        `json:"project"`
	Panes   []string      `json:"panes"`
	Layout  *remoteLayout `json:"layout"`
}

// remoteProject is one of the host's projects, as project.list sends it: the
// sidebar draws its sessions under the folder they belong to, and its + asks
// the host for a session there, so the new shell starts in that folder rather
// than in whichever project the host happened to have selected.
type remoteProject struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

type remotePane struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Cwd          string          `json:"cwd"`
	Focused      bool            `json:"focused"`
	Activity     *agentActivity  `json:"activity"`
	Agent        *remoteAgent    `json:"agent"`
	Branch       *remoteBranch   `json:"branch"`
	Worktree     *remoteWorktree `json:"worktree"`
	WorktreeBusy string          `json:"worktreeBusy"`
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

// remoteWorktree is the checkout the host moved a pane into, as its own
// header draws it: the pull request's state colours the Merge chip, and
// missing says the checkout was deleted under the host, which only the host
// can tell.
type remoteWorktree struct {
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Base       string `json:"base"`
	PR         int    `json:"pr"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Detected   bool   `json:"detected"`
	Missing    bool   `json:"missing"`
	Busy       string `json:"busy"`
	Checks     string `json:"checks"`
	Review     string `json:"review"`
	Mergeable  string `json:"mergeable"`
	MergeState string `json:"mergeState"`
}

func (wt *remoteWorktree) status() worktreeStatus {
	if wt == nil {
		return worktreeStatus{}
	}
	return worktreeStatus{
		State:      wt.State,
		MergeState: wt.MergeState,
		Mergeable:  wt.Mergeable,
		Review:     wt.Review,
		Checks:     wt.Checks,
	}
}

// worktreeResult is what a pane.worktree call answers. The operations that
// work in the background answer at once, but leaving a checkout has to say
// what the checkout would lose, so the client can ask before asking again.
type worktreeResult struct {
	Left   bool   `json:"left"`
	Dirty  bool   `json:"dirty"`
	Ahead  int    `json:"ahead"`
	Agent  bool   `json:"agent"`
	Error  string `json:"error,omitempty"`
	Branch string `json:"branch"`
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

// remoteProbeHTTP is the short dial a route check gets: a LAN address that
// is not there should not hold the link up.
var remoteProbeHTTP = &http.Client{Timeout: 700 * time.Millisecond}

// reachable reports whether addr answers as this host, over plain HTTP: the
// LAN addresses a Mac reports are its own server, not the relay.
func (c remoteClient) reachable(addr string) bool {
	body, err := json.Marshal(map[string]any{
		"id": newID(), "method": "ping", "params": map[string]any{},
	})
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/api/rpc", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := remoteProbeHTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// scheme is the one this client dials with.
func (c remoteClient) scheme() string {
	if c.Secure {
		return "https://"
	}
	return "http://"
}

func (c remoteClient) call(method string, params map[string]any, out any) error {
	return c.do(remoteHTTP, method, params, out)
}

// stream is call for the pane mirror, which the host may hold open while the
// pane is quiet.
func (c remoteClient) stream(method string, params map[string]any, out any) error {
	return c.do(remoteStreamHTTP, method, params, out)
}

func (c remoteClient) do(client *http.Client, method string, params map[string]any, out any) error {
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
	req, err := http.NewRequest(http.MethodPost, c.scheme()+c.Address+"/api/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := client.Do(req)
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
func pairRemoteHost(address string, secure bool, code, name string) (remoteHost, error) {
	body, err := json.Marshal(map[string]string{
		"code":   code,
		"device": "gexi on " + deviceName(),
	})
	if err != nil {
		return remoteHost{}, err
	}
	scheme := "http://"
	if secure {
		scheme = "https://"
	}
	resp, err := remoteHTTP.Post(scheme+address+"/api/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return remoteHost{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Token     string   `json:"token"`
		Host      string   `json:"host"`
		Addresses []string `json:"addresses"`
		Error     string   `json:"error"`
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
	lan := make([]string, 0, len(out.Addresses))
	for _, addr := range out.Addresses {
		if addr != address {
			lan = append(lan, addr)
		}
	}
	return remoteHost{Name: name, Address: address, Token: out.Token, Secure: secure, LAN: lan}, nil
}

// normalizeRemoteAddress takes what the user typed and makes an address the
// client can dial: host:port, with the remote port when they left it out.
// The scheme decides both TLS and the port a bare host gets, so an https://
// relay address never falls back to the cleartext port.
func normalizeRemoteAddress(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	secure := false
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		secure = strings.EqualFold(u.Scheme, "https")
		if port := u.Port(); port != "" {
			return net.JoinHostPort(u.Hostname(), port), secure
		}
		return net.JoinHostPort(u.Hostname(), defaultPortFor(secure)), secure
	}
	host := raw
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if h, port, err := net.SplitHostPort(host); err == nil {
		return net.JoinHostPort(h, port), false
	}
	return net.JoinHostPort(host, defaultPortFor(false)), false
}

// defaultPortFor is the port a typed address without one gets: 443 over TLS,
// and gexi's own port otherwise.
func defaultPortFor(secure bool) string {
	if secure {
		return "443"
	}
	return strconv.Itoa(remotePort)
}

// relayClientURL is where another device reaches this Mac when it goes
// through the self-hosted relay, and empty when it does not.
func (w *workspace) relayClientURL() string {
	// A build that knows a relay answers at it even when this Mac is not the
	// one hosting the tunnel: the code is still all a device types.
	if builtinRelayURL != "" {
		return builtinRelayURL
	}
	if !w.settings.RelayEnabled || !w.settings.relay().ready() {
		return ""
	}
	return strings.TrimSpace(w.settings.RelayURL)
}

// remoteAddressLabel is how a saved host's address reads in a menu or a list:
// with the scheme when it is the one that decides TLS.
func remoteAddressLabel(host remoteHost) string {
	if host.Secure {
		return "https://" + host.Address
	}
	return host.Address
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
	// A relay makes the address redundant: another device reaches this Mac
	// there, so the dialog asks for the code alone.
	relay := w.relayClientURL()
	dialogFrame(c, tk, &w.hostOpen, 460, func() {
		body := locales.RemoteAddBody()
		if relay != "" {
			body = ""
		}
		dialogHeading(c, tk, locales.RemoteAddTitle(), body)
		dialogBody(c, func() {
			field := func(label, placeholder string, value *string) {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					dialogLabel(c, tk, label)
					dialogInput(c, tk, value, placeholder)
				})
			}
			if relay != "" {
				ui.Column(c).FillWidth().Children(func() {
					dialogInput(c, tk, &w.hostCode, locales.RemoteAddCode()).
						OnSubmit(func() { w.submitHost() })
				})
				dialogError(c, tk, w.hostErr)
				return
			}
			field(locales.RemoteAddName(), locales.RemoteAddNamePlaceholder(), &w.hostName)
			field(locales.RemoteAddAddress(), "192.168.1.23:"+strconv.Itoa(remotePort), &w.hostAddr)
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				dialogLabel(c, tk, locales.RemoteAddCode())
				dialogInput(c, tk, &w.hostCode, "000000").OnSubmit(func() { w.submitHost() })
			})
			dialogError(c, tk, w.hostErr)
		})
		label := locales.RemoteConnect()
		if w.hostBusy {
			label = locales.RemoteConnecting()
		}
		dialogFooter(c, tk,
			dialogAction{label: locales.Cancel(), kind: dialogQuiet, run: func() {
				w.hostOpen = false
				w.invalidate()
			}},
			dialogAction{label: label, kind: dialogPrimary, run: w.submitHost},
		)
	})
}

// submitHost pairs off the UI thread, then keeps the host and opens it.
func (w *workspace) submitHost() {
	if w.hostBusy {
		return
	}
	raw := w.hostAddr
	if strings.TrimSpace(raw) == "" {
		// The relay's address stands in for one the user did not type.
		raw = w.relayClientURL()
	}
	address, secure := normalizeRemoteAddress(raw)
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
		host, err := pairRemoteHost(address, secure, code, name)
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
		if m.Item(host.Name + "  " + remoteAddressLabel(host)).Chosen() {
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
	// project is the host's project the session runs in, so the sidebar can
	// put the session under it and the + beside it can name it.
	project string
	// panes are the window's panes for the host's, keyed by the host's pane
	// id: a pane that stays across polls keeps its terminal and screen.
	panes map[string]*pane
}

type remotePaneLink struct {
	conn   *remoteConn
	id     string
	title  string
	branch *remoteBranch
	// worktree is the checkout the host moved the pane into, and
	// worktreeBusy the one it is making: the header draws the same chips
	// the host's does, and presses them here.
	worktree     *remoteWorktree
	worktreeBusy string
	sink         *remoteSink
	// viewW and viewH are the size the host laid its pane's view out at, and
	// gridCols and gridRows the grid that size was drawn from: the mirror is
	// shown at exactly that screen.
	viewW, viewH       float32
	gridCols, gridRows int
	// cellW and cellH are a cell's size in DIPs, measured from those, and
	// wantCols and wantRows the grid this card fits, sent with the watch so
	// the other Mac takes it while its window is not the one in use.
	cellW, cellH       float32
	wantCols, wantRows atomic.Int32
	// stream is the pane's poll loop while the canvas shows it, nil while it
	// is off stage.
	stream chan struct{}
	// wake cuts an idle stream's sleep short when a key goes out, so the
	// echo does not wait for the slow tick.
	wake chan struct{}
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
	projects []remoteProject
	sessions []remoteSession
	panes    []remotePane

	stop chan struct{}

	// stream is the pushed frames connection, when it is up; streamMu
	// guards it and the retry time after it dies.
	streamMu    sync.Mutex
	streamConn  *remoteStream
	streamRetry time.Time

	// address is the route the calls go to now, and routeAt is when the
	// routes were last tried: the LAN first, the relay when it does not
	// answer.
	address string
	routeAt time.Time
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
		client:  remoteClient{Name: host.Name, Address: host.Address, Token: host.Token, Secure: host.Secure},
		loading: true,
		stop:    make(chan struct{}),
		address: host.Address,
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
	if s := rc.streamConn; s != nil {
		s.fail(errRemoteNotPaired)
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

// chooseRoute keeps the link on the route that answers: the Mac's LAN
// address when this machine shares its network, the relay otherwise. It
// rechecks now and then, and at once after a route fails.
func (rc *remoteConn) chooseRoute() {
	if rc.address == "" {
		rc.setRoute(rc.host.Address, rc.host.Secure)
	}
	rc.setRoute(rc.address, rc.host.Secure && rc.address == rc.host.Address)
	if time.Since(rc.routeAt) < 15*time.Second {
		return
	}
	rc.routeAt = time.Now()
	for _, addr := range rc.host.LAN {
		if rc.client.reachable(addr) {
			rc.setRoute(addr, false)
			return
		}
	}
	rc.setRoute(rc.host.Address, rc.host.Secure)
}

// setRoute points the link at addr. A stream is redialed when the route
// changes under it, so a Mac that comes home moves its mirrors to the LAN
// too instead of keeping the relay's round trip.
func (rc *remoteConn) setRoute(addr string, secure bool) {
	if rc.client.Address == addr && rc.client.Secure == secure {
		rc.address = addr
		return
	}
	rc.address = addr
	rc.client.Address = addr
	rc.client.Secure = secure
	rc.resetStream()
}

// resetStream drops the pushed frames connection; the next pane that wants
// a frame dials a new one on the current route.
func (rc *remoteConn) resetStream() {
	rc.streamMu.Lock()
	s := rc.streamConn
	rc.streamConn = nil
	rc.streamMu.Unlock()
	if s != nil {
		s.fail(errStreamClosed)
	}
}

func (rc *remoteConn) refresh() {
	if rc.stopped() || rc.ws.quitting {
		return
	}
	rc.chooseRoute()
	var sessions []remoteSession
	err := rc.client.call("session.list", nil, &sessions)
	if err != nil && rc.address != rc.host.Address {
		// The LAN route just died; the relay is the one to try.
		rc.setRoute(rc.host.Address, rc.host.Secure)
		rc.routeAt = time.Time{}
		rc.chooseRoute()
		err = rc.client.call("session.list", nil, &sessions)
	}
	if err != nil {
		rc.fail(err)
		return
	}
	// A host from before the projects came over sends none, and its sessions
	// show flat under the host, as they did.
	var projects []remoteProject
	_ = rc.client.call("project.list", nil, &projects)
	var panes []remotePane
	_ = rc.client.call("pane.list", map[string]any{"all": true}, &panes)
	rc.update(func() {
		rc.loading = false
		rc.err = ""
		rc.projects = projects
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
		s.remote.project = rs.Project
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
		p.remote.worktree = hp.Worktree
		p.remote.worktreeBusy = hp.WorktreeBusy
	}
	return leaf(p)
}

func (rc *remoteConn) newPane(s *session, paneID string) *pane {
	p := &pane{
		ID:     remotePaneID(rc.host.Address, paneID),
		ws:     rc.ws,
		remote: &remotePaneLink{conn: rc, id: paneID, wake: make(chan struct{}, 1)},
	}
	sink := newRemoteSink(rc, paneID, p.remote.wake)
	lightTheme, darkTheme := rc.ws.terminalThemes()
	term, err := terminal.New(terminal.Options{
		Conn:        sink,
		Scrollback:  4 << 20,
		Transparent: true,
		Theme:       lightTheme,
		DarkTheme:   darkTheme,
		Font:        rc.ws.terminalTextFont(),
		Padding:     terminal.Padding{X: 12, Y: 10},
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
const (
	remoteStreamInterval = 16 * time.Millisecond
	// remoteIdleInterval is the tick a pane with nothing on it gets when the
	// pushed stream is down and the mirror has to poll: the mirror still
	// wakes on the first byte of output, and a key cuts the tick short
	// through the pane's wake channel.
	remoteIdleInterval = 120 * time.Millisecond
)

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
	rc.streamMu.Lock()
	s := rc.streamConn
	rc.streamMu.Unlock()
	if s != nil {
		s.unwatch(p.remote.id)
	}
}

func (rc *remoteConn) streamLoop(p *pane, stop chan struct{}) {
	sink := p.remote.sink
	offset := int64(0)
	snapshot := true
	cols, rows := 0, 0
	viewW, viewH := float32(0), float32(0)
	var lastOutput time.Time
	for {
		select {
		case <-stop:
			return
		case <-rc.stop:
			return
		default:
		}
		var view remoteStreamView
		wantCols, wantRows := int(p.remote.wantCols.Load()), int(p.remote.wantRows.Load())
		params := map[string]any{
			"pane":     p.remote.id,
			"offset":   offset,
			"snapshot": snapshot,
		}
		if wantCols > 0 && wantRows > 0 {
			params["cols"], params["rows"] = wantCols, wantRows
		}
		// The host keeps its own screen size: this pane is laid out like a
		// local one, at this window's size, and the bytes are shown in it.
		pushed := false
		err := error(nil)
		if v, ok := rc.nextPushedView(p, stop, wantCols, wantRows, snapshot); ok {
			view = v
			pushed = true
		} else {
			if rc.stopped() {
				return
			}
			err = rc.client.stream("pane.stream", params, &view)
		}
		if err != nil {
			if errors.Is(err, errRemoteNotPaired) {
				rc.fail(err)
				return
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		snapshot = false
		// The mirror is the other Mac's screen at its own grid.
		if view.Reset || (view.Cols > 0 && (view.Cols != cols || view.Rows != rows)) {
			cols, rows = view.Cols, view.Rows
			if cols > 0 && rows > 0 {
				p.term.Resize(cols, rows)
			}
		}
		if view.ViewW != viewW || view.ViewH != viewH {
			w, h := view.ViewW, view.ViewH
			viewW, viewH = w, h
			gc, gr := cols, rows
			rc.update(func() {
				p.remote.viewW, p.remote.viewH = w, h
				p.remote.gridCols, p.remote.gridRows = gc, gr
			})
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
		if pushed {
			continue
		}
		if view.Data != "" || view.Reset {
			lastOutput = time.Now()
		}
		wait := remoteStreamInterval
		if view.Data == "" && view.Snapshot == "" && !view.Reset &&
			time.Since(lastOutput) > time.Second {
			wait = remoteIdleInterval
		}
		timer := time.NewTimer(wait)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-rc.stop:
			timer.Stop()
			return
		case <-p.remote.wake:
			timer.Stop()
		case <-timer.C:
		}
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

// MARK: the host's projects in the sidebar

// remoteGroupKey is where the sidebar keeps whether a host project is folded.
func remoteGroupKey(projectID, hostProjectID string) string {
	return projectID + ":project:" + hostProjectID
}

// remoteTargetProject is the host project a session made from this Mac
// belongs to: the one the selected session runs in, else the first the host
// lists, else none at all, so the host chooses. Main thread.
func (w *workspace) remoteTargetProject(rc *remoteConn) string {
	if rc == nil {
		return ""
	}
	if s := w.selected(); s != nil && s.remote != nil && s.remote.conn == rc && s.remote.project != "" {
		return s.remote.project
	}
	if len(rc.projects) > 0 {
		return rc.projects[0].ID
	}
	return ""
}

// remoteProjectGroup is one of a host's projects and the sessions that run in
// it, as the sidebar draws them under the host.
type remoteProjectGroup struct {
	project  remoteProject
	sessions []*session
}

// groups splits a host's sessions by the project each runs in, in the order
// the host lists its projects. A session whose project the host did not name
// goes last, with no folder row of its own.
func (rc *remoteConn) groups(sessions []*session) []remoteProjectGroup {
	byProject := map[string][]*session{}
	var order []string
	for _, s := range sessions {
		id := ""
		if s.remote != nil {
			id = s.remote.project
		}
		if _, ok := byProject[id]; !ok {
			order = append(order, id)
		}
		byProject[id] = append(byProject[id], s)
	}
	groups := make([]remoteProjectGroup, 0, len(rc.projects)+1)
	seen := map[string]bool{}
	for _, rp := range rc.projects {
		groups = append(groups, remoteProjectGroup{project: rp, sessions: byProject[rp.ID]})
		seen[rp.ID] = true
	}
	var rest []*session
	for _, id := range order {
		if !seen[id] {
			rest = append(rest, byProject[id]...)
		}
	}
	if len(rest) > 0 {
		groups = append(groups, remoteProjectGroup{sessions: rest})
	}
	return groups
}

// MARK: the pane's buttons, on the host

// newSession asks the host for a session in projectID, the host's project
// the sidebar's folder row names, and opens it once the poll brings it. An
// empty project leaves the choice to the host, as a client from before the
// projects came over did.
func (rc *remoteConn) newSession(projectID string) {
	params := map[string]any{}
	if projectID != "" {
		params["project"] = projectID
	}
	go func() {
		var out struct {
			Session string `json:"session"`
		}
		if err := rc.client.call("session.new", params, &out); err != nil {
			rc.fail(err)
			return
		}
		rc.refresh()
		if out.Session == "" {
			return
		}
		id := remoteSessionID(rc.host.Address, out.Session)
		rc.update(func() { rc.ws.selectSession(id) })
	}()
}

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

// closePane closes one of the host's panes. The card fades at once, as a
// local one does; the poll that sees the host's pane gone takes it out.
func (rc *remoteConn) closePane(paneID string) {
	rc.markClosing(paneID, "", true)
	go func() {
		if err := rc.client.call("pane.close", map[string]any{"pane": paneID}, nil); err != nil {
			rc.markClosing(paneID, "", false)
			rc.fail(err)
			return
		}
		rc.refresh()
	}()
}

// closeSession ends a session on the host, its panes fading here at once.
func (rc *remoteConn) closeSession(sessionID string) {
	rc.markClosing("", sessionID, true)
	go func() {
		if err := rc.client.call("session.close", map[string]any{"session": sessionID}, nil); err != nil {
			rc.markClosing("", sessionID, false)
			rc.fail(err)
			return
		}
		rc.refresh()
	}()
}

// markClosing fades the pane the host is about to close, or every pane of the
// session, and takes the fading back when the host refused. update keeps it on
// the main thread, where the panes live.
func (rc *remoteConn) markClosing(paneID, sessionID string, closing bool) {
	rc.update(func() {
		for _, s := range rc.project.Sessions {
			if s.remote == nil {
				continue
			}
			if sessionID != "" && s.remote.id != sessionID {
				continue
			}
			for id, p := range s.remote.panes {
				if paneID != "" && id != paneID {
					continue
				}
				p.closing = closing
				if closing {
					p.closingAt = time.Now()
				}
			}
		}
		rc.ws.invalidate()
	})
}

// focusPane tells the host which of its panes has the keyboard here.
func (rc *remoteConn) focusPane(paneID string) {
	go func() {
		_ = rc.client.call("pane.focus", map[string]any{"pane": paneID}, nil)
	}()
}

// paneWorktree runs one of the header's checkout actions on the pane's host.
// The answer says something only to leave, whose prompt would be the host's
// window, which the user here never sees: the client asks instead.
func (rc *remoteConn) paneWorktree(p *pane, action string) {
	if p == nil || p.remote == nil {
		return
	}
	id := p.remote.id
	go func() {
		var out worktreeResult
		err := rc.client.call("pane.worktree", map[string]any{
			"pane":   id,
			"action": action,
		}, &out)
		if err != nil {
			if errors.Is(err, errRemoteNotPaired) {
				rc.fail(err)
				return
			}
			rc.update(func() { rc.ws.flash(err.Error()) })
			return
		}
		rc.refresh()
		if action != "leave" {
			return
		}
		rc.update(func() {
			switch {
			case out.Error != "":
				rc.ws.flash(out.Error)
			case out.Agent:
				rc.ws.flash(locales.WorktreeAgentRunning())
			case !out.Left && (out.Dirty || out.Ahead > 0):
				rc.ws.remoteLeaveTarget = p
				rc.ws.remoteLeaveMsg = remoteLeaveMessage(out.Dirty, out.Ahead, out.Branch)
				rc.ws.remoteLeaveOpen = true
				rc.ws.invalidate()
			}
		})
	}()
}

// remoteLeaveMessage is the alert that asks whether a checkout with work it
// would lose should still be left, as the host's own prompt says it.
func remoteLeaveMessage(dirty bool, ahead int, branch string) string {
	msg := ""
	if ahead > 0 {
		msg = locales.WorktreeLeaveUnmerged(strconv.Itoa(ahead), branch)
	}
	if dirty {
		if msg != "" {
			msg += "\n\n"
		}
		msg += locales.WorktreeLeaveDirty()
	}
	return msg
}

// MARK: the pane's checkout, on the host

// remoteWorktreeButton is the header's checkout control for a pane on
// another Mac: the chips the host's header shows, drawn from what its poll
// brought, with every press going back to the host, since the checkout and
// its pull request are there.
func (w *workspace) remoteWorktreeButton(c *ui.Context, tk tokens, p *pane) {
	link := p.remote
	if link == nil {
		return
	}
	wt := link.worktree
	if wt == nil {
		label := "W"
		action := func() { link.conn.paneWorktree(p, "create") }
		hint := locales.WorktreeCreateHint()
		if link.worktreeBusy != "" {
			hint = locales.WorktreeCreating()
			action = func() {}
		}
		el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(headerRadius).
			Border(1, ui.Transparent).
			Center().Cursor(ui.CursorPointer).Tooltip(hint)
		if el.Hovered() {
			el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
		}
		el.OnClick(action).Children(func() {
			if link.worktreeBusy != "" {
				worktreeSpinner(c, tk, locales.WorktreeCreating())
				return
			}
			ui.Text(c, label).FontSize(11).FontWeight(600).
				TextColor(tk.Foreground).OnClick(action)
		})
		return
	}
	// The checkout can be deleted while the host runs. There is no git work
	// to offer then, only the way to clear the binding.
	if wt.Missing {
		action := func() { link.conn.paneWorktree(p, "drop") }
		el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(headerRadius).
			Border(1, ui.Transparent).
			Center().Cursor(ui.CursorPointer).Tooltip(locales.WorktreeGoneHint())
		if el.Hovered() {
			el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
		}
		el.OnClick(action).Children(func() {
			ui.Text(c, locales.WorktreeGone()).FontSize(11).FontWeight(600).
				TextColor(tk.MutedForeground).OnClick(action)
		})
		return
	}
	if wt.State == worktreeMerged {
		if wt.Detected {
			w.remotePullRequestChip(c, tk, p, wt, true)
			return
		}
		action := func() { link.conn.paneWorktree(p, "archive") }
		w.remoteWorktreeActionChip(c, tk, p, wt, ui.Transparent, tk.Foreground,
			locales.WorktreeArchiveHint(), action, func() {
				ui.Text(c, locales.WorktreeArchive()).FontSize(11).FontWeight(600).
					TextColor(tk.Foreground).OnClick(action)
			})
		return
	}
	if busy := worktreeBusyLabelOf(wt.Busy); busy != "" {
		worktreeChip(c, tk).OnClick(func() {}).Tooltip(busy).Children(func() {
			shimmerText(c, tk, busy)
		})
		return
	}
	if wt.PR == 0 {
		action := func() { link.conn.paneWorktree(p, "pr") }
		w.remoteWorktreeActionChip(c, tk, p, wt, ui.Transparent, tk.Foreground,
			wt.Branch+" → "+wt.Base, action, func() {
				ui.Text(c, locales.WorktreePR()).FontSize(11).FontWeight(600).
					TextColor(tk.Foreground).OnClick(action)
			})
		return
	}
	w.remotePullRequestChip(c, tk, p, wt, wt.State != "open")
	if wt.State == "open" {
		w.remoteMergeChip(c, tk, p, wt)
	}
}

// remotePullRequestChip shows the number and opens the page here, where it
// is read, as Conductor's #8376 ↗. onItsOwn says no Merge button follows it,
// so the chip carries the worktree menu itself.
func (w *workspace) remotePullRequestChip(c *ui.Context, tk tokens, p *pane, wt *remoteWorktree, onItsOwn bool) {
	action := func() {
		if wt.URL != "" {
			openPath(wt.URL)
		}
	}
	content := func() {
		ui.Text(c, "#"+strconv.Itoa(wt.PR)).FontSize(11).FontWeight(600).
			TextColor(tk.Foreground).OnClick(action)
		ui.Text(c, "↗").FontSize(10).TextColor(tk.MutedForeground).OnClick(action)
	}
	hint := remotePRStatusHint(wt)
	if onItsOwn {
		w.remoteWorktreeActionChip(c, tk, p, wt, ui.Transparent, tk.Foreground, hint, action, content)
		return
	}
	worktreeChip(c, tk).Tooltip(hint).OnClick(action).Children(content)
}

// remotePRStatusHint is the tip over the pull request chip: the host's own
// figures, as its chip says them.
func remotePRStatusHint(wt *remoteWorktree) string {
	parts := []string{}
	if wt.Checks != "" {
		parts = append(parts, "checks: "+wt.Checks)
	}
	if wt.Review != "" {
		parts = append(parts, "review: "+strings.ToLower(wt.Review))
	}
	if wt.Mergeable != "" {
		parts = append(parts, "mergeable: "+strings.ToLower(wt.Mergeable))
	}
	if wt.MergeState != "" {
		parts = append(parts, "state: "+strings.ToLower(wt.MergeState))
	}
	hint := strings.Join(parts, " · ")
	if hint == "" {
		hint = wt.Branch + " → " + wt.Base
	}
	if wt.URL != "" {
		hint = wt.URL + "\n" + hint
	}
	return hint
}

// remoteMergeChip is the filled Merge button, coloured by the status the
// host's refresh brought: green can merge, amber is still checking, red is
// blocked.
func (w *workspace) remoteMergeChip(c *ui.Context, tk tokens, p *pane, wt *remoteWorktree) {
	label, ready, danger := w.mergeStatusOf(wt.status())
	bg := tk.Waiting
	if ready {
		bg = tk.Working
	} else if danger {
		bg = tk.Danger
	}
	fg := tk.Background
	hint := label
	if blocker := w.mergeBlockerOf(wt.status()); blocker != "" {
		hint = locales.WorktreeMergeBlocked(blocker)
	}
	action := func() { p.remote.conn.paneWorktree(p, "merge") }
	w.remoteWorktreeActionChip(c, tk, p, wt, bg, fg, hint, action, func() {
		ui.Text(c, locales.WorktreeMerge()).FontSize(11).FontWeight(600).
			TextColor(fg).OnClick(action)
	})
}

// remoteWorktreeActionChip is the host's worktreeActionChip over a checkout
// this Mac only sees: the label runs the action, and the chevron opens the
// one menu item, leaving the checkout, which the host then carries out.
func (w *workspace) remoteWorktreeActionChip(c *ui.Context, tk tokens, p *pane, wt *remoteWorktree,
	bg, fg ui.Color, hint string, action func(), label func()) {
	quiet := bg == ui.Transparent
	group := ui.Row(c).Height(22).Radius(headerRadius).Center().
		Border(1, ui.Transparent)
	switch {
	case !quiet:
		group = group.Background(bg)
	case group.Hovered():
		group = capsule(c, group, tk, tk.Control.Alpha(0.3), tk.Control)
	}
	group.Children(func() {
		// A checkout gexi only found has no Leave menu: the chip is one
		// press, so it takes the whole pill.
		main := ui.Row(c).Height(22).Padding(0, 6, 0, 8).
			Center().Cursor(ui.CursorPointer).Tooltip(hint).OnClick(action)
		if wt.Detected {
			main = main.Padding(0, 8, 0, 8)
		}
		main.Radius(headerRadius).Children(label)
		if wt.Detected {
			return
		}
		rule := tk.Control
		if !quiet {
			rule = fg.Alpha(0.28)
		}
		ui.Box(c).Width(1).Height(12).Background(rule)
		chevron := fg
		if quiet {
			chevron = tk.MutedForeground
		}
		seg := ui.Row(c).Width(18).Height(22).Radius(headerRadius).
			Center().Cursor(ui.CursorPointer).Tooltip(locales.WorktreeLeaveHint())
		seg.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(locales.WorktreeLeave()).Disabled(wt.Busy != "").Chosen() {
				if p.remote != nil {
					p.remote.conn.paneWorktree(p, "leave")
				}
			}
		}).Children(func() {
			if src := iconSource("chevron-down", colorHex(chevron)); src != nil {
				ui.Image(c, src).Width(10).Height(10).IgnoreInput(true)
			}
		})
	})
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
	wake   chan struct{}
	mu     sync.Mutex
	cond   *sync.Cond
	buf    [][]byte
	out    chan []byte
	done   chan struct{}
	once   sync.Once
}

// wantGrid is the terminal size the card aw by ah fits: what the other Mac's
// pane is asked to take while this window is the one in use.
func (l *remotePaneLink) wantGrid(aw, ah float32) (int, int) {
	cw, ch := l.cellW, l.cellH
	if cw <= 0 {
		cw = 7.55
	}
	if ch <= 0 {
		ch = 19.2
	}
	cols := int((aw-24)/cw) - 1
	rows := int((ah-20)/ch) - 1
	if cols < 10 {
		cols = 10
	}
	if rows < 3 {
		rows = 3
	}
	return cols, rows
}

// measureCell takes a cell's size from the first stream that names the
// host's view: the same font draws both ends.
func (l *remotePaneLink) measureCell() {
	if l.cellW == 0 && l.viewW > 24 && l.gridCols > 0 {
		l.cellW = (l.viewW - 24) / float32(l.gridCols)
	}
	if l.cellH == 0 && l.viewH > 20 && l.gridRows > 0 {
		l.cellH = (l.viewH - 20) / float32(l.gridRows)
	}
}

// mirrorSize is the size the other Mac's screen is drawn at here: the size
// that Mac laid its view out at, and a guess before the first stream names
// it.
func mirrorSize(p *pane) (float32, float32) {
	if p != nil && p.remote != nil && p.remote.viewW > 0 && p.remote.viewH > 0 {
		return p.remote.viewW, p.remote.viewH
	}
	cols, rows := 0, 0
	if p != nil && p.term != nil {
		cols, rows = p.term.Size()
	}
	return float32(max(cols, 80))*7.55 + 24, float32(max(rows, 24))*19.2 + 20
}

func newRemoteSink(rc *remoteConn, paneID string, wake chan struct{}) *remoteSink {
	c := &remoteSink{
		rc:     rc,
		paneID: paneID,
		wake:   wake,
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
// Resize is where a local pane's pty would be told the size. The mirror
// takes the card's size itself, like any other terminal view, so there is
// nothing to tell and nothing leaves this Mac.
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

// sendLoop sends what was typed, in order, to the pane the sink shows. The
// keys typed while a send is in flight go out together in the next one: a
// word typed at a remote pane should cost one round trip, not one per key.
func (c *remoteSink) sendLoop() {
	for {
		var b []byte
		select {
		case <-c.done:
			return
		case b = <-c.out:
		}
		b = append([]byte(nil), b...)
		for {
			select {
			case more := <-c.out:
				b = append(b, more...)
				continue
			default:
			}
			break
		}
		_ = c.rc.client.call("pane.send", map[string]any{
			"pane": c.paneID,
			"text": string(b),
		}, nil)
		// The echo is the next frame; do not wait out an idle tick for it.
		if c.wake != nil {
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}
	}
}
