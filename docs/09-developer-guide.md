# دليل المطور — Developer Guide
## كيفية تطوير وتوسيع sqlite-server

---

## 1. إعداد بيئة التطوير

### المتطلبات

```bash
# Linux/macOS
go version          # يجب Go 1.22+
git version
make --version      # اختياري

# التحقق من إصدارات التبعيات
go list -m all | grep "modernc.org/sqlite"
# modernc.org/sqlite v1.33.1
```

### Clone والإعداد

```bash
git clone https://github.com/sqlite-server/sqlite-server
cd sqlite-server

# تنزيل التبعيات (أول مرة: 2-5 دقائق)
go mod download

# بناء للتأكد
CGO_ENABLED=0 go build ./...
echo "✅ Build successful"

# تشغيل الاختبارات
go test ./tests/unit/... -v
echo "✅ Unit tests pass"
```

---

## 2. إضافة قاعدة ترجمة SQL جديدة

### مثال: إضافة دعم `REGEXP`

**الخطوة 1**: إضافة keyword للـ lexer (إذا لزم)

```go
// sql/lexer/token.go
const (
    // ... الموجود ...
    KW_OF
    KW_REGEXP   // ← جديد
)

// في IsKeyword():
return t.Kind >= KW_CREATE && t.Kind <= KW_REGEXP
```

```go
// sql/lexer/lexer.go — في خريطة keywords
"REGEXP": KW_REGEXP,
```

**الخطوة 2**: إضافة AST node (إذا لزم)

```go
// sql/ast/ast.go
// إذا كان REGEXP تعبيراً ثنائياً، يكفي BinaryExpr الموجود
// مع Op = "REGEXP"
```

**الخطوة 3**: إضافة قاعدة Rewriter

```go
// sql/planner/rewriter.go
func (rw *Rewriter) binaryExpr(e *ast.BinaryExpr) ast.Expr {
    left := rw.expr(e.Left)
    right := rw.expr(e.Right)
    
    // REGEXP → استخدم SQLite's REGEXP (يحتاج user-defined function)
    // أو ترجم إلى LIKE إذا كان pattern بسيطاً
    if strings.EqualFold(e.Op, "REGEXP") {
        return &ast.RawExpr{
            SQL: fmt.Sprintf("%s REGEXP %s",
                left.String(), right.String()),
        }
    }
    
    return &ast.BinaryExpr{Left: left, Op: e.Op, Right: right}
}
```

**الخطوة 4**: كتابة اختبار

```go
// tests/unit/translator_test.go
func TestRewriteRegexp(t *testing.T) {
    got := rewrite(t, "SELECT * FROM t WHERE name REGEXP '^test'")
    if !strings.Contains(strings.ToUpper(got), "REGEXP") {
        t.Fatalf("expected REGEXP, got: %q", got)
    }
}
```

**الخطوة 5**: تشغيل الاختبار

```bash
go test ./tests/unit/... -run TestRewriteRegexp -v
```

---

## 3. إضافة استعلام كتالوج افتراضي جديد

**مثال**: إضافة دعم `pg_catalog.pg_indexes`

```go
// internal/catalog/catalog.go
func (vc *VirtualCatalog) Handle(ctx context.Context, db *sql.Conn, pgSQL string) (*pgproto.QueryResult, bool) {
    lower := strings.ToLower(pgSQL)
    
    // أضف الفحص في quick filter
    if !strings.Contains(lower, "pg_catalog") &&
       !strings.Contains(lower, "pg_indexes") { ... }
    
    // أضف معالج جديد
    if strings.Contains(lower, "pg_indexes") ||
       strings.Contains(lower, "pg_catalog.pg_indexes") {
        return vc.pgIndexes(ctx, db, lower)
    }
}

// أضف الدالة
func (vc *VirtualCatalog) pgIndexes(ctx context.Context, db *sql.Conn, lower string) (*pgproto.QueryResult, bool) {
    // استعلم من SQLite
    rows, err := db.QueryContext(ctx, `
        SELECT name, tbl_name, sql
        FROM sqlite_master
        WHERE type = 'index' AND name NOT LIKE 'sqlite_%'
        ORDER BY name
    `)
    if err != nil {
        return nil, true
    }
    defer rows.Close()
    
    cols := []pgproto.ColumnDesc{
        {Name: "indexname",  TypeOID: pgproto.OIDText, TypeSize: -1},
        {Name: "tablename",  TypeOID: pgproto.OIDText, TypeSize: -1},
        {Name: "indexdef",   TypeOID: pgproto.OIDText, TypeSize: -1},
    }
    
    var resultRows [][]interface{}
    for rows.Next() {
        var name, tbl string
        var sqlDef *string
        rows.Scan(&name, &tbl, &sqlDef)
        def := ""
        if sqlDef != nil {
            def = *sqlDef
        }
        resultRows = append(resultRows, []interface{}{name, tbl, def})
    }
    
    return &pgproto.QueryResult{
        Columns: cols,
        Rows:    resultRows,
        Tag:     fmt.Sprintf("SELECT %d", len(resultRows)),
    }, true
}
```

