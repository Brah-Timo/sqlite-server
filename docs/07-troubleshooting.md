# استكشاف الأخطاء — Troubleshooting Guide

---

## أخطاء الاتصال

### خطأ: `connection refused`

```
pq: dial tcp 127.0.0.1:5432: connect: connection refused
```

**السبب**: الخادم لم يبدأ أو يعمل على منفذ مختلف.

**الحل**:
```bash
# تحقق هل الخادم يعمل
ss -tlnp | grep 5432
# أو
netstat -an | grep 5432

# تحقق من السجلات
cat /var/log/sqlite-server.log

# أعد تشغيل الخادم
./sqlite-server --no-auth -- myapp.db
```

### خطأ: `too many connections`

```
pq: FATAL [53300]: sorry, too many clients already
```

**الحل**:
```bash
# زيادة الحد الأقصى
./sqlite-server --max-conn 500 --no-auth -- myapp.db

# أو تحقق من الاتصالات المفتوحة
# في psql:
SELECT count(*) FROM pg_stat_activity;
```

### خطأ: `authentication failed`

```
pq: FATAL [28P01]: password authentication failed
```

**الحل**:
```bash
# وضع التطوير — تعطيل المصادقة
./sqlite-server --no-auth -- myapp.db

# أو تأكد من كلمة المرور الصحيحة
# حالياً المصادقة تقبل أي كلمة مرور إذا لم يكن --no-auth
```

---

## أخطاء SQL

### خطأ: `disk I/O error`

```
pq: FATAL [58030]: disk I/O error (1802) (XX000)
```

**السبب**: ملف قاعدة البيانات لا يمكن فتحه أو الوصول إليه.

**الحل**:
```bash
# تحقق من الصلاحيات
ls -la /path/to/database.db

# تحقق من المساحة
df -h /path/to/

# إنشاء ملف جديد
./sqlite-server --no-auth -- /tmp/new_test.db
```

### خطأ: `column "x" does not exist`

**السبب**: اسم عمود غير موجود في الجدول.

**الحل**:
```sql
-- تحقق من أعمدة الجدول
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_name = 'your_table';
```

### خطأ: `syntax error`

**السبب**: استخدام SQL syntax غير مدعوم.

**الحل**:
```bash
# اختبر الترجمة مباشرة
# أضف أمر debug أو راجع docs/06-sql-compatibility.md
```

---

## أخطاء البناء

### `go: command not found`

```bash
# Linux
export PATH=$PATH:/usr/local/go/bin
# أو أضف لـ ~/.bashrc

# macOS (Homebrew)
export PATH=$PATH:/opt/homebrew/opt/go/bin

# Windows PowerShell
$env:PATH = "$env:PATH;C:\Go\bin"
```

### `go.sum: mismatch`

```bash
rm go.sum
GONOSUMDB="*" GOFLAGS="-mod=mod" go mod tidy
```

### `cannot find package`

```bash
# تحقق من الموديول
cat go.mod | head -3

# تنظيف وإعادة تنزيل
go clean -modcache
go mod download
```

### `signal: killed` عند `go vet`

**السبب**: نقص ذاكرة RAM عند تحليل `modernc.org/sqlite` الكبير.

**الحل**:
```bash
# vet على حزمك فقط
go vet github.com/sqlite-server/sqlite-server/internal/pgproto
go vet github.com/sqlite-server/sqlite-server/sql/...
go vet github.com/sqlite-server/sqlite-server/internal/engine
go vet github.com/sqlite-server/sqlite-server/internal/catalog
```

---

## أخطاء الاختبارات

### الاختبارات التكاملية تُخطئ (SKIP)

```
--- SKIP: TestPing (0.00s)
    crud_test.go: sqlite-server not available at localhost:15432
```

**السبب**: الخادم لم يبدأ قبل الاختبارات.

**الحل**:
```bash
# شغّل الخادم أولاً
./sqlite-server --addr 127.0.0.1:15432 --no-auth -- /tmp/test.db &
sleep 2

# ثم شغّل الاختبارات
go test ./tests/integration/... -v -timeout 120s
```

### `sqlite-server-test-bin: no such file or directory`

**السبب**: TestMain يحاول بناء ملف تنفيذي من المسار النسبي `../../cmd/sqlite-server`.

**الحل**: شغّل الخادم يدوياً وحدد المتغير:
```bash
SQLITE_SERVER_ADDR=localhost:15432 go test ./tests/integration/...
```

---

## أسئلة شائعة (FAQ)

### هل يمكن استخدام sqlite-server مع GORM؟

```go
// نعم! استخدم postgres driver
dsn := "host=localhost user=test password=test dbname=test port=5432 sslmode=disable"
db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
```

### هل يدعم الاتصالات المتزامنة؟

نعم. يدعم حتى `--max-conn` (افتراضياً 100) اتصالاً متزامناً.  
القراءات تعمل بالتوازي. الكتابات تُسلسَل عبر `writerScheduler`.

### هل البيانات آمنة من الفساد؟

مع WAL mode (`--wal=true` الافتراضي):
- الكتابات ذرية
- يتعافى من انقطاع الكهرباء
- لا يوجد فساد بيانات

### ما الفرق بين `--no-auth` و المصادقة العادية؟

```
--no-auth: أي اسم مستخدم وأي كلمة مرور مقبولة (للتطوير فقط)
بدون --no-auth: حالياً الكود يقبل أي كلمة مرور أيضاً (TODO: خدمة مصادقة حقيقية)
للإنتاج: استخدم TLS + شبكة خاصة
```

### هل يدعم TLS؟

```bash
# توليد شهادة self-signed للاختبار
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes \
  -subj "/CN=localhost"

# تشغيل مع TLS
./sqlite-server --ssl-cert cert.pem --ssl-key key.pem -- myapp.db

# اتصال psql مع TLS
psql "postgresql://test:test@localhost:5432/test?sslmode=require"
```

### كيف أعمل نسخة احتياطية؟

```bash
# الطريقة الأبسط: نسخ الملف مع إيقاف الكتابة
sqlite3 myapp.db ".backup backup_$(date +%Y%m%d).db"

# أو نسخ مباشر (آمن مع WAL mode)
cp myapp.db backup_$(date +%Y%m%d).db
cp myapp.db-wal backup_$(date +%Y%m%d).db-wal   # اختياري
```

### كيف أراقب الأداء؟

```bash
# عدد الاتصالات
# في psql أو أي عميل:
SELECT pg_backend_pid();  -- PID الجلسة الحالية

# عرض PRAGMA stats من SQLite مباشرة
# (استخدم sqlite3 CLI على ملف الـ .db مباشرة)
sqlite3 myapp.db "PRAGMA page_count; PRAGMA page_size; PRAGMA wal_checkpoint;"
```
