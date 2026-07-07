/**
 * Example 04 — TypeScript + pg
 * Application: Task Management System
 *
 * sqlite-server compatibility fixes applied:
 *  - INTEGER PRIMARY KEY AUTOINCREMENT  (not SERIAL)
 *  - TEXT DEFAULT (DATETIME('now'))     (not TIMESTAMP DEFAULT NOW())
 *  - INTEGER 0/1 for booleans          (not BOOLEAN TRUE/FALSE)
 *  - LOWER(col) LIKE $1                (not ILIKE)
 *  - updated_at via DATETIME('now')    (not NOW())
 *  - One DDL per execute()             (each table separately)
 *  - No inline CHECK constraints that use ()
 *
 * Prerequisites:
 *   npm install
 *   npx ts-node app.ts
 *
 * Server must be running:
 *   ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- tasks.db
 */

import { Pool, PoolClient, QueryResult } from "pg";

// ─── Configuration ────────────────────────────────────────────────────────────

const DB_CONFIG = {
  host: "localhost",
  port: 5432,
  user: "test",
  password: "test",
  database: "test",
  max: 10,
  idleTimeoutMillis: 30000,
  connectionTimeoutMillis: 5000,
};

// ─── Domain Types ─────────────────────────────────────────────────────────────

type TaskStatus   = "todo" | "in_progress" | "review" | "done" | "cancelled";
type TaskPriority = "low" | "medium" | "high" | "critical";
type UserRole     = "admin" | "manager" | "developer" | "viewer";

interface User {
  id:         number;
  username:   string;
  email:      string;
  full_name:  string;
  role:       UserRole;
  is_active:  number; // INTEGER: 1=active, 0=inactive
  created_at: string;
}

interface Project {
  id:          number;
  name:        string;
  description: string;
  owner_id:    number;
  is_archived: number; // INTEGER: 1=archived, 0=active
  created_at:  string;
  updated_at:  string;
}

interface Task {
  id:               number;
  project_id:       number;
  title:            string;
  description:      string;
  status:           TaskStatus;
  priority:         TaskPriority;
  assignee_id:      number | null;
  reporter_id:      number;
  due_date:         string | null;
  estimated_hours:  number | null;
  actual_hours:     number | null;
  created_at:       string;
  updated_at:       string;
}

interface Comment {
  id:         number;
  task_id:    number;
  author_id:  number;
  body:       string;
  created_at: string;
  updated_at: string;
}

interface Tag {
  id:    number;
  name:  string;
  color: string;
}

// ─── Input DTOs ───────────────────────────────────────────────────────────────

interface CreateUserInput {
  username:  string;
  email:     string;
  full_name: string;
  role:      UserRole;
}

interface CreateProjectInput {
  name:        string;
  description: string;
  owner_id:    number;
}

interface CreateTaskInput {
  project_id:      number;
  title:           string;
  description:     string;
  priority:        TaskPriority;
  assignee_id?:    number;
  reporter_id:     number;
  due_date?:       string;
  estimated_hours?: number;
}

interface UpdateTaskInput {
  title?:           string;
  description?:     string;
  status?:          TaskStatus;
  priority?:        TaskPriority;
  assignee_id?:     number | null;
  due_date?:        string | null;
  actual_hours?:    number;
}

// ─── View Types ───────────────────────────────────────────────────────────────

interface TaskWithDetails extends Task {
  project_name:   string;
  assignee_name:  string | null;
  reporter_name:  string;
  tag_count:      number;
  comment_count:  number;
}

interface ProjectStats {
  project_id:       number;
  project_name:     string;
  total_tasks:      number;
  todo_count:       number;
  in_progress_count:number;
  done_count:       number;
  completion_rate:  number;
  overdue_tasks:    number;
}

interface UserWorkload {
  user_id:              number;
  full_name:            string;
  assigned_tasks:       number;
  in_progress:          number;
  avg_completion_hours: number | null;
}

// ─── Database Helper ──────────────────────────────────────────────────────────

class Database {
  private pool: Pool;

  constructor(config: typeof DB_CONFIG) {
    this.pool = new Pool(config);
    this.pool.on("error", (err: Error) => {
      console.error("Unexpected error on idle client:", err.message);
    });
  }

  async query<T>(sql: string, params: unknown[] = []): Promise<T[]> {
    const result: QueryResult<T> = await this.pool.query<T>(sql, params);
    return result.rows;
  }

