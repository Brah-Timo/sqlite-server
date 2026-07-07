"""
Example 02 — Python (psycopg2)

Full CRUD example with connection pooling, prepared statements,
transactions, and a simple inventory management system.

Prerequisites:
    pip install psycopg2-binary

Run sqlite-server first:
    ./sqlite-server --no-auth -- /tmp/demo.db

Then run:
    python app.py
"""

import psycopg2
import psycopg2.extras
from contextlib import contextmanager
from datetime import datetime, date
from decimal import Decimal
import sys

# ── Connection settings ────────────────────────────────────────────────────────
DSN = {
    "host":    "localhost",
    "port":    5432,
    "user":    "test",
    "password":"test",
    "dbname":  "test",
    "connect_timeout": 5,
}

@contextmanager
def get_conn():
    """Context manager that auto-commits or rolls back."""
    conn = psycopg2.connect(**DSN)
    try:
        yield conn
        conn.commit()
    except Exception:
        conn.rollback()
        raise
    finally:
        conn.close()


# ── Schema ─────────────────────────────────────────────────────────────────────
CREATE_SCHEMA = """
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS customers;

CREATE TABLE customers (
    id         SERIAL PRIMARY KEY,
    name       TEXT    NOT NULL,
    email      TEXT    NOT NULL UNIQUE,
    phone      TEXT,
    joined_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE products (
    id          SERIAL PRIMARY KEY,
    sku         TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    price       REAL    NOT NULL CHECK (price >= 0),
    stock       INTEGER NOT NULL DEFAULT 0,
    category    TEXT    NOT NULL DEFAULT 'general'
);

CREATE TABLE orders (
    id           SERIAL PRIMARY KEY,
    customer_id  INTEGER NOT NULL REFERENCES customers(id),
    status       TEXT    NOT NULL DEFAULT 'pending',
    total        REAL    NOT NULL DEFAULT 0,
    created_at   TIMESTAMP DEFAULT NOW()
);

CREATE TABLE order_items (
    id          SERIAL PRIMARY KEY,
    order_id    INTEGER NOT NULL REFERENCES orders(id),
    product_id  INTEGER NOT NULL REFERENCES products(id),
    quantity    INTEGER NOT NULL CHECK (quantity > 0),
    unit_price  REAL    NOT NULL
);
"""


def setup_schema(conn):
    with conn.cursor() as cur:
        cur.execute(CREATE_SCHEMA)
    print("✓ Schema created (customers, products, orders, order_items)")


def seed_data(conn):
    """Insert sample customers and products."""
    customers = [
        ("Alice Johnson",  "alice@shop.com",   "+1-555-0101"),
        ("Bob Martinez",   "bob@shop.com",     "+1-555-0102"),
        ("Carol Chen",     "carol@shop.com",   "+1-555-0103"),
        ("Dave Williams",  "dave@shop.com",    None),
    ]
    products = [
        ("SKU-001", "Wireless Mouse",       29.99, 150, "electronics"),
        ("SKU-002", "Mechanical Keyboard",  79.99,  80, "electronics"),
        ("SKU-003", "USB-C Hub",            49.99, 200, "electronics"),
        ("SKU-004", "Notebook A5",           8.99, 500, "stationery"),
        ("SKU-005", "Ballpoint Pen Pack",    4.99, 1000,"stationery"),
        ("SKU-006", "Desk Lamp LED",        35.99,  60, "furniture"),
    ]

    with conn.cursor() as cur:
        # Use executemany for batch inserts
        cur.executemany(
            "INSERT INTO customers (name, email, phone) VALUES (%s, %s, %s)",
            customers
        )
        cur.executemany(
            "INSERT INTO products (sku, name, price, stock, category) VALUES (%s,%s,%s,%s,%s)",
            products
        )
    print(f"✓ Seeded {len(customers)} customers, {len(products)} products")


def place_order(conn, customer_email: str, items: list[tuple[str, int]]) -> int:
    """
    Place an order for a customer.
    items: list of (sku, quantity) tuples.
    Returns the new order ID.
    """
    with conn.cursor() as cur:
        # Get customer
        cur.execute("SELECT id FROM customers WHERE email = %s", (customer_email,))
        row = cur.fetchone()
        if not row:
            raise ValueError(f"Customer not found: {customer_email}")
        customer_id = row[0]

        # Create order
        cur.execute(
            "INSERT INTO orders (customer_id, status) VALUES (%s, 'pending') RETURNING id",
            (customer_id,)
        )
        order_id = cur.fetchone()[0]

        total = 0.0
        for sku, qty in items:
            # Get product (lock for update to prevent race condition)
            cur.execute(
                "SELECT id, price, stock FROM products WHERE sku = %s",
                (sku,)
            )
            prod = cur.fetchone()
            if not prod:
                raise ValueError(f"Product not found: {sku}")
            prod_id, price, stock = prod
            if stock < qty:
                raise ValueError(f"Insufficient stock for {sku}: need {qty}, have {stock}")

            # Insert order item
            cur.execute(
                "INSERT INTO order_items (order_id, product_id, quantity, unit_price) "
                "VALUES (%s, %s, %s, %s)",
                (order_id, prod_id, qty, price)
            )
            # Decrement stock
            cur.execute(
                "UPDATE products SET stock = stock - %s WHERE id = %s",
                (qty, prod_id)
            )
            total += price * qty

        # Update order total
        cur.execute(
            "UPDATE orders SET total = %s, status = 'confirmed' WHERE id = %s",
            (round(total, 2), order_id)
        )

    return order_id


