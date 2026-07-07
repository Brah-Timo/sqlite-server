#!/usr/bin/env ruby
# frozen_string_literal: true

# Example 09 — Ruby + pg gem
# Application: Library Management System
#
# Demonstrates:
#  - PG::Connection to sqlite-server via PostgreSQL wire protocol
#  - exec_params() for parameterized queries (prevents SQL injection)
#  - exec() for DDL and non-parameterized statements
#  - PG::Result iteration with row hashes
#  - Transaction block: conn.transaction { |c| ... }
#  - Prepared statements: prepare() + exec_prepared()
#  - Complex JOINs, aggregates, subqueries
#  - Date arithmetic using DATE() and STRFTIME()
#  - Struct-based entity models
#  - Module-based repository pattern
#  - pp for pretty printing result structures
#
# Prerequisites:
#   gem install pg
#   ruby app.rb
#
# Server must be running:
#   ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- library.db

require 'pg'
require 'date'

# ─── Configuration ─────────────────────────────────────────────────────────

DB_CONFIG = {
  host:     '127.0.0.1',
  port:     5432,
  user:     'admin',
  password: 'secret',
  dbname:   'library'
}.freeze

# ─── Domain Structs ────────────────────────────────────────────────────────

Member   = Struct.new(:id, :full_name, :email, :phone, :membership_type, :joined_at, keyword_init: true)
Author   = Struct.new(:id, :full_name, :nationality, :born_year, keyword_init: true)
Book     = Struct.new(:id, :isbn, :title, :author_id, :genre, :year, :copies_total, :copies_available, keyword_init: true)
Loan     = Struct.new(:id, :book_id, :member_id, :loan_date, :due_date, :return_date, :status, keyword_init: true)
Fine     = Struct.new(:id, :loan_id, :member_id, :amount, :paid, keyword_init: true)

# ─── Schema Setup ──────────────────────────────────────────────────────────

def setup_schema(conn)
  puts "Setting up schema..."

  conn.exec(<<~SQL)
    CREATE TABLE IF NOT EXISTS members (
      id              INTEGER PRIMARY KEY AUTOINCREMENT,
      full_name       TEXT NOT NULL,
      email           TEXT NOT NULL UNIQUE,
      phone           TEXT,
      membership_type TEXT NOT NULL DEFAULT 'standard',
      joined_at       TEXT NOT NULL DEFAULT (DATE('now'))
    )
  SQL

  conn.exec(<<~SQL)
    CREATE TABLE IF NOT EXISTS authors (
      id          INTEGER PRIMARY KEY AUTOINCREMENT,
      full_name   TEXT NOT NULL,
      nationality TEXT NOT NULL DEFAULT 'Unknown',
      born_year   INTEGER
    )
  SQL

  conn.exec(<<~SQL)
    CREATE TABLE IF NOT EXISTS books (
      id               INTEGER PRIMARY KEY AUTOINCREMENT,
      isbn             TEXT NOT NULL UNIQUE,
      title            TEXT NOT NULL,
      author_id        INTEGER NOT NULL REFERENCES authors(id),
      genre            TEXT NOT NULL DEFAULT 'General',
      year             INTEGER,
      copies_total     INTEGER NOT NULL DEFAULT 1,
      copies_available INTEGER NOT NULL DEFAULT 1
    )
  SQL

  conn.exec(<<~SQL)
    CREATE TABLE IF NOT EXISTS loans (
      id          INTEGER PRIMARY KEY AUTOINCREMENT,
      book_id     INTEGER NOT NULL REFERENCES books(id),
      member_id   INTEGER NOT NULL REFERENCES members(id),
      loan_date   TEXT NOT NULL DEFAULT (DATE('now')),
      due_date    TEXT NOT NULL,
      return_date TEXT,
      status      TEXT NOT NULL DEFAULT 'active'
    )
  SQL

  conn.exec(<<~SQL)
    CREATE TABLE IF NOT EXISTS fines (
      id        INTEGER PRIMARY KEY AUTOINCREMENT,
      loan_id   INTEGER NOT NULL REFERENCES loans(id),
      member_id INTEGER NOT NULL REFERENCES members(id),
      amount    REAL NOT NULL DEFAULT 0,
      paid      INTEGER NOT NULL DEFAULT 0
    )
  SQL

  puts "Schema ready.\n\n"
