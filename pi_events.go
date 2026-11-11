package main

import (
	"os"
	"path/filepath"
	"sync"
)

// Pi has no hook settings file the way Claude Code does. Its lifecycle
// reaches the window through an extension gexi passes with `-e`; the
// extension speaks the control socket directly rather than running a helper
// binary, so Pi never has to fork gexi to report anything.
const piEventsTemplate = `// Written by gexi. Reports Pi's lifecycle to the window over its control
// socket, the way Claude Code's hooks do.
import { connect } from "node:net";

// report hands one event to the window and resolves with its result.
// turn.start answers with the one-time worktree note, which goes into the
// prompt; every other event only updates the pane's state.
function report(kind: string, message = ""): Promise<any> {
	const socket = process.env.GEXI_SOCKET || "";
	const pane = process.env.GEXI_PANE || "";
	if (!socket || !pane) return Promise.resolve(null);
	const params: any = { kind, agent: "pi", pane };
	if (message) params.message = message;
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
			const id = "pi-" + process.pid + "-" + Date.now();
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

function textOf(content: any): string {
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	const parts: string[] = [];
	for (const part of content) {
		if (part && part.type === "text" && typeof part.text === "string") parts.push(part.text);
	}
	return parts.join("\n");
}

export default function (pi: any) {
	let last = "";

	pi.on("session_start", () => {
		report("session.start");
	});

	pi.on("input", async (event: any) => {
		if (event.source === "extension") return;
		last = "";
		// The text is the pane's first message when this is a new
		// conversation; gexi names the pane from it.
		const result = await report("turn.start", event.text);
		const context = result && typeof result.context === "string" ? result.context : "";
		if (context) return { action: "transform", text: event.text + "\n\n" + context };
	});

	pi.on("message_end", (event: any) => {
		if (event.message?.role !== "assistant") return;
		const text = textOf(event.message.content);
		if (text) last = text;
	});

	// agent_end can still be followed by retries, compaction, or queued work;
	// agent_settled is where Pi will not continue on its own.
	pi.on("agent_settled", () => {
		report("turn.end", last);
	});

	// Only extensions that ask the user through ctx.ui raise these: stock Pi
	// has no permission prompt.
	pi.on("ui_prompt_start", () => {
		report("needs-input");
	});
	pi.on("ui_prompt_end", () => {
		report("turn.start");
	});

	pi.on("session_shutdown", () => {
		report("session.end");
	});
}
`

var (
	piEventsOnce sync.Once
	piEventsPath string
	piEventsErr  error
)

// installPiEvents writes the extension once per process and returns its path.
func installPiEvents() (string, error) {
	piEventsOnce.Do(func() {
		piEventsPath, piEventsErr = writePiEvents()
	})
	return piEventsPath, piEventsErr
}

func piEventsFile() string {
	return filepath.Join(supportDir(), "pi", "gexi-events.ts")
}

func writePiEvents() (string, error) {
	path := piEventsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(piEventsTemplate), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// piLaunchCommand opens a fresh Pi conversation with the event extension. If
// the extension cannot be written, Pi still runs, just without a live state.
func piLaunchCommand() string {
	command := "pi"
	if path, err := installPiEvents(); err == nil {
		command += " -e " + shellQuote(path)
	}
	return command
}

// piResumeCommand brings back the newest conversation in the directory, with
// the same extension so it keeps reporting.
func piResumeCommand() string {
	return piLaunchCommand() + " --continue"
}