  async queryOne<T>(sql: string, params: unknown[] = []): Promise<T | null> {
    const rows = await this.query<T>(sql, params);
    return rows.length > 0 ? rows[0] : null;
  }

  async execute(sql: string, params: unknown[] = []): Promise<number> {
    const result = await this.pool.query(sql, params);
    return result.rowCount ?? 0;
  }

  async transaction<T>(fn: (client: PoolClient) => Promise<T>): Promise<T> {
    const client = await this.pool.connect();
    try {
      await client.query("BEGIN");
      const result = await fn(client);
      await client.query("COMMIT");
      return result;
    } catch (err) {
      await client.query("ROLLBACK");
      throw err;
    } finally {
      client.release();
    }
  }

  async close(): Promise<void> {
    await this.pool.end();
  }
}

// ─── Schema Setup ─────────────────────────────────────────────────────────────

const DROP_TABLES = [
  "DROP TABLE IF EXISTS task_tags",
  "DROP TABLE IF EXISTS comments",
  "DROP TABLE IF EXISTS tasks",
  "DROP TABLE IF EXISTS projects",
  "DROP TABLE IF EXISTS tags",
  "DROP TABLE IF EXISTS users",
];

const CREATE_TABLES = [
  `CREATE TABLE users (
     id         INTEGER PRIMARY KEY AUTOINCREMENT,
     username   TEXT NOT NULL UNIQUE,
     email      TEXT NOT NULL UNIQUE,
     full_name  TEXT NOT NULL,
     role       TEXT NOT NULL DEFAULT 'developer',
     is_active  INTEGER NOT NULL DEFAULT 1,
     created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
   )`,

  `CREATE TABLE projects (
     id          INTEGER PRIMARY KEY AUTOINCREMENT,
     name        TEXT NOT NULL,
     description TEXT NOT NULL DEFAULT '',
     owner_id    INTEGER NOT NULL REFERENCES users(id),
     is_archived INTEGER NOT NULL DEFAULT 0,
     created_at  TEXT NOT NULL DEFAULT (DATETIME('now')),
     updated_at  TEXT NOT NULL DEFAULT (DATETIME('now'))
   )`,

  `CREATE TABLE tasks (
     id              INTEGER PRIMARY KEY AUTOINCREMENT,
     project_id      INTEGER NOT NULL REFERENCES projects(id),
     title           TEXT NOT NULL,
     description     TEXT NOT NULL DEFAULT '',
     status          TEXT NOT NULL DEFAULT 'todo',
     priority        TEXT NOT NULL DEFAULT 'medium',
     assignee_id     INTEGER REFERENCES users(id),
     reporter_id     INTEGER NOT NULL REFERENCES users(id),
     due_date        TEXT,
     estimated_hours REAL,
     actual_hours    REAL,
     created_at      TEXT NOT NULL DEFAULT (DATETIME('now')),
     updated_at      TEXT NOT NULL DEFAULT (DATETIME('now'))
   )`,

  `CREATE TABLE comments (
     id         INTEGER PRIMARY KEY AUTOINCREMENT,
     task_id    INTEGER NOT NULL REFERENCES tasks(id),
     author_id  INTEGER NOT NULL REFERENCES users(id),
     body       TEXT NOT NULL,
     created_at TEXT NOT NULL DEFAULT (DATETIME('now')),
     updated_at TEXT NOT NULL DEFAULT (DATETIME('now'))
   )`,

  `CREATE TABLE tags (
     id    INTEGER PRIMARY KEY AUTOINCREMENT,
     name  TEXT NOT NULL UNIQUE,
     color TEXT NOT NULL DEFAULT '#6B7280'
   )`,

  `CREATE TABLE task_tags (
     task_id INTEGER NOT NULL REFERENCES tasks(id),
     tag_id  INTEGER NOT NULL REFERENCES tags(id),
     PRIMARY KEY (task_id, tag_id)
   )`,
];

async function setupSchema(db: Database): Promise<void> {
  console.log("Setting up schema...");
  for (const sql of DROP_TABLES)   await db.execute(sql);
  for (const sql of CREATE_TABLES) await db.execute(sql);
  console.log("Schema ready.\n");
}

// ─── Repository: Users ────────────────────────────────────────────────────────

class UserRepository {
  constructor(private db: Database) {}

  async create(input: CreateUserInput): Promise<User> {
    const rows = await this.db.query<User>(
      `INSERT INTO users (username, email, full_name, role, is_active)
       VALUES ($1, $2, $3, $4, 1) RETURNING *`,
      [input.username, input.email, input.full_name, input.role]
    );
    return rows[0];
  }

