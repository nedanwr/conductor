// KernelEvent discriminated union + envelope + KernelEventType.
//
// The event log is the single source of truth; every other table is a
// projection of these events. The union is exhaustive and discriminated on
// `type`, so any switch over it can be terminated by a `never` check and a new
// variant breaks the build wherever it is unhandled.
//
// There is deliberately no `project.created` variant — project registration is
// a direct write to root data, not an event-sourced operation. `projectId` is
// carried on the in-memory envelope for consumer convenience only; it is not a
// persisted column on the events table.

import type {
  EventSeq,
  ProjectId,
  RunId,
  TaskId,
  WorktreeId,
} from "./ids.js";
import type { TaskStatus } from "./tasks.js";

/**
 * The computed context handed to an agent at the moment a run starts. It is
 * embedded in full inside the `run.started` payload — never a reference — so
 * the log is a faithful record of exactly what the agent was given and replay
 * reconstructs identical state.
 */
export interface ContextBundle {
  readonly taskId: TaskId;
  readonly intent?: string;
  readonly recentDecisions: readonly ContextBundleDecision[];
  readonly contracts: readonly ContextBundleContract[];
  /**
   * True when `contracts` could not be narrowed to those the task actually
   * references and instead contains every project contract. Callers are told
   * the set is unfiltered rather than being silently misled.
   */
  readonly contractsUnfiltered: boolean;
  readonly maxTokens?: number;
}

export interface ContextBundleDecision {
  readonly id: string;
  readonly summary: string;
  readonly rationale: string;
}

export interface ContextBundleContract {
  readonly id: string;
  readonly name: string;
  readonly version: number;
  readonly shape: string;
  readonly body: string;
}

/** Resolution of an approval the agent requested. */
export type ApprovalDecision = "approved" | "denied";

/** Every event `type` literal — the discriminant of `KernelEvent`. */
export type KernelEventType =
  | "task.created"
  | "task.dep_added"
  | "task.status_changed"
  | "task.worktree_assigned"
  | "run.started"
  | "run.completed"
  | "run.failed"
  | "run.killed"
  | "run.paused"
  | "run.resumed"
  | "agent.reasoning"
  | "agent.message"
  | "agent.tool_call"
  | "agent.tool_result"
  | "agent.diff"
  | "agent.approval_requested"
  | "agent.approval_resolved";

/** The envelope every event carries, parameterized by its `type` and payload. */
interface EventEnvelope<Type extends KernelEventType, Payload> {
  readonly seq: EventSeq;
  readonly ts: number;
  readonly type: Type;
  readonly runId?: RunId;
  readonly taskId?: TaskId;
  readonly projectId?: ProjectId;
  readonly payload: Payload;
}

// --- task.* -----------------------------------------------------------------

export interface TaskCreatedPayload {
  readonly intent: string;
  readonly status: TaskStatus;
  readonly parentTaskId?: TaskId;
}

export interface TaskDepAddedPayload {
  readonly dependsOnTaskId: TaskId;
}

export interface TaskStatusChangedPayload {
  readonly status: TaskStatus;
  readonly previousStatus?: TaskStatus;
}

export interface TaskWorktreeAssignedPayload {
  readonly worktreeId: WorktreeId;
  readonly path: string;
  readonly baseRef: string;
  readonly branchName: string;
}

// --- run.* ------------------------------------------------------------------

export interface RunStartedPayload {
  readonly provider: string;
  readonly model: string;
  /** The full context bundle the agent is given — embedded, not referenced. */
  readonly bundle: ContextBundle;
}

export type RunCompletedPayload = Record<never, never>;

export interface RunFailedPayload {
  readonly error: string;
}

export interface RunKilledPayload {
  readonly reason?: string;
}

export type RunPausedPayload = Record<never, never>;
export type RunResumedPayload = Record<never, never>;

// --- agent.* ----------------------------------------------------------------

export interface AgentReasoningPayload {
  readonly text: string;
}

export interface AgentMessagePayload {
  readonly text: string;
}

export interface AgentToolCallPayload {
  readonly callId: string;
  readonly tool: string;
  readonly args: unknown;
}

export interface AgentToolResultPayload {
  readonly callId: string;
  readonly ok: boolean;
  readonly result?: unknown;
}

export interface AgentDiffPayload {
  readonly path: string;
  readonly patch: string;
}

export interface AgentApprovalRequestedPayload {
  readonly approvalId: string;
  readonly summary: string;
}

export interface AgentApprovalResolvedPayload {
  readonly approvalId: string;
  readonly decision: ApprovalDecision;
}

// --- the union --------------------------------------------------------------

export type KernelEvent =
  | EventEnvelope<"task.created", TaskCreatedPayload>
  | EventEnvelope<"task.dep_added", TaskDepAddedPayload>
  | EventEnvelope<"task.status_changed", TaskStatusChangedPayload>
  | EventEnvelope<"task.worktree_assigned", TaskWorktreeAssignedPayload>
  | EventEnvelope<"run.started", RunStartedPayload>
  | EventEnvelope<"run.completed", RunCompletedPayload>
  | EventEnvelope<"run.failed", RunFailedPayload>
  | EventEnvelope<"run.killed", RunKilledPayload>
  | EventEnvelope<"run.paused", RunPausedPayload>
  | EventEnvelope<"run.resumed", RunResumedPayload>
  | EventEnvelope<"agent.reasoning", AgentReasoningPayload>
  | EventEnvelope<"agent.message", AgentMessagePayload>
  | EventEnvelope<"agent.tool_call", AgentToolCallPayload>
  | EventEnvelope<"agent.tool_result", AgentToolResultPayload>
  | EventEnvelope<"agent.diff", AgentDiffPayload>
  | EventEnvelope<"agent.approval_requested", AgentApprovalRequestedPayload>
  | EventEnvelope<"agent.approval_resolved", AgentApprovalResolvedPayload>;

/** Narrow a `KernelEvent` to the variant carrying a specific `type`. */
export type KernelEventOf<T extends KernelEventType> = Extract<
  KernelEvent,
  { type: T }
>;
