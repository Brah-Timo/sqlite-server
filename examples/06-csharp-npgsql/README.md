# Example 06 — C# + Npgsql

**Application**: HR Management System  
**Language**: C# 12 / .NET 8  
**Driver**: `Npgsql` 8.0.3

## What It Demonstrates

- `NpgsqlConnection` / `NpgsqlCommand` / `NpgsqlDataReader` — core async CRUD
- `await using` pattern for proper resource disposal
- C# records as immutable entity types (`Employee`, `Department`, `LeaveRequest`, etc.)
- Async/await throughout with `ConfigureAwait(false)` for library code
- Parameterized queries with positional `$1`, `$2`, ... syntax
- `RETURNING *` to fetch inserted row without a second SELECT
- `BeginTransactionAsync()` + `CommitAsync()` / `RollbackAsync()`
- Primary constructor syntax (C# 12) for repositories
- Nullable reference types for foreign keys (`int?`, `string?`)
- Complex `JOIN` queries returning flat projection records
- Performance review ratings with star visualization

## Prerequisites

- .NET 8 SDK (or .NET 6/7 — change `TargetFramework` in `.csproj`)
- sqlite-server running on port 5432

## Run

```bash
dotnet run
```

## PowerShell

```powershell
# Restore packages and run
dotnet run

# Build release binary
dotnet publish -c Release -r win-x64 --self-contained true -o .\publish\

# Run the self-contained exe
.\publish\HrApp.exe
```

## Build Self-Contained Executable

```powershell
# Windows x64 (no .NET runtime required on target machine)
dotnet publish -c Release -r win-x64 --self-contained true `
               -p:PublishSingleFile=true -o .\dist\win\

# Linux x64
dotnet publish -c Release -r linux-x64 --self-contained true `
               -p:PublishSingleFile=true -o .\dist\linux\

# macOS ARM64 (Apple Silicon)
dotnet publish -c Release -r osx-arm64 --self-contained true `
               -p:PublishSingleFile=true -o .\dist\mac\
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- hr.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- hr.db
```

## Connection String

Edit `CONNECTION_STRING` constant at the top of `Program.cs`:

```csharp
const string CONNECTION_STRING =
    "Host=127.0.0.1;Port=5432;Username=admin;Password=secret;Database=hr;" +
    "Timeout=10;Command Timeout=30";
```

## Expected Output

```
HR Management System — sqlite-server Npgsql Example
=====================================================

Connected to: 127.0.0.1:5432  (server version: 14.0)

Setting up schema...
Schema ready.

─────────────────────────────────────────────────────────────────
  3. All Employees with Department
─────────────────────────────────────────────────────────────────
  Name                    Title                      Department       Salary  Manager
  ───────────────────────────────────────────────────────────────────────────────────
  Alice Johnson           Senior Engineer            Engineering   $132,000  Sarah Chen
  Bob Smith               Backend Engineer           Engineering    $95,000  Sarah Chen
  ...

─────────────────────────────────────────────────────────────────
  9. Average Performance Ratings
─────────────────────────────────────────────────────────────────
  Employee                Avg Rating  Stars
  ──────────────────────────────────────────────────────
  Alice Johnson                  5.0  ★★★★★
  Eve Davis                      5.0  ★★★★★
  Bob Smith                      4.0  ★★★★☆
  ...
```