  async findById(id: number): Promise<User | null> {
    return this.db.queryOne<User>("SELECT * FROM users WHERE id = $1", [id]);
  }

  async findAll(activeOnly: boolean = true): Promise<User[]> {
    const sql = activeOnly
      ? "SELECT * FROM users WHERE is_active = 1 ORDER BY full_name"
      : "SELECT * FROM users ORDER BY full_name";
    return this.db.query<User>(sql);
  }

  async deactivate(id: number): Promise<boolean> {
    const count = await this.db.execute(
      "UPDATE users SET is_active = 0 WHERE id = $1", [id]
    );
    return count > 0;
  }

  async getWorkload(): Promise<UserWorkload[]> {
    return this.db.query<UserWorkload>(
      `SELECT
         u.id            AS user_id,
         u.full_name,
         COUNT(t.id)     AS assigned_tasks,
         SUM(CASE WHEN t.status = 'in_progress' THEN 1 ELSE 0 END) AS in_progress,
         AVG(t.actual_hours) AS avg_completion_hours
       FROM users u
       LEFT JOIN tasks t ON t.assignee_id = u.id
         AND t.status NOT IN ('done', 'cancelled')
       WHERE u.is_active = 1
       GROUP BY u.id, u.full_name
       ORDER BY assigned_tasks DESC`
    );
  }
}

// ─── Repository: Projects ─────────────────────────────────────────────────────

class ProjectRepository {
  constructor(private db: Database) {}

  async create(input: CreateProjectInput): Promise<Project> {
    const rows = await this.db.query<Project>(
      `INSERT INTO projects (name, description, owner_id)
       VALUES ($1, $2, $3) RETURNING *`,
      [input.name, input.description, input.owner_id]
    );
    return rows[0];
  }

  async findAll(includeArchived: boolean = false): Promise<Project[]> {
    const sql = includeArchived
      ? "SELECT * FROM projects ORDER BY name"
      : "SELECT * FROM projects WHERE is_archived = 0 ORDER BY name";
    return this.db.query<Project>(sql);
  }

  async archive(id: number): Promise<boolean> {
    const count = await this.db.execute(
      "UPDATE projects SET is_archived = 1 WHERE id = $1", [id]
    );
    return count > 0;
  }

  async getStats(): Promise<ProjectStats[]> {
    return this.db.query<ProjectStats>(
      `SELECT
         p.id   AS project_id,
         p.name AS project_name,
         COUNT(t.id) AS total_tasks,
         SUM(CASE WHEN t.status = 'todo'        THEN 1 ELSE 0 END) AS todo_count,
         SUM(CASE WHEN t.status = 'in_progress' THEN 1 ELSE 0 END) AS in_progress_count,
         SUM(CASE WHEN t.status = 'done'        THEN 1 ELSE 0 END) AS done_count,
         ROUND(
           CAST(SUM(CASE WHEN t.status = 'done' THEN 1 ELSE 0 END) AS REAL) /
           NULLIF(COUNT(t.id), 0) * 100,
           1
         ) AS completion_rate,
         SUM(CASE WHEN t.due_date < DATE('now') AND t.status NOT IN ('done','cancelled') THEN 1 ELSE 0 END) AS overdue_tasks
       FROM projects p
       LEFT JOIN tasks t ON t.project_id = p.id
       WHERE p.is_archived = 0
       GROUP BY p.id, p.name
       ORDER BY total_tasks DESC`
    );
  }
}

// ─── Repository: Tasks ────────────────────────────────────────────────────────

class TaskRepository {
  constructor(private db: Database) {}

  async create(input: CreateTaskInput): Promise<Task> {
    const rows = await this.db.query<Task>(
      `INSERT INTO tasks
         (project_id, title, description, status, priority, assignee_id,
          reporter_id, due_date, estimated_hours)
       VALUES ($1,$2,$3,'todo',$4,$5,$6,$7,$8) RETURNING *`,
      [
        input.project_id,
        input.title,
        input.description,
        input.priority,
        input.assignee_id ?? null,
        input.reporter_id,
        input.due_date ?? null,
        input.estimated_hours ?? null,
      ]
    );
    return rows[0];
  }

  async findById(id: number): Promise<Task | null> {
    return this.db.queryOne<Task>("SELECT * FROM tasks WHERE id = $1", [id]);
  }

  async findByProject(projectId: number): Promise<Task[]> {
    return this.db.query<Task>(
      "SELECT * FROM tasks WHERE project_id = $1 ORDER BY priority DESC, created_at",
      [projectId]
    );
  }

