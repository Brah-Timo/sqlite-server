# Example 04 — TypeScript + pg

**Application**: Task Management System  
**Language**: TypeScript 5  
**Driver**: `pg` (node-postgres) with `@types/pg`

## What It Demonstrates

- Typed interfaces for every database entity (`User`, `Task`, `Project`, `Comment`, `Tag`)
- Generic `Database` helper class with `query<T>()`, `queryOne<T>()`, `execute()`, `transaction()`
- Repository pattern — `UserRepository`, `ProjectRepository`, `TaskRepository`, etc.
- Input DTOs (`CreateTaskInput`, `UpdateTaskInput`) with optional fields using TypeScript `?:`
- Dynamic `UPDATE` builder that only sets provided fields
- Complex `JOIN` queries returning typed result rows (`TaskWithDetails`, `ProjectStats`)
- Tag system with many-to-many join table (`task_tags`)
- Typed transaction using `PoolClient`
- LOWER()+LIKE pattern for case-insensitive search (sqlite-server compatible)

## Prerequisites

- Node.js ≥ 18
- sqlite-server running on port 5432

## Run

```bash
# Install dependencies
npm install

# Run directly with ts-node (no compile step)
npm run dev

# Or compile first, then run
npm run build
npm start
```

## PowerShell

```powershell
# Install dependencies
npm install

# Run with ts-node
npx ts-node app.ts

# Or build to JavaScript first
npx tsc
node dist/app.js
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- tasks.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- tasks.db
```

## Connection Config

Edit `DB_CONFIG` at the top of `app.ts`:

```typescript
const DB_CONFIG = {
  host: "127.0.0.1",
  port: 5432,
  user: "admin",
  password: "secret",
  database: "tasks",
  max: 10,              // connection pool size
};
```

## Expected Output

```
Setting up schema...
Schema ready.

────────────────────────────────────────────────────────────
  1. Create Users
────────────────────────────────────────────────────────────
  Created: Alice Johnson (manager)
  Created: Bob Smith (developer)
  ...

────────────────────────────────────────────────────────────
  11. Project Statistics
────────────────────────────────────────────────────────────
  project_id  project_name      total_tasks  done_count  completion_rate
  ──────────────────────────────────────────────────────────────────────
  1           Web Application   2            0           0
  2           REST API          2            2           100
```
