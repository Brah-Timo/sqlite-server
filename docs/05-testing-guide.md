# دليل الاختبارات — Testing Guide
## جميع الأوامر بالتفصيل

---

## نظرة عامة على الاختبارات

| النوع | الملف | الوصف | يحتاج خادماً؟ |
|-------|-------|-------|--------------|
| Unit | `tests/unit/translator_test.go` | اختبار planner/rewriter بدون DB | ❌ |
| Unit | `tests/unit/messages_test.go` | اختبار pgproto types & OIDs | ❌ |
| Integration | `tests/integration/crud_test.go` | اختبار E2E مع خادم حقيقي | ✅ |

---

## 1. الاختبارات الوحدوية (Unit Tests)

لا تحتاج إلى تشغيل الخادم — تعمل بشكل مستقل تماماً.

### Bash

```bash
export PATH=$PATH:/usr/local/go/bin  # أو مسار Go على جهازك
cd /path/to/sqlite-server

# تشغيل جميع الاختبارات الوحدوية مع verbose
go test ./tests/unit/... -v -timeout 60s

# تشغيل مع race detector (للكشف عن race conditions)
go test ./tests/unit/... -race -timeout 60s

# تشغيل مع قياس التغطية
go test ./tests/unit/... -cover -timeout 60s

# تشغيل مع تغطية تفصيلية + ملف HTML
go test ./tests/unit/... -coverprofile=coverage.out -timeout 60s
go tool cover -html=coverage.out -o coverage.html
open coverage.html  # أو xdg-open على Linux
```

### PowerShell (Windows)

```powershell
cd C:\sqlite-server

# الاختبارات الوحدوية
go test .\tests\unit\... -v -timeout 60s

# مع race detector
go test .\tests\unit\... -race -timeout 60s

# مع تغطية
go test .\tests\unit\... -cover -timeout 60s

# تغطية HTML
go test .\tests\unit\... -coverprofile=coverage.out -timeout 60s
go tool cover -html=coverage.out -o coverage.html
Start-Process coverage.html
```

### الناتج المتوقع

```
=== RUN   TestSQLiteTypeToOID
--- PASS: TestSQLiteTypeToOID (0.00s)
=== RUN   TestOIDToTypeName
--- PASS: TestOIDToTypeName (0.00s)
=== RUN   TestColumnDescDefaults
--- PASS: TestColumnDescDefaults (0.00s)
=== RUN   TestQueryResultIsSelect
--- PASS: TestQueryResultIsSelect (0.00s)
=== RUN   TestQueryResultRowCount
--- PASS: TestQueryResultRowCount (0.00s)
=== RUN   TestBigEndianInt32Encoding
--- PASS: TestBigEndianInt32Encoding (0.00s)
=== RUN   TestBigEndianInt16Encoding
--- PASS: TestBigEndianInt16Encoding (0.00s)
=== RUN   TestDecodeParamValueText
--- PASS: TestDecodeParamValueText (0.00s)
=== RUN   TestDecodeParamValueNil
--- PASS: TestDecodeParamValueNil (0.00s)
=== RUN   TestOIDConstants
--- PASS: TestOIDConstants (0.00s)
=== RUN   TestLexerTokenizes
--- PASS: TestLexerTokenizes (0.00s)
=== RUN   TestLexerHandlesQuotedStrings
--- PASS: TestLexerHandlesQuotedStrings (0.00s)
=== RUN   TestLexerHandlesDoubleQuotedIdents
--- PASS: TestLexerHandlesDoubleQuotedIdents (0.00s)
=== RUN   TestRewriteSelectOne
--- PASS: TestRewriteSelectOne (0.00s)
=== RUN   TestRewriteNowFunction
--- PASS: TestRewriteNowFunction (0.00s)
=== RUN   TestRewriteILIKE
--- PASS: TestRewriteILIKE (0.00s)
=== RUN   TestRewriteSerialType
--- PASS: TestRewriteSerialType (0.00s)
=== RUN   TestRewritePlaceholders
--- PASS: TestRewritePlaceholders (0.00s)
...
PASS
ok  	github.com/sqlite-server/sqlite-server/tests/unit	0.004s
```

---

## 2. اختبار اختبارات محددة

```bash
# تشغيل اختبار واحد بالاسم
go test ./tests/unit/... -run TestSQLiteTypeToOID -v

# تشغيل اختبارات تبدأ بـ "Rewrite"
go test ./tests/unit/... -run TestRewrite -v

# تشغيل اختبارات تحتوي على "OID"
go test ./tests/unit/... -run OID -v

# تشغيل من ملف معين فقط
go test ./tests/unit/ -run TestOIDConstants -v
```

---

## 3. اختبارات التكامل (Integration Tests)

تتصل بخادم حقيقي عبر بروتوكول PostgreSQL.

### الطريقة 1: تشغيل الخادم يدوياً ثم الاختبارات