end

# ─── Member Repository ──────────────────────────────────────────────────────

module MemberRepo
  def self.create(conn, full_name:, email:, phone: nil, membership_type: 'standard')
    result = conn.exec_params(
      'INSERT INTO members (full_name, email, phone, membership_type) VALUES ($1,$2,$3,$4) RETURNING *',
      [full_name, email, phone, membership_type]
    )
    row_to_member(result.first)
  end

  def self.find(conn, id)
    result = conn.exec_params('SELECT * FROM members WHERE id = $1', [id])
    result.ntuples.zero? ? nil : row_to_member(result.first)
  end

  def self.find_all(conn)
    conn.exec('SELECT * FROM members ORDER BY full_name').map { |r| row_to_member(r) }
  end

  def self.find_by_type(conn, type)
    conn.exec_params(
      'SELECT * FROM members WHERE membership_type = $1 ORDER BY full_name', [type]
    ).map { |r| row_to_member(r) }
  end

  def self.row_to_member(r)
    Member.new(
      id:              r['id'].to_i,
      full_name:       r['full_name'],
      email:           r['email'],
      phone:           r['phone'],
      membership_type: r['membership_type'],
      joined_at:       r['joined_at']
    )
  end
end

# ─── Author Repository ──────────────────────────────────────────────────────

module AuthorRepo
  def self.create(conn, full_name:, nationality: 'Unknown', born_year: nil)
    result = conn.exec_params(
      'INSERT INTO authors (full_name, nationality, born_year) VALUES ($1,$2,$3) RETURNING *',
      [full_name, nationality, born_year]
    )
    row_to_author(result.first)
  end

  def self.find_all(conn)
    conn.exec('SELECT * FROM authors ORDER BY full_name').map { |r| row_to_author(r) }
  end

  def self.row_to_author(r)
    Author.new(
      id:          r['id'].to_i,
      full_name:   r['full_name'],
      nationality: r['nationality'],
      born_year:   r['born_year']&.to_i
    )
  end
end

# ─── Book Repository ────────────────────────────────────────────────────────

module BookRepo
  def self.create(conn, isbn:, title:, author_id:, genre:, year: nil, copies: 1)
    result = conn.exec_params(
      'INSERT INTO books (isbn, title, author_id, genre, year, copies_total, copies_available)
       VALUES ($1,$2,$3,$4,$5,$6,$6) RETURNING *',
      [isbn, title, author_id, genre, year, copies]
    )
    row_to_book(result.first)
  end

  def self.find(conn, id)
    result = conn.exec_params('SELECT * FROM books WHERE id = $1', [id])
    result.ntuples.zero? ? nil : row_to_book(result.first)
  end

  def self.find_available(conn)
    conn.exec('SELECT * FROM books WHERE copies_available > 0 ORDER BY title')
        .map { |r| row_to_book(r) }
  end

  def self.search(conn, query)
    pattern = "%#{query.downcase}%"
    conn.exec_params(
      'SELECT b.*, a.full_name AS author_name
       FROM books b JOIN authors a ON a.id = b.author_id
       WHERE LOWER(b.title) LIKE $1 OR LOWER(a.full_name) LIKE $1
       ORDER BY b.title',
      [pattern]
    ).map { |r| row_to_book(r) }
  end

  def self.decrement_available(conn, id)
    conn.exec_params('UPDATE books SET copies_available = copies_available - 1 WHERE id = $1', [id])
  end

  def self.increment_available(conn, id)
    conn.exec_params('UPDATE books SET copies_available = copies_available + 1 WHERE id = $1', [id])
  end

  def self.row_to_book(r)
    Book.new(
      id:               r['id'].to_i,
      isbn:             r['isbn'],
      title:            r['title'],
      author_id:        r['author_id'].to_i,
      genre:            r['genre'],
      year:             r['year']&.to_i,
      copies_total:     r['copies_total'].to_i,
      copies_available: r['copies_available'].to_i
    )
  end
