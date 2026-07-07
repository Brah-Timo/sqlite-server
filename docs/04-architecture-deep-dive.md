# البنية الداخلية التفصيلية — Architecture Deep Dive

---

## 1. pgproto — حزمة الأوراق (Leaf Package)

**الملف**: `internal/pgproto/types.go`

هذه الحزمة هي **الحل الأساسي لمشكلة Import Cycle**.

### مشكلة Import Cycle (قبل pgproto)

```
wire ──→ pool ──→ engine ──→ catalog ──→ wire   ❌ دائري!
```

كان `wire` يستورد `pool` لإدارة الاتصالات، و`pool` يستورد `engine` لتنفيذ الاستعلامات،  
و`engine` يستورد `catalog` للكتالوج الافتراضي، و`catalog` يستورد `wire` للأنواع المشتركة.

### الحل: pgproto كحزمة ورقة

```
wire    ─┐
engine  ─┼──→ pgproto   ✅ لا توجد حلقات!
catalog ─┤
pool    ─┘
```

`pgproto` لا تستورد أي حزمة داخلية — تستورد فقط `"strings"` من المكتبة القياسية.

### المحتويات

```go
// ثوابت OID — معرّفات أنواع PostgreSQL القياسية
const (
    OIDBool        uint32 = 16    // boolean
    OIDInt4        uint32 = 23    // integer
    OIDText        uint32 = 25    // text
    OIDFloat8      uint32 = 701   // double precision
    OIDTimestamp   uint32 = 1114  // timestamp
    OIDUUID        uint32 = 2950  // uuid
    // ... 25+ ثابت آخر
)

// وصف عمود نتيجة الاستعلام
type ColumnDesc struct {
    Name     string  // اسم العمود
    TableOID uint32  // OID الجدول (0 إذا لم يكن من جدول)
    AttrNum  int16   // رقم السمة
    TypeOID  uint32  // نوع البيانات
    TypeSize int16   // حجم النوع (-1 = متغير)
    TypeMod  int32   // معدّل النوع
    Format   int16   // 0=نص, 1=ثنائي
}

// نتيجة تنفيذ استعلام SQL
type QueryResult struct {
    Tag          string         // "SELECT 5", "INSERT 0 1", ...
    Columns      []ColumnDesc   // أعمدة النتيجة (فارغ للـ non-SELECT)
    Rows         [][]interface{} // الصفوف
    RowsAffected int64          // للـ INSERT/UPDATE/DELETE
}

// تحويل نوع SQLite → PostgreSQL OID
func SQLiteTypeToOID(sqliteType string) (oid uint32, size int16)
```

### type aliases في wire/types.go

```go
// wire/types.go — لا يُعيد تعريف أي شيء، فقط aliases
package wire

import "github.com/sqlite-server/sqlite-server/internal/pgproto"

type ColumnDesc  = pgproto.ColumnDesc   // alias (= وليس :=)
type QueryResult = pgproto.QueryResult  // alias

const OIDBool = pgproto.OIDBool
// الكود القديم في wire يستمر بدون تعديل ✅
```

---

## 2. wire — بروتوكول PostgreSQL Wire v3

### server.go — المستمع TCP

```go
type Server struct {
    cfg         ServerConfig
    listener    net.Listener
    activeConns sync.WaitGroup   // لانتظار الاتصالات عند الإيقاف
    connCount   atomic.Int64      // عدد الاتصالات المفتوحة حالياً
    done        chan struct{}      // للإشارة بإيقاف التشغيل
    closeOnce   sync.Once
    tlsCfg      *tls.Config
}
```

**دورة حياة الخادم**:
```
NewServer() → ListenAndServe()
    │
    ▼
net.Listen("tcp", addr)   أو   tls.Listen("tcp", addr, tlsCfg)
    │
    ▼
لكل اتصال: go handleConn(conn)   ← goroutine منفصلة لكل عميل
    │
    ▼
على SIGTERM/SIGINT:
    Shutdown() → close(done) → listener.Close()
    activeConns.Wait() → ينتظر إنهاء جميع الجلسات
```

### session.go — الجلسة

