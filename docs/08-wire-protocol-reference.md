# PostgreSQL Wire Protocol v3 — المرجع الكامل
## كيف يُنفَّذ البروتوكول داخل sqlite-server

---

## مقدمة

بروتوكول PostgreSQL Wire v3 هو بروتوكول ثنائي يعمل فوق TCP.  
sqlite-server ينفذه **كاملاً من الصفر** في `internal/wire/`.

---

## 1. بنية الرسائل

### رسائل الـ Frontend (من العميل)

كل رسالة (بعد المصافحة) لها هذا الشكل:

```
┌─────────────────────────────────────────────────┐
│  Byte 1: message type (char)                     │
│  Int32:  message length (يشمل الـ 4 bytes نفسها) │
│  Body:   (length - 4) bytes                      │
└─────────────────────────────────────────────────┘
```

### رسائل الـ Backend (من الخادم)

نفس البنية.

### رسالة Startup (استثناء — بدون type byte)

```
┌─────────────────────────────────────────────────┐
│  Int32:  total length                            │
│  Int32:  protocol version (196608 = v3.0)        │
│  String: "user\0username\0database\0dbname\0\0"  │
└─────────────────────────────────────────────────┘
```

---

## 2. جميع رسائل Frontend → الكود المُعالج

| Type | Char | الوصف | المعالج |
|------|------|-------|---------|
| Query | `Q` | Simple Query | `handleSimpleQuery()` |
| Parse | `P` | Extended: تجهيز statement | `handleParse()` |
| Bind | `B` | Extended: ربط parameters | `handleBind()` |
| Describe | `D` | Extended: وصف statement/portal | `handleDescribe()` |
| Execute | `E` | Extended: تنفيذ portal | `handleExecute()` |
| Close | `C` | إغلاق statement/portal | `handleClose()` |
| Sync | `S` | مزامنة (end of pipeline) | `handleSync()` |
| Flush | `H` | تدفيق البيانات المعلقة | `handleFlush()` |
| CopyData | `d` | COPY data chunk | `handleCopyData()` |
| CopyDone | `c` | COPY انتهى | `handleCopyDone()` |
| CopyFail | `f` | COPY فشل | `handleCopyFail()` |
| Terminate | `X` | إنهاء الجلسة | `handleTerminate()` |
| FunctionCall | `F` | (legacy) غير مدعوم | `handleFunctionCall()` |

---

## 3. جميع رسائل Backend → الكود المُرسِل

| Type | Char | الوصف | الملف |
|------|------|-------|-------|
| AuthenticationOk | `R` + 0 | تمت المصادقة | `auth.go` |
| AuthenticationCleartextPassword | `R` + 3 | اطلب كلمة مرور | `auth.go` |
| ParameterStatus | `S` | إعدادات الجلسة | `startup.go` |
| BackendKeyData | `K` | PID + SecretKey | `startup.go` |
| ReadyForQuery | `Z` | جاهز + TxStatus | `ready.go` |
| RowDescription | `T` | أسماء وأنواع الأعمدة | `messages.go` |
| DataRow | `D` | صف من البيانات | `messages.go` |
| CommandComplete | `C` | انتهى الأمر: "SELECT 5" | `messages.go` |
| ErrorResponse | `E` | رسالة خطأ منسقة | `error.go` |
| NoticeResponse | `N` | تحذير | `error.go` |
| ParseComplete | `1` | Parse تمت | `extended_query.go` |
| BindComplete | `2` | Bind تم | `extended_query.go` |
| CloseComplete | `3` | Close تم | `extended_query.go` |
| NoData | `n` | لا توجد أعمدة | `extended_query.go` |
| ParameterDescription | `t` | وصف parameters | `extended_query.go` |
| EmptyQueryResponse | `I` | استعلام فارغ | `simple_query.go` |

---

## 4. تنسيق RowDescription ('T')

```
'T' | Int32(length) | Int16(numCols)
  للعمود:
    String(name)\0
    Int32(tableOID)      = 0 إذا لم يكن من جدول
    Int16(attrNum)       = 0 إذا لم يكن عموداً فيزيائياً
    Int32(typeOID)       = مثل 23 لـ INTEGER
    Int16(typeSize)      = -1 إذا كان متغير الطول
    Int32(typeMod)       = -1 عادةً
    Int16(format)        = 0 (text) أو 1 (binary)
```

