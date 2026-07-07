# sqlite-server — نظرة عامة شاملة
# Overview & How It Works

---

## ما هو sqlite-server؟

**sqlite-server** هو خادم قاعدة بيانات مكتوب بلغة Go يقوم بعرض قاعدة بيانات SQLite عبر **بروتوكول PostgreSQL Wire Protocol v3**.  
هذا يعني أن أي برنامج أو مكتبة تتحدث مع PostgreSQL يمكنها الاتصال بـ sqlite-server دون أي تغيير، بينما جميع البيانات تُخزَّن في ملف `.db` واحد على القرص.

```
DBeaver / psql / pgAdmin / GORM / Hibernate
           │
           │  PostgreSQL Wire Protocol v3 (TCP/IP)
           │
     ┌─────▼──────────────────────────────────┐
     │          sqlite-server                  │
     │   يتلقى الاستعلامات بصيغة PostgreSQL    │
     │   يترجمها إلى SQLite SQL                │
     │   يُعيد النتائج بصيغة PostgreSQL        │
     └─────────────────┬──────────────────────┘
                       │
               ┌───────▼────────┐
               │  ملف .db       │
               │  SQLite 3.x    │
               └────────────────┘
```

---

## لماذا sqlite-server؟

| الميزة | التفاصيل |
|--------|----------|
| **صفر CGO** | يستخدم `modernc.org/sqlite` الذي يُحوّل SQLite من C إلى Go — لا توجد تبعيات C |
| **ملف واحد** | كل البيانات في ملف `.db` — سهل النسخ الاحتياطي والنقل |
| **بروتوكول كامل** | يدعم Startup + Auth + Simple Query + Extended Query (Prepared Statements) |
| **ترجمة SQL** | PostgreSQL SQL → SQLite SQL تلقائياً: SERIAL, ILIKE, EXTRACT, `::` casts |
| **كتالوج افتراضي** | `information_schema` و `pg_catalog` تعمل بدون تعديل |
| **WAL mode** | كاتب واحد + قراء متعددون متزامنون |
| **TLS** | دعم اختياري لـ TLS |

---

## كيف تعمل — الخطوات الكاملة

عندما يتصل عميل PostgreSQL بـ sqlite-server، تمر الاتصال بالمراحل التالية:

### المرحلة 1: التشغيل (Startup)

```
Client                         Server
  │                              │
  │──── 4 bytes (length) ───────▶│
  │──── 4 bytes (version=196608)▶│  ← 3.0 = (3<<16)|0
  │──── key\0value\0...\0\0 ────▶│  ← user=test, database=mydb, application_name=psql
  │                              │
  │◀─── AuthenticationCleartextPassword ('R') ─│  إذا كان --no-auth=false
  │──── Password message ('p') ─▶│
  │◀─── AuthenticationOk ('R') ──│
  │◀─── ParameterStatus ('S') ───│  server_version=14.5 ...
  │◀─── BackendKeyData ('K') ────│  PID + SecretKey
  │◀─── ReadyForQuery ('Z') ─────│  TxStatus='I'
```

### المرحلة 2: Simple Query Protocol

```
Client                         Server
  │                              │
  │──── Query ('Q') ────────────▶│  "SELECT * FROM users"
  │                              │  ← يُعيد الكود في simple_query.go
  │◀─── RowDescription ('T') ────│  أسماء الأعمدة + أنواعها
  │◀─── DataRow ('D') ──────────│  صف، صف، صف...
  │◀─── CommandComplete ('C') ───│  "SELECT 5"
  │◀─── ReadyForQuery ('Z') ─────│
```

### المرحلة 3: Extended Query Protocol (Prepared Statements)

```
Client                         Server
  │                              │
  │──── Parse ('P') ────────────▶│  name="" sql="SELECT * FROM t WHERE id=$1"
  │──── Bind ('B') ─────────────▶│  portal="" params=[42]
  │──── Describe ('D') ─────────▶│  'P' (portal)
  │──── Execute ('E') ──────────▶│  portal="" maxRows=0
  │──── Sync ('S') ─────────────▶│
  │                              │
  │◀─── ParseComplete ('1') ─────│
  │◀─── BindComplete ('2') ──────│
  │◀─── RowDescription ('T') ────│
  │◀─── DataRow ('D') ──────────│
  │◀─── CommandComplete ('C') ───│
  │◀─── ReadyForQuery ('Z') ─────│
```

---

## بنية المشروع

