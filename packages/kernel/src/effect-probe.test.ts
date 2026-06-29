// Effect v4 beta idiom probe — verify against the installed beta, not docs.
// Asserts the *installed* API shape for the four idioms the kernel leans on:
// Context.Service, acquireRelease scopes, PubSub, and Data.TaggedError. If a
// later beta bump changes these shapes, this test breaks first — before any
// real module does.
//
// Installed: effect@4.0.0-beta.91 (single unified `effect` package).
import { Context, Data, Effect, PubSub, Scope } from "effect";
import { describe, expect, it } from "vitest";

describe("effect v4 beta idioms", () => {
  it("Context.Service produces a yieldable, providable service key", async () => {
    class Greeter extends Context.Service<Greeter, { greet: () => string }>()(
      "Greeter"
    ) {}

    const program = Effect.gen(function* () {
      const g = yield* Greeter;
      return g.greet();
    });

    const result = await Effect.runPromise(
      program.pipe(Effect.provideService(Greeter, { greet: () => "hello" }))
    );
    expect(result).toBe("hello");
  });

  it("acquireRelease runs the release on scope close", async () => {
    const order: string[] = [];

    const program = Effect.gen(function* () {
      yield* Effect.acquireRelease(
        Effect.sync(() => order.push("acquire")),
        () => Effect.sync(() => order.push("release"))
      );
      order.push("use");
    });

    await Effect.runPromise(Effect.scoped(program));
    expect(order).toEqual(["acquire", "use", "release"]);
  });

  it("PubSub delivers published values to a scoped subscriber", async () => {
    const program = Effect.gen(function* () {
      const pubsub = yield* PubSub.unbounded<number>();
      const sub = yield* PubSub.subscribe(pubsub);
      yield* PubSub.publish(pubsub, 42);
      return yield* PubSub.take(sub);
    });

    const result = await Effect.runPromise(Effect.scoped(program));
    expect(result).toBe(42);
  });

  it("Data.TaggedError carries its tag and is catchable by tag", async () => {
    class WidgetError extends Data.TaggedError("WidgetError")<{
      readonly reason: string;
    }> {}

    const program = Effect.fail(new WidgetError({ reason: "boom" })).pipe(
      Effect.catchTag("WidgetError", (e) => Effect.succeed(e.reason))
    );

    const result = await Effect.runPromise(program);
    expect(result).toBe("boom");
  });

  it("Scope is an importable service tag (smoke)", () => {
    expect(Scope.Scope).toBeDefined();
  });
});
