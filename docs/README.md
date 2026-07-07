# sqlite-server — الوثائق الكاملة

## قائمة الملفات

| # | الملف | المحتوى |
|---|-------|---------|
| 01 | [01-overview.md](01-overview.md) | نظرة عامة، كيف يعمل، هيكل المشروع، تدفق البيانات الكامل |
| 02 | [02-installation-and-build.md](02-installation-and-build.md) | بناء الملف التنفيذي، إنشاء .exe، Cross-compilation لكل المنصات |
| 03 | [03-running-and-cli.md](03-running-and-cli.md) | تشغيل الخادم، جميع CLI flags، PowerShell & Bash، الاتصال من أدوات مختلفة |
| 04 | [04-architecture-deep-dive.md](04-architecture-deep-dive.md) | البنية الداخلية التفصيلية، الحزم، الأنماط، Import cycle fix |
| 05 | [05-testing-guide.md](05-testing-guide.md) | الاختبارات الوحدوية والتكاملية، جميع الأوامر، سكريبتات CI |
| 06 | [06-sql-compatibility.md](06-sql-compatibility.md) | ما يُترجَم تلقائياً، جداول التوافق، الكتالوج الافتراضي، القيود |
| 07 | [07-troubleshooting.md](07-troubleshooting.md) | استكشاف الأخطاء، FAQ، حلول للمشاكل الشائعة |
| 08 | [08-wire-protocol-reference.md](08-wire-protocol-reference.md) | مرجع بروتوكول PostgreSQL Wire v3، تنسيق الرسائل، تسلسل الاتصال |
| 09 | [09-developer-guide.md](09-developer-guide.md) | كيفية إضافة ميزات جديدة، أنماط الكود، المساهمة |
| 10 | [10-performance-and-production.md](10-performance-and-production.md) | ضبط الأداء، النشر (Docker/k8s/systemd)، النسخ الاحتياطي، الأمان |

---

## البداية السريعة (3 أوامر)

```bash
# 1. بناء
go build -o sqlite-server ./cmd/sqlite-server

# 2. تشغيل
./sqlite-server --no-auth -- myapp.db

# 3. اتصال
psql -h localhost -p 5432 -U test -c "SELECT 1"
```

---

## أهم المسارات في الكود

| المسار | الوظيفة |
|--------|---------|
| `internal/wire/session.go` | حلقة الأوامر الرئيسية لكل اتصال |
| `internal/wire/startup.go` | بروتوكول المصافحة مع العميل |
| `sql/planner/rewriter.go` | ترجمة PostgreSQL SQL → SQLite SQL |
| `internal/catalog/catalog.go` | الكتالوج الافتراضي (pg_catalog, information_schema) |
| `internal/pool/connpool.go` | إدارة اتصالات SQLite + WAL scheduler |
| `internal/pgproto/types.go` | الأنواع المشتركة (حل Import Cycle) |
| `cmd/sqlite-server/main.go` | CLI entry point |

---

## بناء .exe لـ Windows

```powershell
# PowerShell
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -o sqlite-server.exe .\cmd\sqlite-server
.\sqlite-server.exe version
```

```bash
# من Linux/macOS
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -o sqlite-server.exe ./cmd/sqlite-server
```