```
sqlite-server/
├── cmd/
│   └── sqlite-server/
│       └── main.go              ← نقطة الدخول (cobra CLI)
│
├── internal/
│   ├── pgproto/
│   │   └── types.go             ← حزمة الأوراق — لا تستورد أي شيء داخلي
│   ├── wire/
│   │   ├── server.go            ← TCP listener + goroutine dispatcher
│   │   ├── session.go           ← حالة الاتصال الواحد + command loop
│   │   ├── startup.go           ← المصافحة الأولى + المصادقة
│   │   ├── auth.go              ← معالجة كلمة المرور
│   │   ├── simple_query.go      ← بروتوكول Simple Query
│   │   ├── extended_query.go    ← Parse/Bind/Describe/Execute/Sync
│   │   ├── messages.go          ← RowDescription, DataRow, CommandComplete
│   │   ├── error.go             ← ErrorResponse
│   │   ├── ready.go             ← ReadyForQuery
│   │   └── types.go             ← type aliases → pgproto
│   ├── pool/
│   │   └── connpool.go          ← SQLite connection pool + WAL scheduler
│   ├── engine/
│   │   ├── executor.go          ← تنفيذ SQL (يربط planner + catalog)
│   │   ├── translator.go        ← مساعدات ترجمة
│   │   └── optimizer.go         ← محسّن الاستعلامات
│   ├── catalog/
│   │   └── catalog.go           ← pg_catalog + information_schema افتراضية
│   └── errors/
│       ├── pgerrors.go          ← نوع PGError
│       └── sqlstate.go          ← ثوابت SQLSTATE
│
├── sql/
│   ├── lexer/
│   │   ├── token.go             ← أنواع الرموز (keywords, literals)
│   │   └── lexer.go             ← المحلل المعجمي
│   ├── ast/
│   │   └── ast.go               ← أنواع عقد شجرة AST
│   ├── parser/
│   │   └── parser.go            ← محلل PostgreSQL Grammar
│   └── planner/
│       ├── planner.go           ← نقطة الدخول: Rewrite(pgSQL) string
│       ├── rewriter.go          ← قواعد إعادة الكتابة
│       └── normalizer.go        ← تطبيع AST
│
├── compat/
│   └── postgres/
│       ├── functions.go         ← جداول توافق الدوال
│       ├── types.go             ← جداول توافق الأنواع
│       └── operators.go         ← جداول توافق العمليات
│
├── tests/
│   ├── unit/
│   │   ├── translator_test.go   ← اختبارات planner بدون خادم
│   │   └── messages_test.go     ← اختبارات pgproto
│   └── integration/
│       └── crud_test.go         ← اختبارات end-to-end مع خادم حقيقي
│
├── configs/
│   ├── dev.yaml                 ← إعدادات التطوير
│   ├── production.yaml          ← إعدادات الإنتاج
│   └── docker-compose.yml       ← Docker Compose
│
├── Makefile                     ← أوامر البناء
├── Dockerfile                   ← صورة Docker
├── go.mod                       ← تعريف الموديول
├── go.sum                       ← مجاميع التحقق
├── README.md
└── CONTRIBUTING.md
```

---

## تدفق البيانات الكامل

```
طلب من العميل (مثال: INSERT INTO users VALUES($1, $2))
                          │
                    internal/wire
                    session.go → commandLoop()
                          │
              ┌───────────▼───────────┐
              │   Simple Query ('Q')  │
              │   أو Parse ('P')       │
              └───────────┬───────────┘
                          │
                    internal/pool
                    ConnPool.Execute()
                          │
              ┌───────────▼───────────┐
              │   internal/catalog    │
              │   هل هو استعلام       │
              │   pg_catalog؟         │
              └──────┬────────┬───────┘
                   نعم       لا
                    │         │
             يُعيد  │    internal/engine
             نتيجة  │    executor.go
             افتراضية    │
                          │
                    sql/planner
                    Planner.Rewrite()
                          │ تحويل PostgreSQL→SQLite
                          │ SELECT 1 ✓
                          │ SERIAL→INTEGER ✓
                          │ $1→? ✓
                          │ ILIKE→LIKE ✓
                          │
                    modernc.org/sqlite
                    قاعدة البيانات الفعلية
                          │
                    النتيجة ترجع عبر
                    pgproto.QueryResult
                          │
                    internal/wire
                    messages.go
                    RowDescription + DataRow
                          │
                    العميل ← TCP ←
```
