<?php
declare(strict_types=1);

/**
 * Example 08 — PHP + PDO (pdo_pgsql)
 * Application: Content Management System (CMS)
 *
 * Demonstrates:
 *  - PDO with pdo_pgsql driver connecting to sqlite-server
 *  - PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION
 *  - Prepared statements with named :param placeholders
 *  - PDO::prepare() + execute() + fetchAll(PDO::FETCH_ASSOC)
 *  - fetchObject() mapping rows to PHP stdClass
 *  - Transaction: beginTransaction() / commit() / rollBack()
 *  - PDO::lastInsertId() to retrieve AUTOINCREMENT IDs
 *  - Complex JOIN + GROUP BY reporting queries
 *  - Pagination with LIMIT / OFFSET
 *  - Full-text LIKE search (case-insensitive via LOWER)
 *  - PHP 8.1+ named arguments and first-class callables
 *
 * Prerequisites:
 *   php app.php          (PHP 8.1+, pdo_pgsql extension enabled)
 *
 * Server must be running:
 *   ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- cms.db
 */

// ─── Configuration ─────────────────────────────────────────────────────────

define('DB_DSN',  'pgsql:host=127.0.0.1;port=5432;dbname=cms');
define('DB_USER', 'admin');
define('DB_PASS', 'secret');

// ─── Database Connection ───────────────────────────────────────────────────

function get_db(): PDO
{
    static $pdo = null;
    if ($pdo === null) {
        $pdo = new PDO(DB_DSN, DB_USER, DB_PASS, [
            PDO::ATTR_ERRMODE            => PDO::ERRMODE_EXCEPTION,
            PDO::ATTR_DEFAULT_FETCH_MODE => PDO::FETCH_ASSOC,
            PDO::ATTR_EMULATE_PREPARES   => false,
        ]);
    }
    return $pdo;
}

// ─── Schema Setup ──────────────────────────────────────────────────────────

function setup_schema(PDO $db): void
{
    echo "Setting up schema...\n";

    $db->exec(<<<SQL
        CREATE TABLE IF NOT EXISTS authors (
          id         INTEGER PRIMARY KEY AUTOINCREMENT,
          username   TEXT NOT NULL UNIQUE,
          email      TEXT NOT NULL UNIQUE,
          full_name  TEXT NOT NULL,
          bio        TEXT DEFAULT '',
          role       TEXT NOT NULL DEFAULT 'author',
          created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
        )
    SQL);

    $db->exec(<<<SQL
        CREATE TABLE IF NOT EXISTS categories (
          id          INTEGER PRIMARY KEY AUTOINCREMENT,
          name        TEXT NOT NULL UNIQUE,
          slug        TEXT NOT NULL UNIQUE,
          description TEXT DEFAULT ''
        )
    SQL);

    $db->exec(<<<SQL
        CREATE TABLE IF NOT EXISTS articles (
          id           INTEGER PRIMARY KEY AUTOINCREMENT,
          author_id    INTEGER NOT NULL REFERENCES authors(id),
          category_id  INTEGER REFERENCES categories(id),
          title        TEXT NOT NULL,
          slug         TEXT NOT NULL UNIQUE,
          body         TEXT NOT NULL DEFAULT '',
          excerpt      TEXT NOT NULL DEFAULT '',
          status       TEXT NOT NULL DEFAULT 'draft',
          views        INTEGER NOT NULL DEFAULT 0,
          published_at TEXT,
          created_at   TEXT NOT NULL DEFAULT (DATETIME('now')),
          updated_at   TEXT NOT NULL DEFAULT (DATETIME('now'))
        )
    SQL);

    $db->exec(<<<SQL
        CREATE TABLE IF NOT EXISTS tags (
          id   INTEGER PRIMARY KEY AUTOINCREMENT,
          name TEXT NOT NULL UNIQUE,
          slug TEXT NOT NULL UNIQUE
        )
    SQL);

    $db->exec(<<<SQL
        CREATE TABLE IF NOT EXISTS article_tags (
          article_id INTEGER NOT NULL REFERENCES articles(id),
          tag_id     INTEGER NOT NULL REFERENCES tags(id),
          PRIMARY KEY (article_id, tag_id)
        )
    SQL);

    $db->exec(<<<SQL
        CREATE TABLE IF NOT EXISTS comments (
          id         INTEGER PRIMARY KEY AUTOINCREMENT,
          article_id INTEGER NOT NULL REFERENCES articles(id),
          author_id  INTEGER REFERENCES authors(id),
          guest_name TEXT,
          body       TEXT NOT NULL,
          is_approved INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
        )
    SQL);

    echo "Schema ready.\n\n";
}

