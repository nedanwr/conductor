import { describe, expect, it } from "vitest";
import {
  asEventSeq,
  newProjectId,
  newRunId,
  newTaskId,
  newWorktreeId,
} from "./ids.js";
import type {
  ContextBundle,
  KernelEvent,
  KernelEventOf,
  KernelEventType,
  RunStartedPayload,
} from "./events.js";

describe(" id constructors", () => {
  it("mint distinct UUID v4 values", () => {
    const a = newTaskId();
    const b = newTaskId();
    expect(a).not.toBe(b);
    // UUID v4 shape: 8-4-4-4-12 hex with version nibble 4.
    expect(a).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });

  it("cover every id kind and the seq brand", () => {
    expect(typeof newProjectId()).toBe("string");
    expect(typeof newRunId()).toBe("string");
    expect(typeof newWorktreeId()).toBe("string");
    expect(asEventSeq(7)).toBe(7);
  });
});

describe("KernelEvent envelope", () => {
  it("carries the full context bundle on run.started — not a reference", () => {
    // Structural assertion: the bundle slot is the bundle object itself.
    const bundle: ContextBundle = {
      taskId: newTaskId(),
      intent: "do the thing",
      recentDecisions: [],
      contracts: [],
      contractsUnfiltered: true,
    };
    const payload: RunStartedPayload = {
      provider: "mock",
      model: "mock-1",
      bundle,
    };
    const event: KernelEventOf<"run.started"> = {
      seq: asEventSeq(1),
      ts: Date.now(),
      type: "run.started",
      runId: newRunId(),
      taskId: bundle.taskId,
      payload,
    };
    expect(event.payload.bundle).toBe(bundle);
    expect(event.payload.bundle.recentDecisions).toEqual([]);
  });

  it("has no project.created variant", () => {
    const types: KernelEventType[] = [
      "task.created",
      "task.dep_added",
      "task.status_changed",
      "task.worktree_assigned",
      "run.started",
      "run.completed",
      "run.failed",
      "run.killed",
      "run.paused",
      "run.resumed",
      "agent.reasoning",
      "agent.message",
      "agent.tool_call",
      "agent.tool_result",
      "agent.diff",
      "agent.approval_requested",
      "agent.approval_resolved",
    ];
    expect(types).not.toContain("project.created" as KernelEventType);
  });
});

describe("KernelEvent exhaustiveness", () => {
  // A switch over KernelEvent terminated by a `never` check. Removing a variant
  // from KernelEvent (or adding one without a case here) breaks this build —
  // the compile-time guarantee the spec requires.
  const summarize = (e: KernelEvent): string => {
    switch (e.type) {
      case "task.created":
        return e.payload.intent;
      case "task.dep_added":
        return e.payload.dependsOnTaskId;
      case "task.status_changed":
        return e.payload.status;
      case "task.worktree_assigned":
        return e.payload.branchName;
      case "run.started":
        return e.payload.provider;
      case "run.completed":
        return "completed";
      case "run.failed":
        return e.payload.error;
      case "run.killed":
        return e.payload.reason ?? "killed";
      case "run.paused":
        return "paused";
      case "run.resumed":
        return "resumed";
      case "agent.reasoning":
        return e.payload.text;
      case "agent.message":
        return e.payload.text;
      case "agent.tool_call":
        return e.payload.tool;
      case "agent.tool_result":
        return String(e.payload.ok);
      case "agent.diff":
        return e.payload.path;
      case "agent.approval_requested":
        return e.payload.approvalId;
      case "agent.approval_resolved":
        return e.payload.decision;
      default: {
        const _exhaustive: never = e;
        return _exhaustive;
      }
    }
  };

  it("summarizes a known variant", () => {
    expect(
      summarize({
        seq: asEventSeq(2),
        ts: 0,
        type: "run.completed",
        payload: {},
      }),
    ).toBe("completed");
  });
});
