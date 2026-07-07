# الأداء والإنتاج — Performance & Production Guide

---

## 1. ضبط الأداء

### إعدادات SQLite الأمثل

```bash
# للإنتاج — أفضل ضبط
./sqlite-server \
  --wal \                      # WAL mode: كاتب + قراء متزامنون
  --max-conn 200 \             # حسب حجم التطبيق
  --busy-timeout 10s \         # انتظر 10 ثوانٍ قبل SQLITE_BUSY
  --addr 0.0.0.0:5432 \
  -- production.db
```

**ما يحدث داخلياً مع WAL=true**:
```
_journal_mode=WAL          ← Write-Ahead Logging
_synchronous=NORMAL        ← أسرع من FULL مع أمان كافٍ
_cache_size=-65536         ← 64 MiB page cache
_mmap_size=268435456       ← 256 MiB memory-mapped I/O
_temp_store=memory         ← الجداول المؤقتة في الذاكرة
_wal_autocheckpoint=1000   ← checkpoint تلقائي كل 1000 frame
```

### Read-heavy Workloads

```bash
# قراءات مكثفة: زيادة connections
./sqlite-server --max-conn 500 --wal -- readonly_data.db

# للقراءة فقط (أسرع: SQLite لا يقفل للكتابة)
./sqlite-server --read-only --max-conn 1000 -- data.db
```

### Write-heavy Workloads

```bash
# الكتابات المكثفة تستفيد من:
# 1. WAL mode (الافتراضي)
# 2. تجميع الكتابات في transactions كبيرة

# مثال: بدلاً من 1000 INSERT منفصل:
-- ❌ بطيء
INSERT INTO t VALUES (1); INSERT INTO t VALUES (2); ...

-- ✅ سريع جداً
BEGIN;
INSERT INTO t VALUES (1);
INSERT INTO t VALUES (2);
...
COMMIT;
```

---

## 2. قياس الأداء

### اختبار بسيط من psql

```bash
# قياس وقت استعلام
time psql -h localhost -p 5432 -U test -c "SELECT COUNT(*) FROM large_table"

# اختبار 1000 استعلام متتالي
for i in {1..1000}; do
  psql -h localhost -p 5432 -U test -c "SELECT 1" > /dev/null
done
```

### اختبار التزامن من Go

```go
package main

import (
    "database/sql"
    "fmt"
    "sync"
    "time"
    _ "github.com/lib/pq"
)

func main() {
    db, _ := sql.Open("postgres",
        "host=localhost port=5432 user=test password=test dbname=test sslmode=disable")
    db.SetMaxOpenConns(50)
    db.SetMaxIdleConns(20)
    
    const numWorkers = 50
    const queriesPerWorker = 1000
    
    start := time.Now()
    var wg sync.WaitGroup
    
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer wg.Done()
            for j := 0; j < queriesPerWorker; j++ {
                var n int
                db.QueryRow("SELECT $1::int", j).Scan(&n)
            }
        }(i)
    }
    
    wg.Wait()
    elapsed := time.Since(start)
    total := numWorkers * queriesPerWorker
    fmt.Printf("Total: %d queries in %v\n", total, elapsed)
    fmt.Printf("QPS: %.0f\n", float64(total)/elapsed.Seconds())
}
```

### pgbench (PostgreSQL benchmark tool)

```bash
# تهيئة
pgbench -h localhost -p 5432 -U test -i -s 10 test
# قد يفشل جزئياً (pgbench يستخدم syntax خاص بـ PG)

# اختبار custom
pgbench -h localhost -p 5432 -U test -c 10 -t 1000 \
  --file=- test << 'EOF'
SELECT 1;
EOF
```

---

## 3. مراقبة الإنتاج (Monitoring)

### فحص صحة الخادم (Health Check)

```bash
# Bash
check_server() {
    if psql -h localhost -p 5432 -U test -c "SELECT 1" > /dev/null 2>&1; then
        echo "✅ sqlite-server is healthy"
        return 0
    else
        echo "❌ sqlite-server is DOWN"
        return 1
    fi
}

# ضمن Docker/k8s healthcheck
# pg_isready يعمل مع sqlite-server
pg_isready -h localhost -p 5432
```

### PowerShell Health Check

```powershell
function Test-SqliteServer {
    param($Host = "localhost", $Port = 5432)
    
    $tcpClient = New-Object System.Net.Sockets.TcpClient
    try {
        $tcpClient.Connect($Host, $Port)
        Write-Host "✅ sqlite-server is responding on $Host`:$Port"
        return $true
    } catch {
        Write-Host "❌ Cannot connect to $Host`:$Port"
        return $false
    } finally {
        $tcpClient.Close()
    }
}

Test-SqliteServer
```

### مراقبة حجم قاعدة البيانات

```bash
# حجم الملف
ls -lh myapp.db myapp.db-wal 2>/dev/null

