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
	let watching: { unsubscribe(): void } | null = null;

	amp.on("session.start", async (event, ctx) => {
		current = event.thread.id;
		lastState = "";
		await report("session.start", "", event.thread.id);
		// A tool approval is the one blocked state Amp does not report as an
		// event; the thread's own state stream carries it.
		watching?.unsubscribe();
		watching = ctx.thread.state.subscribe((state) => {
			if (event.thread.id !== current) return;
			const previous = lastState;
			lastState = state;
			if (state === "awaiting-approval") {
				report("needs-input", "", event.thread.id);
			} else if (state === "running" && previous !== "running") {
				report("turn.resume", "", event.thread.id);
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
				report("turn.resume", "", event.thread.id);
			} else if (initial === "awaiting-approval") {
				report("needs-input", "", event.thread.id);
			}
		} catch {
			// A thread that has gone away has no state to seed; the event
			// stream above still reports its next transition.
		}
	});

	amp.on("agent.start", async (event, ctx) => {
		// A background thread is not the one this pane shows.
		if (current && event.thread.id !== current) return;
		// The turn's message is the one the user just sent. A turn that starts
		// without it — a resumed thread's first, say — still has that message
		// as the thread's last user message, which is what names the pane.
		let message = event.message;
		if (!message) {
			try {
				const last = (await ctx.thread.messages({ from: "end", limit: 1, roles: ["user"] })).at(-1);
				message = textOf(last?.content);
			} catch {}
		}
		const result = await report("turn.start", message, event.thread.id);
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
		report("turn.end", last, event.thread.id);
	});

	// Amp calls dispose on a graceful exit, which is where the pane learns
	// that its agent is gone even if a turn was still running.
	amp.onDispose(() => report("session.end", "", current));
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
