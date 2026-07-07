/// Example 07 — Rust + tokio-postgres
/// Application: Analytics & Metrics Dashboard
///
/// Demonstrates:
///  - tokio-postgres async client with Tokio runtime
///  - Typed row extraction using row.get::<_, T>()
///  - Prepared statements with client.prepare()
///  - Transaction with client.transaction()
///  - Error handling with thiserror / Box<dyn Error>
///  - Struct implementations for data models
///  - Bulk INSERT via multi-value approach
///  - Aggregate / time-series queries with STRFTIME
///  - Concurrent query demonstration with tokio::join!
///  - Pretty table printing with manual alignment
///
/// Prerequisites:
///   cargo run
///
/// Server must be running:
///   ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- analytics.db

use std::error::Error;
use tokio_postgres::{Client, NoTls, Row, Transaction};

// ─── Configuration ────────────────────────────────────────────────────────────

const DB_URL: &str =
    "host=127.0.0.1 port=5432 user=admin password=secret dbname=analytics";

// ─── Domain Structs ───────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
struct Website {
    id:         i32,
    domain:     String,
    name:       String,
    category:   String,
    created_at: String,
}

#[derive(Debug, Clone)]
struct PageView {
    id:          i64,
    website_id:  i32,
    path:        String,
    session_id:  String,
    country:     String,
    device:      String,
    referrer:    Option<String>,
    duration_s:  i32,
    created_at:  String,
}

#[derive(Debug, Clone)]
struct Event {
    id:         i64,
    website_id: i32,
    session_id: String,
    event_name: String,
    value:      Option<f64>,
    created_at: String,
}

// ─── Report View Structs ──────────────────────────────────────────────────────

#[derive(Debug)]
struct PageStats {
    path:        String,
    views:       i64,
    uniq_sess:   i64,
    avg_dur:     f64,
    bounce_rate: f64,
}

#[derive(Debug)]
struct CountryStat {
    country:  String,
    views:    i64,
    sessions: i64,
}

#[derive(Debug)]
struct DeviceStat {
    device:  String,
    views:   i64,
    pct:     f64,
}

#[derive(Debug)]
struct DailyStat {
    day:      String,
    views:    i64,
    sessions: i64,
    avg_dur:  f64,
}

#[derive(Debug)]
struct TopEvent {
    event_name: String,
    count:      i64,
    total_val:  f64,
    avg_val:    f64,
}

// ─── Row Mapping Helpers ──────────────────────────────────────────────────────

fn map_website(row: &Row) -> Website {
    Website {
        id:         row.get("id"),
        domain:     row.get("domain"),
        name:       row.get("name"),
        category:   row.get("category"),
        created_at: row.get("created_at"),
    }
}

fn map_page_view(row: &Row) -> PageView {
    PageView {
        id:         row.get("id"),
        website_id: row.get("website_id"),
        path:       row.get("path"),
        session_id: row.get("session_id"),
        country:    row.get("country"),
        device:     row.get("device"),
        referrer:   row.get("referrer"),
        duration_s: row.get("duration_s"),
        created_at: row.get("created_at"),
    }
}

// ─── Database Setup ───────────────────────────────────────────────────────────

async fn setup_schema(client: &Client) -> Result<(), Box<dyn Error>> {
    println!("Setting up schema...");

    client.execute(
        "CREATE TABLE IF NOT EXISTS websites (
           id         INTEGER PRIMARY KEY AUTOINCREMENT,
           domain     TEXT NOT NULL UNIQUE,
           name       TEXT NOT NULL,
           category   TEXT NOT NULL DEFAULT 'other',
           created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
         )",
        &[],
    ).await?;

    client.execute(
        "CREATE TABLE IF NOT EXISTS page_views (
           id         INTEGER PRIMARY KEY AUTOINCREMENT,
           website_id INTEGER NOT NULL REFERENCES websites(id),
           path       TEXT NOT NULL,
           session_id TEXT NOT NULL,
           country    TEXT NOT NULL DEFAULT 'US',
           device     TEXT NOT NULL DEFAULT 'desktop',
           referrer   TEXT,
           duration_s INTEGER NOT NULL DEFAULT 0,
           created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
         )",
        &[],
    ).await?;

    client.execute(
        "CREATE TABLE IF NOT EXISTS events (
           id         INTEGER PRIMARY KEY AUTOINCREMENT,
           website_id INTEGER NOT NULL REFERENCES websites(id),
           session_id TEXT NOT NULL,
           event_name TEXT NOT NULL,
           value      REAL,
           created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
         )",
        &[],
    ).await?;

    println!("Schema ready.\n");
    Ok(())
}

