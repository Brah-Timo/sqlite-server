# تشغيل الخادم — Running the Server
## الأوامر الكاملة مع PowerShell و Bash

---

## الاستخدام الأساسي

```
sqlite-server [flags] <database.db>
sqlite-server version
sqlite-server --help
```

> ⚠️ **مهم**: اسم ملف قاعدة البيانات هو **argument موضعي** — يجب وضعه بعد `--`  
> أو بعد جميع الخيارات مباشرةً.

---

## جميع خيارات CLI

| الخيار | القيمة الافتراضية | الوصف |
|--------|-----------------|-------|
| `--addr` | `0.0.0.0:5432` | عنوان TCP للاستماع (host:port) |
| `--max-conn` | `100` | أقصى عدد اتصالات متزامنة |
| `--wal` | `true` | تفعيل WAL journal mode |
| `--no-auth` | `false` | تعطيل المصادقة (وضع التطوير) |
| `--ssl-cert` | فارغ | مسار شهادة TLS (PEM) |
| `--ssl-key` | فارغ | مسار مفتاح TLS الخاص (PEM) |
| `--busy-timeout` | `5s` | مهلة SQLite عند الانتظار |
| `--log-level` | `info` | مستوى السجل: debug/info/warn/error |
| `--read-only` | `false` | فتح قاعدة البيانات للقراءة فقط |

---

## أمثلة تشغيل — PowerShell (Windows)

### تشغيل بسيط بدون مصادقة

```powershell
# تشغيل بدون مصادقة على المنفذ الافتراضي 5432
.\sqlite-server.exe --no-auth -- myapp.db

# الناتج المتوقع:
# sqlite-server dev
#   database  : myapp.db
#   listen    : 0.0.0.0:5432
#   max-conn  : 100
#   wal-mode  : true
#   read-only : false
#   auth      : false
#
# Ready. Accepting connections on 0.0.0.0:5432
# [info] sqlite-server listening on 0.0.0.0:5432
```

### تشغيل على منفذ مخصص

```powershell
.\sqlite-server.exe --addr "127.0.0.1:5433" --no-auth -- dev.db
```

### تشغيل مع مصادقة (الإنتاج)

```powershell
.\sqlite-server.exe `
  --addr "0.0.0.0:5432" `
  --max-conn 200 `
  --wal `
  --busy-timeout 10s `
  --log-level info `
  -- "C:\Data\production.db"
```

### تشغيل مع TLS

```powershell
.\sqlite-server.exe `
  --addr "0.0.0.0:5432" `
  --ssl-cert "C:\certs\server.crt" `
  --ssl-key  "C:\certs\server.key" `
  -- "C:\Data\secure.db"
```

### تشغيل كـ Background Job في PowerShell

```powershell
# تشغيل في الخلفية
$job = Start-Job -ScriptBlock {
    Set-Location "C:\sqlite-server"
    .\sqlite-server.exe --no-auth -- myapp.db
}
Write-Host "Server started as Job ID: $($job.Id)"

# مراقبة السجلات
Receive-Job $job -Keep | Select-Object -Last 10

# إيقاف الخادم
Stop-Job $job
Remove-Job $job
```

### تسجيل الخادم كـ Windows Service

```powershell
# باستخدام NSSM (Non-Sucking Service Manager)
# تنزيل NSSM من https://nssm.cc

nssm install sqlite-server "C:\sqlite-server\sqlite-server.exe"
nssm set sqlite-server AppParameters "--no-auth -- C:\data\myapp.db"
nssm set sqlite-server AppDirectory "C:\sqlite-server"
nssm set sqlite-server DisplayName "SQLite PostgreSQL Server"
nssm set sqlite-server Description "Exposes SQLite over PostgreSQL wire protocol"
nssm set sqlite-server Start SERVICE_AUTO_START
nssm set sqlite-server AppStdout "C:\logs\sqlite-server.log"
nssm set sqlite-server AppStderr "C:\logs\sqlite-server-error.log"

# تشغيل الخدمة
nssm start sqlite-server

# التحقق من الحالة
nssm status sqlite-server

# إيقاف الخدمة
nssm stop sqlite-server
```

---

## أمثلة تشغيل — Bash (Linux/macOS)

### تشغيل بسيط

```bash
./sqlite-server --no-auth -- myapp.db
```

### تشغيل في الخلفية

```bash
# تشغيل في الخلفية مع تسجيل السجلات
nohup ./sqlite-server --no-auth -- myapp.db > /var/log/sqlite-server.log 2>&1 &
echo "Server PID: $!"

# أو باستخدام &
./sqlite-server --no-auth -- myapp.db &
SERVER_PID=$!
echo "Started: PID=$SERVER_PID"

# انتظار حتى يكون جاهزاً
for i in $(seq 1 30); do
  if ss -tlnp 2>/dev/null | grep -q ':5432'; then
    echo "Server ready!"
    break
  fi
  sleep 0.2
done

# إيقاف الخادم
kill $SERVER_PID
```

### تشغيل كـ systemd Service (Linux)

```bash
# إنشاء ملف الخدمة
sudo tee /etc/systemd/system/sqlite-server.service << 'EOF'
[Unit]
Description=SQLite PostgreSQL Wire Protocol Server
After=network.target

[Service]
Type=simple
User=www-data
Group=www-data
WorkingDirectory=/opt/sqlite-server
ExecStart=/opt/sqlite-server/sqlite-server --addr 0.0.0.0:5432 --wal -- /var/data/app.db
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sqlite-server

[Install]
WantedBy=multi-user.target
EOF

# تفعيل وتشغيل
sudo systemctl daemon-reload
sudo systemctl enable sqlite-server
sudo systemctl start sqlite-server

# التحقق من الحالة
sudo systemctl status sqlite-server

# مشاهدة السجلات
sudo journalctl -u sqlite-server -f

# إيقاف
sudo systemctl stop sqlite-server
```

