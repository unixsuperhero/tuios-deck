// installed by tuios-deck
// managed by tuios-deck; `deck setup install omp-activity` overwrites this file
// and `deck setup uninstall omp-activity` removes it. Put your own extensions
// beside it instead of editing it.
// TUIOS_DECK_SETUP_ID=omp-activity
// TUIOS_DECK_SETUP_VERSION=1
// @ts-nocheck
//
// Feeds the tuios activity ring for an oh-my-pi (omp) pane. The extension
// tuios installs beside this one reports the pane's state; this one reports
// what the agent does, through the same set-agent-state verb: the prompt
// typed, each tool call starting and ending, and the end of a turn. With it,
// `tuios agent-activity` and the deck's Activity tab show omp the way they
// show Claude Code.
//
// Nothing here changes omp's behaviour. Every report is a detached process
// whose failure is ignored.

import { spawn } from "node:child_process";

const TUIOS = process.env.TUIOS_DECK_TUIOS || "tuios";
const TEXT_MAX = 2000;
const TARGET_MAX = 400;

function clip(s, n) {
  if (typeof s !== "string") return "";
  return s.length > n ? s.slice(0, n) + "…" : s;
}

// target is the one line the ring shows beside a tool name: the command of
// a shell tool, the path of a file tool, else the arguments as JSON.
export function targetOf(args) {
  if (args == null) return "";
  if (typeof args === "string") return clip(args, TARGET_MAX);
  if (typeof args !== "object") return clip(String(args), TARGET_MAX);
  for (const key of ["command", "cmd", "path", "file_path", "filePath", "pattern", "query", "url", "prompt", "description"]) {
    if (typeof args[key] === "string" && args[key] !== "") return clip(args[key], TARGET_MAX);
  }
  try {
    return clip(JSON.stringify(args), TARGET_MAX);
  } catch {
    return "";
  }
}

// filesOf lists the files a tool call names, for the ring's files field.
export function filesOf(args) {
  if (args == null || typeof args !== "object") return [];
  const out = [];
  for (const key of ["path", "file_path", "filePath", "file"]) {
    if (typeof args[key] === "string" && args[key] !== "") out.push(args[key]);
  }
  if (Array.isArray(args.paths)) for (const p of args.paths) if (typeof p === "string") out.push(p);
  return out.slice(0, 20);
}

// lastAssistantText is what the agent last said in a finished turn.
function lastAssistantText(messages) {
  if (!Array.isArray(messages)) return "";
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (!m || m.role !== "assistant") continue;
    const parts = Array.isArray(m.content) ? m.content : [m.content];
    const text = parts
      .map((p) => (typeof p === "string" ? p : p && p.type === "text" ? p.text : ""))
      .filter(Boolean)
      .join("\n");
    if (text) return text;
  }
  return "";
}

// activityFor maps one omp event to the activity set-agent-state records,
// and the state that goes with it. null means the event is not reported.
export function activityFor(name, event) {
  switch (name) {
    case "input":
      if (!event || typeof event.text !== "string" || event.text === "") return null;
      return { state: "working", activity: { event: "prompt", text: clip(event.text, TEXT_MAX) } };
    case "tool_execution_start":
      return {
        state: "working",
        activity: { event: "tool", tool: event.toolName, target: targetOf(event.args), files: filesOf(event.args) },
      };
    case "tool_execution_end": {
      const failed = event.isError === true;
      const a = { event: failed ? "tool_failed" : "tool_done", tool: event.toolName, ok: !failed };
      if (failed) a.text = clip(resultText(event.result), TEXT_MAX);
      return { state: "working", activity: a };
    }
    case "agent_end":
      if (event && event.willContinue) return null;
      return { state: "done", activity: { event: "turn_end", text: clip(lastAssistantText(event && event.messages), TEXT_MAX) } };
    default:
      return null;
  }
}

function resultText(result) {
  if (typeof result === "string") return result;
  if (result && typeof result === "object") {
    if (typeof result.text === "string") return result.text;
    if (Array.isArray(result.content)) {
      return result.content.map((p) => (p && typeof p.text === "string" ? p.text : "")).filter(Boolean).join("\n");
    }
    try {
      return JSON.stringify(result);
    } catch {}
  }
  return "";
}

// report runs `tuios call set-agent-state` for the pane this omp runs in.
// The pane and session come from the environment tuios gives every pane.
export function report(payload, ctx) {
  const params = {
    session: process.env.TUIOS_SESSION,
    window: process.env.TUIOS_PANE_ID,
    harness: "omp",
    ...payload,
  };
  try {
    const id = ctx?.sessionManager?.getSessionId?.();
    if (typeof id === "string" && id !== "") params.agent_session_id = id;
  } catch {}
  try {
    const file = ctx?.sessionManager?.getSessionFile?.();
    if (typeof file === "string" && file !== "") params.transcript_path = file;
  } catch {}
  try {
    const child = spawn(TUIOS, ["call", "set-agent-state", JSON.stringify(params)], {
      stdio: "ignore",
      detached: false,
      windowsHide: true,
    });
    child.on("error", () => {});
  } catch {
    // Reporting must not interrupt omp.
  }
}

export default function (pi) {
  if (process.env.TUIOS_ENV !== "1" || !process.env.TUIOS_PANE_ID) return;
  // Only the main agent in the TUI is the pane's own agent. Subagents and
  // RPC runs report nothing here.
  const mainTUI = (ctx) => ctx?.mode === "tui" && (ctx?.agent?.kind ?? "main") === "main";
  for (const name of ["input", "tool_execution_start", "tool_execution_end", "agent_end"]) {
    pi.on(name, (event, ctx) => {
      if (!mainTUI(ctx)) return;
      const payload = activityFor(name, event);
      if (payload) report(payload, ctx);
    });
  }
}
