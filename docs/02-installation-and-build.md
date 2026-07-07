# التثبيت والبناء — Installation & Build Guide
## أوامر PowerShell / Bash كاملة

---

## المتطلبات الأساسية

| الأداة | الإصدار الأدنى | الملاحظة |
|--------|---------------|----------|
| Go     | 1.22+         | [https://go.dev/dl/](https://go.dev/dl/) |
| Git    | أي إصدار      | لاستنساخ المشروع |
| make   | اختياري       | للاختصارات |

> ⚠️ **ملاحظة مهمة**: المشروع يستخدم `modernc.org/sqlite` الذي يُحوّل مصدر SQLite C إلى Go.  
> أول عملية بناء تستغرق **2–5 دقائق** لتجميع هذا الكود المُولَّد.  
> الإنشاءات اللاحقة تعمل من الكاش وتستغرق ثوانٍ فقط.

---

## الاستنساخ والبناء الأساسي

### PowerShell (Windows)

```powershell
# 1. استنساخ المشروع
git clone https://github.com/sqlite-server/sqlite-server
cd sqlite-server

# 2. تنزيل التبعيات (أول مرة فقط)
go mod download

# 3. بناء الملف التنفيذي لـ Windows
$env:CGO_ENABLED = "0"
go build -o sqlite-server.exe .\cmd\sqlite-server

# 4. التحقق من البناء
.\sqlite-server.exe version
# الناتج: sqlite-server dev

# 5. تشغيل سريع (بدون مصادقة)
.\sqlite-server.exe --no-auth -- myapp.db
```

### Bash (Linux / macOS)

```bash
# 1. استنساخ المشروع
git clone https://github.com/sqlite-server/sqlite-server
cd sqlite-server

# 2. تنزيل التبعيات
go mod download

# 3. بناء الملف التنفيذي
CGO_ENABLED=0 go build -o sqlite-server ./cmd/sqlite-server

# 4. التحقق
./sqlite-server version

# 5. تشغيل
./sqlite-server --no-auth -- myapp.db
```

---

## بناء الملف التنفيذي .exe (Windows)

### من Linux/macOS (Cross-Compilation)

```bash
# بناء ملف .exe لـ Windows 64-bit من Linux
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -o sqlite-server-windows-amd64.exe ./cmd/sqlite-server

# بناء ملف .exe لـ Windows ARM64
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 \
  go build -o sqlite-server-windows-arm64.exe ./cmd/sqlite-server

ls -lh sqlite-server-windows-*.exe
```

### من Windows (PowerShell)

```powershell
# بناء لـ Windows x64 (الجهاز الحالي)
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -o sqlite-server.exe .\cmd\sqlite-server

# بناء بمعلومات الإصدار
$VERSION = git describe --tags --always 2>$null
if (-not $VERSION) { $VERSION = "1.0.0" }
$COMMIT = git rev-parse --short HEAD 2>$null
if (-not $COMMIT) { $COMMIT = "unknown" }
$BUILD_DATE = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")

go build `
  -ldflags "-s -w -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$BUILD_DATE" `
  -trimpath `
  -o sqlite-server.exe `
  .\cmd\sqlite-server

# التحقق من الملف
.\sqlite-server.exe version
Get-Item sqlite-server.exe | Select-Object Name, Length
```

### من Windows (cmd.exe)

```cmd
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -o sqlite-server.exe .\cmd\sqlite-server
sqlite-server.exe version
```

---

## بناء لجميع المنصات (Cross-Platform)

### باستخدام Makefile (Linux/macOS)

```bash
# بناء لجميع المنصات دفعة واحدة
make build-all

# الناتج في مجلد dist/
# dist/sqlite-server-linux-amd64
# dist/sqlite-server-linux-arm64
# dist/sqlite-server-darwin-amd64
# dist/sqlite-server-darwin-arm64
# dist/sqlite-server-windows-amd64.exe
# dist/sqlite-server-freebsd-amd64
```

### يدوياً (Bash)

```bash
BINARY="sqlite-server"
MODULE="./cmd/sqlite-server"

# إنشاء مجلد الناتج
mkdir -p dist

# Linux AMD64
GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build -o dist/${BINARY}-linux-amd64   ${MODULE}

# Linux ARM64 (Raspberry Pi, AWS Graviton)
GOOS=linux   GOARCH=arm64  CGO_ENABLED=0 go build -o dist/${BINARY}-linux-arm64   ${MODULE}

# macOS Intel
GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 go build -o dist/${BINARY}-darwin-amd64  ${MODULE}

# macOS Apple Silicon (M1/M2/M3)
GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 go build -o dist/${BINARY}-darwin-arm64  ${MODULE}

# Windows x64
GOOS=windows GOARCH=amd64  CGO_ENABLED=0 go build -o dist/${BINARY}-windows-amd64.exe ${MODULE}

# FreeBSD
GOOS=freebsd GOARCH=amd64  CGO_ENABLED=0 go build -o dist/${BINARY}-freebsd-amd64 ${MODULE}

echo "=== الملفات المُنشأة ==="
ls -lh dist/
```

### يدوياً (PowerShell على Windows)

```powershell
$BINARY = "sqlite-server"
$MODULE = ".\cmd\sqlite-server"

New-Item -ItemType Directory -Force -Path dist | Out-Null

# Linux AMD64
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -o "dist\${BINARY}-linux-amd64" $MODULE

# Windows AMD64
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -o "dist\${BINARY}-windows-amd64.exe" $MODULE

# macOS ARM64
$env:GOOS="darwin"; $env:GOARCH="arm64"; $env:CGO_ENABLED="0"
go build -o "dist\${BINARY}-darwin-arm64" $MODULE

# إعادة الإعدادات لحالة الجهاز
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

Get-ChildItem dist\ | Format-Table Name, Length
```

---

## بناء Docker Image

### PowerShell / Bash

```bash
# بناء صورة Docker
docker build -t sqlite-server:latest .

# تشغيل من Docker
docker run -d \
  --name sqlite-server \
  -p 5432:5432 \
  -v $(pwd)/data:/data \
  sqlite-server:latest \
  --no-auth -- /data/myapp.db

# التحقق من الاتصال
psql -h localhost -p 5432 -U test -c "SELECT 1"
```

### docker-compose

```bash
# باستخدام ملف configs/docker-compose.yml
docker-compose -f configs/docker-compose.yml up -d

# مراقبة السجلات
docker-compose -f configs/docker-compose.yml logs -f

# إيقاف
docker-compose -f configs/docker-compose.yml down
```

---

## الفرق بين أوضاع البناء

### Debug Build (للتطوير)

```bash
# بدون تقليص — يسمح بـ debugging أفضل
go build -o sqlite-server ./cmd/sqlite-server
```

### Release Build (للإنتاج)

```bash
# -s: حذف symbol table
# -w: حذف DWARF debug info
# -trimpath: حذف مسارات الجهاز المحلي من الملف
CGO_ENABLED=0 go build \
  -ldflags "-s -w -X main.Version=v1.0.0" \
  -trimpath \
  -o sqlite-server \
  ./cmd/sqlite-server

# مقارنة الأحجام
ls -lh sqlite-server  # الإنتاج ~16MB
```

### مع رقم الإصدار الكامل

```bash
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

CGO_ENABLED=0 go build \
  -ldflags "-s -w \
    -X main.Version=${VERSION} \
    -X main.Commit=${COMMIT} \
    -X main.BuildDate=${BUILD_DATE}" \
  -trimpath \
  -o sqlite-server \
  ./cmd/sqlite-server

./sqlite-server version
# sqlite-server v1.2.3-abc1234 (built 2026-07-07T11:00:00Z)
```

---

## أوامر go mod

```bash
# تنزيل التبعيات بدون شبكة
go mod download

# تنظيف التبعيات غير المستخدمة
GONOSUMDB="*" go mod tidy

# التحقق من سلامة الحزم
go mod verify
# الناتج: all modules verified

# عرض قائمة التبعيات
go list -m all

# التحقق من التبعيات مع الإصدارات
go mod graph
```

---

## استكشاف الأخطاء الشائعة عند البناء

### خطأ: go: command not found

```bash
# Linux — إيجاد مسار Go
find /usr /home /opt -name "go" -type f 2>/dev/null | grep bin/go
export PATH=$PATH:/usr/local/go/bin

# إضافة دائمة لـ ~/.bashrc
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

### خطأ: go.sum mismatch

```bash
# حذف go.sum وإعادة توليده
rm go.sum
GONOSUMDB="*" GOFLAGS="-mod=mod" go mod tidy
```

### خطأ: cannot find package

```bash
# التحقق من مسار الموديول
cat go.mod | head -3
# يجب أن يظهر: module github.com/sqlite-server/sqlite-server

# تنزيل جميع التبعيات مجدداً
go clean -modcache
go mod download
```

### البناء بطيء جداً (أول مرة)

```
البناء الأول يستغرق 3-5 دقائق — هذا طبيعي تماماً.
modernc.org/sqlite يُترجم ~150,000 سطر من C إلى Go في أول مرة.
التالية تعمل من الكاش في أقل من 10 ثوانٍ.
```
