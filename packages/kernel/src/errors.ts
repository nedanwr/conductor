// The typed-error surface — a Data.TaggedError set. Each fallible operation
// surfaces one of these in the Effect `E` channel; there is no thrown control
// flow and no unknown/any on the public surface.
//
// Subtypes are added by the module that owns them. `DbError` lands here first,
// alongside the database boundary; the rest (EventLogError, CycleError,
// GitError, ProviderError, ProjectInitError, ProjectValidationError,
// TaskNotReadyError) are introduced as their owning modules arrive.

import { Data } from "effect";

/**
 * Any failure originating at the database boundary: a rejected libsql call, a
 * failed transaction, a migration error. `cause` carries the underlying value
 * for diagnostics without leaking it as an `unknown` on the typed surface.
 */
export class DbError extends Data.TaggedError("DbError")<{
  readonly operation: "query" | "execute" | "transaction" | "migrate";
  readonly message: string;
  readonly cause: unknown;
}> {}