end

# ─── Loan Repository ────────────────────────────────────────────────────────

module LoanRepo
  # Checkout a book — decrements stock inside a transaction
  def self.checkout(conn, book_id:, member_id:, loan_days: 14)
    conn.transaction do |c|
      # Verify availability
      result = c.exec_params(
        'SELECT copies_available FROM books WHERE id = $1', [book_id]
      )
      available = result.first['copies_available'].to_i
      raise "Book ##{book_id} has no available copies" if available.zero?

      due = (Date.today + loan_days).to_s

      loan = c.exec_params(
        "INSERT INTO loans (book_id, member_id, loan_date, due_date)
         VALUES ($1,$2,DATE('now'),$3) RETURNING *",
        [book_id, member_id, due]
      ).first

      c.exec_params(
        'UPDATE books SET copies_available = copies_available - 1 WHERE id = $1', [book_id]
      )

      row_to_loan(loan)
    end
  end

  # Return a book
  def self.return_book(conn, loan_id:)
    conn.transaction do |c|
      loan = c.exec_params('SELECT * FROM loans WHERE id = $1', [loan_id]).first
      raise "Loan ##{loan_id} not found" unless loan
      raise "Loan ##{loan_id} already returned" if loan['status'] == 'returned'

      # Mark returned
      c.exec_params(
        "UPDATE loans SET return_date = DATE('now'), status = 'returned' WHERE id = $1",
        [loan_id]
      )

      # Increment available copies
      c.exec_params(
        'UPDATE books SET copies_available = copies_available + 1 WHERE id = $1',
        [loan['book_id'].to_i]
      )

      # Calculate overdue fine (0.50/day)
      due_date    = Date.parse(loan['due_date'])
      return_date = Date.today
      if return_date > due_date
        days_late = (return_date - due_date).to_i
        fine_amt  = days_late * 0.50
        c.exec_params(
          'INSERT INTO fines (loan_id, member_id, amount) VALUES ($1,$2,$3)',
          [loan_id, loan['member_id'].to_i, fine_amt]
        )
        puts "    Fine issued: $#{'%.2f' % fine_amt} (#{days_late} days late)"
      end

      true
    end
  end

  def self.find_active(conn, member_id: nil)
    if member_id
      conn.exec_params(
        "SELECT l.*, b.title AS book_title, m.full_name AS member_name
         FROM loans l
         JOIN books b ON b.id = l.book_id
         JOIN members m ON m.id = l.member_id
         WHERE l.member_id = $1 AND l.status = 'active'
         ORDER BY l.due_date",
        [member_id]
      ).map { |r| row_to_loan(r) }
    else
      conn.exec(
        "SELECT l.*, b.title AS book_title, m.full_name AS member_name
         FROM loans l
         JOIN books b ON b.id = l.book_id
         JOIN members m ON m.id = l.member_id
         WHERE l.status = 'active'
         ORDER BY l.due_date"
      ).map { |r| row_to_loan(r) }
    end
  end

  def self.find_overdue(conn)
    conn.exec(
      "SELECT l.id, b.title, m.full_name AS member_name,
              l.due_date,
              CAST((JULIANDAY(DATE('now')) - JULIANDAY(l.due_date)) AS INTEGER) AS days_overdue
       FROM loans l
       JOIN books b  ON b.id = l.book_id
       JOIN members m ON m.id = l.member_id
       WHERE l.status = 'active' AND l.due_date < DATE('now')
       ORDER BY l.due_date"
    ).to_a
  end

  def self.row_to_loan(r)
    Loan.new(
      id:          r['id'].to_i,
      book_id:     r['book_id'].to_i,
      member_id:   r['member_id'].to_i,
      loan_date:   r['loan_date'],
      due_date:    r['due_date'],
      return_date: r['return_date'],
      status:      r['status']
    )
  end
end

# ─── Reporting ─────────────────────────────────────────────────────────────