---

## 4. إضافة نوع بيانات جديد

**مثال**: دعم `VECTOR` type (لـ AI embeddings)

```go
// internal/pgproto/types.go
const (
    // ... الموجود ...
    OIDVector uint32 = 16384  // custom OID لـ vector
)

// في SQLiteTypeToOID:
case "VECTOR", "EMBEDDING":
    return OIDVector, -1

// في OIDToTypeName:
case OIDVector:
    return "vector"
```

```go
// compat/postgres/types.go
// إضافة التعيين
var TypeAliases = map[string]string{
    "VECTOR":    "BLOB",   // مخزَّن كـ BLOB في SQLite
    "EMBEDDING": "BLOB",
}
```

---

## 5. هيكل الاختبارات

### كتابة unit test جديد

```go
// tests/unit/myfeature_test.go
package unit

import (
    "testing"
    "strings"
)

func TestMyNewFeature(t *testing.T) {
    // استخدم newPlanner() الموجودة
    p := newPlanner()
    
    result, err := p.Rewrite("MY CUSTOM SQL HERE")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    
    if !strings.Contains(strings.ToUpper(result), "EXPECTED_OUTPUT") {
        t.Errorf("expected EXPECTED_OUTPUT in: %q", result)
    }
}
```

### كتابة integration test جديد

```go
// tests/integration/crud_test.go — أضف داخل الملف الموجود

func TestMyIntegrationFeature(t *testing.T) {
    db := connectDB(t)  // تتصل بالخادم أو تُخطئ (Skip) إذا لم يكن يعمل
    defer db.Close()
    ctx := context.Background()
    
    // إنشاء الجدول
    mustExec(t, db, `DROP TABLE IF EXISTS my_test_table`)
    mustExec(t, db, `CREATE TABLE my_test_table (id SERIAL PRIMARY KEY, data TEXT)`)
    defer db.ExecContext(ctx, `DROP TABLE IF EXISTS my_test_table`)
    
    // الاختبار
    mustExec(t, db, `INSERT INTO my_test_table (data) VALUES ($1)`, "test_value")
    
    var data string
    err := db.QueryRowContext(ctx, `SELECT data FROM my_test_table`).Scan(&data)
    if err != nil {
        t.Fatalf("select: %v", err)
    }
    if data != "test_value" {
        t.Errorf("got %q, want %q", data, "test_value")
    }
}
```

---

## 6. أنماط الكود المتبعة

### خطأ PGPROTO

```go
// دائماً استخدم errors.PGError للأخطاء المُرسَلة للعميل
import "github.com/sqlite-server/sqlite-server/internal/errors"

err := errors.NewPGError("42P01", "undefined_table",
    fmt.Sprintf("relation %q does not exist", tableName))
return s.sendError(err)
```

### استخدام Context

```go
// دائماً مرّر context للعمليات التي قد تستغرق وقتاً
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

result, err := pool.Execute(ctx, conn, sql, args)
```

### إغلاق الموارد

```go
// دائماً defer Close() فوراً بعد فتح المورد
rows, err := db.QueryContext(ctx, sql, args...)
if err != nil {
    return nil, err
}
defer rows.Close()  // ← مباشرة بعد التحقق من الخطأ
```

### قراءة من TCP

```go
// دائماً استخدم io.ReadFull لضمان قراءة كل البايتات
buf := make([]byte, 4)
if _, err := io.ReadFull(s.reader, buf); err != nil {
    return fmt.Errorf("read length: %w", err)
}
```

---

## 7. المساهمة (Contributing)

### سير العمل

```bash
# 1. Fork ثم clone
git clone https://github.com/YOUR_USERNAME/sqlite-server
cd sqlite-server

# 2. إنشاء branch
git checkout -b feat/add-regexp-support

# 3. تطوير واختبار
go test ./tests/unit/... -v -race

# 4. تنسيق الكود
go fmt ./...

# 5. Commit
git add -A
git commit -m "feat(planner): add REGEXP operator translation"

# 6. Push
git push origin feat/add-regexp-support

# 7. فتح Pull Request
```

### معايير الكود

- كل دالة عامة (exported) يجب أن تحتوي على doc comment
- الاختبارات مطلوبة للميزات الجديدة
- لا break changes بدون نقاش
- `go fmt ./...` قبل كل commit