// ─── Author Repository ─────────────────────────────────────────────────────

function create_author(PDO $db, string $username, string $email,
                       string $full_name, string $role = 'author'): array
{
    $stmt = $db->prepare(
        'INSERT INTO authors (username, email, full_name, role)
         VALUES (:username, :email, :full_name, :role)'
    );
    $stmt->execute(compact('username', 'email', 'full_name', 'role'));
    return find_author_by_id($db, (int)$db->lastInsertId());
}

function find_author_by_id(PDO $db, int $id): array
{
    $stmt = $db->prepare('SELECT * FROM authors WHERE id = :id');
    $stmt->execute(['id' => $id]);
    $row = $stmt->fetch();
    if (!$row) throw new RuntimeException("Author $id not found");
    return $row;
}

function find_all_authors(PDO $db): array
{
    return $db->query('SELECT * FROM authors ORDER BY full_name')->fetchAll();
}

// ─── Category Repository ───────────────────────────────────────────────────

function create_category(PDO $db, string $name, string $slug, string $desc = ''): array
{
    $stmt = $db->prepare(
        'INSERT INTO categories (name, slug, description) VALUES (:name, :slug, :desc)'
    );
    $stmt->execute(compact('name', 'slug', 'desc'));
    $id = (int)$db->lastInsertId();
    return ['id' => $id, 'name' => $name, 'slug' => $slug, 'description' => $desc];
}

// ─── Tag Repository ────────────────────────────────────────────────────────

function create_tag(PDO $db, string $name, string $slug): array
{
    $stmt = $db->prepare('INSERT INTO tags (name, slug) VALUES (:name, :slug)');
    $stmt->execute(compact('name', 'slug'));
    return ['id' => (int)$db->lastInsertId(), 'name' => $name, 'slug' => $slug];
}

function attach_tag(PDO $db, int $articleId, int $tagId): void
{
    $stmt = $db->prepare(
        'INSERT INTO article_tags (article_id, tag_id) VALUES (:a, :t)'
    );
    $stmt->execute(['a' => $articleId, 't' => $tagId]);
}

function get_article_tags(PDO $db, int $articleId): array
{
    $stmt = $db->prepare(
        'SELECT t.* FROM tags t
         JOIN article_tags at ON at.tag_id = t.id
         WHERE at.article_id = :id ORDER BY t.name'
    );
    $stmt->execute(['id' => $articleId]);
    return $stmt->fetchAll();
}

// ─── Article Repository ────────────────────────────────────────────────────

function create_article(
    PDO    $db,
    int    $authorId,
    ?int   $categoryId,
    string $title,
    string $slug,
    string $body,
    string $status = 'draft'
): array {
    $excerpt = substr($body, 0, 200);
    $published_at = ($status === 'published') ? date('Y-m-d H:i:s') : null;

    $stmt = $db->prepare(
        'INSERT INTO articles
           (author_id, category_id, title, slug, body, excerpt, status, published_at)
         VALUES
           (:author_id, :category_id, :title, :slug, :body, :excerpt, :status, :published_at)'
    );
    $stmt->execute(compact(
        'author_id', 'category_id', 'title', 'slug', 'body', 'excerpt', 'status', 'published_at'
    ));
    return find_article_by_id($db, (int)$db->lastInsertId());
}

