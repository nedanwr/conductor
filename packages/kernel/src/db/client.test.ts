// Db service gate: the four-method surface; query/execute round-trip; commit;
// rollback on both error and interruption; the libsql client closing when its
// scope closes.
import { Context, Effect, Exit, Layer, Scope } from "effect";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { type DbApi, type DbExecutor, Db, layer } from "./client.js";
import { projects } from "./schema.js";

const migrationsFolder = fileURLToPath(new URL("./migrations", import.meta.url));

// A unique on-disk libsql file per call. `:memory:` would give each connection
// its own database, so an on-disk file is used to exercise one shared db.
const freshDbUrl = (): string =>
  `file:${join(tmpdir(), `kernel-db-${crypto.randomUUID()}.db`)}`;

// A fresh in-memory database per program, migrated up front.
const withDb = <A, E>(
  use: (db: DbApi) => Effect.Effect<A, E, Scope.Scope>,
): Promise<A> =>
  Effect.gen(function* () {
    const db = yield* Db;
    yield* db.migrate(migrationsFolder);
    return yield* use(db);
  }).pipe(
    Effect.provide(layer({ url: freshDbUrl() })),
    Effect.scoped,
    Effect.runPromise,
  );

const insertProject = (db: DbExecutor, id: string) =>
  db.execute((d) =>
    d.insert(projects).values({
      id,
      name: id,
      repoPath: `/tmp/${id}`,
      defaultBranch: "main",
      createdAt: 0,
    }),
  );

const allProjects = (db: DbApi) => db.query((d) => d.select().from(projects));

describe("Db service", () => {
  it("exposes exactly query, execute, transaction, migrate", async () => {
    const keys = await withDb((db) => Effect.succeed(Object.keys(db).sort()));
    expect(keys).toEqual(["execute", "migrate", "query", "transaction"]);
  });

  it("query and execute round-trip through the Drizzle builder", async () => {
    const rows = await withDb((db) =>
      Effect.gen(function* () {
        yield* insertProject(db, "p1");
        return yield* allProjects(db);
      }),
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]?.repoPath).toBe("/tmp/p1"); // field mapping preserved
  });

  it("commits a successful transaction", async () => {
    const rows = await withDb((db) =>
      Effect.gen(function* () {
        yield* db.transaction((tx) => insertProject(tx, "committed"));
        return yield* allProjects(db);
      }),
    );
    expect(rows.map((r) => r.id)).toEqual(["committed"]);
  });

  it("rolls back when the transaction body fails", async () => {
    const rows = await withDb((db) =>
      Effect.gen(function* () {
        const exit = yield* db
          .transaction((tx) =>
            Effect.gen(function* () {
              yield* insertProject(tx, "doomed");
              return yield* Effect.fail("boom" as const);
            }),
          )
          .pipe(Effect.exit);
        expect(Exit.isFailure(exit)).toBe(true);
        return yield* allProjects(db);
      }),
    );
    expect(rows).toHaveLength(0);
  });

  it("rolls back when the transaction body is interrupted", async () => {
    const rows = await withDb((db) =>
      Effect.gen(function* () {
        yield* db
          .transaction((tx) =>
            Effect.gen(function* () {
              yield* insertProject(tx, "interrupted");
              return yield* Effect.interrupt;
            }),
          )
          .pipe(Effect.exit);
        return yield* allProjects(db);
      }),
    );
    expect(rows).toHaveLength(0);
  });
});

describe("Db scope", () => {
  it("closes the libsql client when the scope closes", async () => {
    const program = Effect.gen(function* () {
      const scope = yield* Scope.make();
      const ctx = yield* Layer.build(layer({ url: freshDbUrl() })).pipe(
        Effect.provideService(Scope.Scope, scope),
      );
      const db = Context.getUnsafe(ctx, Db);
      yield* db.migrate(migrationsFolder);
      yield* insertProject(db, "open"); // works while the scope is open
      yield* Scope.close(scope, Exit.succeed(undefined));
      // The client is now closed; further use fails as a typed DbError.
      return yield* insertProject(db, "after").pipe(Effect.exit);
    });
    const exit = await Effect.runPromise(program);
    expect(Exit.isFailure(exit)).toBe(true);
  });
});
