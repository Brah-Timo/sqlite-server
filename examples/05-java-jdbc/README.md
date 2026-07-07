# Example 05 — Java JDBC

**Application**: E-Commerce System  
**Language**: Java 21  
**Driver**: PostgreSQL JDBC Driver (`org.postgresql:postgresql:42.7.2`)

## What It Demonstrates

- JDBC `DriverManager.getConnection()` with PostgreSQL URL to sqlite-server
- `PreparedStatement` for every parameterized query (prevents SQL injection)
- `ResultSet` mapped to Java records (`Customer`, `Product`, `Order`, etc.)
- `JDBC Batch` inserts via `addBatch()` / `executeBatch()`
- Manual transaction management with `setAutoCommit(false)` + `commit()` / `rollback()`
- **Savepoints** — rolling back only to `before_items` on stock error
- Complex reporting queries with `GROUP BY`, `SUM`, `AVG`, `COUNT`
- `STRFTIME` for date grouping (sqlite-server compatible)
- Java records (Java 16+) as immutable data holders
- Text blocks (Java 15+) for multi-line SQL strings

## Prerequisites

- Java 21 (Java 17+ minimum for records + text blocks)
- Maven 3.6+
- sqlite-server running on port 5432

## Run with Maven

```bash
# Compile and run in one step
mvn compile exec:java

# Or build fat JAR and run
mvn package
java -jar target/sqlite-server-java-jdbc-1.0.0-jar-with-dependencies.jar
```

## Run with PowerShell

```powershell
# Compile and run
mvn compile exec:java

# Build standalone JAR
mvn package
java -jar target\sqlite-server-java-jdbc-1.0.0-jar-with-dependencies.jar
```

## Manual Compile (no Maven)

```bash
# Download driver
wget https://jdbc.postgresql.org/download/postgresql-42.7.2.jar

# Compile
javac -cp postgresql-42.7.2.jar \
      src/main/java/com/example/EcommerceApp.java \
      -d out/

# Run
java -cp "out:postgresql-42.7.2.jar" com.example.EcommerceApp
```

## PowerShell (no Maven)

```powershell
# Download driver
Invoke-WebRequest -Uri "https://jdbc.postgresql.org/download/postgresql-42.7.2.jar" `
                  -OutFile "postgresql.jar"

# Compile
javac -cp postgresql.jar `
      src\main\java\com\example\EcommerceApp.java `
      -d out\

# Run
java -cp "out;postgresql.jar" com.example.EcommerceApp
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- ecommerce.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- ecommerce.db
```

## Connection String

```java
// In EcommerceApp.java — change these constants:
private static final String JDBC_URL = "jdbc:postgresql://127.0.0.1:5432/ecommerce";
private static final String DB_USER  = "admin";
private static final String DB_PASS  = "secret";
```

## Expected Output

```
E-Commerce System — sqlite-server JDBC Example
================================================

Setting up schema...
Schema ready.

─────────────────────────────────────────────────────────────────
  1. Create Product Categories
─────────────────────────────────────────────────────────────────
  Created: Electronics          (id=1, parent=none)
  Created: Phones               (id=2, parent=Electronics)
  ...

─────────────────────────────────────────────────────────────────
  7. Sales Report by Product
─────────────────────────────────────────────────────────────────
  Product                       Category      Orders       Qty       Revenue
  ────────────────────────────────────────────────────────────────────────────
  MacBook Pro 16                Laptops            1         1    $ 2499.99
  iPhone 16 Pro                 Phones             1         1    $ 1099.99
  ...
```
