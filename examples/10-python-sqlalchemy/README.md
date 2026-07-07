# Example 10 — Python + SQLAlchemy ORM

**Application**: School Management System  
**Language**: Python 3.10+  
**Driver**: SQLAlchemy 2.0 ORM + `psycopg2-binary`

## What It Demonstrates

- SQLAlchemy 2.0 **DeclarativeBase** with `Mapped[]` type annotations
- `mapped_column()` with type inference from Python type hints
- `relationship()` with `back_populates` for bidirectional navigation
- Many-to-many via `Table(...)` association object (enrollment with extra `grade` column)
- `secondary=enrollment` in `relationship()` for M2M
- `selectinload()` and `joinedload()` for eager loading (N+1 prevention)
- `Session.add()` / `Session.commit()` / `Session.rollback()`
- ORM `select()` statements with `.where()`, `.order_by()`, `.limit()`
- `func.count()`, `func.avg()`, `func.sum()`, `func.min()`, `func.max()` aggregates
- `and_()` / `or_()` compound filter expressions
- `func.lower().contains()` for case-insensitive search
- Core `text()` queries mixed with ORM for complex UPDATE
- `@property` for computed fields (`full_name`)
- `Base.metadata.create_all()` / `drop_all()` for schema management
- Connection string: `postgresql+psycopg2://user:pass@host:port/db`

## Prerequisites

- Python 3.10+
- sqlite-server running on port 5432

## Install

```bash
pip install -r requirements.txt

# Or with virtual environment (recommended)
python -m venv venv
source venv/bin/activate       # Linux/macOS
.\venv\Scripts\Activate.ps1    # Windows PowerShell

pip install -r requirements.txt
```

## Run

```bash
python app.py
```

## PowerShell

```powershell
# Create virtual environment
python -m venv venv
.\venv\Scripts\Activate.ps1

# Install dependencies
pip install -r requirements.txt

# Run
python app.py
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- school.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- school.db
```

## Connection Config

Edit `DATABASE_URL` at the top of `app.py`:

```python
DATABASE_URL = "postgresql+psycopg2://admin:secret@127.0.0.1:5432/school"
```

Alternative connection string format:
```python
# With environment variable
import os
DATABASE_URL = os.getenv("DATABASE_URL", "postgresql+psycopg2://admin:secret@127.0.0.1:5432/school")
```

## SQLAlchemy 2.0 vs 1.x

This example uses **SQLAlchemy 2.0** style:

| Feature | 1.x (old) | 2.0 (this example) |
|---------|-----------|---------------------|
| Models | `Base = declarative_base()` | `class Base(DeclarativeBase)` |
| Columns | `Column(Integer, ...)` | `mapped_column(Integer, ...)` |
| Types | No annotations | `Mapped[int]`, `Mapped[Optional[str]]` |
| Queries | `session.query(User)` | `select(User)` statement |
| Execute | `session.query().all()` | `session.execute(stmt).scalars().all()` |

## Expected Output

```
School Management System — sqlite-server SQLAlchemy ORM Example
=================================================================

Schema created (all tables).

─────────────────────────────────────────────────────────────────
  8. Students with Their Enrolled Courses (ORM SELECT)
─────────────────────────────────────────────────────────────────
  Alice Williams          GPA=90.25  courses=[CS101, CS201, MATH101, ENG101]
  Bob Anderson            GPA=84.83  courses=[CS101, CS201, CS301, MATH101]
  ...

─────────────────────────────────────────────────────────────────
  10. Course Enrollment & Grade Statistics
─────────────────────────────────────────────────────────────────
  Code        Name                                      Enr     Avg    Min    Max
  ──────────────────────────────────────────────────────────────────────────────
  CS101       Introduction to Programming                 6    79.2   68.0   92.5
  MATH101     Calculus I                                  5    95.0   95.0   95.0
  ...

─────────────────────────────────────────────────────────────────
  12. Top Students by GPA
─────────────────────────────────────────────────────────────────
  #1  Grace Lee              GPA=96.50  year=2022
  #2  Carol Martinez         GPA=95.00  year=2023
  ...
```
