// Branded ID aliases + constructor helpers over crypto.randomUUID().
//
// Every identifier in the system is a branded alias so two ids of different
// kinds can never be assigned to one another, even though they are strings
// (or, for EventSeq, numbers) at runtime. Constructors mint UUID v4 values via
// the standard-library crypto.randomUUID(); no `uuid` package is used.

declare const brand: unique symbol;

/** A nominal brand over a base type `T`, tagged by the literal `B`. */
type Branded<T, B extends string> = T & { readonly [brand]: B };

export type ProjectId = Branded<string, "ProjectId">;
export type TaskId = Branded<string, "TaskId">;
export type RunId = Branded<string, "RunId">;
export type WorktreeId = Branded<string, "WorktreeId">;

/** Monotonic sequence number assigned by the event log (the `events.seq` column). */
export type EventSeq = Branded<number, "EventSeq">;

export const newProjectId = (): ProjectId => crypto.randomUUID() as ProjectId;
export const newTaskId = (): TaskId => crypto.randomUUID() as TaskId;
export const newRunId = (): RunId => crypto.randomUUID() as RunId;
export const newWorktreeId = (): WorktreeId =>
  crypto.randomUUID() as WorktreeId;

/**
 * Wrap a raw number as an `EventSeq`. The event log is the sole authority that
 * assigns sequence numbers; callers reconstructing events from storage use this
 * to brand the persisted value.
 */
export const asEventSeq = (n: number): EventSeq => n as EventSeq;