# معلومات داخلية عبر PRAGMA
sqlite3 myapp.db "
PRAGMA page_count;      -- عدد الصفحات
PRAGMA page_size;       -- حجم الصفحة (عادة 4096)
PRAGMA wal_checkpoint;  -- checkpoint يدوي
PRAGMA integrity_check; -- فحص سلامة قاعدة البيانات
"
```

---

## 4. النشر في الإنتاج

### Docker

```dockerfile
# Dockerfile (الموجود في المشروع)
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w" \
    -o sqlite-server ./cmd/sqlite-server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /build/sqlite-server /usr/local/bin/
VOLUME ["/data"]
EXPOSE 5432
ENTRYPOINT ["sqlite-server"]
CMD ["--no-auth", "--", "/data/app.db"]
```

```bash
# بناء وتشغيل
docker build -t sqlite-server .
docker run -d \
  -p 5432:5432 \
  -v /host/data:/data \
  --name sqlite-server \
  sqlite-server \
  --wal --addr 0.0.0.0:5432 -- /data/app.db

# التحقق
docker logs sqlite-server
docker exec sqlite-server sqlite-server version
```

### Kubernetes

```yaml
# k8s-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sqlite-server
spec:
  replicas: 1  # ملف واحد فقط — لا يمكن تعدد replicas
  selector:
    matchLabels:
      app: sqlite-server
  template:
    metadata:
      labels:
        app: sqlite-server
    spec:
      containers:
      - name: sqlite-server
        image: sqlite-server:latest
        args: ["--wal", "--addr", "0.0.0.0:5432", "--", "/data/app.db"]
        ports:
        - containerPort: 5432
        volumeMounts:
        - name: data
          mountPath: /data
        livenessProbe:
          tcpSocket:
            port: 5432
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "1Gi"
            cpu: "1000m"
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: sqlite-data-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: sqlite-server
spec:
  selector:
    app: sqlite-server
  ports:
  - port: 5432
    targetPort: 5432
  type: ClusterIP
```

### systemd (Linux)

```ini
# /etc/systemd/system/sqlite-server.service
[Unit]
Description=SQLite PostgreSQL Wire Protocol Server
Documentation=https://github.com/sqlite-server/sqlite-server
After=network.target
Wants=network.target

[Service]
Type=simple
User=sqlite-server
Group=sqlite-server
WorkingDirectory=/opt/sqlite-server
ExecStart=/opt/sqlite-server/sqlite-server \
    --addr 0.0.0.0:5432 \
    --wal \
    --max-conn 200 \
    --busy-timeout 10s \
    --log-level info \
    -- /var/data/app.db
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30

# Security
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=/var/data

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sqlite-server

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd -r -s /bin/false sqlite-server
sudo mkdir -p /var/data
sudo chown sqlite-server:sqlite-server /var/data
sudo cp sqlite-server /opt/sqlite-server/
sudo systemctl daemon-reload
sudo systemctl enable --now sqlite-server
sudo systemctl status sqlite-server
```

---

## 5. النسخ الاحتياطي والاستعادة

```bash
# ── النسخ الاحتياطي ──────────────────────────────────

# الطريقة 1: نسخ الملف مباشرة (آمن مع WAL mode)
cp /var/data/app.db /backup/app_$(date +%Y%m%d_%H%M%S).db

# الطريقة 2: SQLite backup command (أكثر أماناً)
sqlite3 /var/data/app.db ".backup /backup/app_$(date +%Y%m%d).db"

# الطريقة 3: Dump كـ SQL
sqlite3 /var/data/app.db .dump > /backup/app_$(date +%Y%m%d).sql

# ── الاستعادة ──────────────────────────────────────────

# من ملف .db
cp /backup/app_20260707.db /var/data/app.db
./sqlite-server --wal -- /var/data/app.db

# من SQL dump
sqlite3 /var/data/app_restored.db < /backup/app_20260707.sql
./sqlite-server --wal -- /var/data/app_restored.db

# ── نسخ احتياطي تلقائي (cron) ────────────────────────

# في crontab:
0 2 * * * sqlite3 /var/data/app.db ".backup /backup/app_$(date +\%Y\%m\%d).db" && \
          find /backup -name "app_*.db" -mtime +30 -delete
```

---

## 6. الأمان في الإنتاج

```bash
# 1. استخدم TLS دائماً في الإنتاج
./sqlite-server \
  --ssl-cert /etc/ssl/certs/server.crt \
  --ssl-key /etc/ssl/private/server.key \
  -- /var/data/app.db

# 2. قيّد الوصول بجدار الحماية
ufw allow from 10.0.0.0/8 to any port 5432  # شبكة داخلية فقط
ufw deny 5432  # حجب من الخارج

# 3. استخدم مستخدم منخفض الصلاحيات
# (موضح في قسم systemd أعلاه)

# 4. شهادة TLS من Let's Encrypt
certbot certonly --standalone -d db.example.com
./sqlite-server \
  --ssl-cert /etc/letsencrypt/live/db.example.com/fullchain.pem \
  --ssl-key /etc/letsencrypt/live/db.example.com/privkey.pem \
  -- /var/data/app.db
```