### تشغيل للاختبار الفوري مع الاتصال

```bash
# نافذة 1: تشغيل الخادم
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- /tmp/test.db

# نافذة 2: اختبار الاتصال بـ psql
psql -h 127.0.0.1 -p 5432 -U test -d test -c "SELECT version();"
# الناتج: version
# PostgreSQL 14.5 on SQLite ...
```

---

## الاتصال بالخادم من مختلف الأدوات

### psql

```bash
# الاتصال الأساسي
psql -h localhost -p 5432 -U test -d test

# تنفيذ أمر واحد
psql -h localhost -p 5432 -U test -c "CREATE TABLE test (id SERIAL, name TEXT);"
psql -h localhost -p 5432 -U test -c "INSERT INTO test (name) VALUES ('hello');"
psql -h localhost -p 5432 -U test -c "SELECT * FROM test;"

# بكلمة مرور
PGPASSWORD=mypassword psql -h localhost -p 5432 -U myuser -d mydb

# باستخدام Connection String
psql "postgresql://test:test@localhost:5432/test"
```

### PowerShell مع psql

```powershell
# تنفيذ أوامر SQL من PowerShell
$env:PGPASSWORD = "test"
psql -h localhost -p 5432 -U test -c "SELECT 1 AS test_connection;"

# تنفيذ ملف SQL
psql -h localhost -p 5432 -U test -f schema.sql
```

### Python (psycopg2)

```python
import psycopg2

conn = psycopg2.connect(
    host="localhost",
    port=5432,
    user="test",
    password="test",    # أي قيمة مع --no-auth
    dbname="test"
)

cur = conn.cursor()
cur.execute("CREATE TABLE IF NOT EXISTS items (id SERIAL PRIMARY KEY, name TEXT)")
cur.execute("INSERT INTO items (name) VALUES (%s) RETURNING id", ("widget",))
row_id = cur.fetchone()[0]
conn.commit()

cur.execute("SELECT * FROM items")
print(cur.fetchall())

cur.close()
conn.close()
```

### Node.js (pg)

```javascript
const { Pool } = require('pg');

const pool = new Pool({
  host: 'localhost',
  port: 5432,
  user: 'test',
  password: 'test',
  database: 'test'
});

async function main() {
  const client = await pool.connect();
  try {
    await client.query(`
      CREATE TABLE IF NOT EXISTS products (
        id SERIAL PRIMARY KEY,
        name TEXT NOT NULL,
        price REAL
      )
    `);
    
    const result = await client.query(
      'INSERT INTO products (name, price) VALUES ($1, $2) RETURNING id',
      ['Widget', 9.99]
    );
    console.log('Inserted ID:', result.rows[0].id);
    
    const rows = await client.query('SELECT * FROM products');
    console.log('Products:', rows.rows);
  } finally {
    client.release();
  }
  await pool.end();
}

main().catch(console.error);
```

### Go (pgx)

```go
package main

import (
    "context"
    "fmt"
    "github.com/jackc/pgx/v5"
)

func main() {
    conn, err := pgx.Connect(context.Background(),
        "postgresql://test:test@localhost:5432/test")
    if err != nil {
        panic(err)
    }
    defer conn.Close(context.Background())

    var greeting string
    err = conn.QueryRow(context.Background(),
        "SELECT 'Hello from sqlite-server!'").Scan(&greeting)
    if err != nil {
        panic(err)
    }
    fmt.Println(greeting)
}
```

### DBeaver (إعدادات الاتصال)

```
نوع الاتصال : PostgreSQL
Host        : localhost
Port        : 5432
Database    : test   (أو أي اسم)
Username    : test   (أو أي اسم مع --no-auth)
Password    : test   (أو أي قيمة مع --no-auth)
SSL         : Disable (ما لم يكن --ssl-cert محدداً)

Advanced → Server time zone: UTC
```

---

## الإشارات (Signals) وإيقاف التشغيل

```bash
# إيقاف تشغيل نظيف (Graceful Shutdown)
# إرسال SIGTERM أو SIGINT (Ctrl+C)
kill -SIGTERM $SERVER_PID
# أو
kill -SIGINT $SERVER_PID

# الخادم يطبع:
# received signal terminated — shutting down gracefully…
# [info] sqlite-server all connections closed
```

### PowerShell — إيقاف نظيف

```powershell
# إرسال Ctrl+C لـ process
$process = Get-Process sqlite-server -ErrorAction SilentlyContinue
if ($process) {
    $process.CloseMainWindow() | Out-Null
    $process.WaitForExit(5000)
    if (-not $process.HasExited) {
        $process.Kill()
    }
    Write-Host "Server stopped."
}
```

---

## متغيرات البيئة المفيدة

```bash
# تجاوز عنوان الخادم في اختبارات التكامل
export SQLITE_SERVER_ADDR="localhost:15432"
export SQLITE_SERVER_DSN="host=localhost port=15432 user=test password=test dbname=test sslmode=disable"

# تعطيل GOSUM للشبكات المقيّدة
export GONOSUMDB="*"
export GOFLAGS="-mod=mod"

# مسار Go إذا لم يكن في PATH
export PATH=$PATH:/usr/local/go/bin
```

### PowerShell

```powershell
$env:SQLITE_SERVER_ADDR = "localhost:15432"
$env:GONOSUMDB = "*"
$env:PATH = "$env:PATH;C:\Go\bin"
```