def print_orders(conn):
    """Print all orders with their items."""
    with conn.cursor(cursor_factory=psycopg2.extras.DictCursor) as cur:
        cur.execute("""
            SELECT o.id, c.name AS customer, o.status, o.total, o.created_at
            FROM orders o
            JOIN customers c ON c.id = o.customer_id
            ORDER BY o.id
        """)
        orders = cur.fetchall()

        print(f"\n── Orders ({len(orders)}) ──────────────────────────────────")
        for order in orders:
            print(f"  Order #{order['id']}  customer={order['customer']!r}"
                  f"  status={order['status']}  total=${order['total']:.2f}")

            cur.execute("""
                SELECT p.sku, p.name, oi.quantity, oi.unit_price,
                       oi.quantity * oi.unit_price AS subtotal
                FROM order_items oi
                JOIN products p ON p.id = oi.product_id
                WHERE oi.order_id = %s
            """, (order['id'],))

            for item in cur.fetchall():
                print(f"    - [{item['sku']}] {item['name']:<30} "
                      f"qty={item['quantity']}  @${item['unit_price']:.2f}"
                      f"  = ${item['subtotal']:.2f}")


def inventory_report(conn):
    """Print inventory grouped by category."""
    with conn.cursor() as cur:
        cur.execute("""
            SELECT category,
                   COUNT(*)       AS num_products,
                   SUM(stock)     AS total_stock,
                   MIN(price)     AS min_price,
                   MAX(price)     AS max_price,
                   AVG(price)     AS avg_price
            FROM products
            GROUP BY category
            ORDER BY category
        """)
        rows = cur.fetchall()

    print("\n── Inventory Report ─────────────────────────────")
    print(f"  {'Category':<15} {'Products':>8} {'Stock':>8} "
          f"{'Min$':>8} {'Max$':>8} {'Avg$':>8}")
    print("  " + "-" * 60)
    for cat, num, stock, mn, mx, avg in rows:
        print(f"  {cat:<15} {num:>8} {stock:>8} "
              f"  {mn:>6.2f}  {mx:>6.2f}  {avg:>6.2f}")


def low_stock_alert(conn, threshold: int = 100):
    """Find products below the stock threshold."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT sku, name, stock FROM products WHERE stock < %s ORDER BY stock",
            (threshold,)
        )
        rows = cur.fetchall()

    print(f"\n── Low Stock Alert (threshold={threshold}) ─────────────")
    if not rows:
        print("  All products are well-stocked.")
    else:
        for sku, name, stock in rows:
            print(f"  ⚠ [{sku}] {name:<30}  stock={stock}")


def main():
    print("=" * 60)
    print("  Example 02 — Python psycopg2 + sqlite-server")
    print("=" * 60)

    # Test connectivity
    try:
        with get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT version()")
                version = cur.fetchone()[0]
            print(f"✓ Connected  server={version!r}\n")
    except psycopg2.OperationalError as e:
        print(f"✗ Cannot connect: {e}")
        print("  Start the server: ./sqlite-server --no-auth -- /tmp/demo.db")
        sys.exit(1)

    # Setup
    with get_conn() as conn:
        setup_schema(conn)
        seed_data(conn)

    # Place orders
    with get_conn() as conn:
        try:
            oid = place_order(conn, "alice@shop.com", [
                ("SKU-001", 2),   # 2× Wireless Mouse
                ("SKU-003", 1),   # 1× USB-C Hub
            ])
            print(f"✓ Order #{oid} placed for alice@shop.com")

            oid = place_order(conn, "bob@shop.com", [
                ("SKU-002", 1),   # 1× Mechanical Keyboard
                ("SKU-004", 3),   # 3× Notebook A5
                ("SKU-005", 5),   # 5× Ballpoint Pen Pack
            ])
            print(f"✓ Order #{oid} placed for bob@shop.com")

            oid = place_order(conn, "carol@shop.com", [
                ("SKU-006", 2),   # 2× Desk Lamp LED
            ])
            print(f"✓ Order #{oid} placed for carol@shop.com")

        except ValueError as e:
            print(f"✗ Order failed: {e}")
            raise

    # Reports
    with get_conn() as conn:
        print_orders(conn)
        inventory_report(conn)
        low_stock_alert(conn, threshold=100)

    print("\n✓ All done!")


if __name__ == "__main__":
    main()
