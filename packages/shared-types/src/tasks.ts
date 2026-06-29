// Task shapes — the natural-language unit of work and its lifecycle.

import type { ProjectId, TaskId, WorktreeId } from "./ids.js";

/**
 * Lifecycle of a task. `pending` until its dependencies clear, `ready` once
 * they do, then through execution to a terminal state. These mirror the
 * `tasks.status` projection column.
 */
export type TaskStatus =
  | "pending"
  | "ready"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled";

/**
 * The declaration used to create a task: what it should accomplish and how it
 * sits in the graph. Ids of dependencies are listed so the graph edges can be
 * established at creation time.
 */
export interface TaskSpec {
  readonly projectId: ProjectId;
  readonly intent: string;
  readonly parentTaskId?: TaskId;
  readonly dependsOn?: readonly TaskId[];
}

/** A materialized task — the projection of its event history. */
export interface Task {
  readonly id: TaskId;
  readonly projectId: ProjectId;
  readonly parentTaskId?: TaskId;
  readonly intent: string;
  readonly status: TaskStatus;
  readonly worktreeId?: WorktreeId;
  readonly createdAt: number;
  readonly updatedAt: number;
}

/**
 * A topologically ordered execution plan over a set of tasks. `order` is a
 * flat sequence respecting dependencies; `ready` is the subset with no
 * outstanding dependencies, executable immediately.
 */
export interface ExecutionPlan {
  readonly order: readonly TaskId[];
  readonly ready: readonly TaskId[];
}