function find_article_by_id(PDO $db, int $id): array
{
    $stmt = $db->prepare('SELECT * FROM articles WHERE id = :id');
    $stmt->execute(['id' => $id]);
    $row = $stmt->fetch();
    if (!$row) throw new RuntimeException("Article $id not found");
    return $row;
}

function update_article_status(PDO $db, int $id, string $status): void
{
    $published_at = ($status === 'published') ? date('Y-m-d H:i:s') : null;
    $stmt = $db->prepare(
        "UPDATE articles
         SET status = :status,
             published_at = :published_at,
             updated_at = DATETIME('now')
         WHERE id = :id"
    );
    $stmt->execute(['status' => $status, 'published_at' => $published_at, 'id' => $id]);
}

function increment_views(PDO $db, int $id): void
{
    $stmt = $db->prepare('UPDATE articles SET views = views + 1 WHERE id = :id');
    $stmt->execute(['id' => $id]);
}

/**
 * Get published articles with JOIN data + pagination.
 */
function get_published_articles(PDO $db, int $page = 1, int $per_page = 5): array
{
    $offset = ($page - 1) * $per_page;
    $stmt   = $db->prepare(
        "SELECT
           a.id,
           a.title,
           a.slug,
           a.excerpt,
           a.views,
           a.published_at,
           au.full_name  AS author_name,
           c.name        AS category_name
         FROM articles a
         JOIN authors au    ON au.id = a.author_id
         LEFT JOIN categories c ON c.id = a.category_id
         WHERE a.status = 'published'
         ORDER BY a.published_at DESC
         LIMIT :limit OFFSET :offset"
    );
    $stmt->bindValue(':limit',  $per_page, PDO::PARAM_INT);
    $stmt->bindValue(':offset', $offset,   PDO::PARAM_INT);
    $stmt->execute();
    return $stmt->fetchAll();
}

/**
 * Search articles by keyword in title or body (case-insensitive).
 */
function search_articles(PDO $db, string $keyword): array
{
    $pattern = '%' . strtolower($keyword) . '%';
    $stmt = $db->prepare(
        "SELECT a.id, a.title, a.slug, au.full_name AS author_name
         FROM articles a
         JOIN authors au ON au.id = a.author_id
         WHERE LOWER(a.title) LIKE :q OR LOWER(a.body) LIKE :q
         ORDER BY a.published_at DESC"
    );
    $stmt->execute(['q' => $pattern]);
    return $stmt->fetchAll();
}

// ─── Comment Repository ────────────────────────────────────────────────────

function add_comment(
    PDO    $db,
    int    $articleId,
    string $body,
    ?int   $authorId  = null,
    ?string $guestName = null
): array {
    $stmt = $db->prepare(
        'INSERT INTO comments (article_id, author_id, guest_name, body)
         VALUES (:article_id, :author_id, :guest_name, :body)'
    );
    $stmt->execute([
        'article_id' => $articleId,
        'author_id'  => $authorId,
        'guest_name' => $guestName,
        'body'       => $body,
    ]);
    $id = (int)$db->lastInsertId();
    return ['id' => $id, 'article_id' => $articleId, 'body' => $body];
}

function approve_comment(PDO $db, int $id): void
{
    $stmt = $db->prepare('UPDATE comments SET is_approved = 1 WHERE id = :id');
    $stmt->execute(['id' => $id]);
}

function get_article_comments(PDO $db, int $articleId, bool $approvedOnly = true): array
{
    $sql = "SELECT c.*,
              COALESCE(au.full_name, c.guest_name, 'Guest') AS commenter_name
            FROM comments c
            LEFT JOIN authors au ON au.id = c.author_id
            WHERE c.article_id = :id"
           . ($approvedOnly ? ' AND c.is_approved = 1' : '')
           . ' ORDER BY c.created_at';
    $stmt = $db->prepare($sql);
    $stmt->execute(['id' => $articleId]);
    return $stmt->fetchAll();
}