```go
type Session struct {
    conn    net.Conn
    reader  *bufio.Reader    // 64KB buffer
    writer  *bufio.Writer    // 64KB buffer
    dbConn  *pool.SQLConn    // اتصال SQLite مخصص لهذه الجلسة
    stmts   map[string]*PreparedStmt  // الجمل المُجهَّزة
    portals map[string]*Portal        // البوابات (Bind → Execute)
    txStatus TxStatus   // 'I' idle, 'T' in-tx, 'E' failed-tx
    params   map[string]string  // server_version, DateStyle, ...
    backendPID  uint32   // PID وهمي لهذه الجلسة
    secretKey   uint32   // مفتاح الإلغاء
}
```

**dispatch — جدول رسائل البروتوكول**:

| الرسالة | الكود | المعالج |
|---------|-------|---------|
| Query | `'Q'` | `handleSimpleQuery()` |
| Parse | `'P'` | `handleParse()` |
| Bind | `'B'` | `handleBind()` |
| Describe | `'D'` | `handleDescribe()` |
| Execute | `'E'` | `handleExecute()` |
| Close | `'C'` | `handleClose()` |
| Sync | `'S'` | `handleSync()` |
| Flush | `'H'` | `handleFlush()` |
| Terminate | `'X'` | `handleTerminate()` |

### startup.go — بروتوكول المصافحة

**تسلسل الرسائل**:

```
1. Client يرسل: [4 bytes length][4 bytes version=196608][params...]
   version 196608 = (3 << 16) | 0 = protocol 3.0

2. Server يتحقق من النوع:
   - 80877103 (sslRequestCode)  → يرد بـ 'S' أو 'N'
   - 80877102 (cancelRequestCode) → يعالج الإلغاء
   - 196608 (protoVersion3)    → يبدأ المصادقة

3. المصادقة:
   إذا --no-auth: يرسل AuthenticationOk مباشرة
   غير ذلك:
     Server → 'R'[4=8][4=3]  (AuthenticationCleartextPassword)
     Client → 'p'[len][password\0]
     Server → 'R'[4=8][4=0]  (AuthenticationOk)

4. بعد المصادقة:
   Server → 'S' ParameterStatus × N  (server_version, DateStyle, ...)
   Server → 'K' BackendKeyData       (PID + SecretKey)
   Server → 'Z' ReadyForQuery        (TxStatus='I')
```

---

## 3. pool — مجمع اتصالات SQLite

**الملف**: `internal/pool/connpool.go`

### ConnPool struct

```go
type ConnPool struct {
    db       *sql.DB           // قاعدة بيانات SQLite (مُشتركة)
    cfg      Config
    executor *engine.Executor  // منفذ الاستعلامات
    wSched   *writerScheduler  // جدولة الكتابة
    mu       sync.Mutex
    open     int               // عدد الجلسات المفتوحة حالياً
}
```

### DSN Builder — معاملات SQLite

```go
func buildDSN(dbPath string, cfg Config) string {
    params := []string{
        "_busy_timeout=5000",      // انتظر 5 ثوانٍ قبل SQLITE_BUSY
        "_foreign_keys=on",        // تفعيل المفاتيح الأجنبية
        "_cache_size=-65536",      // 64 MiB page cache
        "_mmap_size=268435456",    // 256 MiB memory-mapped I/O
        "_temp_store=memory",      // الجداول المؤقتة في الذاكرة
    }
    if cfg.WALMode {
        params = append(params,
            "_journal_mode=WAL",   // Write-Ahead Logging
            "_synchronous=NORMAL", // أسرع من FULL مع أمان كافٍ
            "_wal_autocheckpoint=1000",
        )
    }
    return fmt.Sprintf("file:%s?%s", dbPath, strings.Join(params, "&"))
}
```

### writerScheduler — مُجدِّل الكتابة

```
مشكلة: SQLite WAL يسمح بكُتّاب متعددين؟ لا! كاتب واحد فقط في وقت واحد.

الحل: goroutine واحدة تنفذ جميع عمليات الكتابة تسلسلياً.
       كل عملية كتابة تُرسَل عبر channel وتنتظر حتى تنتهي.

┌──────────────────────────────────────────────────────────┐
│                     writerScheduler                       │
│                                                           │
│  Goroutine 1 ─→ Submit(fn) ──┐                           │
│  Goroutine 2 ─→ Submit(fn) ──┤  queue chan ──→ writer    │
│  Goroutine 3 ─→ Submit(fn) ──┘  (buffer=128)   goroutine │
│                                                           │
│  Submit() تُحجب حتى يُنفَّذ fn ويُعاد job.result         │
└──────────────────────────────────────────────────────────┘

القراءة (SELECT): تعمل مباشرة دون المرور بـ writerScheduler
```