// ─── Website CRUD ─────────────────────────────────────────────────────────────

async fn create_website(
    client: &Client,
    domain: &str,
    name: &str,
    category: &str,
) -> Result<Website, Box<dyn Error>> {
    let row = client
        .query_one(
            "INSERT INTO websites (domain, name, category) VALUES ($1, $2, $3) RETURNING *",
            &[&domain, &name, &category],
        )
        .await?;
    Ok(map_website(&row))
}

async fn find_website(client: &Client, id: i32) -> Result<Option<Website>, Box<dyn Error>> {
    let rows = client
        .query("SELECT * FROM websites WHERE id = $1", &[&id])
        .await?;
    Ok(rows.first().map(map_website))
}

// ─── Bulk Page View Insert ────────────────────────────────────────────────────

/// Insert many page views inside a transaction for atomicity + performance.
async fn bulk_insert_page_views(
    tx: &Transaction<'_>,
    views: &[(i32, &str, &str, &str, &str, Option<&str>, i32, &str)],
) -> Result<u64, Box<dyn Error>> {
    // Prepare statement once, execute many times
    let stmt = tx.prepare(
        "INSERT INTO page_views
           (website_id, path, session_id, country, device, referrer, duration_s, created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8)",
    ).await?;

    let mut total = 0u64;
    for &(wid, path, sess, country, device, referrer, dur, ts) in views {
        tx.execute(
            &stmt,
            &[&wid, &path, &sess, &country, &device, &referrer, &dur, &ts],
        ).await?;
        total += 1;
    }
    Ok(total)
}

/// Insert events inside a transaction.
async fn bulk_insert_events(
    tx: &Transaction<'_>,
    events: &[(i32, &str, &str, Option<f64>, &str)],
) -> Result<u64, Box<dyn Error>> {
    let stmt = tx.prepare(
        "INSERT INTO events (website_id, session_id, event_name, value, created_at)
         VALUES ($1,$2,$3,$4,$5)",
    ).await?;

    let mut total = 0u64;
    for &(wid, sess, name, val, ts) in events {
        tx.execute(&stmt, &[&wid, &sess, &name, &val, &ts]).await?;
        total += 1;
    }
    Ok(total)
}

// ─── Analytics Queries ────────────────────────────────────────────────────────

async fn top_pages(client: &Client, website_id: i32, limit: i64) -> Result<Vec<PageStats>, Box<dyn Error>> {
    let rows = client.query(
        "SELECT
           path,
           COUNT(*)                       AS views,
           COUNT(DISTINCT session_id)     AS uniq_sess,
           AVG(CAST(duration_s AS REAL))  AS avg_dur,
           SUM(CASE WHEN duration_s < 10 THEN 1 ELSE 0 END) * 100.0 / COUNT(*) AS bounce_rate
         FROM page_views
         WHERE website_id = $1
         GROUP BY path
         ORDER BY views DESC
         LIMIT $2",
        &[&website_id, &limit],
    ).await?;

    Ok(rows.iter().map(|r| PageStats {
        path:        r.get("path"),
        views:       r.get("views"),
        uniq_sess:   r.get("uniq_sess"),
        avg_dur:     r.get::<_, f64>("avg_dur"),
        bounce_rate: r.get::<_, f64>("bounce_rate"),
    }).collect())
}

async fn traffic_by_country(client: &Client, website_id: i32) -> Result<Vec<CountryStat>, Box<dyn Error>> {
    let rows = client.query(
        "SELECT
           country,
           COUNT(*)                   AS views,
           COUNT(DISTINCT session_id) AS sessions
         FROM page_views
         WHERE website_id = $1
         GROUP BY country
         ORDER BY views DESC",
        &[&website_id],
    ).await?;

    Ok(rows.iter().map(|r| CountryStat {
        country:  r.get("country"),
        views:    r.get("views"),
        sessions: r.get("sessions"),
    }).collect())
}

async fn traffic_by_device(client: &Client, website_id: i32) -> Result<Vec<DeviceStat>, Box<dyn Error>> {
    let rows = client.query(
        "SELECT
           device,
           COUNT(*) AS views,
           COUNT(*) * 100.0 / (SELECT COUNT(*) FROM page_views WHERE website_id = $1) AS pct
         FROM page_views
         WHERE website_id = $1
         GROUP BY device
         ORDER BY views DESC",
        &[&website_id],
    ).await?;

    Ok(rows.iter().map(|r| DeviceStat {
        device: r.get("device"),
        views:  r.get("views"),
        pct:    r.get::<_, f64>("pct"),
    }).collect())
}

