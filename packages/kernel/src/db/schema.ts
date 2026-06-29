// Drizzle schema — nine tables. Foreign keys are declared on every referencing
// column; referential integrity is enforced at the schema level, not by
// convention. `projects` is root data (written directly), every other table is
// either event-sourced projection state or a provenance/context artifact.
//
// Two deliberate shapes to note: `events` has NO project_id column (project
// scoping is derived via join through task_id / run_id), and `contracts` is
// unique on (project_id, name, version).

import {
  type AnySQLiteColumn,
  index,
  integer,
  primaryKey,
  sqliteTable,
  text,
  unique,
} from "drizzle-orm/sqlite-core";

export const projects = sqliteTable("projects", {
  id: text("id").primaryKey(),
  name: text("name").notNull(),
  repoPath: text("repo_path").notNull(),
  defaultBranch: text("default_branch").notNull(),
  createdAt: integer("created_at").notNull(),
});

export const worktrees = sqliteTable("worktrees", {
  id: text("id").primaryKey(),
  projectId: text("project_id")
    .notNull()
    .references(() => projects.id),
  path: text("path").notNull(),
  baseRef: text("base_ref").notNull(),
  branchName: text("branch_name").notNull(),
  status: text("status").notNull(),
});

export const tasks = sqliteTable("tasks", {
  id: text("id").primaryKey(),
  projectId: text("project_id")
    .notNull()
    .references(() => projects.id),
  parentTaskId: text("parent_task_id").references(
    (): AnySQLiteColumn => tasks.id,
  ),
  intent: text("intent").notNull(),
  status: text("status").notNull(),
  worktreeId: text("worktree_id").references(() => worktrees.id),
  createdAt: integer("created_at").notNull(),
  updatedAt: integer("updated_at").notNull(),
});

export const taskDeps = sqliteTable(
  "task_deps",
  {
    taskId: text("task_id")
      .notNull()
      .references(() => tasks.id),
    dependsOnTaskId: text("depends_on_task_id")
      .notNull()
      .references(() => tasks.id),
  },
  (t) => [primaryKey({ columns: [t.taskId, t.dependsOnTaskId] })],
);

export const taskRuns = sqliteTable("task_runs", {
  id: text("id").primaryKey(),
  taskId: text("task_id")
    .notNull()
    .references(() => tasks.id),
  provider: text("provider").notNull(),
  sessionHandle: text("session_handle"),
  status: text("status").notNull(),
  startedAt: integer("started_at"),
  endedAt: integer("ended_at"),
});

export const events = sqliteTable(
  "events",
  {
    seq: integer("seq").primaryKey({ autoIncrement: true }),
    runId: text("run_id").references(() => taskRuns.id),
    taskId: text("task_id").references(() => tasks.id),
    ts: integer("ts").notNull(),
    type: text("type").notNull(),
    payloadJson: text("payload_json").notNull(),
  },
  (t) => [index("events_by_run").on(t.runId, t.seq)],
);

export const contracts = sqliteTable(
  "contracts",
  {
    id: text("id").primaryKey(),
    projectId: text("project_id")
      .notNull()
      .references(() => projects.id),
    name: text("name").notNull(),
    version: integer("version").notNull(),
    shape: text("shape").notNull(),
    body: text("body").notNull(),
    sourceTaskId: text("source_task_id").references(() => tasks.id),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [unique().on(t.projectId, t.name, t.version)],
);

export const decisions = sqliteTable("decisions", {
  id: text("id").primaryKey(),
  taskId: text("task_id").references(() => tasks.id),
  runId: text("run_id").references(() => taskRuns.id),
  summary: text("summary").notNull(),
  rationale: text("rationale").notNull(),
  refsJson: text("refs_json"),
  createdAt: integer("created_at").notNull(),
});

export const contextItems = sqliteTable("context_items", {
  id: text("id").primaryKey(),
  projectId: text("project_id")
    .notNull()
    .references(() => projects.id),
  kind: text("kind").notNull(),
  key: text("key").notNull(),
  valueJson: text("value_json").notNull(),
  metadataJson: text("metadata_json"),
  createdAt: integer("created_at").notNull(),
});