**مثال بالكود**:
```go
// messages.go
func (s *Session) sendRowDescription(cols []pgproto.ColumnDesc) error {
    var body []byte
    // 2 bytes: عدد الأعمدة
    body = appendInt16(body, int16(len(cols)))
    
    for _, col := range cols {
        body = append(body, []byte(col.Name)...)
        body = append(body, 0) // null terminator
        body = appendInt32(body, int32(col.TableOID))
        body = appendInt16(body, col.AttrNum)
        body = appendInt32(body, int32(col.TypeOID))
        body = appendInt16(body, col.TypeSize)
        body = appendInt32(body, col.TypeMod)
        body = appendInt16(body, col.Format)
    }
    
    return s.writeMessage('T', body)
}
```

---

## 5. تنسيق DataRow ('D')

```
'D' | Int32(length) | Int16(numCols)
  لكل قيمة:
    Int32(valueLength)   = -1 إذا كانت NULL
    Bytes(value)         = القيمة كنص (للـ text format)
```

**مثال بالكود**:
```go
// messages.go
func (s *Session) sendDataRow(row []interface{}) error {
    var body []byte
    body = appendInt16(body, int16(len(row)))
    
    for _, val := range row {
        if val == nil {
            body = appendInt32(body, -1) // NULL
            continue
        }
        str := fmt.Sprintf("%v", val)
        body = appendInt32(body, int32(len(str)))
        body = append(body, []byte(str)...)
    }
    
    return s.writeMessage('D', body)
}
```

---

## 6. تنسيق ErrorResponse ('E')

```
'E' | Int32(length)
  لكل حقل:
    Byte(fieldType)  = 'S' severity, 'C' code, 'M' message, 'D' detail...
    String(value)\0
  Byte(0)  = نهاية الحقول
```

**حقول SQLSTATE المستخدمة**:

| Code | المعنى |
|------|--------|
| `08P01` | protocol_violation |
| `23505` | unique_violation |
| `23502` | not_null_violation |
| `23503` | foreign_key_violation |
| `42P01` | undefined_table |
| `42703` | undefined_column |
| `53300` | too_many_connections |
| `55P03` | lock_not_available |
| `57014` | query_canceled |
| `58030` | io_error |
| `0A000` | feature_not_supported |
| `28P01` | invalid_password |

---

## 7. تسلسل Extended Query Protocol بالتفصيل

```
Parse message ('P'):
  Byte(0) = message type
  Int32   = length
  String\0 = statement name ("" = unnamed)
  String\0 = query string
  Int16   = number of parameter type OIDs
  [Int32 × N] = parameter type OIDs (0 = unspecified)

Bind message ('B'):
  Byte(0) = message type
  Int32   = length
  String\0 = destination portal ("" = unnamed)
  String\0 = source statement ("" = unnamed)
  Int16   = number of parameter format codes
  [Int16 × N] = format codes (0=text, 1=binary)
  Int16   = number of parameter values
  [Int32 + Bytes] × N = parameter values
  Int16   = number of result format codes
  [Int16 × N] = result format codes

Execute message ('E'):
  Byte(0) = message type
  Int32   = length
  String\0 = portal name ("" = unnamed)
  Int32   = max rows (0 = no limit)
```

---

## 8. Transaction State Machine

```
        ┌─────────────────────────────────────────────────────┐
        │                    TxIdle ('I')                      │
        │            لا توجد معاملة نشطة                        │
        └──────┬─────────────────────────────────┬────────────┘
               │ BEGIN / START TRANSACTION        │
               ▼                                  │
        ┌──────────────────────────┐              │
        │      TxOpen ('T')        │              │
        │   معاملة نشطة            │              │
        └──────┬───────────────────┘              │
               │                                  │
        ┌──────▼──────┐   ┌────────────┐          │
        │  COMMIT     │   │  ROLLBACK  │          │
        │             │   │            │          │
        └──────┬──────┘   └─────┬──────┘          │
               │                │                 │
               └────────────────┘                 │
                        │                         │
                        ▼                         │
               TxIdle ◄──────────────────────────┘
               
               خطأ SQL في معاملة مفتوحة
               ──────────────────────────►  TxFailed ('E')
               يجب ROLLBACK للعودة لـ TxIdle
```

---

## 9. SSLRequest Protocol

```
Client:  Int32(8) | Int32(80877103)   ← SSLRequest

Server يدعم TLS:
  Server: 'S'
  [TLS Handshake يبدأ هنا]
  Client يعيد إرسال Startup message عبر TLS

Server لا يدعم TLS:
  Server: 'N'
  Client: يعيد إرسال Startup message بدون TLS
```

---

## 10. CancelRequest Protocol

```
Client: Int32(16) | Int32(80877102) | Int32(PID) | Int32(SecretKey)

ملاحظة:
- هذا يُرسَل على اتصال TCP جديد (وليس الاتصال الحالي)
- الخادم لا يرد على هذه الرسالة — يُغلق الاتصال فوراً
- sqlite-server: يتلقى الرسالة لكن SQLite لا تدعم إلغاء الاستعلامات
```