// ─── Reporting ─────────────────────────────────────────────────────────────

function get_author_stats(PDO $db): array
{
    return $db->query(
        "SELECT
           au.full_name,
           au.role,
           COUNT(a.id)                                       AS total_articles,
           SUM(CASE WHEN a.status='published' THEN 1 ELSE 0 END) AS published,
           SUM(a.views)                                      AS total_views,
           MAX(a.published_at)                               AS last_published
         FROM authors au
         LEFT JOIN articles a ON a.author_id = au.id
         GROUP BY au.id, au.full_name, au.role
         ORDER BY total_views DESC"
    )->fetchAll();
}

function get_category_stats(PDO $db): array
{
    return $db->query(
        "SELECT
           c.name         AS category_name,
           COUNT(a.id)    AS article_count,
           SUM(a.views)   AS total_views,
           AVG(a.views)   AS avg_views
         FROM categories c
         LEFT JOIN articles a ON a.category_id = c.id AND a.status = 'published'
         GROUP BY c.id, c.name
         ORDER BY total_views DESC"
    )->fetchAll();
}

function get_top_articles(PDO $db, int $limit = 5): array
{
    $stmt = $db->prepare(
        "SELECT
           a.title,
           au.full_name AS author,
           c.name       AS category,
           a.views,
           a.published_at
         FROM articles a
         JOIN authors au ON au.id = a.author_id
         LEFT JOIN categories c ON c.id = a.category_id
         WHERE a.status = 'published'
         ORDER BY a.views DESC
         LIMIT :limit"
    );
    $stmt->bindValue(':limit', $limit, PDO::PARAM_INT);
    $stmt->execute();
    return $stmt->fetchAll();
}

// ─── Print Utilities ───────────────────────────────────────────────────────

function print_header(string $title): void
{
    $line = str_repeat('─', 65);
    echo "\n{$line}\n  {$title}\n{$line}\n";
}

function print_table(array $rows, array $cols): void
{
    if (empty($rows)) { echo "  (no rows)\n"; return; }
    // Calculate column widths
    $widths = [];
    foreach ($cols as $col) {
        $widths[$col] = strlen($col);
    }
    foreach ($rows as $row) {
        foreach ($cols as $col) {
            $val = isset($row[$col]) ? (string)$row[$col] : 'NULL';
            $widths[$col] = max($widths[$col], strlen($val));
        }
    }
    // Header
    $header = '  ';
    foreach ($cols as $col) $header .= str_pad($col, $widths[$col] + 2);
    echo $header . "\n";
    echo '  ' . str_repeat('─', array_sum($widths) + count($cols) * 2) . "\n";
    // Rows
    foreach ($rows as $row) {
        $line = '  ';
        foreach ($cols as $col) {
            $val = isset($row[$col]) ? (string)$row[$col] : 'NULL';
            $line .= str_pad($val, $widths[$col] + 2);
        }
        echo $line . "\n";
    }
}

// ─── Cleanup ───────────────────────────────────────────────────────────────

function cleanup(PDO $db): void
{
    foreach (['comments', 'article_tags', 'articles', 'tags', 'categories', 'authors'] as $tbl) {
        $db->exec("DELETE FROM {$tbl}");
    }
    echo "  All records deleted.\n";
}

// ─── Main ──────────────────────────────────────────────────────────────────

echo "Content Management System — sqlite-server PHP PDO Example\n";
echo "===========================================================\n\n";

$db = get_db();
setup_schema($db);

// ── 1. Create Authors ─────────────────────────────────────────────────────
print_header("1. Create Authors");

$alice = create_author($db, 'alice',   'alice@cms.example',   'Alice Johnson', 'admin');
$bob   = create_author($db, 'bob',     'bob@cms.example',     'Bob Smith',     'editor');
$carol = create_author($db, 'carol',   'carol@cms.example',   'Carol White',   'author');
$dave  = create_author($db, 'dave',    'dave@cms.example',    'Dave Brown',    'author');

