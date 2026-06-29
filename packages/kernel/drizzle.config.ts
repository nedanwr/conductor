import { defineConfig } from "drizzle-kit";

// Drizzle Kit config — libsql, local file mode. Points at the schema (empty
// for now; the nine-table schema lands later). Generated migrations are the
// ONLY place raw SQL lives.
export default defineConfig({
  dialect: "sqlite",
  schema: "./src/db/schema.ts",
  out: "./src/db/migrations",
  dbCredentials: {
    url: "file:./.orchestrator/kernel.db",
  },
});