```bash
# نافذة ترمينال 1 — تشغيل الخادم
cd /path/to/sqlite-server
./sqlite-server --addr 127.0.0.1:15432 --no-auth -- /tmp/test_integration.db

# نافذة ترمينال 2 — تشغيل الاختبارات
cd /path/to/sqlite-server
SQLITE_SERVER_ADDR=localhost:15432 \
  go test ./tests/integration/... -v -timeout 120s

# تنظيف بعد الانتهاء
# Ctrl+C في النافذة الأولى
rm -f /tmp/test_integration.db
```

### الطريقة 2: سكريبت متكامل (Bash)

```bash
#!/bin/bash
# run_integration.sh

set -e
export PATH=$PATH:/usr/local/go/bin

DB_FILE="/tmp/sqlite_inttest_$(date +%s).db"
ADDR="127.0.0.1:15432"
LOG="/tmp/sqlite-server-test.log"

# بناء الملف التنفيذي
echo "Building sqlite-server..."
go build -o /tmp/sqlite-server-bin ./cmd/sqlite-server

# تشغيل الخادم في الخلفية
echo "Starting server..."
/tmp/sqlite-server-bin --addr $ADDR --no-auth -- $DB_FILE > $LOG 2>&1 &
SERVER_PID=$!

# انتظار جهوزية الخادم
echo "Waiting for server..."
for i in $(seq 1 30); do
    if ss -tlnp 2>/dev/null | grep -q ':15432'; then
        echo "Server ready (attempt $i)"
        break
    fi
    sleep 0.5
done

# تشغيل الاختبارات
echo "Running integration tests..."
SQLITE_SERVER_ADDR=$ADDR \
    go test ./tests/integration/... -v -timeout 120s
EXIT_CODE=$?

# تنظيف
echo "Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
rm -f $DB_FILE /tmp/sqlite-server-bin $LOG

exit $EXIT_CODE
```

```bash
chmod +x run_integration.sh
./run_integration.sh
```

### الطريقة 3: PowerShell (Windows)

```powershell
# run_integration.ps1

$ErrorActionPreference = "Stop"

$DbFile = "$env:TEMP\sqlite_inttest_$(Get-Date -Format 'yyyyMMddHHmmss').db"
$Addr = "127.0.0.1:15432"
$LogFile = "$env:TEMP\sqlite-server-test.log"

# بناء
Write-Host "Building sqlite-server..."
go build -o "$env:TEMP\sqlite-server-test.exe" .\cmd\sqlite-server

# تشغيل الخادم
Write-Host "Starting server..."
$ServerProcess = Start-Process `
    -FilePath "$env:TEMP\sqlite-server-test.exe" `
    -ArgumentList "--addr", $Addr, "--no-auth", "--", $DbFile `
    -RedirectStandardOutput $LogFile `
    -RedirectStandardError $LogFile `
    -PassThru `
    -WindowStyle Hidden

# انتظار الجهوزية
Write-Host "Waiting for server..."
$ready = $false
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Milliseconds 300
    $conn = Test-NetConnection -ComputerName "127.0.0.1" -Port 15432 -WarningAction SilentlyContinue
    if ($conn.TcpTestSucceeded) {
        Write-Host "Server ready!"
        $ready = $true
        break
    }
}

if (-not $ready) {
    Write-Error "Server failed to start"
    $ServerProcess.Kill()
    exit 1
}

# تشغيل الاختبارات
$env:SQLITE_SERVER_ADDR = $Addr
try {
    go test .\tests\integration\... -v -timeout 120s
    $ExitCode = $LASTEXITCODE
} finally {
    # تنظيف
    $ServerProcess.Kill() | Out-Null
    Remove-Item $DbFile -ErrorAction SilentlyContinue
    Remove-Item "$env:TEMP\sqlite-server-test.exe" -ErrorAction SilentlyContinue
    $env:SQLITE_SERVER_ADDR = $null
}

exit $ExitCode
```

---

## 4. قائمة اختبارات التكامل الكاملة

### A — الاتصال الأساسي

| الاختبار | ما يختبره |
|----------|----------|
| `TestPing` | `db.PingContext()` يُرجع nil |
| `TestVersion` | `SELECT version()` يُرجع نصاً غير فارغ |
| `TestSelectOne` | `SELECT 1` يُرجع القيمة 1 |

### B — DDL

| الاختبار | ما يختبره |
|----------|----------|
| `TestCreateAndDropTable` | CREATE TABLE + DROP TABLE + تأكيد الحذف |
| `TestCreateIndex` | CREATE INDEX + DROP INDEX |

### C — CRUD الكامل

| الاختبار | ما يختبره |
|----------|----------|
| `TestInsertSelectUpdateDelete` | INSERT × 3 → SELECT → UPDATE → DELETE → COUNT=0 |

### D — المعاملات (Transactions)