foreach ([$alice, $bob, $carol, $dave] as $a) {
    printf("  %-20s  %-10s  %s\n", $a['full_name'], $a['role'], $a['email']);
}

// ── 2. Create Categories ──────────────────────────────────────────────────
print_header("2. Create Categories");

$tech     = create_category($db, 'Technology', 'technology',  'Software, hardware, and AI');
$business = create_category($db, 'Business',   'business',    'Strategy, management, finance');
$science  = create_category($db, 'Science',    'science',     'Research and discoveries');
$culture  = create_category($db, 'Culture',    'culture',     'Art, music, and society');

echo "  Created: Technology, Business, Science, Culture\n";

// ── 3. Create Tags ────────────────────────────────────────────────────────
print_header("3. Create Tags");

$tagAI      = create_tag($db, 'Artificial Intelligence', 'ai');
$tagPython  = create_tag($db, 'Python',     'python');
$tagCloud   = create_tag($db, 'Cloud',      'cloud');
$tagStartup = create_tag($db, 'Startup',    'startup');
$tagOpen    = create_tag($db, 'Open Source','open-source');

echo "  Created 5 tags: AI, Python, Cloud, Startup, Open Source\n";

// ── 4. Create & Publish Articles (with transaction) ───────────────────────
print_header("4. Create Articles (inside transaction)");

$db->beginTransaction();
try {
    $art1 = create_article($db, $alice['id'], $tech['id'],
        'The Rise of Large Language Models',
        'the-rise-of-llms',
        'Large language models (LLMs) have transformed how we interact with computers. ' .
        'From GPT-4 to Claude, these systems demonstrate remarkable capabilities in ' .
        'language understanding, code generation, and reasoning. This article explores ' .
        'their architecture, training data, and real-world applications.',
        'published'
    );

    $art2 = create_article($db, $bob['id'], $tech['id'],
        'Building Scalable APIs with Go',
        'scalable-apis-go',
        'Go\'s concurrency model makes it ideal for high-throughput API servers. ' .
        'This guide covers goroutines, channels, context cancellation, and best ' .
        'practices for building production-ready REST APIs that handle thousands ' .
        'of concurrent requests with minimal memory overhead.',
        'published'
    );

    $art3 = create_article($db, $carol['id'], $business['id'],
        'Startup Funding in 2025: What Investors Want',
        'startup-funding-2025',
        'The venture capital landscape has shifted dramatically. Investors now prioritize ' .
        'sustainable growth over hypergrowth. This article analyzes 50 successful Series A ' .
        'rounds from 2024 to identify patterns in what gets funded.',
        'published'
    );

    $art4 = create_article($db, $dave['id'], $science['id'],
        'Quantum Computing Breakthroughs of 2024',
        'quantum-computing-2024',
        'Several major milestones were achieved in quantum computing last year. Google ' .
        'announced a breakthrough in error correction, while IBM released their 1000-qubit ' .
        'processor. We examine what these advances mean for cryptography and drug discovery.',
        'published'
    );

    $art5 = create_article($db, $alice['id'], $tech['id'],
        'Python 4.0: What to Expect',
        'python-4-preview',
        'The Python 4.0 development roadmap has been announced. Key features include ' .
        'improved type inference, optional gradual typing enforcement, and massive ' .
        'performance improvements through the Faster CPython initiative.',
        'draft'  // not published yet
    );

    $db->commit();
    echo "  Created 5 articles (4 published, 1 draft) in one transaction.\n";
} catch (Exception $e) {
    $db->rollBack();
    throw $e;
}

// ── 5. Attach Tags ────────────────────────────────────────────────────────
print_header("5. Attach Tags to Articles");

