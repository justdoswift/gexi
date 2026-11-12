package main

import (
	"os"
	"path/filepath"
	"sync"
)

// Amp has a plugin API rather than a hook settings file. gexi writes one
// system plugin, which Amp loads for every thread on this Mac; the plugin
// reports only when it runs inside a gexi pane, so Amp anywhere else stays
// untouched. It speaks the control socket directly, like Pi's extension, so
// no helper process is forked per event.
const ampEventsTemplate = `// Written by gexi. Reports Amp's lifecycle to the window over its control
// socket, the way Claude Code's hooks and Pi's extension do.
import { connect } from "node:net";
import type { PluginAPI } from "@ampcode/plugin";

export const description = "Reports Amp turns to a gexi pane";

// report hands one event to the window and resolves with its result.
// turn.start answers with the one-time worktree note, which goes into the
// prompt; every other event only updates the pane's state.
function report(kind: string, message = "", session = ""): Promise<any> {
	const socket = process.env.GEXI_SOCKET || "";
	const pane = process.env.GEXI_PANE || "";
	if (!socket || !pane) return Promise.resolve(null);
	const params: any = { kind, agent: "amp", pane };
	if (message) params.message = message;
	if (session) params.session = session;
	return new Promise((resolve) => {
		let settled = false;
		let buffer = "";
		const conn = connect(socket);
		const done = (value: any) => {
			if (settled) return;
			settled = true;
			conn.destroy();
			resolve(value);
		};
		conn.setTimeout(1500, () => done(null));
		conn.on("connect", () => {
			const id = "amp-" + process.pid + "-" + Date.now();
			conn.write(JSON.stringify({ id, method: "event", params }) + "\n");
		});
		conn.on("data", (chunk) => {
			buffer += chunk.toString();
			if (!buffer.includes("\n")) return;
			try {
				done(JSON.parse(buffer.split("\n")[0]).result ?? null);
			} catch {
				done(null);
			}
		});
		conn.on("error", () => done(null));
		conn.on("close", () => done(null));
	});
}

// textOf keeps the prose of an assistant message and drops thinking and tool
// calls, which are not words a banner can show.
function textOf(content: any): string {
	if (!Array.isArray(content)) return "";
	const parts: string[] = [];
	for (const block of content) {
		if (block && block.type === "text" && typeof block.text === "string") parts.push(block.text);
	}
	return parts.join("\n");
}

export default function (amp: PluginAPI) {
	// Amp runs this plugin for every session on the Mac. Without a pane there
	// is nothing to report to, so it does not even register.
	if (!process.env.GEXI_PANE) return;

	let current = "";
	let lastState = "";
	let lastKind = "";
	let named = false;
	let watching: { unsubscribe(): void } | null = null;

	// send reports one event and remembers what the pane was last told, so
	// the thread's own state can end a turn the agent.end hook never did.
	const send = (kind: string, message = "", session = ""): Promise<any> => {
		lastKind = kind;
		if (kind === "turn.start" && message) named = true;
		return report(kind, message, session);
	};

	// nameFromThread names the pane from the message the turn began with.
	// The agent.start hook carries it, but a resumed thread's first turn can
	// start before the plugin sees the session; the thread's state stream
	// still says a turn is running, so its last user message is the prompt.
	// This runs in an event callback, never a hook the host waits on, so the
	// lookup cannot hold the turn up.
	const nameFromThread = async (threadId: string, ctx: any) => {
		if (named) return;
		try {
			const last = (await ctx.thread.messages({ from: "end", limit: 1, roles: ["user"] })).at(-1);
			const text = textOf(last?.content);
			// The turn may have ended while the message was looked up; a
			// name does not start a new one.
			if (text && lastState === "running" && threadId === current) send("turn.start", text, threadId);
		} catch {
			// A thread that cannot be read has no message to name it by;
			// the terminal's own title still names the pane.
		}
	};

	amp.on("session.start", async (event, ctx) => {
		current = event.thread.id;
		lastState = "";
		lastKind = "";
		named = false;
		await send("session.start", "", event.thread.id);
		// A tool approval is the one blocked state Amp does not report as an
		// event; the thread's own state stream carries it, and so does the
		// end of a turn whose agent.end hook never came.
		watching?.unsubscribe();
		watching = ctx.thread.state.subscribe((state) => {
			if (event.thread.id !== current) return;
			const previous = lastState;
			lastState = state;
			if (state === "awaiting-approval") {
				send("needs-input", "", event.thread.id);
			} else if (state === "running" && previous !== "running") {
				send("turn.resume", "", event.thread.id);
				void nameFromThread(event.thread.id, ctx);
			} else if (state === "idle" && previous === "running") {
				// The hook says how a turn ended; the state says that it
				// did. Give the hook a moment to land before saying so
				// without the agent's last words.
				setTimeout(() => {
					// A new turn in the meantime is not this turn's end.
					if (event.thread.id === current && lastState === "idle" && lastKind !== "turn.end") send("turn.end", "", event.thread.id);
				}, 1000);
			}
		});
		// A pane can open or switch to a thread whose turn is already
		// running. agent.start fired before this plugin saw the session, so
		// seed the pane from the thread's current state; otherwise the
		// sidebar loader would stop while the terminal is still thinking.
		try {
			const initial = await ctx.thread.state.get();
			if (event.thread.id !== current) return;
			lastState = initial;
			if (initial === "running") {
				send("turn.resume", "", event.thread.id);
				void nameFromThread(event.thread.id, ctx);
			} else if (initial === "awaiting-approval") {
				send("needs-input", "", event.thread.id);
			}
		} catch {
			// A thread that has gone away has no state to seed; the event
			// stream above still reports its next transition.
		}
	});

	amp.on("agent.start", async (event) => {
		// A background thread is not the one this pane shows.
		if (current && event.thread.id !== current) return;
		// The turn's message is the one the user just sent; a turn that
		// starts without it is named from the thread's own last message by
		// the state stream above.
		const message = typeof event.message === "string" ? event.message : "";
		if (!message) return;
		const result = await send("turn.start", message, event.thread.id);
		const context = result && typeof result.context === "string" ? result.context : "";
		if (context) return { message: { content: context } };
	});

	amp.on("agent.end", (event) => {
		if (current && event.thread.id !== current) return;
		let last = "";
		for (const message of event.messages ?? []) {
			if (message.role !== "assistant") continue;
			const text = textOf(message.content);
			if (text) last = text;
		}
		send("turn.end", last, event.thread.id);
	});

	// Amp calls dispose on a graceful exit, which is where the pane learns
	// that its agent is gone even if a turn was still running.
	amp.onDispose(() => send("session.end", "", current));
}
`

var (
	ampEventsOnce sync.Once
	ampEventsPath string
	ampEventsErr  error
)

// installAmpEvents writes Amp's system plugin once per process and returns its
// path. The file is gexi's alone; Amp's other plugins stay where they are.
func installAmpEvents() (string, error) {
	ampEventsOnce.Do(func() {
		ampEventsPath, ampEventsErr = writeAmpEvents(ampEventsFile())
	})
	return ampEventsPath, ampEventsErr
}

// ampConfigDir follows Amp's own config location: $XDG_CONFIG_HOME/amp, or
// ~/.config/amp when it is unset.
func ampConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "amp")
	}
	return filepath.Join(homeDir(), ".config", "amp")
}

func ampEventsFile() string {
	return filepath.Join(ampConfigDir(), "plugins", "gexi-events.ts")
}

func writeAmpEvents(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Do not touch the file when it is already current: Amp watches plugins,
	// and an unchanged file should not look like a reload.
	if old, err := os.ReadFile(path); err == nil && string(old) == ampEventsTemplate {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(ampEventsTemplate), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