| الاختبار | ما يختبره |
|----------|----------|
| `TestTransactionCommit` | BEGIN → INSERT → COMMIT → بيانات موجودة |
| `TestTransactionRollback` | BEGIN → INSERT → ROLLBACK → بيانات محذوفة |
| `TestTransactionIsolation` | tx1 لا يرى تغييرات tx2 غير المُعتمَدة |
| `TestSavepoint` | SAVEPOINT → عمليات → ROLLBACK TO sp → RELEASE |

### E — Prepared Statements

| الاختبار | ما يختبره |
|----------|----------|
| `TestPreparedStatementCRUD` | db.Prepare() → Exec × N → Query × 1 |
| `TestPreparedStatementReuseAcrossTransactions` | إعادة استخدام stmt عبر txns مختلفة |
| `TestParameterTypes` | INTEGER, TEXT, REAL, BOOLEAN, NULL عبر params |

### F — كتالوج DBeaver

| الاختبار | ما يختبره |
|----------|----------|
| `TestInformationSchemaTables` | `information_schema.tables` يُعيد الجداول الصحيحة |
| `TestInformationSchemaColumns` | `information_schema.columns` لجدول معين |
| `TestPGTablesQuery` | `pg_catalog.pg_tables` يعمل |

### G — دوال النظام

| الاختبار | ما يختبره |
|----------|----------|
| `TestCurrentDatabase` | `SELECT current_database()` |
| `TestCurrentSchema` | `SELECT current_schema()` |
| `TestPGBackendPID` | `SELECT pg_backend_pid()` يُرجع عدداً صحيحاً |
| `TestSetStatement` | `SET client_encoding = 'UTF8'` بدون خطأ |
| `TestShowStatement` | `SHOW server_version` يُرجع نتيجة |

### H — معالجة الأنواع

| الاختبار | ما يختبره |
|----------|----------|
| `TestNullValues` | INSERT NULL → SELECT يُرجع nil |
| `TestDataTypeRoundTrip` | INTEGER, TEXT, REAL, BOOLEAN round-trip |

### I — التزامن والميزات المتقدمة

| الاختبار | ما يختبره |
|----------|----------|
| `TestConcurrentConnections` | 10 goroutines × INSERT/SELECT متزامنة |
| `TestReturning` | `INSERT ... RETURNING id` يُرجع الصفوف |
| `TestSQLTranslation` | ILIKE, EXTRACT تعمل صحيحاً |
| `TestOrderByLimitOffset` | ORDER BY + LIMIT + OFFSET |
| `TestJoins` | INNER JOIN, LEFT JOIN |

---

## 5. اختبار بناء جميع الحزم

```bash
# الطريقة الأسرع: بناء كل شيء للتحقق
go build ./...

# بناء كل حزمة بشكل منفصل مع تقرير
for pkg in \
    ./internal/pgproto/ \
    ./internal/errors/ \
    ./sql/lexer/ \
    ./sql/ast/ \
    ./sql/parser/ \
    ./sql/planner/ \
    ./internal/catalog/ \
    ./internal/engine/ \
    ./internal/pool/ \
    ./internal/wire/ \
    ./compat/... \
    ./cmd/sqlite-server/; do
    
    if go build $pkg 2>/dev/null; then
        echo "✅ $pkg"
    else
        echo "❌ $pkg"
        go build $pkg  # أظهر الخطأ
    fi
done
```

---

## 6. تشغيل جميع الاختبارات دفعة واحدة (CI)

```bash
#!/bin/bash
# ci.sh — يُشبه ما تفعله CI/CD pipelines

set -e
export PATH=$PATH:/usr/local/go/bin
cd /path/to/sqlite-server

echo "=== Format Check ==="
if [ -n "$(gofmt -l .)" ]; then
    echo "❌ Code not formatted. Run: go fmt ./..."
    gofmt -l .
    exit 1
fi
echo "✅ Format OK"

echo "=== Build ==="
CGO_ENABLED=0 go build ./...
echo "✅ Build OK"

echo "=== Unit Tests ==="
go test ./tests/unit/... -race -cover -timeout 60s
echo "✅ Unit Tests OK"

echo "=== go mod verify ==="
go mod verify
echo "✅ Module OK"

echo "=== Build Binary ==="
CGO_ENABLED=0 go build -o /tmp/sqlite-server-ci ./cmd/sqlite-server
echo "✅ Binary built: $(/tmp/sqlite-server-ci version)"

echo ""
echo "✅✅✅ All checks passed!"
```

```bash
chmod +x ci.sh && ./ci.sh
```

---

## 7. Makefile — الأوامر المختصرة

```bash
# بناء
make build

# اختبارات وحدوية
make test-unit

# اختبارات تكامل
make test-integration

# تغطية
make coverage

# تنسيق
make fmt

# بناء لجميع المنصات
make build-all

# تنظيف
make clean

# مساعدة
make help
```