  async findByAssignee(assigneeId: number): Promise<Task[]> {
    return this.db.query<Task>(
      `SELECT * FROM tasks
       WHERE assignee_id = $1 AND status NOT IN ('done', 'cancelled')
       ORDER BY priority DESC, due_date`,
      [assigneeId]
    );
  }

  async update(id: number, input: UpdateTaskInput): Promise<Task | null> {
    const sets: string[] = [];
    const values: unknown[] = [];
    let idx = 1;

    if (input.title       !== undefined) { sets.push(`title = $${idx++}`);        values.push(input.title); }
    if (input.description !== undefined) { sets.push(`description = $${idx++}`);  values.push(input.description); }
    if (input.status      !== undefined) { sets.push(`status = $${idx++}`);       values.push(input.status); }
    if (input.priority    !== undefined) { sets.push(`priority = $${idx++}`);     values.push(input.priority); }
    if (input.assignee_id !== undefined) { sets.push(`assignee_id = $${idx++}`);  values.push(input.assignee_id); }
    if (input.due_date    !== undefined) { sets.push(`due_date = $${idx++}`);     values.push(input.due_date); }
    if (input.actual_hours!== undefined) { sets.push(`actual_hours = $${idx++}`); values.push(input.actual_hours); }

    if (sets.length === 0) return this.findById(id);

    sets.push(`updated_at = DATETIME('now')`);
    values.push(id);

    const rows = await this.db.query<Task>(
      `UPDATE tasks SET ${sets.join(", ")} WHERE id = $${idx} RETURNING *`,
      values
    );
    return rows.length > 0 ? rows[0] : null;
  }

  async findWithDetails(projectId?: number): Promise<TaskWithDetails[]> {
    const where  = projectId ? "WHERE t.project_id = $1" : "";
    const params = projectId ? [projectId] : [];

    return this.db.query<TaskWithDetails>(
      `SELECT
         t.*,
         p.name          AS project_name,
         a.full_name     AS assignee_name,
         r.full_name     AS reporter_name,
         COUNT(DISTINCT tt.tag_id) AS tag_count,
         COUNT(DISTINCT c.id)      AS comment_count
       FROM tasks t
       JOIN projects p ON p.id = t.project_id
       LEFT JOIN users a ON a.id = t.assignee_id
       JOIN users r ON r.id = t.reporter_id
       LEFT JOIN task_tags tt ON tt.task_id = t.id
       LEFT JOIN comments c ON c.task_id = t.id
       ${where}
       GROUP BY t.id, p.name, a.full_name, r.full_name
       ORDER BY t.priority DESC, t.created_at`,
      params
    );
  }

  async searchByTitle(query: string): Promise<Task[]> {
    const pattern = `%${query.toLowerCase()}%`;
    return this.db.query<Task>(
      `SELECT * FROM tasks
       WHERE LOWER(title) LIKE $1 OR LOWER(description) LIKE $1
       ORDER BY created_at DESC`,
      [pattern]
    );
  }

  async getOverdue(): Promise<Task[]> {
    return this.db.query<Task>(
      `SELECT * FROM tasks
       WHERE due_date < DATE('now')
         AND status NOT IN ('done', 'cancelled')
       ORDER BY due_date`
    );
  }
}

// ─── Repository: Comments ─────────────────────────────────────────────────────

class CommentRepository {
  constructor(private db: Database) {}

  async add(taskId: number, authorId: number, body: string): Promise<Comment> {
    const rows = await this.db.query<Comment>(
      `INSERT INTO comments (task_id, author_id, body)
       VALUES ($1, $2, $3) RETURNING *`,
      [taskId, authorId, body]
    );
    return rows[0];
  }

  async findByTask(taskId: number): Promise<Array<Comment & { author_name: string }>> {
    return this.db.query(
      `SELECT c.*, u.full_name AS author_name
       FROM comments c
       JOIN users u ON u.id = c.author_id
       WHERE c.task_id = $1
       ORDER BY c.created_at`,
      [taskId]
    );
  }
}

// ─── Repository: Tags ─────────────────────────────────────────────────────────

class TagRepository {
  constructor(private db: Database) {}

  async create(name: string, color: string): Promise<Tag> {
    const rows = await this.db.query<Tag>(
      "INSERT INTO tags (name, color) VALUES ($1, $2) RETURNING *",
      [name, color]
    );
    return rows[0];
  }

