// Drives the omp activity extension with a fake pi and a fake tuios, then
// checks what it would have reported. Run by scripts/e2e-setup.sh under bun.
import { readFileSync } from "node:fs";

const extPath = process.argv[2];
const log = process.env.FAKE_TUIOS_LOG!;
const mod = await import(extPath);

// Pure mapping first.
const checks: [string, boolean][] = [];
const want = (name: string, ok: boolean) => checks.push([name, ok]);

want("input → prompt", JSON.stringify(mod.activityFor("input", { text: "fix the tests" })) ===
  JSON.stringify({ state: "working", activity: { event: "prompt", text: "fix the tests" } }));
want("empty input is skipped", mod.activityFor("input", { text: "" }) === null);
want("tool start → tool with command target",
  mod.activityFor("tool_execution_start", { toolName: "bash", args: { command: "go test ./..." } }).activity.target === "go test ./...");
want("tool start names the file", JSON.stringify(mod.filesOf({ path: "a.go" })) === '["a.go"]');
want("tool end ok → tool_done", mod.activityFor("tool_execution_end", { toolName: "bash", isError: false }).activity.event === "tool_done");
want("tool end error → tool_failed with text",
  (() => { const a = mod.activityFor("tool_execution_end", { toolName: "bash", isError: true, result: "boom" }).activity; return a.event === "tool_failed" && a.text === "boom" && a.ok === false; })());
want("agent_end → turn_end, state done",
  (() => { const r = mod.activityFor("agent_end", { messages: [{ role: "assistant", content: [{ type: "text", text: "done here" }] }] }); return r.state === "done" && r.activity.event === "turn_end" && r.activity.text === "done here"; })());
want("agent_end that continues is skipped", mod.activityFor("agent_end", { willContinue: true }) === null);
want("long target is clipped", mod.targetOf({ command: "x".repeat(1000) }).length <= 401);

// Then the wiring: register on a fake pi, fire events, read the fake tuios log.
const handlers: Record<string, Function> = {};
const pi = { on: (name: string, fn: Function) => { handlers[name] = fn; } };
mod.default(pi);
want("subscribes to the four events", ["input", "tool_execution_start", "tool_execution_end", "agent_end"].every((n) => typeof handlers[n] === "function"));
const ctx = { mode: "tui", agent: { kind: "main" }, sessionManager: { getSessionId: () => "sess-123", getSessionFile: () => "/tmp/s.jsonl" } };
handlers["tool_execution_start"]({ toolName: "bash", args: { command: "ls" } }, ctx);
handlers["tool_execution_start"]({ toolName: "bash", args: { command: "ls" } }, { ...ctx, agent: { kind: "subagent" } });
await new Promise((r) => setTimeout(r, 400));
const lines = readFileSync(log, "utf8").trim().split("\n").filter(Boolean);
want("one report for the main agent, none for the subagent", lines.length === 1);
const p = lines.length ? JSON.parse(lines[0]) : {};
want("report names the pane, harness and session id",
  p.session === process.env.TUIOS_SESSION && p.window === process.env.TUIOS_PANE_ID && p.harness === "omp" && p.agent_session_id === "sess-123");
want("report carries the activity", p.activity?.event === "tool" && p.activity?.target === "ls");

let fails = 0;
for (const [name, ok] of checks) { console.log((ok ? "PASS " : "FAIL ") + name); if (!ok) fails++; }
process.exit(fails);