module Reports
  def self.most_borrowed(conn, limit: 5)
    conn.exec_params(
      'SELECT
         b.title,
         a.full_name  AS author,
         b.genre,
         COUNT(l.id)  AS loan_count
       FROM books b
       JOIN authors a ON a.id = b.author_id
       LEFT JOIN loans l ON l.book_id = b.id
       GROUP BY b.id, b.title, a.full_name, b.genre
       ORDER BY loan_count DESC
       LIMIT $1',
      [limit]
    ).to_a
  end

  def self.active_members(conn, limit: 5)
    conn.exec_params(
      "SELECT
         m.full_name,
         m.membership_type,
         COUNT(l.id)           AS total_loans,
         SUM(CASE WHEN l.status='active' THEN 1 ELSE 0 END) AS active_loans
       FROM members m
       LEFT JOIN loans l ON l.member_id = m.id
       GROUP BY m.id, m.full_name, m.membership_type
       ORDER BY total_loans DESC
       LIMIT $1",
      [limit]
    ).to_a
  end

  def self.genre_popularity(conn)
    conn.exec(
      'SELECT
         b.genre,
         COUNT(DISTINCT b.id)  AS book_count,
         COUNT(l.id)           AS total_loans
       FROM books b
       LEFT JOIN loans l ON l.book_id = b.id
       GROUP BY b.genre
       ORDER BY total_loans DESC'
    ).to_a
  end

  def self.monthly_loans(conn)
    conn.exec(
      "SELECT
         STRFTIME('%Y-%m', loan_date) AS month,
         COUNT(*)                     AS loans
       FROM loans
       GROUP BY STRFTIME('%Y-%m', loan_date)
       ORDER BY month"
    ).to_a
  end

  def self.outstanding_fines(conn)
    conn.exec(
      "SELECT
         m.full_name,
         SUM(f.amount)              AS total_fines,
         SUM(CASE WHEN f.paid=1 THEN f.amount ELSE 0 END) AS paid,
         SUM(CASE WHEN f.paid=0 THEN f.amount ELSE 0 END) AS outstanding
       FROM fines f
       JOIN members m ON m.id = f.member_id
       GROUP BY m.id, m.full_name
       ORDER BY outstanding DESC"
    ).to_a
  end
end

# ─── Print Utilities ────────────────────────────────────────────────────────

def print_header(title)
  line = '─' * 65
  puts "\n#{line}\n  #{title}\n#{line}"
end

def print_table(rows, cols)
  return puts "  (no rows)" if rows.empty?

  widths = cols.each_with_object({}) { |c, h| h[c] = c.length }
  rows.each { |r| cols.each { |c| widths[c] = [widths[c], r[c].to_s.length].max } }

  header = '  ' + cols.map { |c| c.to_s.ljust(widths[c] + 2) }.join
  sep    = '  ' + cols.map { |c| '─' * (widths[c] + 2) }.join
  puts header
  puts sep
  rows.each do |r|
    puts '  ' + cols.map { |c| r[c].to_s.ljust(widths[c] + 2) }.join
  end
end

# ─── Main ──────────────────────────────────────────────────────────────────

puts "Library Management System — sqlite-server Ruby pg Example"
puts "==========================================================\n\n"

