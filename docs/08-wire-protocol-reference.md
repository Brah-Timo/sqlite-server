# PostgreSQL Wire Protocol v3 Reference

This document describes the PostgreSQL Frontend/Backend Protocol version 3.0 as
implemented by sqlite-server. It covers message formats, byte layouts, state
machines, and deviations from the official PostgreSQL specification.

---

## Table of Contents

1. [Protocol Overview](#protocol-overview)
2. [Connection Lifecycle](#connection-lifecycle)
3. [Message Format](#message-format)
   - [Frontend → Backend (Client Messages)](#frontend--backend-client-messages)
   - [Backend → Frontend (Server Messages)](#backend--frontend-server-messages)
4. [Startup Phase](#startup-phase)
   - [SSLRequest](#sslrequest)
   - [StartupMessage](#startupmessage)
   - [Authentication Messages](#authentication-messages)
   - [ParameterStatus](#parameterstatus)
   - [BackendKeyData](#backendkeydata)
5. [Simple Query Protocol](#simple-query-protocol)
   - [Query (`Q`)](#query-q)
   - [RowDescription (`T`)](#rowdescription-t)
   - [DataRow (`D`)](#datarow-d)
   - [CommandComplete (`C`)](#commandcomplete-c)
   - [EmptyQueryResponse (`I`)](#emptyqueryresponse-i)
   - [ReadyForQuery (`Z`)](#readyforquery-z)
6. [Extended Query Protocol](#extended-query-protocol)
   - [Parse (`P`)](#parse-p)
   - [ParseComplete (`1`)](#parsecomplete-1)
   - [Bind (`B`)](#bind-b)
   - [BindComplete (`2`)](#bindcomplete-2)
   - [Execute (`E`)](#execute-e)
   - [Describe (`D`)](#describe-d)
   - [ParameterDescription (`t`)](#parameterdescription-t)
   - [Sync (`S`)](#sync-s)
   - [Close (`C`)](#close-c)
   - [CloseComplete (`3`)](#closecomplete-3)
7. [Error and Notice Messages](#error-and-notice-messages)
   - [ErrorResponse (`E`)](#errorresponse-e)
   - [SQLSTATE Codes](#sqlstate-codes)
8. [Copy Protocol](#copy-protocol)
9. [OID Constants](#oid-constants)
10. [State Machine](#state-machine)
11. [Implementation Notes](#implementation-notes)

---

## Protocol Overview

The PostgreSQL Wire Protocol v3 is a **message-based, length-prefixed binary
protocol** over TCP. Each message has:

1. A **type byte** (1 byte, ASCII character)
2. A **length** (4 bytes, big-endian int32, includes the length field itself)
3. A **payload** (variable length)

The startup phase uses a slightly different format (no type byte).

All multi-byte integer fields use **network byte order (big-endian)**.

All strings are **null-terminated** (`\x00`).

---

## Connection Lifecycle

```
TCP CONNECT
    │
    ▼
[Optional SSLRequest / SSLResponse]
    │
    ▼
StartupMessage  ──────────────────────────► server
    │
    ▼
Authentication exchange
    │
    ▼
ParameterStatus (N messages)  ◄──────────── server
BackendKeyData                ◄──────────── server
ReadyForQuery ('I')           ◄──────────── server
    │
    ┌─────────────────────────────────────────┐
    │           Command Loop                  │
    │                                         │
    │  Simple Query:                          │
    │    Query ('Q') ──────────────────────►  │
    │    RowDescription + DataRow(s) ◄──────  │
    │    CommandComplete + ReadyForQuery ◄──  │
    │                                         │
    │  Extended Query:                        │
    │    Parse ('P') ──────────────────────►  │
    │    ParseComplete ◄───────────────────   │
    │    Bind ('B') ───────────────────────►  │
    │    BindComplete ◄────────────────────   │
    │    Execute ('E') ────────────────────►  │
    │    DataRow(s) ◄──────────────────────   │
    │    CommandComplete ◄──────────────────  │
    │    Sync ('S') ───────────────────────►  │
    │    ReadyForQuery ◄────────────────────  │
    └─────────────────────────────────────────┘
    │
    ▼
Terminate ('X') ──────────────────────────► server
TCP CLOSE
```

---

## Message Format

### Frontend → Backend (Client Messages)

| Byte | Name | Description |
|------|------|-------------|
| `Q` | Query | Simple query string |
| `P` | Parse | Prepare a statement |
| `B` | Bind | Bind parameters to a prepared statement |
| `E` | Execute | Execute a portal |
| `D` | Describe | Describe a prepared statement or portal |
| `S` | Sync | Synchronise after extended query |
| `C` | Close | Close a prepared statement or portal |
| `H` | Flush | Flush output buffer (advisory) |
| `X` | Terminate | Close connection |
| `p` | Password | Password/authentication data |
| `d` | CopyData | Data in a COPY operation |
| `c` | CopyDone | End of COPY data |
| `f` | CopyFail | COPY failed |

### Backend → Frontend (Server Messages)

| Byte | Name | Description |
|------|------|-------------|
| `R` | Authentication | Auth response |
| `K` | BackendKeyData | PID + secret key |
| `S` | ParameterStatus | Server parameter value |
| `Z` | ReadyForQuery | Transaction state |
| `T` | RowDescription | Column metadata |
| `D` | DataRow | One row of data |
| `C` | CommandComplete | Statement tag |
| `I` | EmptyQueryResponse | Empty query string received |
| `E` | ErrorResponse | Error details |
| `N` | NoticeResponse | Warning/notice |
| `1` | ParseComplete | Parse succeeded |
| `2` | BindComplete | Bind succeeded |
| `3` | CloseComplete | Close succeeded |
| `t` | ParameterDescription | Parameter OIDs for prepared stmt |
| `n` | NoData | No row data follows |
| `s` | PortalSuspended | Portal suspended (partial Execute) |
| `G` | CopyInResponse | Server ready to receive COPY data |
| `H` | CopyOutResponse | Server ready to send COPY data |
| `d` | CopyData | COPY data chunk |
| `c` | CopyDone | End of COPY data |

---

## Startup Phase

### SSLRequest

The client may send an `SSLRequest` before the startup message to request TLS.

```
Byte layout:
  Int32(8)       — message length (always 8)
  Int32(80877103) — SSL request code

Server response:
  'S'  — SSL supported, proceed with TLS handshake
  'N'  — SSL not supported, continue without TLS
```

sqlite-server responds `'S'` if `--ssl-cert` / `--ssl-key` are set, otherwise `'N'`.

### StartupMessage

Sent after optional TLS negotiation. **Has no type byte.**

```
Byte layout:
  Int32     — total message length (including this field)
  Int32     — protocol version (196608 = 3.0)
  String    — "user\0" + username + "\0"
  String    — "database\0" + database_name + "\0"
  String    — optional additional parameters ("application_name\0myapp\0" …)
  Byte      — '\0' (terminator)
```

sqlite-server reads `user` and `database` from the startup message. All other
parameters (`application_name`, `client_encoding`, etc.) are acknowledged but ignored.

### Authentication Messages

Type byte: `R`

```
Byte layout:
  Byte1('R')
  Int32     — length
  Int32     — auth type
```

| Auth type | Meaning | sqlite-server |
|-----------|---------|--------------|
| `0` | AuthenticationOk | Sent when `--no-auth` is set |
| `3` | AuthenticationCleartextPassword | Not used |
| `5` | AuthenticationMD5Password | Sent when auth is enabled |

**MD5 password flow:**

Server sends:
```
'R' + Int32(12) + Int32(5) + Byte4(salt)
```

Client responds with a `PasswordMessage` containing
`MD5(MD5(password + username) + salt)` prefixed with `"md5"`.

Server validates and sends `'R' + Int32(8) + Int32(0)` (AuthenticationOk).

### ParameterStatus

Sent after authentication, one message per parameter.

```
Byte1('S')
Int32     — length
String    — parameter name (null-terminated)
String    — parameter value (null-terminated)
```

sqlite-server sends these parameters:

| Parameter | Value |
|-----------|-------|
| `server_version` | `14.0` (compatibility string) |
| `client_encoding` | `UTF8` |
| `server_encoding` | `UTF8` |
| `DateStyle` | `ISO, MDY` |
| `TimeZone` | `UTC` |
| `integer_datetimes` | `on` |
| `standard_conforming_strings` | `on` |

### BackendKeyData

```
Byte1('K')
Int32(12)     — length
Int32         — process ID (goroutine ID or connection counter)
Int32         — secret key (random 32-bit value)
```

The client uses these values to send `CancelRequest` messages. sqlite-server
records them but query cancellation via `CancelRequest` is not yet implemented.

---

## Simple Query Protocol

### Query (`Q`)

```
Byte1('Q')
Int32     — length (4 + len(query) + 1)
String    — SQL query string (null-terminated)
```

The server processes the entire string as a single query (or multiple statements
separated by `;`). For each statement it sends the appropriate response sequence,
followed by `ReadyForQuery`.

### RowDescription (`T`)

Sent before `DataRow` messages for `SELECT` results.

```
Byte1('T')
Int32       — length
Int16       — number of fields (columns)

For each column:
  String    — column name (null-terminated)
  Int32     — table OID (0 if not from a table)
  Int16     — column attribute number (0 if not from a table)
  Int32     — data type OID
  Int16     — data type size (-1 for variable-length)
  Int32     — type modifier (-1 for no modifier)
  Int16     — format code (0 = text, 1 = binary)
```

### DataRow (`D`)

One message per row.

```
Byte1('D')
Int32       — length
Int16       — number of columns

For each column value:
  Int32     — value length (-1 for NULL)
  ByteN     — value bytes (text format: UTF-8 string representation)
```

sqlite-server always sends values in **text format** (format code `0`).
Binary format is not currently supported.

### CommandComplete (`C`)

```
Byte1('C')
Int32       — length
String      — command tag (null-terminated)
```

Command tag examples:

| Statement | Tag |
|-----------|-----|
| `SELECT` returning 5 rows | `SELECT 5` |
| `INSERT` 1 row | `INSERT 0 1` |
| `UPDATE` 3 rows | `UPDATE 3` |
| `DELETE` 2 rows | `DELETE 2` |
| `CREATE TABLE` | `CREATE TABLE` |
| `DROP TABLE` | `DROP TABLE` |
| `BEGIN` | `BEGIN` |
| `COMMIT` | `COMMIT` |
| `ROLLBACK` | `ROLLBACK` |

### EmptyQueryResponse (`I`)

Sent when the client sends an empty query string.

```
Byte1('I')
Int32(4)    — length (no payload)
```

### ReadyForQuery (`Z`)

```
Byte1('Z')
Int32(5)    — length
Byte1       — transaction status indicator
```

| Status byte | Meaning |
|-------------|---------|
| `I` | Idle (no open transaction) |
| `T` | In a transaction block |
| `E` | In a failed transaction (must ROLLBACK) |

---

## Extended Query Protocol

### Parse (`P`)

```
Byte1('P')
Int32         — length
String        — prepared statement name (empty = unnamed)
String        — query string (null-terminated)
Int16         — number of parameter data types
Int32[]       — OID of each parameter (0 = unspecified)
```

### ParseComplete (`1`)

```
Byte1('1')
Int32(4)      — length (no payload)
```

### Bind (`B`)

```
Byte1('B')
Int32         — length
String        — portal name (empty = unnamed)
String        — prepared statement name (empty = unnamed)
Int16         — number of parameter format codes
Int16[]       — format codes (0=text, 1=binary)
Int16         — number of parameter values
For each value:
  Int32       — value length (-1 = NULL)
  ByteN       — value bytes
Int16         — number of result format codes
Int16[]       — result format codes
```

### BindComplete (`2`)

```
Byte1('2')
Int32(4)      — length
```

### Execute (`E`)

```
Byte1('E')
Int32         — length
String        — portal name (null-terminated)
Int32         — max rows to return (0 = no limit)
```

### Describe (`D`)

Requests metadata for a prepared statement or portal.

```
Byte1('D')
Int32         — length
Byte1         — 'S' for statement, 'P' for portal
String        — name (null-terminated)
```

Server responds with `ParameterDescription` (for statements) and/or
`RowDescription` (for statements/portals that return rows), or `NoData` if no
rows will be returned.

### ParameterDescription (`t`)

```
Byte1('t')
Int32         — length
Int16         — number of parameters
Int32[]       — OID of each parameter
```

### Sync (`S`)

```
Byte1('S')
Int32(4)      — length
```

After `Sync`, the server sends `ReadyForQuery`. This closes the current
extended query pipeline.

### Close (`C`)

```
Byte1('C')
Int32         — length
Byte1         — 'S' for statement, 'P' for portal
String        — name (null-terminated)
```

### CloseComplete (`3`)

```
Byte1('3')
Int32(4)      — length
```

---

## Error and Notice Messages

### ErrorResponse (`E`)

```
Byte1('E')
Int32         — length
Repeated fields:
  Byte1       — field type code
  String      — field value (null-terminated)
Byte1('\0')   — message terminator
```

**Field type codes:**

| Code | Meaning | Example |
|------|---------|---------|
| `S` | Severity | `ERROR` |
| `V` | Severity (localised) | `ERROR` |
| `C` | SQLSTATE code | `42601` |
| `M` | Message | `syntax error at or near "SELCT"` |
| `D` | Detail | additional context |
| `H` | Hint | suggested fix |
| `P` | Position | character position in query string |
| `W` | Where | stack trace for functions |
| `F` | File | source file |
| `L` | Line | source line number |
| `R` | Routine | source routine name |

### SQLSTATE Codes

sqlite-server uses the SQLSTATE codes from `internal/errors/sqlstate.go`:

| Code | Name | Trigger |
|------|------|---------|
| `00000` | `successful_completion` | — |
| `08000` | `connection_exception` | Connection-level errors |
| `08006` | `connection_failure` | TCP errors |
| `0A000` | `feature_not_supported` | Unsupported SQL features |
| `22000` | `data_exception` | Invalid data |
| `22P02` | `invalid_text_representation` | e.g. non-integer for INT column |
| `23000` | `integrity_constraint_violation` | Generic constraint |
| `23505` | `unique_violation` | UNIQUE constraint |
| `23503` | `foreign_key_violation` | FK constraint |
| `25000` | `invalid_transaction_state` | e.g. COMMIT with no BEGIN |
| `28000` | `invalid_authorization_specification` | Bad password |
| `28P01` | `invalid_password` | MD5 mismatch |
| `3D000` | `invalid_catalog_name` | Database not found |
| `42000` | `syntax_error_or_access_rule_violation` | Generic SQL error |
| `42601` | `syntax_error` | Parser error |
| `42703` | `undefined_column` | Column not found |
| `42P01` | `undefined_table` | Table not found |
| `53300` | `too_many_connections` | max-conn reached |
| `57014` | `query_canceled` | Query timeout |
| `XX000` | `internal_error` | Unexpected server error |

---

## Copy Protocol

sqlite-server does **not** implement the COPY protocol. Sending a `COPY` statement
returns an `ErrorResponse` with code `0A000` (feature_not_supported).

---

## OID Constants

Type OIDs used in `RowDescription` and `ParameterDescription` messages. These are
defined in `internal/pgproto/types.go`:

| OID | Type name | Go constant |
|-----|-----------|-------------|
| 16 | `bool` | `OIDBool` |
| 17 | `bytea` | `OIDByteA` |
| 18 | `char` | `OIDChar` |
| 20 | `int8` (bigint) | `OIDInt8` |
| 21 | `int2` (smallint) | `OIDInt2` |
| 23 | `int4` (integer) | `OIDInt4` |
| 25 | `text` | `OIDText` |
| 26 | `oid` | `OIDOID` |
| 114 | `json` | `OIDJSON` |
| 700 | `float4` | `OIDFloat4` |
| 701 | `float8` | `OIDFloat8` |
| 1043 | `varchar` | `OIDVarchar` |
| 1082 | `date` | `OIDDate` |
| 1083 | `time` | `OIDTime` |
| 1114 | `timestamp` | `OIDTimestamp` |
| 1184 | `timestamptz` | `OIDTimestampTZ` |
| 1186 | `interval` | `OIDInterval` |
| 1700 | `numeric` | `OIDNumeric` |
| 2950 | `uuid` | `OIDUUID` |
| 3802 | `jsonb` | `OIDJSONB` |

---

## State Machine

The session state machine in `internal/wire/session.go`:

```
┌─────────────────────────────────────────────────────────┐
│                         States                          │
│                                                         │
│  StateStartup  ─── startup packet received ──►          │
│  StateAuth     ─── authentication passed ──►            │
│  StateReady    ◄── ReadyForQuery sent                   │
│       │                                                 │
│       ├── recv 'Q' ──► handleSimpleQuery ──► StateReady │
│       │                                                 │
│       ├── recv 'P' ──► handleParse                      │
│       │    recv 'B' ──► handleBind                      │
│       │    recv 'E' ──► handleExecute                   │
│       │    recv 'S' ──► handleSync ──► StateReady       │
│       │                                                 │
│       ├── recv 'D' ──► handleDescribe                   │
│       ├── recv 'C' ──► handleClose                      │
│       ├── recv 'H' ──► flush (no-op)                    │
│       └── recv 'X' ──► close connection                 │
└─────────────────────────────────────────────────────────┘
```

On any error during command processing, the server sends `ErrorResponse` and returns
to `StateReady` (inside a failed transaction block, indicated by `'E'` in the next
`ReadyForQuery`).

---

## Implementation Notes

### Deviations from the official PostgreSQL spec

| Feature | PostgreSQL | sqlite-server |
|---------|-----------|--------------|
| Binary format (format code `1`) | Supported | Not supported — always text |
| `CancelRequest` | Supported | Accepted but ignored |
| `COPY` protocol | Supported | Returns `feature_not_supported` |
| `NOTIFY` / `LISTEN` | Supported | Returns `feature_not_supported` |
| Multiple statements in one `Query` | Supported | Supported (split by `;`) |
| `PortalSuspended` (partial Execute) | Supported | Not supported — always returns all rows |
| SSL renegotiation | Supported | Not supported after initial handshake |
| SCRAM-SHA-256 auth | Supported | Not supported — MD5 or trust only |

### Text encoding

All values are sent as **UTF-8 text**. The server announces `client_encoding=UTF8`.
Binary encoding (`format code 1`) is not supported — the server will use text
format regardless of what the client requests in the `Bind` message.

### Integer sizes

All length fields in the protocol use **signed** 32-bit big-endian integers.
The length value **includes** the 4 bytes of the length field itself.

For example, a `ReadyForQuery` message has length `5` (4 bytes for length +
1 byte for status indicator).