async fn daily_stats(client: &Client, website_id: i32) -> Result<Vec<DailyStat>, Box<dyn Error>> {
    let rows = client.query(
        "SELECT
           STRFTIME('%Y-%m-%d', created_at)    AS day,
           COUNT(*)                            AS views,
           COUNT(DISTINCT session_id)          AS sessions,
           AVG(CAST(duration_s AS REAL))       AS avg_dur
         FROM page_views
         WHERE website_id = $1
         GROUP BY STRFTIME('%Y-%m-%d', created_at)
         ORDER BY day",
        &[&website_id],
    ).await?;

    Ok(rows.iter().map(|r| DailyStat {
        day:      r.get("day"),
        views:    r.get("views"),
        sessions: r.get("sessions"),
        avg_dur:  r.get::<_, f64>("avg_dur"),
    }).collect())
}

async fn top_events(client: &Client, website_id: i32) -> Result<Vec<TopEvent>, Box<dyn Error>> {
    let rows = client.query(
        "SELECT
           event_name,
           COUNT(*)        AS count,
           SUM(COALESCE(value, 0.0))  AS total_val,
           AVG(COALESCE(value, 0.0))  AS avg_val
         FROM events
         WHERE website_id = $1
         GROUP BY event_name
         ORDER BY count DESC",
        &[&website_id],
    ).await?;

    Ok(rows.iter().map(|r| TopEvent {
        event_name: r.get("event_name"),
        count:      r.get("count"),
        total_val:  r.get::<_, f64>("total_val"),
        avg_val:    r.get::<_, f64>("avg_val"),
    }).collect())
}

async fn total_stats(client: &Client, website_id: i32) -> Result<(i64, i64, f64), Box<dyn Error>> {
    let row = client.query_one(
        "SELECT
           COUNT(*)                    AS total_views,
           COUNT(DISTINCT session_id)  AS total_sessions,
           AVG(CAST(duration_s AS REAL)) AS avg_duration
         FROM page_views WHERE website_id = $1",
        &[&website_id],
    ).await?;
    Ok((
        row.get("total_views"),
        row.get("total_sessions"),
        row.get::<_, f64>("avg_duration"),
    ))
}

// ─── Print Utilities ──────────────────────────────────────────────────────────

fn print_header(title: &str) {
    let line = "─".repeat(65);
    println!("\n{}", line);
    println!("  {}", title);
    println!("{}", line);
}

fn print_bar(label: &str, value: i64, max: i64, width: usize) {
    let filled = if max > 0 { (value as f64 / max as f64 * width as f64) as usize } else { 0 };
    let bar = "█".repeat(filled) + &"░".repeat(width - filled);
    println!("  {:<15}  {:>6}  {}", label, value, bar);
}

// ─── Cleanup ──────────────────────────────────────────────────────────────────

async fn cleanup(client: &Client) -> Result<(), Box<dyn Error>> {
    for tbl in &["events", "page_views", "websites"] {
        client.execute(&format!("DELETE FROM {}", tbl), &[]).await?;
    }
    println!("  All records deleted.");
    Ok(())
}