PG::Connection.open(**DB_CONFIG) do |conn|

  setup_schema(conn)

  # ── 1. Register Members ─────────────────────────────────────────────────
  print_header("1. Register Members")

  alice = MemberRepo.create(conn, full_name: 'Alice Johnson',  email: 'alice@library.org',  phone: '555-0101', membership_type: 'premium')
  bob   = MemberRepo.create(conn, full_name: 'Bob Smith',      email: 'bob@library.org',    phone: '555-0102')
  carol = MemberRepo.create(conn, full_name: 'Carol White',    email: 'carol@library.org',  phone: '555-0103', membership_type: 'student')
  dave  = MemberRepo.create(conn, full_name: 'Dave Brown',     email: 'dave@library.org',   membership_type: 'premium')
  eve   = MemberRepo.create(conn, full_name: 'Eve Davis',      email: 'eve@library.org',    phone: '555-0105')

  [alice, bob, carol, dave, eve].each do |m|
    printf("  %-22s  %-10s  %s\n", m.full_name, m.membership_type, m.email)
  end

  # ── 2. Add Authors ───────────────────────────────────────────────────────
  print_header("2. Add Authors")

  tolkien  = AuthorRepo.create(conn, full_name: 'J.R.R. Tolkien',     nationality: 'British',   born_year: 1892)
  orwell   = AuthorRepo.create(conn, full_name: 'George Orwell',      nationality: 'British',   born_year: 1903)
  dumas    = AuthorRepo.create(conn, full_name: 'Alexandre Dumas',    nationality: 'French',    born_year: 1802)
  asimov   = AuthorRepo.create(conn, full_name: 'Isaac Asimov',       nationality: 'American',  born_year: 1920)
  rowling  = AuthorRepo.create(conn, full_name: 'J.K. Rowling',       nationality: 'British',   born_year: 1965)
  doyle    = AuthorRepo.create(conn, full_name: 'Arthur Conan Doyle', nationality: 'British',   born_year: 1859)

  AuthorRepo.find_all(conn).each do |a|
    printf("  %-25s  %-12s  born %s\n", a.full_name, a.nationality, a.born_year || '?')
  end

  # ── 3. Add Books ─────────────────────────────────────────────────────────
  print_header("3. Add Books to Catalog")

  b1 = BookRepo.create(conn, isbn: '978-0-618-26025-5', title: 'The Lord of the Rings', author_id: tolkien.id,  genre: 'Fantasy',     year: 1954, copies: 3)
  b2 = BookRepo.create(conn, isbn: '978-0-452-28423-4', title: 'Nineteen Eighty-Four',  author_id: orwell.id,   genre: 'Dystopian',   year: 1949, copies: 4)
  b3 = BookRepo.create(conn, isbn: '978-0-14-044926-3', title: 'The Count of Monte Cristo', author_id: dumas.id, genre: 'Adventure', year: 1844, copies: 2)
  b4 = BookRepo.create(conn, isbn: '978-0-553-29335-7', title: 'Foundation',            author_id: asimov.id,   genre: 'Sci-Fi',      year: 1951, copies: 3)
  b5 = BookRepo.create(conn, isbn: '978-0-439-02348-1', title: "Harry Potter and the Sorcerer's Stone", author_id: rowling.id, genre: 'Fantasy', year: 1997, copies: 5)
  b6 = BookRepo.create(conn, isbn: '978-0-14-043787-1', title: 'The Hound of the Baskervilles', author_id: doyle.id, genre: 'Mystery', year: 1902, copies: 2)
  b7 = BookRepo.create(conn, isbn: '978-0-553-38009-1', title: 'I, Robot',              author_id: asimov.id,   genre: 'Sci-Fi',      year: 1950, copies: 3)
  b8 = BookRepo.create(conn, isbn: '978-0-618-57144-6', title: 'The Hobbit',            author_id: tolkien.id,  genre: 'Fantasy',     year: 1937, copies: 4)

  printf("  %-45s  %-12s  %5s  %7s\n", "Title", "Genre", "Year", "Copies")
  puts "  " + "─" * 74
  [b1,b2,b3,b4,b5,b6,b7,b8].each do |b|
    printf("  %-45s  %-12s  %5d  %7d\n", b.title, b.genre, b.year || 0, b.copies_total)
  end

  # ── 4. Checkout Books (using prepared statements) ─────────────────────────
  print_header("4. Checkout Books")

  # Pre-define prepared statement for loans
  conn.prepare('find_loan', 'SELECT * FROM loans WHERE id = $1')

  loan1 = LoanRepo.checkout(conn, book_id: b1.id, member_id: alice.id, loan_days: 14)
  loan2 = LoanRepo.checkout(conn, book_id: b2.id, member_id: bob.id,   loan_days: 14)
  loan3 = LoanRepo.checkout(conn, book_id: b4.id, member_id: carol.id, loan_days: 21)
  loan4 = LoanRepo.checkout(conn, book_id: b5.id, member_id: dave.id,  loan_days: 14)
  loan5 = LoanRepo.checkout(conn, book_id: b5.id, member_id: eve.id,   loan_days: 14)
  loan6 = LoanRepo.checkout(conn, book_id: b8.id, member_id: alice.id, loan_days: 14)

  printf("  Loan #%-3d  %-45s  due %s\n", loan1.id, 'The Lord of the Rings → Alice',            loan1.due_date)
  printf("  Loan #%-3d  %-45s  due %s\n", loan2.id, 'Nineteen Eighty-Four → Bob',                loan2.due_date)
  printf("  Loan #%-3d  %-45s  due %s\n", loan3.id, 'Foundation → Carol',                        loan3.due_date)
  printf("  Loan #%-3d  %-45s  due %s\n", loan4.id, "Harry Potter (copy 1) → Dave",              loan4.due_date)
  printf("  Loan #%-3d  %-45s  due %s\n", loan5.id, "Harry Potter (copy 2) → Eve",               loan5.due_date)
  printf("  Loan #%-3d  %-45s  due %s\n", loan6.id, 'The Hobbit → Alice',                        loan6.due_date)

  # ── 5. Test Checkout when No Copies Available ─────────────────────────────
  print_header("5. Test Borrow Unavailable Book")

  b3_book = BookRepo.find(conn, b3.id)
  loan_c3 = LoanRepo.checkout(conn, book_id: b3.id, member_id: alice.id)
  loan_c3b = LoanRepo.checkout(conn, book_id: b3.id, member_id: bob.id)

  begin
    LoanRepo.checkout(conn, book_id: b3.id, member_id: carol.id)
    puts "  ERROR: Should have raised!"
  rescue RuntimeError => e
    puts "  Correctly rejected: #{e.message}"
  end

  # ── 6. Return Books ───────────────────────────────────────────────────────
  print_header("6. Return Books")

  LoanRepo.return_book(conn, loan_id: loan2.id)
  LoanRepo.return_book(conn, loan_id: loan3.id)
  puts "  Returned: Nineteen Eighty-Four (Bob)"
  puts "  Returned: Foundation (Carol)"

  # ── 7. Active Loans ───────────────────────────────────────────────────────
  print_header("7. Active Loans")

  active = LoanRepo.find_active(conn)
  active.each do |l|
    printf("  Loan #%-3d  book_id=%-3d  member_id=%-3d  due=%s\n",
      l.id, l.book_id, l.member_id, l.due_date)
  end

  # ── 8. Alice's Loans ──────────────────────────────────────────────────────
  print_header("8. Alice's Active Loans")

  alice_loans = LoanRepo.find_active(conn, member_id: alice.id)
  alice_loans.each { |l| puts "  Loan ##{l.id}  book_id=#{l.book_id}  due=#{l.due_date}" }

  # ── 9. Book Search ────────────────────────────────────────────────────────
  print_header("9. Search Books for 'asimov'")

  results = BookRepo.search(conn, 'asimov')
  puts "  Found #{results.size} result(s):"
  results.each { |b| puts "    - #{b.title} (#{b.genre}, #{b.year})" }

  # ── 10. Most Borrowed Books ───────────────────────────────────────────────
  print_header("10. Most Borrowed Books")

  print_table(Reports.most_borrowed(conn, limit: 6), %w[title author genre loan_count])

  # ── 11. Most Active Members ───────────────────────────────────────────────
  print_header("11. Most Active Members")

  print_table(Reports.active_members(conn, limit: 5), %w[full_name membership_type total_loans active_loans])

  # ── 12. Genre Popularity ──────────────────────────────────────────────────
  print_header("12. Genre Popularity")

  print_table(Reports.genre_popularity(conn), %w[genre book_count total_loans])

  # ── 13. Available Books ───────────────────────────────────────────────────
  print_header("13. Currently Available Books")

  avail = BookRepo.find_available(conn)
  avail.each do |b|
    printf("  %-45s  %d/%d copies free\n", b.title, b.copies_available, b.copies_total)
  end

  # ── 14. Cleanup ───────────────────────────────────────────────────────────
  print_header("14. Cleanup")

  %w[fines loans books authors members].each { |t| conn.exec("DELETE FROM #{t}") }
  puts "  All records deleted."

  print_header("Done — All 14 steps completed successfully!")
  puts
end