  async attachToTask(taskId: number, tagId: number): Promise<void> {
    await this.db.execute(
      "INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2)",
      [taskId, tagId]
    );
  }

  async findByTask(taskId: number): Promise<Tag[]> {
    return this.db.query<Tag>(
      `SELECT t.* FROM tags t
       JOIN task_tags tt ON tt.tag_id = t.id
       WHERE tt.task_id = $1
       ORDER BY t.name`,
      [taskId]
    );
  }

  async findAll(): Promise<Tag[]> {
    return this.db.query<Tag>("SELECT * FROM tags ORDER BY name");
  }
}

// ─── Print Utilities ──────────────────────────────────────────────────────────

function printHeader(title: string): void {
  const line = "─".repeat(60);
  console.log(`\n${line}\n  ${title}\n${line}`);
}

function printTable<T extends object>(rows: T[]): void {
  if (rows.length === 0) { console.log("  (no rows)"); return; }
  const keys = Object.keys(rows[0]) as (keyof T)[];
  const widths: Record<string, number> = {};
  keys.forEach(k => {
    widths[k as string] = Math.max(
      String(k).length,
      ...rows.map(r => String(r[k] ?? "NULL").length)
    );
  });
  const fmt = (r: T) =>
    keys.map(k => String(r[k] ?? "NULL").padEnd(widths[k as string])).join("  │  ");
  const sep = keys.map(k => "─".repeat(widths[k as string])).join("──┼──");
  console.log("  " + keys.map(k => String(k).padEnd(widths[k as string])).join("  │  "));
  console.log("  " + sep);
  rows.forEach(r => console.log("  " + fmt(r)));
}

// ─── Main ─────────────────────────────────────────────────────────────────────