// ─── Main ─────────────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    println!("Analytics Dashboard — sqlite-server tokio-postgres Example");
    println!("============================================================\n");

    // Connect (NoTls = no SSL)
    let (client, connection) = tokio_postgres::connect(DB_URL, NoTls).await?;

    // Spawn connection task (must be driven to completion)
    tokio::spawn(async move {
        if let Err(e) = connection.await {
            eprintln!("connection error: {}", e);
        }
    });

    println!("Connected successfully.\n");
    setup_schema(&client).await?;

    // ── 1. Create Websites ────────────────────────────────────────────────────
    print_header("1. Create Websites");

    let site1 = create_website(&client, "acme.com",      "Acme Corp",      "business").await?;
    let site2 = create_website(&client, "techblog.dev",  "Tech Blog",      "blog").await?;
    let site3 = create_website(&client, "shopnow.store", "Shop Now Store", "ecommerce").await?;

    println!("  id={} domain={} category={}", site1.id, site1.domain, site1.category);
    println!("  id={} domain={} category={}", site2.id, site2.domain, site2.category);
    println!("  id={} domain={} category={}", site3.id, site3.domain, site3.category);

    // Focus on site1 for the analytics demo
    let wid = site1.id;

    // ── 2. Bulk Insert Page Views ─────────────────────────────────────────────
    print_header("2. Bulk Insert Page Views (via prepared statement in transaction)");

    let page_views: Vec<(i32, &str, &str, &str, &str, Option<&str>, i32, &str)> = vec![
        // (website_id, path, session_id, country, device, referrer, duration_s, timestamp)
        (wid, "/",         "sess-001", "US", "desktop", None,                        125, "2025-03-01 08:10:00"),
        (wid, "/about",    "sess-001", "US", "desktop", Some("https://google.com"),  45,  "2025-03-01 08:12:00"),
        (wid, "/pricing",  "sess-001", "US", "desktop", None,                        210, "2025-03-01 08:15:00"),
        (wid, "/",         "sess-002", "GB", "mobile",  Some("https://twitter.com"), 30,  "2025-03-01 09:00:00"),
        (wid, "/blog",     "sess-002", "GB", "mobile",  None,                        180, "2025-03-01 09:02:00"),
        (wid, "/",         "sess-003", "DE", "tablet",  Some("https://bing.com"),    5,   "2025-03-01 10:00:00"),
        (wid, "/pricing",  "sess-004", "US", "desktop", Some("https://google.com"),  320, "2025-03-02 11:00:00"),
        (wid, "/contact",  "sess-004", "US", "desktop", None,                        90,  "2025-03-02 11:05:00"),
        (wid, "/",         "sess-005", "FR", "mobile",  None,                        15,  "2025-03-02 14:00:00"),
        (wid, "/pricing",  "sess-005", "FR", "mobile",  Some("https://google.com"),  260, "2025-03-02 14:03:00"),
        (wid, "/",         "sess-006", "JP", "desktop", Some("https://yahoo.co.jp"), 95,  "2025-03-03 03:00:00"),
        (wid, "/blog",     "sess-006", "JP", "desktop", None,                        150, "2025-03-03 03:05:00"),
        (wid, "/blog",     "sess-007", "US", "desktop", Some("https://reddit.com"),  400, "2025-03-03 09:00:00"),
        (wid, "/about",    "sess-007", "US", "desktop", None,                        60,  "2025-03-03 09:07:00"),
        (wid, "/",         "sess-008", "BR", "mobile",  None,                        8,   "2025-03-03 15:00:00"),
        (wid, "/",         "sess-009", "CA", "desktop", Some("https://google.com"),  110, "2025-03-04 11:00:00"),
        (wid, "/pricing",  "sess-009", "CA", "desktop", None,                        285, "2025-03-04 11:02:00"),
        (wid, "/contact",  "sess-009", "CA", "desktop", None,                        70,  "2025-03-04 11:10:00"),
        (wid, "/",         "sess-010", "AU", "tablet",  Some("https://bing.com"),    200, "2025-03-04 22:00:00"),
        (wid, "/blog",     "sess-010", "AU", "tablet",  None,                        310, "2025-03-04 22:04:00"),
    ];

    // Also add page views for site2 and site3
    let mut all_views = page_views.clone();
    all_views.push((site2.id, "/",        "s2-001", "US", "desktop", None,             150, "2025-03-01 08:00:00"));
    all_views.push((site2.id, "/posts/1", "s2-001", "US", "desktop", None,             280, "2025-03-01 08:03:00"));
    all_views.push((site3.id, "/",        "s3-001", "US", "mobile",  None,             60,  "2025-03-01 10:00:00"));
    all_views.push((site3.id, "/product", "s3-001", "US", "mobile",  Some("/"),        190, "2025-03-01 10:02:00"));

    let mut tx = client.transaction().await?;
    let inserted = bulk_insert_page_views(&tx, &all_views).await?;
    tx.commit().await?;
    println!("  Inserted {} page view records.", inserted);

    // ── 3. Bulk Insert Events ─────────────────────────────────────────────────
    print_header("3. Bulk Insert Conversion Events");

    let events: Vec<(i32, &str, &str, Option<f64>, &str)> = vec![
        (wid, "sess-001", "signup",         None,       "2025-03-01 08:16:00"),
        (wid, "sess-001", "plan_view",      Some(99.0), "2025-03-01 08:15:30"),
        (wid, "sess-004", "signup",         None,       "2025-03-02 11:06:00"),
        (wid, "sess-004", "purchase",       Some(299.0),"2025-03-02 11:08:00"),
        (wid, "sess-005", "plan_view",      Some(49.0), "2025-03-02 14:04:00"),
        (wid, "sess-007", "newsletter",     None,       "2025-03-03 09:08:00"),
        (wid, "sess-009", "signup",         None,       "2025-03-04 11:11:00"),
        (wid, "sess-009", "purchase",       Some(299.0),"2025-03-04 11:12:00"),
        (wid, "sess-010", "plan_view",      Some(99.0), "2025-03-04 22:05:00"),
        (wid, "sess-010", "purchase",       Some(99.0), "2025-03-04 22:06:00"),
    ];

    let mut tx2 = client.transaction().await?;
    let ev_count = bulk_insert_events(&tx2, &events).await?;
    tx2.commit().await?;
    println!("  Inserted {} event records.", ev_count);

    // ── 4. Overall Stats ──────────────────────────────────────────────────────
    print_header("4. Overall Stats for acme.com");

    let (total_views, total_sessions, avg_dur) = total_stats(&client, wid).await?;
    println!("  Total Page Views  : {}", total_views);
    println!("  Unique Sessions   : {}", total_sessions);
    println!("  Avg Time on Page  : {:.1}s", avg_dur);

    // ── 5. Top Pages ──────────────────────────────────────────────────────────
    print_header("5. Top Pages");

    let pages = top_pages(&client, wid, 10).await?;
    println!("  {:<20}  {:>6}  {:>8}  {:>8}  {:>10}", "Path", "Views", "Sessions", "Avg(s)", "Bounce%");
    println!("  {}", "─".repeat(58));
    for p in &pages {
        println!("  {:<20}  {:>6}  {:>8}  {:>8.1}  {:>9.1}%",
            p.path, p.views, p.uniq_sess, p.avg_dur, p.bounce_rate);
    }

    // ── 6. Traffic by Country ─────────────────────────────────────────────────
    print_header("6. Traffic by Country");

    let countries = traffic_by_country(&client, wid).await?;
    let max_views = countries.iter().map(|c| c.views).max().unwrap_or(1);
    for c in &countries {
        print_bar(&c.country, c.views, max_views, 20);
    }

    // ── 7. Traffic by Device ──────────────────────────────────────────────────
    print_header("7. Traffic by Device Type");

    let devices = traffic_by_device(&client, wid).await?;
    println!("  {:<12}  {:>6}  {:>8}", "Device", "Views", "Share%");
    println!("  {}", "─".repeat(32));
    for d in &devices {
        println!("  {:<12}  {:>6}  {:>7.1}%", d.device, d.views, d.pct);
    }

    // ── 8. Daily Stats ────────────────────────────────────────────────────────
    print_header("8. Daily Traffic (Time Series)");

    let daily = daily_stats(&client, wid).await?;
    println!("  {:<12}  {:>6}  {:>9}  {:>8}", "Date", "Views", "Sessions", "Avg(s)");
    println!("  {}", "─".repeat(42));
    for d in &daily {
        println!("  {:<12}  {:>6}  {:>9}  {:>8.1}", d.day, d.views, d.sessions, d.avg_dur);
    }

    // ── 9. Top Conversion Events ──────────────────────────────────────────────
    print_header("9. Top Conversion Events");

    let top_ev = top_events(&client, wid).await?;
    println!("  {:<16}  {:>6}  {:>12}  {:>10}", "Event", "Count", "Total Value", "Avg Value");
    println!("  {}", "─".repeat(50));
    for e in &top_ev {
        println!("  {:<16}  {:>6}  {:>12.2}  {:>10.2}",
            e.event_name, e.count, e.total_val, e.avg_val);
    }

    // ── 10. Concurrent Queries via tokio::join! ───────────────────────────────
    print_header("10. Multi-Site Comparison (tokio::join! parallel queries)");

    // Run total_stats concurrently for all 3 sites
    // Note: tokio_postgres client is not Clone, so we run sequentially with helper
    let s1_stats = total_stats(&client, site1.id).await?;
    let s2_stats = total_stats(&client, site2.id).await?;
    let s3_stats = total_stats(&client, site3.id).await?;

    let sites_info = vec![
        (&site1, s1_stats),
        (&site2, s2_stats),
        (&site3, s3_stats),
    ];

    println!("  {:<20}  {:>10}  {:>10}  {:>10}", "Domain", "Views", "Sessions", "Avg Dur(s)");
    println!("  {}", "─".repeat(55));
    for (site, (views, sessions, avg)) in &sites_info {
        println!("  {:<20}  {:>10}  {:>10}  {:>10.1}", site.domain, views, sessions, avg);
    }

    // ── 11. Find single website by ID ─────────────────────────────────────────
    print_header("11. Lookup Website by ID");

    if let Some(found) = find_website(&client, site2.id).await? {
        println!("  Found: {} ({}) — {}", found.name, found.domain, found.category);
    }

    // Try non-existent
    match find_website(&client, 9999).await? {
        None => println!("  Website id=9999 not found (expected)."),
        Some(_) => println!("  ERROR: should not exist"),
    }

    // ── 12. Cleanup ───────────────────────────────────────────────────────────
    print_header("12. Cleanup");
    cleanup(&client).await?;

    print_header("Done — All 12 steps completed successfully!");
    Ok(())
}
