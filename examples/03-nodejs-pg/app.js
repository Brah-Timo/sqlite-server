/**
 * Example 03 — Node.js (pg / node-postgres)
 *
 * A blog platform backend demonstrating:
 *   - Connection pooling
 *   - Parameterised queries
 *   - Transactions
 *   - Prepared statements
 *   - JOIN queries
 *   - async/await patterns
 *
 * Prerequisites:
 *   npm install pg
 *
 * Run sqlite-server first:
 *   ./sqlite-server --no-auth -- /tmp/demo.db
 *
 * Then run:
 *   node app.js
 */

'use strict';

const { Pool } = require('pg');

// ── Connection pool ────────────────────────────────────────────────────────────
const pool = new Pool({
  host:     'localhost',
  port:     5432,
  user:     'test',
  password: 'test',
  database: 'test',
  max:      10,
  idleTimeoutMillis:    30_000,
  connectionTimeoutMillis: 5_000,
});

pool.on('error', (err) => {
  console.error('Unexpected pool error:', err.message);
});

// ── Schema ─────────────────────────────────────────────────────────────────────
const SCHEMA_SQL = `
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS authors;

CREATE TABLE authors (
  id         SERIAL PRIMARY KEY,
  username   TEXT NOT NULL UNIQUE,
  email      TEXT NOT NULL UNIQUE,
  bio        TEXT,
  created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE tags (
  id   SERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE posts (
  id          SERIAL PRIMARY KEY,
  author_id   INTEGER NOT NULL REFERENCES authors(id),
  title       TEXT    NOT NULL,
  content     TEXT    NOT NULL,
  published   BOOLEAN NOT NULL DEFAULT FALSE,
  views       INTEGER NOT NULL DEFAULT 0,
  created_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE comments (
  id         SERIAL PRIMARY KEY,
  post_id    INTEGER NOT NULL REFERENCES posts(id),
  author_id  INTEGER NOT NULL REFERENCES authors(id),
  body       TEXT    NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);
`;

// ── Repository: Authors ────────────────────────────────────────────────────────
const AuthorRepo = {
  async create(client, { username, email, bio }) {
    const { rows } = await client.query(
      `INSERT INTO authors (username, email, bio)
       VALUES ($1, $2, $3) RETURNING id, username, email`,
      [username, email, bio]
    );
    return rows[0];
  },

  async findByUsername(client, username) {
    const { rows } = await client.query(
      'SELECT * FROM authors WHERE username = $1',
      [username]
    );
    return rows[0] || null;
  },

  async listAll(client) {
    const { rows } = await client.query('SELECT * FROM authors ORDER BY id');
    return rows;
  },
};

// ── Repository: Posts ──────────────────────────────────────────────────────────
const PostRepo = {
  async create(client, { authorId, title, content, published = false }) {
    const { rows } = await client.query(
      `INSERT INTO posts (author_id, title, content, published)
       VALUES ($1, $2, $3, $4) RETURNING id, title`,
      [authorId, title, content, published]
    );
    return rows[0];
  },

  async publish(client, postId) {
    const { rowCount } = await client.query(
      'UPDATE posts SET published = TRUE WHERE id = $1',
      [postId]
    );
    return rowCount;
  },

  async incrementViews(client, postId) {
    await client.query(
      'UPDATE posts SET views = views + 1 WHERE id = $1',
      [postId]
    );
  },

  async findPublished(client) {
    const { rows } = await client.query(`
      SELECT p.id, p.title, p.views, p.created_at,
             a.username AS author
      FROM posts p
      JOIN authors a ON a.id = p.author_id
      WHERE p.published = TRUE
      ORDER BY p.created_at DESC
    `);
    return rows;
  },

  async search(client, keyword) {
    const { rows } = await client.query(`
      SELECT p.id, p.title, a.username AS author
      FROM posts p
      JOIN authors a ON a.id = p.author_id
      WHERE p.title ILIKE $1 OR p.content ILIKE $1
      ORDER BY p.id
    `, [`%${keyword}%`]);
    return rows;
  },

  async stats(client) {
    const { rows } = await client.query(`
      SELECT a.username,
             COUNT(p.id)   AS total_posts,
             SUM(p.views)  AS total_views,
             SUM(CASE WHEN p.published THEN 1 ELSE 0 END) AS published_posts
      FROM authors a
      LEFT JOIN posts p ON p.author_id = a.id
      GROUP BY a.id, a.username
      ORDER BY total_posts DESC
    `);
    return rows;
  },
};

// ── Repository: Comments ───────────────────────────────────────────────────────
const CommentRepo = {
  async create(client, { postId, authorId, body }) {
    const { rows } = await client.query(
      `INSERT INTO comments (post_id, author_id, body)
       VALUES ($1, $2, $3) RETURNING id`,
      [postId, authorId, body]
    );
    return rows[0];
  },

  async forPost(client, postId) {
    const { rows } = await client.query(`
      SELECT c.id, c.body, c.created_at, a.username
      FROM comments c
      JOIN authors a ON a.id = c.author_id
      WHERE c.post_id = $1
      ORDER BY c.created_at
    `, [postId]);
    return rows;
  },
};