---

## 4. engine — منفذ SQL

**الملف**: `internal/engine/executor.go`

### تسلسل التنفيذ

```go
func (e *Executor) Execute(ctx, conn, pgSQL, args) (*QueryResult, error) {
    // 1. فحص الكتالوج الافتراضي أولاً
    if result, handled := e.catalog.Handle(ctx, db, pgSQL); handled {
        return result, nil  // pg_catalog.* أو information_schema.*
    }

    // 2. ترجمة SQL: PostgreSQL → SQLite
    sqliteSQL, err := e.plan.Rewrite(pgSQL)

    // 3. تحويل $1,$2 → ?,?
    sqliteSQL = normalizePlaceholders(sqliteSQL)

    // 4. تحديد نوع الأمر
    cmd := commandType(sqliteSQL)  // SELECT, INSERT, ...

    // 5. التنفيذ حسب النوع
    switch cmd {
    case "SELECT": return executeQuery(ctx, conn, sqliteSQL, args)
    case "INSERT", "UPDATE", "DELETE": return executeExec(...)
    case "BEGIN": conn.ExecContext(ctx, "BEGIN")
    case "COMMIT": conn.ExecContext(ctx, "COMMIT")
    // ...
    }
}
```

### executeQuery — استعلامات SELECT

```go
func executeQuery(ctx, db, sql, args) (*QueryResult, error) {
    rows, _ := db.QueryContext(ctx, sql, args...)
    defer rows.Close()

    // 1. استخراج metadata الأعمدة
    colTypes, _ := rows.ColumnTypes()
    cols := make([]pgproto.ColumnDesc, len(colTypes))
    for i, ct := range colTypes {
        oid, size := pgproto.SQLiteTypeToOID(ct.DatabaseTypeName())
        cols[i] = pgproto.ColumnDesc{Name: ct.Name(), TypeOID: oid, TypeSize: size}
    }

    // 2. قراءة جميع الصفوف
    var resultRows [][]interface{}
    for rows.Next() {
        vals := make([]interface{}, len(cols))
        // Scan → تحويل []byte إلى string
        rows.Scan(ptrs...)
        resultRows = append(resultRows, vals)
    }

    return &pgproto.QueryResult{
        Columns: cols,
        Rows:    resultRows,
        Tag:     fmt.Sprintf("SELECT %d", len(resultRows)),
    }, nil
}
```

---

## 5. catalog — الكتالوج الافتراضي

**الملف**: `internal/catalog/catalog.go`

### كيف يعمل

```go
func (vc *VirtualCatalog) Handle(ctx, db, pgSQL) (*pgproto.QueryResult, bool) {
    lower := strings.ToLower(pgSQL)

    // فلتر سريع: إذا لم يذكر كلمات مفتاحية للكتالوج، أكمل للـ SQLite
    if !strings.Contains(lower, "pg_catalog") &&
       !strings.Contains(lower, "information_schema") { ... }

    // الاستعلامات الخاصة
    // SELECT version() → "PostgreSQL 14.5 on SQLite ..."
    // SELECT current_database() → "main"
    // SELECT current_schema() → "public"
    // SELECT pg_backend_pid() → رقم عشوائي

    // information_schema.tables → استعلم من sqlite_master
    // information_schema.columns → استعلم من PRAGMA table_info
    // pg_catalog.pg_tables → استعلم من sqlite_master
}
```

### information_schema.tables

```sql
-- الاستعلام الداخلي ضد SQLite
SELECT name as table_name FROM sqlite_master
WHERE type='table' AND name NOT LIKE 'sqlite_%'
ORDER BY name
```

### information_schema.columns

```sql
-- PRAGMA table_info لكل جدول
PRAGMA table_info(table_name)
-- الأعمدة: cid, name, type, notnull, dflt_value, pk
```

---

## 6. sql/planner — محرك ترجمة SQL

### خط الأنابيب (Pipeline)

```
pgSQL string
    │
    ▼ Lexer (lexer.go)
Tokens []Token
    │
    ▼ Parser (parser.go)
AST (ast.Stmt)
    │
    ▼ Normalizer (normalizer.go)
AST مُعيَّر
    │
    ▼ Rewriter (rewriter.go)
AST مُعاد كتابته
    │
    ▼ Generator
sqliteSQL string
```

### قواعد إعادة الكتابة (Rewriter Rules)

