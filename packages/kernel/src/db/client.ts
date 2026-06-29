// The Db service — the SOLE home of @libsql/client and the raw Drizzle
// instance. Nothing outside this file imports @libsql/client, holds the raw
// libsql client, or issues SQL by hand; every caller goes through the four
// methods below and builds statements with the Drizzle query builder.
//
// `Effect.tryPromise` wrapping libsql/Drizzle calls is permitted ONLY here —
// this is the one async seam at the database boundary. The libsql client is
// acquired with `acquireRelease`, so it closes when the owning scope closes.
//
// Note on the bound-handle surface: Drizzle has no connection-less builder for
// writes, so a connection-bound Drizzle handle is *lent* to the `query` /
// `execute` / `transaction` callbacks (typed as `KernelDb`). It is never
// stored or returned, and the underlying libsql client never escapes — callers
// can build and run statements but cannot reach the raw connection.

import { Context, Effect, Exit, Layer, Scope } from "effect";
import { type Client, type InStatement, createClient } from "@libsql/client";
import { type LibSQLDatabase, drizzle } from "drizzle-orm/libsql";
import { migrate as drizzleMigrate } from "drizzle-orm/libsql/migrator";
import { DbError } from "../errors.js";
import * as schema from "./schema.js";

/** Connection-bound Drizzle handle lent to callbacks; builds and runs statements. */
export type KernelDb = LibSQLDatabase<typeof schema>;

/** Configuration for the database connection. `url` is a libsql file: or memory URL. */
export interface DbConfig {
  readonly url: string;
}

/** The read/write surface — shared by the top-level Db and a transaction handle. */
export interface DbExecutor {
  /** Run a read built against the lent handle; returns the builder's rows. */
  query<T>(build: (db: KernelDb) => PromiseLike<T>): Effect.Effect<T, DbError>;
  /** Run a write built against the lent handle; discards any returned rows. */
  execute(
    build: (db: KernelDb) => PromiseLike<unknown>
  ): Effect.Effect<void, DbError>;
}

/** The handle lent to a transaction body; its statements run inside the transaction. */
export type DbTx = DbExecutor;

export interface DbApi extends DbExecutor {
  /**
   * Run `body` inside a single transaction. Commits when `body` succeeds; rolls
   * back on failure OR interruption. The `DbTx` handle runs `body`'s statements
   * inside the transaction.
   */
  transaction<T, E, R>(
    body: (tx: DbTx) => Effect.Effect<T, E, R>
  ): Effect.Effect<T, E | DbError, R>;
  /** Apply pending migrations from `migrationsFolder`. */
  migrate(migrationsFolder: string): Effect.Effect<void, DbError>;
}

export class Db extends Context.Service<Db, DbApi>()("Db") {}

const failWith =
  (operation: DbError["operation"], message: string) =>
  (cause: unknown): DbError =>
    new DbError({ operation, message, cause });

const make = (config: DbConfig): Effect.Effect<DbApi, DbError, Scope.Scope> =>
  Effect.gen(function* () {
    const client = yield* Effect.acquireRelease(
      Effect.sync(() => createClient({ url: config.url })),
      (c) => Effect.sync(() => c.close())
    );

    // Enforce referential integrity per connection — libsql defaults FKs off.
    yield* Effect.tryPromise({
      try: () => client.execute("PRAGMA foreign_keys = ON"),
      catch: failWith("execute", "failed to enable foreign keys")
    });

    const db = drizzle(client, { schema });

    // One executor surface, reused for the top-level handle and for each
    // transaction-bound handle — only the underlying Drizzle handle differs.
    const executorOver = (handle: KernelDb): DbExecutor => ({
      query: (build) =>
        Effect.tryPromise({
          try: () => build(handle),
          catch: failWith("query", "query failed")
        }),
      execute: (build) =>
        Effect.tryPromise({
          try: () => Promise.resolve(build(handle)),
          catch: failWith("execute", "statement failed")
        }).pipe(Effect.asVoid)
    });

    const top = executorOver(db);

    const transaction: DbApi["transaction"] = (body) =>
      Effect.uninterruptibleMask((restore) =>
        Effect.gen(function* () {
          const itx = yield* Effect.tryPromise({
            try: () => client.transaction("write"),
            catch: failWith("transaction", "failed to open transaction")
          });

          // Bind a Drizzle handle to the interactive transaction. The Drizzle
          // libsql session only ever calls execute/batch on its client, so the
          // adapter forwarding those to the transaction routes every statement
          // through it — with full row mapping preserved.
          const txClient = {
            execute: (stmt: InStatement) => itx.execute(stmt),
            batch: (stmts: InStatement[]) => itx.batch(stmts)
          } as unknown as Client;
          const txDb = drizzle(txClient, { schema });

          return yield* restore(body(executorOver(txDb))).pipe(
            Effect.onExit((exit) =>
              Exit.isSuccess(exit)
                ? Effect.tryPromise({
                    try: () => itx.commit(),
                    catch: failWith("transaction", "commit failed")
                  })
                : Effect.ignore(
                    Effect.tryPromise({
                      try: () => itx.rollback(),
                      catch: failWith("transaction", "rollback failed")
                    })
                  )
            )
          );
        })
      );

    const migrate: DbApi["migrate"] = (migrationsFolder) =>
      Effect.tryPromise({
        try: () => drizzleMigrate(db, { migrationsFolder }),
        catch: failWith("migrate", "migration failed")
      });

    return { query: top.query, execute: top.execute, transaction, migrate };
  });

/** The Db layer — acquires the libsql client in the kernel scope, closed on release. */
export const layer = (config: DbConfig): Layer.Layer<Db, DbError> =>
  Layer.effect(Db, make(config));