// ── Helpers ────────────────────────────────────────────────────────────────────
function printTable(title, rows) {
  if (!rows || rows.length === 0) {
    console.log(`\n── ${title}: (empty)`);
    return;
  }
  console.log(`\n── ${title} (${rows.length} rows) ${'─'.repeat(30)}`);
  const keys = Object.keys(rows[0]);
  const header = keys.map(k => k.padEnd(20)).join('  ');
  console.log('  ' + header);
  console.log('  ' + '─'.repeat(header.length));
  for (const row of rows) {
    const line = keys.map(k => String(row[k] ?? 'NULL').padEnd(20)).join('  ');
    console.log('  ' + line);
  }
}

// ── Main ───────────────────────────────────────────────────────────────────────
async function main() {
  console.log('='.repeat(60));
  console.log('  Example 03 — Node.js pg + sqlite-server');
  console.log('='.repeat(60));

  // Test connection
  try {
    const { rows } = await pool.query('SELECT version()');
    console.log(`✓ Connected  server=${JSON.stringify(rows[0].version)}`);
  } catch (err) {
    console.error(`✗ Cannot connect: ${err.message}`);
    console.error('  Start the server: ./sqlite-server --no-auth -- /tmp/demo.db');
    process.exit(1);
  }

  const client = await pool.connect();
  try {
    // ── Setup ────────────────────────────────────────────────────────────────
    await client.query(SCHEMA_SQL);
    console.log('✓ Schema created\n');

    // ── Seed authors ─────────────────────────────────────────────────────────
    const alice = await AuthorRepo.create(client, {
      username: 'alice_writes',
      email:    'alice@blog.com',
      bio:      'Full-stack developer and tech blogger.',
    });
    const bob = await AuthorRepo.create(client, {
      username: 'bob_codes',
      email:    'bob@blog.com',
      bio:      'Go enthusiast and open-source contributor.',
    });
    const carol = await AuthorRepo.create(client, {
      username: 'carol_dev',
      email:    'carol@blog.com',
      bio:      'Python developer specialising in data engineering.',
    });
    console.log(`✓ Created authors: alice(${alice.id}), bob(${bob.id}), carol(${carol.id})`);

    // ── Seed posts (inside transaction) ──────────────────────────────────────
    await client.query('BEGIN');
    const postsData = [
      { authorId: alice.id, title: 'Getting Started with Go',
        content:  'Go is a statically typed compiled language...', published: true  },
      { authorId: alice.id, title: 'PostgreSQL Wire Protocol Deep Dive',
        content:  'The PG wire protocol is a binary TCP protocol...', published: true  },
      { authorId: alice.id, title: 'Draft: Advanced Generics',
        content:  'Go 1.18 introduced generics...', published: false },
      { authorId: bob.id,   title: 'SQLite Internals',
        content:  'SQLite stores data in a B-tree structure...', published: true  },
      { authorId: bob.id,   title: 'Building CLI Tools with Cobra',
        content:  'Cobra is a popular Go CLI framework...', published: true  },
      { authorId: carol.id, title: 'Python Async Patterns',
        content:  'asyncio provides the foundation for async Python...', published: true  },
    ];

    const posts = [];
    for (const pd of postsData) {
      const post = await PostRepo.create(client, pd);
      posts.push(post);
    }
    await client.query('COMMIT');
    console.log(`✓ Created ${posts.length} posts`);

    // ── Simulate views ────────────────────────────────────────────────────────
    const viewCounts = [45, 120, 0, 88, 55, 202];
    for (let i = 0; i < posts.length; i++) {
      for (let v = 0; v < viewCounts[i]; v++) {
        await PostRepo.incrementViews(client, posts[i].id);
      }
    }
    console.log('✓ Simulated post views');

    // ── Add comments ──────────────────────────────────────────────────────────
    await CommentRepo.create(client, {
      postId:   posts[0].id,  // Getting Started with Go
      authorId: bob.id,
      body:     'Great intro! I especially liked the goroutine explanation.',
    });
    await CommentRepo.create(client, {
      postId:   posts[0].id,
      authorId: carol.id,
      body:     'Could you also cover error handling patterns?',
    });
    await CommentRepo.create(client, {
      postId:   posts[3].id,  // SQLite Internals
      authorId: alice.id,
      body:     'Excellent deep dive into the B-tree structure!',
    });
    console.log('✓ Added comments');

    // ── Query: published posts ────────────────────────────────────────────────
    const published = await PostRepo.findPublished(client);
    printTable('Published Posts', published);

    // ── Query: search ─────────────────────────────────────────────────────────
    const searchResults = await PostRepo.search(client, 'go');
    printTable('Search "go"', searchResults);

    // ── Query: comments on first post ─────────────────────────────────────────
    const comments = await CommentRepo.forPost(client, posts[0].id);
    printTable(`Comments on post #${posts[0].id}`, comments);

    // ── Query: author stats ────────────────────────────────────────────────────
    const stats = await PostRepo.stats(client);
    printTable('Author Statistics', stats);

    // ── Cleanup ───────────────────────────────────────────────────────────────
    await client.query('DROP TABLE IF EXISTS comments, posts, tags, authors');
    console.log('\n✓ Cleanup done');

  } finally {
    client.release();
    await pool.end();
    console.log('✓ Connection pool closed\n✓ All done!');
  }
}

main().catch((err) => {
  console.error('Fatal:', err.message);
  process.exit(1);
});