| القاعدة | مثال المدخل | مثال الناتج |
|---------|------------|------------|
| SERIAL | `id SERIAL PRIMARY KEY` | `id INTEGER PRIMARY KEY AUTOINCREMENT` |
| BIGSERIAL | `id BIGSERIAL` | `id INTEGER PRIMARY KEY AUTOINCREMENT` |
| BOOLEAN | `active BOOLEAN DEFAULT TRUE` | `active INTEGER DEFAULT 1` |
| ILIKE | `name ILIKE '%test%'` | `LOWER(name) LIKE LOWER('%test%')` |
| NOW() | `SELECT NOW()` | `SELECT DATETIME('now')` |
| EXTRACT | `EXTRACT(YEAR FROM dt)` | `CAST(STRFTIME('%Y', dt) AS INTEGER)` |
| `::` cast | `'42'::INTEGER` | `CAST('42' AS INTEGER)` |
| `$N` | `WHERE id = $1` | `WHERE id = ?` |
| ON CONFLICT | `INSERT ON CONFLICT DO NOTHING` | `INSERT OR IGNORE` |
| ON CONFLICT UPDATE | `INSERT ON CONFLICT DO UPDATE` | `INSERT OR REPLACE` |
| SET | `SET client_encoding = 'UTF8'` | `SELECT 1` (no-op) |
| SHOW | `SHOW server_version` | `SELECT '14.5'` |

### RawExpr — الحل لمشكلة unexported interface

```go
// في sql/ast/ast.go (وليس في planner)
type RawExpr struct{ SQL string }

// يجب أن يكون في نفس الحزمة مع Expr interface
// لأن exprTag() هي method غير مُصدَّرة (unexported)
func (e *RawExpr) nodeTag() {}
func (e *RawExpr) exprTag() {}
func (e *RawExpr) String() string { return e.SQL }
```

---

## 7. errors — ترجمة أخطاء SQLite → PostgreSQL

**الملفان**: `internal/errors/pgerrors.go` و `sqlstate.go`

```go
// PGError يُمثّل خطأ PostgreSQL قياسي
type PGError struct {
    Severity string  // ERROR, FATAL, WARNING
    Code     string  // SQLSTATE code (5 أحرف)
    Message  string
    Detail   string
    Hint     string
}

// TranslateSQLiteError يُحوّل أخطاء SQLite إلى PGError
func TranslateSQLiteError(err error) error {
    // SQLITE_CONSTRAINT → 23505 (unique_violation)
    // SQLITE_BUSY       → 55P03 (lock_not_available)
    // SQLITE_READONLY   → 25006 (read_only_sql_transaction)
    // ...
}
```

---

## 8. تدفق الرسائل الكاملة — Extended Query

مثال: `SELECT * FROM users WHERE id = $1` مع المعامل `42`

```
┌─ Client ─────────────────────────────────────────────────────────────┐
│                                                                       │
│  Parse('P'):  name="", sql="SELECT * FROM users WHERE id = $1"       │
│  Bind('B'):   portal="", params=[42], formats=[0 (text)]             │
│  Describe('D'): 'P' portal                                            │
│  Execute('E'): portal="", maxRows=0                                   │
│  Sync('S')                                                            │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
         │
         │ TCP
         ▼
┌─ wire/extended_query.go ─────────────────────────────────────────────┐
│                                                                       │
│  handleParse():                                                        │
│    stmt.RewrittenSQL = pool.Rewrite("SELECT * FROM users WHERE id=$1")│
│    → "SELECT * FROM users WHERE id=?"                                 │
│    stmts[""] = stmt                                                   │
│    → sendParseComplete()  '1'                                         │
│                                                                       │
│  handleBind():                                                         │
│    portal.Args = [string("42")]  (DecodeParamValue)                  │
│    portals[""] = portal                                               │
│    → sendBindComplete()  '2'                                          │
│                                                                       │
│  handleDescribe():                                                     │
│    pool.DescribeColumns(rewrittenSQL)                                 │
│    → sendRowDescription()  'T'                                        │
│                                                                       │
│  handleExecute():                                                      │
│    pool.Execute(rewrittenSQL, args=[42])                              │
│    → لكل صف: sendDataRow()  'D'                                      │
│    → sendCommandComplete()  'C' "SELECT N"                            │
│                                                                       │
│  handleSync():                                                         │
│    → sendReadyForQuery()  'Z'                                         │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```