async function main(): Promise<void> {
  const db       = new Database(DB_CONFIG);
  const users    = new UserRepository(db);
  const projects = new ProjectRepository(db);
  const tasks    = new TaskRepository(db);
  const comments = new CommentRepository(db);
  const tags     = new TagRepository(db);

  try {
    await setupSchema(db);

    // ── 1. Create Users ───────────────────────────────────────────────────────
    printHeader("1. Create Users");
    const alice = await users.create({ username: "alice", email: "alice@example.com", full_name: "Alice Johnson", role: "manager" });
    const bob   = await users.create({ username: "bob",   email: "bob@example.com",   full_name: "Bob Smith",     role: "developer" });
    const carol = await users.create({ username: "carol", email: "carol@example.com", full_name: "Carol White",   role: "developer" });
    console.log(`  Created: ${alice.full_name} (${alice.role})`);
    console.log(`  Created: ${bob.full_name} (${bob.role})`);
    console.log(`  Created: ${carol.full_name} (${carol.role})`);

    // ── 2. Create Projects ────────────────────────────────────────────────────
    printHeader("2. Create Projects");
    const webApp = await projects.create({ name: "Web Application", description: "Customer-facing portal", owner_id: alice.id });
    const api    = await projects.create({ name: "REST API",         description: "Backend microservices",  owner_id: alice.id });
    console.log(`  Created: ${webApp.name} (id=${webApp.id})`);
    console.log(`  Created: ${api.name} (id=${api.id})`);

    // ── 3. Create Tags ────────────────────────────────────────────────────────
    printHeader("3. Create Tags");
    const tagBug      = await tags.create("bug",      "#EF4444");
    const tagFeature  = await tags.create("feature",  "#3B82F6");
    const tagUrgent   = await tags.create("urgent",   "#F59E0B");
    const tagBackend  = await tags.create("backend",  "#8B5CF6");
    const tagFrontend = await tags.create("frontend", "#10B981");
    const allTags = await tags.findAll();
    printTable(allTags);

    // ── 4. Create Tasks with Transaction ─────────────────────────────────────
    printHeader("4. Create Tasks (inside transaction)");
    const [task1, task2, task3, task4] = await db.transaction(async (client) => {
      const t1 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,assignee_id,reporter_id,due_date,estimated_hours)
         VALUES ($1,$2,$3,'todo','high',$4,$5,$6,$7) RETURNING *`,
        [webApp.id, "Implement login page", "OAuth2 + JWT flow", bob.id, alice.id, "2025-06-30", 8]
      );
      const t2 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,assignee_id,reporter_id,estimated_hours)
         VALUES ($1,$2,$3,'in_progress','critical',$4,$5,$6) RETURNING *`,
        [api.id, "Fix auth bypass", "CVE-2025-0001 patch", carol.id, alice.id, 4]
      );
      const t3 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,reporter_id,due_date)
         VALUES ($1,$2,$3,'todo','medium',$4,$5) RETURNING *`,
        [webApp.id, "Add dark mode toggle", "User preference in localStorage", alice.id, "2025-08-15"]
      );
      const t4 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,assignee_id,reporter_id,actual_hours)
         VALUES ($1,$2,$3,'done','low',$4,$5,$6) RETURNING *`,
        [api.id, "Update README", "Add API docs section", bob.id, carol.id, 2]
      );
      return [t1.rows[0], t2.rows[0], t3.rows[0], t4.rows[0]];
    });
    console.log(`  Created: "${task1.title}" [${task1.priority}/${task1.status}]`);
    console.log(`  Created: "${task2.title}" [${task2.priority}/${task2.status}]`);
    console.log(`  Created: "${task3.title}" [${task3.priority}/${task3.status}]`);
    console.log(`  Created: "${task4.title}" [${task4.priority}/${task4.status}]`);

    // ── 5. Attach Tags ────────────────────────────────────────────────────────
    printHeader("5. Attach Tags");
    await tags.attachToTask(task1.id, tagFeature.id);
    await tags.attachToTask(task1.id, tagFrontend.id);
    await tags.attachToTask(task2.id, tagBug.id);
    await tags.attachToTask(task2.id, tagUrgent.id);
    await tags.attachToTask(task2.id, tagBackend.id);
    const task1Tags = await tags.findByTask(task1.id);
    console.log(`  Tags on "${task1.title}": ${task1Tags.map(t => t.name).join(", ")}`);
    const task2Tags = await tags.findByTask(task2.id);
    console.log(`  Tags on "${task2.title}": ${task2Tags.map(t => t.name).join(", ")}`);

    // ── 6. Add Comments ───────────────────────────────────────────────────────
    printHeader("6. Add Comments");
    await comments.add(task2.id, alice.id, "Critical — needs immediate attention.");
    await comments.add(task2.id, carol.id, "On it. Header injection issue.");
    await comments.add(task1.id, bob.id,   "Starting Monday. Need design specs.");
    const task2Comments = await comments.findByTask(task2.id);
    console.log(`  Comments on "${task2.title}":`);
    task2Comments.forEach(c => console.log(`    [${c.author_name}]: ${c.body}`));

    // ── 7. Update Task Status ─────────────────────────────────────────────────
    printHeader("7. Update Tasks");
    const updated = await tasks.update(task2.id, { status: "done", actual_hours: 3.5 });
    console.log(`  "${updated!.title}" → status=${updated!.status}, actual_hours=${updated!.actual_hours}`);
    const reassigned = await tasks.update(task3.id, { assignee_id: bob.id, priority: "high" });
    console.log(`  "${reassigned!.title}" → assignee=bob, priority=${reassigned!.priority}`);

    // ── 8. Tasks with Full Details ────────────────────────────────────────────
    printHeader("8. All Tasks with Details");
    const detailed = await tasks.findWithDetails();
    detailed.forEach(t => {
      console.log(
        `  [${t.priority.toUpperCase().padEnd(8)}] ${t.title.padEnd(30)} ` +
        `${t.status.padEnd(12)} assignee=${t.assignee_name ?? "unassigned"} ` +
        `tags=${t.tag_count} comments=${t.comment_count}`
      );
    });

    // ── 9. Search Tasks ───────────────────────────────────────────────────────
    printHeader("9. Search Tasks");
    const searchResult = await tasks.searchByTitle("auth");
    console.log(`  Search "auth" → ${searchResult.length} result(s):`);
    searchResult.forEach(t => console.log(`    - ${t.title}`));

    // ── 10. User Workload ─────────────────────────────────────────────────────
    printHeader("10. User Workload Report");
    const workload = await users.getWorkload();
    printTable(workload);

    // ── 11. Project Statistics ────────────────────────────────────────────────
    printHeader("11. Project Statistics");
    const stats = await projects.getStats();
    printTable(stats);

    // ── 12. Cleanup ───────────────────────────────────────────────────────────
    printHeader("12. Cleanup");
    for (const tbl of ["task_tags","comments","tasks","projects","tags","users"]) {
      await db.execute(`DROP TABLE IF EXISTS ${tbl}`);
    }
    console.log("  All tables dropped.");

    printHeader("Done — All 12 steps completed!");

  } catch (err) {
    console.error("Fatal error:", err);
    process.exit(1);
  } finally {
    await db.close();
  }
}

main();