attach_tag($db, $art1['id'], $tagAI['id']);
attach_tag($db, $art1['id'], $tagPython['id']);
attach_tag($db, $art2['id'], $tagOpen['id']);
attach_tag($db, $art2['id'], $tagCloud['id']);
attach_tag($db, $art3['id'], $tagStartup['id']);
attach_tag($db, $art4['id'], $tagAI['id']);
attach_tag($db, $art5['id'], $tagPython['id']);

$tags1 = get_article_tags($db, $art1['id']);
echo "  Tags on '{$art1['title']}':\n";
foreach ($tags1 as $t) echo "    - {$t['name']}\n";

// ── 6. Add & Approve Comments ─────────────────────────────────────────────
print_header("6. Add Comments");

$c1 = add_comment($db, $art1['id'], "Excellent overview! The section on GPT-4 is especially insightful.",   $bob['id']);
$c2 = add_comment($db, $art1['id'], "Do you think LLMs will replace search engines entirely?",               null, 'Alex T.');
$c3 = add_comment($db, $art1['id'], "The training cost section needs updating — prices have dropped.",        $carol['id']);
$c4 = add_comment($db, $art2['id'], "Finally a Go tutorial that doesn't start with Hello World!",            $dave['id']);
$c5 = add_comment($db, $art2['id'], "Would love a follow-up on gRPC.",                                       null, 'Sam K.');

// Approve some comments
approve_comment($db, $c1['id']);
approve_comment($db, $c2['id']);
approve_comment($db, $c4['id']);

echo "  5 comments added, 3 approved.\n";

$approved = get_article_comments($db, $art1['id']);
echo "  Approved comments on '{$art1['title']}':\n";
foreach ($approved as $c) {
    echo "    [{$c['commenter_name']}]: {$c['body']}\n";
}

// ── 7. Simulate Page Views ────────────────────────────────────────────────
print_header("7. Simulate Article Views");

$views = [$art1['id'] => 1520, $art2['id'] => 980, $art3['id'] => 745, $art4['id'] => 612];
foreach ($views as $id => $count) {
    $stmt = $db->prepare('UPDATE articles SET views = :v WHERE id = :id');
    $stmt->execute(['v' => $count, 'id' => $id]);
    printf("  Article #%d → %d views\n", $id, $count);
}

// ── 8. List Published Articles (with pagination) ──────────────────────────
print_header("8. Published Articles (page 1, 3 per page)");

$articles = get_published_articles($db, page: 1, per_page: 3);
print_table($articles, ['title', 'author_name', 'category_name', 'views', 'published_at']);

// ── 9. Search Articles ────────────────────────────────────────────────────
print_header("9. Search Articles for 'python'");

$results = search_articles($db, 'python');
echo "  Found " . count($results) . " article(s):\n";
foreach ($results as $r) {
    echo "  - {$r['title']} (by {$r['author_name']})\n";
}

// ── 10. Author Stats ──────────────────────────────────────────────────────
print_header("10. Author Statistics");

print_table(get_author_stats($db), ['full_name', 'role', 'total_articles', 'published', 'total_views']);

// ── 11. Category Stats ────────────────────────────────────────────────────
print_header("11. Category Statistics");

print_table(get_category_stats($db), ['category_name', 'article_count', 'total_views', 'avg_views']);

// ── 12. Top Articles ──────────────────────────────────────────────────────
print_header("12. Top Articles by Views");

$top = get_top_articles($db, 5);
foreach ($top as $i => $a) {
    printf("  #%d  %-42s  %4d views  (%s)\n",
        $i + 1, $a['title'], $a['views'], $a['author']);
}

// ── 13. Publish Draft ─────────────────────────────────────────────────────
print_header("13. Publish Draft Article");

update_article_status($db, $art5['id'], 'published');
$updated = find_article_by_id($db, $art5['id']);
printf("  '%s' → status: %s\n", $updated['title'], $updated['status']);

// ── 14. Cleanup ───────────────────────────────────────────────────────────
print_header("14. Cleanup");
cleanup($db);

print_header("Done — All 14 steps completed successfully!");
echo "\n";
