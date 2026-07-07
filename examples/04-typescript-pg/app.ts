/**
 * Example 04 — TypeScript + pg
 * Application: Task Management System
 *
 * Demonstrates:
 *  - Typed interfaces for all database entities
 *  - Generic repository pattern with TypeScript generics
 *  - Async/await with proper error handling
 *  - Prepared statements via pg parameterized queries
 *  - Transaction management with rollback safety
 *  - Enum types mapped to TypeScript union types
 *  - Complex JOINs returning typed results
 *  - Connection pool configuration with types
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
  host: "127.0.0.1",
  port: 5432,
  user: "admin",
  password: "secret",
  database: "tasks",
  max: 10,
  idleTimeoutMillis: 30000,
  connectionTimeoutMillis: 5000,
};

// ─── Domain Types ─────────────────────────────────────────────────────────────

type TaskStatus = "todo" | "in_progress" | "review" | "done" | "cancelled";
type TaskPriority = "low" | "medium" | "high" | "critical";
type UserRole = "admin" | "manager" | "developer" | "viewer";

interface User {
  id: number;
  username: string;
  email: string;
  full_name: string;
  role: UserRole;
  is_active: boolean;
  created_at: string;
}

interface Project {
  id: number;
  name: string;
  description: string;
  owner_id: number;
  is_archived: boolean;
  created_at: string;
  updated_at: string;
}

interface Task {
  id: number;
  project_id: number;
  title: string;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  assignee_id: number | null;
  reporter_id: number;
  due_date: string | null;
  estimated_hours: number | null;
  actual_hours: number | null;
  created_at: string;
  updated_at: string;
}

interface Comment {
  id: number;
  task_id: number;
  author_id: number;
  body: string;
  created_at: string;
  updated_at: string;
}

interface Tag {
  id: number;
  name: string;
  color: string;
}

// ─── Input DTOs (Data Transfer Objects) ──────────────────────────────────────

interface CreateUserInput {
  username: string;
  email: string;
  full_name: string;
  role: UserRole;
}

interface CreateProjectInput {
  name: string;
  description: string;
  owner_id: number;
}

interface CreateTaskInput {
  project_id: number;
  title: string;
  description: string;
  priority: TaskPriority;
  assignee_id?: number;
  reporter_id: number;
  due_date?: string;
  estimated_hours?: number;
}

interface UpdateTaskInput {
  title?: string;
  description?: string;
  status?: TaskStatus;
  priority?: TaskPriority;
  assignee_id?: number | null;
  due_date?: string | null;
  actual_hours?: number;
}

// ─── View / Report Types ──────────────────────────────────────────────────────

interface TaskWithDetails extends Task {
  project_name: string;
  assignee_name: string | null;
  reporter_name: string;
  tag_count: number;
  comment_count: number;
}

interface ProjectStats {
  project_id: number;
  project_name: string;
  total_tasks: number;
  todo_count: number;
  in_progress_count: number;
  done_count: number;
  completion_rate: number;
  overdue_tasks: number;
}

interface UserWorkload {
  user_id: number;
  full_name: string;
  assigned_tasks: number;
  in_progress: number;
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

// ─── Repository: Users ────────────────────────────────────────────────────────

class UserRepository {
  constructor(private db: Database) {}

  async create(input: CreateUserInput): Promise<User> {
    const rows = await this.db.query<User>(
      `INSERT INTO users (username, email, full_name, role, is_active)
       VALUES ($1, $2, $3, $4, TRUE)
       RETURNING *`,
      [input.username, input.email, input.full_name, input.role]
    );
    return rows[0];
  }

  async findById(id: number): Promise<User | null> {
    return this.db.queryOne<User>(
      "SELECT * FROM users WHERE id = $1",
      [id]
    );
  }

  async findByUsername(username: string): Promise<User | null> {
    return this.db.queryOne<User>(
      "SELECT * FROM users WHERE username = $1",
      [username]
    );
  }

  async findAll(activeOnly: boolean = true): Promise<User[]> {
    const sql = activeOnly
      ? "SELECT * FROM users WHERE is_active = TRUE ORDER BY full_name"
      : "SELECT * FROM users ORDER BY full_name";
    return this.db.query<User>(sql);
  }

  async findByRole(role: UserRole): Promise<User[]> {
    return this.db.query<User>(
      "SELECT * FROM users WHERE role = $1 AND is_active = TRUE ORDER BY full_name",
      [role]
    );
  }

  async deactivate(id: number): Promise<boolean> {
    const count = await this.db.execute(
      "UPDATE users SET is_active = FALSE WHERE id = $1",
      [id]
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
         AVG(t.actual_hours)  AS avg_completion_hours
       FROM users u
       LEFT JOIN tasks t ON t.assignee_id = u.id
         AND t.status NOT IN ('done', 'cancelled')
       WHERE u.is_active = TRUE
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
       VALUES ($1, $2, $3)
       RETURNING *`,
      [input.name, input.description, input.owner_id]
    );
    return rows[0];
  }

  async findById(id: number): Promise<Project | null> {
    return this.db.queryOne<Project>(
      "SELECT * FROM projects WHERE id = $1",
      [id]
    );
  }

  async findAll(includeArchived: boolean = false): Promise<Project[]> {
    const sql = includeArchived
      ? "SELECT * FROM projects ORDER BY name"
      : "SELECT * FROM projects WHERE is_archived = FALSE ORDER BY name";
    return this.db.query<Project>(sql);
  }

  async findByOwner(ownerId: number): Promise<Project[]> {
    return this.db.query<Project>(
      "SELECT * FROM projects WHERE owner_id = $1 AND is_archived = FALSE ORDER BY name",
      [ownerId]
    );
  }

  async archive(id: number): Promise<boolean> {
    const count = await this.db.execute(
      "UPDATE projects SET is_archived = TRUE WHERE id = $1",
      [id]
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
       WHERE p.is_archived = FALSE
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
       VALUES ($1, $2, $3, 'todo', $4, $5, $6, $7, $8)
       RETURNING *`,
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
    return this.db.queryOne<Task>(
      "SELECT * FROM tasks WHERE id = $1",
      [id]
    );
  }

  async findByProject(projectId: number, status?: TaskStatus): Promise<Task[]> {
    if (status) {
      return this.db.query<Task>(
        "SELECT * FROM tasks WHERE project_id = $1 AND status = $2 ORDER BY priority DESC, created_at",
        [projectId, status]
      );
    }
    return this.db.query<Task>(
      "SELECT * FROM tasks WHERE project_id = $1 ORDER BY priority DESC, created_at",
      [projectId]
    );
  }

  async findByAssignee(assigneeId: number): Promise<Task[]> {
    return this.db.query<Task>(
      `SELECT * FROM tasks
       WHERE assignee_id = $1 AND status NOT IN ('done', 'cancelled')
       ORDER BY priority DESC, due_date NULLS LAST`,
      [assigneeId]
    );
  }

  async update(id: number, input: UpdateTaskInput): Promise<Task | null> {
    const sets: string[] = [];
    const values: unknown[] = [];
    let idx = 1;

    if (input.title !== undefined)       { sets.push(`title = $${idx++}`);        values.push(input.title); }
    if (input.description !== undefined) { sets.push(`description = $${idx++}`);  values.push(input.description); }
    if (input.status !== undefined)      { sets.push(`status = $${idx++}`);       values.push(input.status); }
    if (input.priority !== undefined)    { sets.push(`priority = $${idx++}`);     values.push(input.priority); }
    if (input.assignee_id !== undefined) { sets.push(`assignee_id = $${idx++}`);  values.push(input.assignee_id); }
    if (input.due_date !== undefined)    { sets.push(`due_date = $${idx++}`);     values.push(input.due_date); }
    if (input.actual_hours !== undefined){ sets.push(`actual_hours = $${idx++}`); values.push(input.actual_hours); }

    if (sets.length === 0) return this.findById(id);

    sets.push(`updated_at = DATETIME('now')`);
    values.push(id);

    const rows = await this.db.query<Task>(
      `UPDATE tasks SET ${sets.join(", ")} WHERE id = $${idx} RETURNING *`,
      values
    );
    return rows.length > 0 ? rows[0] : null;
  }

  async delete(id: number): Promise<boolean> {
    const count = await this.db.execute("DELETE FROM tasks WHERE id = $1", [id]);
    return count > 0;
  }

  async findWithDetails(projectId?: number): Promise<TaskWithDetails[]> {
    const where = projectId ? `WHERE t.project_id = $1` : "";
    const params = projectId ? [projectId] : [];

    return this.db.query<TaskWithDetails>(
      `SELECT
         t.*,
         p.name          AS project_name,
         a.full_name     AS assignee_name,
         r.full_name     AS reporter_name,
         COUNT(DISTINCT tt.tag_id)   AS tag_count,
         COUNT(DISTINCT c.id)        AS comment_count
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

  async getOverdue(): Promise<TaskWithDetails[]> {
    return this.db.query<TaskWithDetails>(
      `SELECT
         t.*,
         p.name          AS project_name,
         a.full_name     AS assignee_name,
         r.full_name     AS reporter_name,
         0               AS tag_count,
         0               AS comment_count
       FROM tasks t
       JOIN projects p ON p.id = t.project_id
       LEFT JOIN users a ON a.id = t.assignee_id
       JOIN users r ON r.id = t.reporter_id
       WHERE t.due_date < DATE('now')
         AND t.status NOT IN ('done', 'cancelled')
       ORDER BY t.due_date`
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

  async delete(id: number, authorId: number): Promise<boolean> {
    const count = await this.db.execute(
      "DELETE FROM comments WHERE id = $1 AND author_id = $2",
      [id, authorId]
    );
    return count > 0;
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

  async detachFromTask(taskId: number, tagId: number): Promise<void> {
    await this.db.execute(
      "DELETE FROM task_tags WHERE task_id = $1 AND tag_id = $2",
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

// ─── Schema Setup ─────────────────────────────────────────────────────────────

async function setupSchema(db: Database): Promise<void> {
  console.log("Setting up schema...");

  await db.execute(`
    CREATE TABLE IF NOT EXISTS users (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      username   TEXT NOT NULL UNIQUE,
      email      TEXT NOT NULL UNIQUE,
      full_name  TEXT NOT NULL,
      role       TEXT NOT NULL DEFAULT 'developer',
      is_active  INTEGER NOT NULL DEFAULT 1,
      created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
    )
  `);

  await db.execute(`
    CREATE TABLE IF NOT EXISTS projects (
      id          INTEGER PRIMARY KEY AUTOINCREMENT,
      name        TEXT NOT NULL,
      description TEXT NOT NULL DEFAULT '',
      owner_id    INTEGER NOT NULL REFERENCES users(id),
      is_archived INTEGER NOT NULL DEFAULT 0,
      created_at  TEXT NOT NULL DEFAULT (DATETIME('now')),
      updated_at  TEXT NOT NULL DEFAULT (DATETIME('now'))
    )
  `);

  await db.execute(`
    CREATE TABLE IF NOT EXISTS tasks (
      id               INTEGER PRIMARY KEY AUTOINCREMENT,
      project_id       INTEGER NOT NULL REFERENCES projects(id),
      title            TEXT NOT NULL,
      description      TEXT NOT NULL DEFAULT '',
      status           TEXT NOT NULL DEFAULT 'todo',
      priority         TEXT NOT NULL DEFAULT 'medium',
      assignee_id      INTEGER REFERENCES users(id),
      reporter_id      INTEGER NOT NULL REFERENCES users(id),
      due_date         TEXT,
      estimated_hours  REAL,
      actual_hours     REAL,
      created_at       TEXT NOT NULL DEFAULT (DATETIME('now')),
      updated_at       TEXT NOT NULL DEFAULT (DATETIME('now'))
    )
  `);

  await db.execute(`
    CREATE TABLE IF NOT EXISTS comments (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      task_id    INTEGER NOT NULL REFERENCES tasks(id),
      author_id  INTEGER NOT NULL REFERENCES users(id),
      body       TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT (DATETIME('now')),
      updated_at TEXT NOT NULL DEFAULT (DATETIME('now'))
    )
  `);

  await db.execute(`
    CREATE TABLE IF NOT EXISTS tags (
      id    INTEGER PRIMARY KEY AUTOINCREMENT,
      name  TEXT NOT NULL UNIQUE,
      color TEXT NOT NULL DEFAULT '#6B7280'
    )
  `);

  await db.execute(`
    CREATE TABLE IF NOT EXISTS task_tags (
      task_id INTEGER NOT NULL REFERENCES tasks(id),
      tag_id  INTEGER NOT NULL REFERENCES tags(id),
      PRIMARY KEY (task_id, tag_id)
    )
  `);

  console.log("Schema ready.\n");
}

// ─── Demo / Main ──────────────────────────────────────────────────────────────

function printHeader(title: string): void {
  const line = "─".repeat(60);
  console.log(`\n${line}`);
  console.log(`  ${title}`);
  console.log(line);
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
  const row = (r: T) => keys.map(k => String(r[k] ?? "NULL").padEnd(widths[k as string])).join("  │  ");
  const sep  = keys.map(k => "─".repeat(widths[k as string])).join("──┼──");
  console.log("  " + keys.map(k => String(k).padEnd(widths[k as string])).join("  │  "));
  console.log("  " + sep);
  rows.forEach(r => console.log("  " + row(r)));
}

async function main(): Promise<void> {
  const db = new Database(DB_CONFIG);
  const users    = new UserRepository(db);
  const projects = new ProjectRepository(db);
  const tasks    = new TaskRepository(db);
  const comments = new CommentRepository(db);
  const tags     = new TagRepository(db);

  try {
    await setupSchema(db);

    // ── 1. Create Users ───────────────────────────────────────────────────────
    printHeader("1. Create Users");

    const alice = await users.create({
      username: "alice",
      email: "alice@example.com",
      full_name: "Alice Johnson",
      role: "manager",
    });
    const bob = await users.create({
      username: "bob",
      email: "bob@example.com",
      full_name: "Bob Smith",
      role: "developer",
    });
    const carol = await users.create({
      username: "carol",
      email: "carol@example.com",
      full_name: "Carol White",
      role: "developer",
    });
    const dave = await users.create({
      username: "dave",
      email: "dave@example.com",
      full_name: "Dave Brown",
      role: "viewer",
    });

    console.log(`  Created: ${alice.full_name} (${alice.role})`);
    console.log(`  Created: ${bob.full_name} (${bob.role})`);
    console.log(`  Created: ${carol.full_name} (${carol.role})`);
    console.log(`  Created: ${dave.full_name} (${dave.role})`);

    // ── 2. Create Projects ────────────────────────────────────────────────────
    printHeader("2. Create Projects");

    const webApp = await projects.create({
      name: "Web Application",
      description: "Customer-facing web portal with React frontend",
      owner_id: alice.id,
    });
    const api = await projects.create({
      name: "REST API",
      description: "Backend microservices in Go",
      owner_id: alice.id,
    });

    console.log(`  Created project: ${webApp.name} (id=${webApp.id})`);
    console.log(`  Created project: ${api.name} (id=${api.id})`);

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
        [webApp.id, "Implement login page", "OAuth2 + JWT flow", bob.id, alice.id, "2025-02-28", 8]
      );
      const t2 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,assignee_id,reporter_id,estimated_hours)
         VALUES ($1,$2,$3,'in_progress','critical',$4,$5,$6) RETURNING *`,
        [api.id, "Fix authentication bypass", "CVE-2025-0001 security patch", carol.id, alice.id, 4]
      );
      const t3 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,reporter_id,due_date)
         VALUES ($1,$2,$3,'todo','medium',$4,$5) RETURNING *`,
        [webApp.id, "Add dark mode toggle", "User preference stored in localStorage", alice.id, "2025-03-15"]
      );
      const t4 = await client.query<Task>(
        `INSERT INTO tasks (project_id,title,description,status,priority,assignee_id,reporter_id,actual_hours)
         VALUES ($1,$2,$3,'done','low',$4,$5,$6) RETURNING *`,
        [api.id, "Update README", "Add API documentation section", bob.id, carol.id, 2]
      );
      return [t1.rows[0], t2.rows[0], t3.rows[0], t4.rows[0]];
    });

    console.log(`  Created: "${task1.title}" [${task1.priority}/${task1.status}]`);
    console.log(`  Created: "${task2.title}" [${task2.priority}/${task2.status}]`);
    console.log(`  Created: "${task3.title}" [${task3.priority}/${task3.status}]`);
    console.log(`  Created: "${task4.title}" [${task4.priority}/${task4.status}]`);

    // ── 5. Attach Tags ────────────────────────────────────────────────────────
    printHeader("5. Attach Tags to Tasks");

    await tags.attachToTask(task1.id, tagFeature.id);
    await tags.attachToTask(task1.id, tagFrontend.id);
    await tags.attachToTask(task2.id, tagBug.id);
    await tags.attachToTask(task2.id, tagUrgent.id);
    await tags.attachToTask(task2.id, tagBackend.id);
    await tags.attachToTask(task3.id, tagFeature.id);
    await tags.attachToTask(task3.id, tagFrontend.id);

    const task1Tags = await tags.findByTask(task1.id);
    console.log(`  Tags on "${task1.title}": ${task1Tags.map(t => t.name).join(", ")}`);
    const task2Tags = await tags.findByTask(task2.id);
    console.log(`  Tags on "${task2.title}": ${task2Tags.map(t => t.name).join(", ")}`);

    // ── 6. Add Comments ───────────────────────────────────────────────────────
    printHeader("6. Add Comments");

    await comments.add(task2.id, alice.id,  "This is critical — assign it to Carol immediately.");
    await comments.add(task2.id, carol.id,  "On it. Looks like a header injection issue.");
    await comments.add(task2.id, bob.id,    "I can help with the integration test once you have a fix.");
    await comments.add(task1.id, bob.id,    "Starting on this Monday. Need design specs first.");
    await comments.add(task1.id, alice.id,  "Figma link: https://figma.com/file/abc123");

    const task2Comments = await comments.findByTask(task2.id);
    console.log(`  Comments on "${task2.title}":`);
    task2Comments.forEach(c =>
      console.log(`    [${c.author_name}]: ${c.body}`)
    );

    // ── 7. Update Task Status ─────────────────────────────────────────────────
    printHeader("7. Update Tasks");

    const updated = await tasks.update(task2.id, {
      status: "done",
      actual_hours: 3.5,
    });
    console.log(`  "${updated!.title}" → status: ${updated!.status}, actual_hours: ${updated!.actual_hours}`);

    const reassigned = await tasks.update(task3.id, {
      assignee_id: bob.id,
      priority: "high",
    });
    console.log(`  "${reassigned!.title}" → assignee: bob, priority: ${reassigned!.priority}`);

    // ── 8. Tasks with Full Details ────────────────────────────────────────────
    printHeader("8. All Tasks with Details");

    const detailed = await tasks.findWithDetails();
    detailed.forEach(t => {
      console.log(
        `  [${t.priority.toUpperCase().padEnd(8)}] ${t.title.padEnd(35)} ` +
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

    // ── 12. Assigned Tasks per User ───────────────────────────────────────────
    printHeader("12. Bob's Open Tasks");

    const bobTasks = await tasks.findByAssignee(bob.id);
    bobTasks.forEach(t =>
      console.log(`  [${t.priority}] ${t.title} — ${t.status}`)
    );
    if (bobTasks.length === 0) console.log("  No open tasks for Bob.");

    // ── 13. Overdue Tasks ─────────────────────────────────────────────────────
    printHeader("13. Overdue Tasks");

    const overdue = await tasks.getOverdue();
    if (overdue.length === 0) {
      console.log("  No overdue tasks — great!");
    } else {
      overdue.forEach(t =>
        console.log(`  OVERDUE: "${t.title}" (due ${t.due_date}) — ${t.project_name}`)
      );
    }

    // ── 14. Cleanup ───────────────────────────────────────────────────────────
    printHeader("14. Cleanup");

    await db.execute("DELETE FROM task_tags");
    await db.execute("DELETE FROM comments");
    await db.execute("DELETE FROM tasks");
    await db.execute("DELETE FROM projects");
    await db.execute("DELETE FROM tags");
    await db.execute("DELETE FROM users");
    console.log("  All records deleted.");

    printHeader("Done — All 14 steps completed successfully!");

  } catch (err) {
    console.error("Fatal error:", err);
    process.exit(1);
  } finally {
    await db.close();
  }
}

main();
