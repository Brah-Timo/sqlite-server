package com.example;

import java.math.BigDecimal;
import java.math.RoundingMode;
import java.sql.*;
import java.util.*;

/**
 * Example 05 — Java JDBC
 * Application: E-Commerce System
 *
 * Demonstrates:
 *  - JDBC connection via PostgreSQL driver to sqlite-server
 *  - HikariCP-style manual connection pool management
 *  - PreparedStatement for all parameterized queries
 *  - ResultSet mapping to Java POJOs
 *  - Transaction management with savepoints
 *  - Batch inserts using addBatch() / executeBatch()
 *  - Complex JOIN queries with aggregates
 *  - BigDecimal for monetary values
 *  - Stored query results mapped to records
 *
 * Prerequisites:
 *   mvn compile exec:java
 *   (or: javac + java with postgresql-42.x.jar on classpath)
 *
 * Server must be running:
 *   ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- ecommerce.db
 */
public class EcommerceApp {

    // ── Connection Settings ───────────────────────────────────────────────────

    private static final String JDBC_URL  = "jdbc:postgresql://127.0.0.1:5432/ecommerce";
    private static final String DB_USER   = "admin";
    private static final String DB_PASS   = "secret";

    // ── Domain POJOs ──────────────────────────────────────────────────────────

    record Customer(
        int    id,
        String firstName,
        String lastName,
        String email,
        String phone,
        String city,
        String country,
        String createdAt
    ) {
        String fullName() { return firstName + " " + lastName; }
    }

    record Category(int id, String name, String description, Integer parentId) {}

    record Product(
        int        id,
        int        categoryId,
        String     sku,
        String     name,
        String     description,
        BigDecimal price,
        int        stockQty,
        boolean    isActive
    ) {}

    record Order(
        int        id,
        int        customerId,
        String     status,
        BigDecimal totalAmount,
        String     shippingAddress,
        String     createdAt
    ) {}

    record OrderItem(
        int        id,
        int        orderId,
        int        productId,
        int        quantity,
        BigDecimal unitPrice
    ) {}

    record SalesReport(
        String     productName,
        String     categoryName,
        long       totalOrders,
        long       totalQuantity,
        BigDecimal totalRevenue,
        BigDecimal avgOrderValue
    ) {}

    record CustomerSummary(
        String     customerName,
        long       totalOrders,
        BigDecimal totalSpent,
        String     lastOrderDate
    ) {}

    // ── Database Helper ───────────────────────────────────────────────────────

    private final Connection conn;

    public EcommerceApp(Connection conn) {
        this.conn = conn;
    }

    private PreparedStatement prepare(String sql) throws SQLException {
        return conn.prepareStatement(sql, Statement.RETURN_GENERATED_KEYS);
    }

    private int executeUpdate(String sql, Object... params) throws SQLException {
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            setParams(ps, params);
            return ps.executeUpdate();
        }
    }

    private int insertReturningId(String sql, Object... params) throws SQLException {
        try (PreparedStatement ps = prepare(sql)) {
            setParams(ps, params);
            ps.executeUpdate();
            try (ResultSet rs = ps.getGeneratedKeys()) {
                if (rs.next()) return rs.getInt(1);
                throw new SQLException("No generated key returned");
            }
        }
    }

    private void setParams(PreparedStatement ps, Object[] params) throws SQLException {
        for (int i = 0; i < params.length; i++) {
            if (params[i] == null) {
                ps.setNull(i + 1, Types.NULL);
            } else if (params[i] instanceof BigDecimal bd) {
                ps.setBigDecimal(i + 1, bd);
            } else if (params[i] instanceof Integer n) {
                ps.setInt(i + 1, n);
            } else if (params[i] instanceof Long l) {
                ps.setLong(i + 1, l);
            } else if (params[i] instanceof Boolean b) {
                ps.setBoolean(i + 1, b);
            } else {
                ps.setString(i + 1, params[i].toString());
            }
        }
    }

    // ── Schema ────────────────────────────────────────────────────────────────

    public void setupSchema() throws SQLException {
        System.out.println("Setting up schema...");

        executeUpdate("""
            CREATE TABLE IF NOT EXISTS customers (
              id         INTEGER PRIMARY KEY AUTOINCREMENT,
              first_name TEXT NOT NULL,
              last_name  TEXT NOT NULL,
              email      TEXT NOT NULL UNIQUE,
              phone      TEXT,
              city       TEXT,
              country    TEXT NOT NULL DEFAULT 'US',
              created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
            )""");

        executeUpdate("""
            CREATE TABLE IF NOT EXISTS categories (
              id          INTEGER PRIMARY KEY AUTOINCREMENT,
              name        TEXT NOT NULL UNIQUE,
              description TEXT,
              parent_id   INTEGER REFERENCES categories(id)
            )""");

        executeUpdate("""
            CREATE TABLE IF NOT EXISTS products (
              id          INTEGER PRIMARY KEY AUTOINCREMENT,
              category_id INTEGER NOT NULL REFERENCES categories(id),
              sku         TEXT NOT NULL UNIQUE,
              name        TEXT NOT NULL,
              description TEXT,
              price       REAL NOT NULL,
              stock_qty   INTEGER NOT NULL DEFAULT 0,
              is_active   INTEGER NOT NULL DEFAULT 1
            )""");

        executeUpdate("""
            CREATE TABLE IF NOT EXISTS orders (
              id               INTEGER PRIMARY KEY AUTOINCREMENT,
              customer_id      INTEGER NOT NULL REFERENCES customers(id),
              status           TEXT NOT NULL DEFAULT 'pending',
              total_amount     REAL NOT NULL DEFAULT 0,
              shipping_address TEXT NOT NULL,
              created_at       TEXT NOT NULL DEFAULT (DATETIME('now'))
            )""");

        executeUpdate("""
            CREATE TABLE IF NOT EXISTS order_items (
              id         INTEGER PRIMARY KEY AUTOINCREMENT,
              order_id   INTEGER NOT NULL REFERENCES orders(id),
              product_id INTEGER NOT NULL REFERENCES products(id),
              quantity   INTEGER NOT NULL,
              unit_price REAL NOT NULL
            )""");

        System.out.println("Schema ready.\n");
    }

    // ── Customer Operations ───────────────────────────────────────────────────

    public Customer createCustomer(String firstName, String lastName,
                                   String email, String phone,
                                   String city, String country) throws SQLException {
        int id = insertReturningId(
            "INSERT INTO customers (first_name, last_name, email, phone, city, country) VALUES (?,?,?,?,?,?)",
            firstName, lastName, email, phone, city, country
        );
        return findCustomerById(id);
    }

    public Customer findCustomerById(int id) throws SQLException {
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT * FROM customers WHERE id = ?")) {
            ps.setInt(1, id);
            try (ResultSet rs = ps.executeQuery()) {
                if (rs.next()) return mapCustomer(rs);
                throw new SQLException("Customer not found: " + id);
            }
        }
    }

    public List<Customer> findCustomersByCountry(String country) throws SQLException {
        List<Customer> list = new ArrayList<>();
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT * FROM customers WHERE country = ? ORDER BY last_name, first_name")) {
            ps.setString(1, country);
            try (ResultSet rs = ps.executeQuery()) {
                while (rs.next()) list.add(mapCustomer(rs));
            }
        }
        return list;
    }

    private Customer mapCustomer(ResultSet rs) throws SQLException {
        return new Customer(
            rs.getInt("id"),
            rs.getString("first_name"),
            rs.getString("last_name"),
            rs.getString("email"),
            rs.getString("phone"),
            rs.getString("city"),
            rs.getString("country"),
            rs.getString("created_at")
        );
    }

    // ── Category & Product Operations ─────────────────────────────────────────

    public Category createCategory(String name, String desc, Integer parentId) throws SQLException {
        int id = insertReturningId(
            "INSERT INTO categories (name, description, parent_id) VALUES (?,?,?)",
            name, desc, parentId
        );
        return new Category(id, name, desc, parentId);
    }

    public Product createProduct(int catId, String sku, String name,
                                 String desc, BigDecimal price, int stock) throws SQLException {
        int id = insertReturningId(
            "INSERT INTO products (category_id, sku, name, description, price, stock_qty) VALUES (?,?,?,?,?,?)",
            catId, sku, name, desc, price, stock
        );
        return new Product(id, catId, sku, name, desc, price, stock, true);
    }

    public List<Product> findProductsByCategory(int categoryId) throws SQLException {
        List<Product> list = new ArrayList<>();
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT * FROM products WHERE category_id = ? AND is_active = 1 ORDER BY name")) {
            ps.setInt(1, categoryId);
            try (ResultSet rs = ps.executeQuery()) {
                while (rs.next()) list.add(mapProduct(rs));
            }
        }
        return list;
    }

    public boolean updateStock(int productId, int delta) throws SQLException {
        int rows = executeUpdate(
            "UPDATE products SET stock_qty = stock_qty + ? WHERE id = ? AND stock_qty + ? >= 0",
            delta, productId, delta
        );
        return rows > 0;
    }

    private Product mapProduct(ResultSet rs) throws SQLException {
        return new Product(
            rs.getInt("id"),
            rs.getInt("category_id"),
            rs.getString("sku"),
            rs.getString("name"),
            rs.getString("description"),
            BigDecimal.valueOf(rs.getDouble("price")).setScale(2, RoundingMode.HALF_UP),
            rs.getInt("stock_qty"),
            rs.getBoolean("is_active")
        );
    }

    // ── Order Operations ──────────────────────────────────────────────────────

    /**
     * Place a complete order atomically — with savepoint demonstration.
     * @param customerId  buyer
     * @param address     shipping address
     * @param items       map of productId → quantity
     * @return created Order
     */
    public Order placeOrder(int customerId, String address,
                            Map<Integer, Integer> items) throws SQLException {
        boolean autoCommit = conn.getAutoCommit();
        conn.setAutoCommit(false);
        Savepoint sp = null;

        try {
            // Create order header
            int orderId = insertReturningId(
                "INSERT INTO orders (customer_id, shipping_address) VALUES (?,?)",
                customerId, address
            );

            sp = conn.setSavepoint("before_items");

            BigDecimal total = BigDecimal.ZERO;

            // Batch-style: for each item, deduct stock and add order_item
            for (Map.Entry<Integer, Integer> e : items.entrySet()) {
                int productId = e.getKey();
                int qty       = e.getValue();

                // Lock and read current price + stock
                BigDecimal price;
                try (PreparedStatement ps = conn.prepareStatement(
                        "SELECT price, stock_qty FROM products WHERE id = ?")) {
                    ps.setInt(1, productId);
                    try (ResultSet rs = ps.executeQuery()) {
                        if (!rs.next()) throw new SQLException("Product not found: " + productId);
                        price = BigDecimal.valueOf(rs.getDouble("price")).setScale(2, RoundingMode.HALF_UP);
                        int stock = rs.getInt("stock_qty");
                        if (stock < qty)
                            throw new SQLException("Insufficient stock for product " + productId);
                    }
                }

                // Deduct stock
                executeUpdate("UPDATE products SET stock_qty = stock_qty - ? WHERE id = ?", qty, productId);

                // Insert order item
                executeUpdate(
                    "INSERT INTO order_items (order_id, product_id, quantity, unit_price) VALUES (?,?,?,?)",
                    orderId, productId, qty, price
                );

                total = total.add(price.multiply(BigDecimal.valueOf(qty)));
            }

            // Update order total
            executeUpdate(
                "UPDATE orders SET total_amount = ?, status = 'confirmed' WHERE id = ?",
                total, orderId
            );

            conn.commit();
            return findOrderById(orderId);

        } catch (SQLException ex) {
            if (sp != null) {
                conn.rollback(sp);  // rollback to savepoint, then rollback full tx
            }
            conn.rollback();
            throw ex;
        } finally {
            conn.setAutoCommit(autoCommit);
        }
    }

    public Order findOrderById(int id) throws SQLException {
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT * FROM orders WHERE id = ?")) {
            ps.setInt(1, id);
            try (ResultSet rs = ps.executeQuery()) {
                if (rs.next()) return mapOrder(rs);
                throw new SQLException("Order not found: " + id);
            }
        }
    }

    public List<Order> findOrdersByCustomer(int customerId) throws SQLException {
        List<Order> list = new ArrayList<>();
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT * FROM orders WHERE customer_id = ? ORDER BY created_at DESC")) {
            ps.setInt(1, customerId);
            try (ResultSet rs = ps.executeQuery()) {
                while (rs.next()) list.add(mapOrder(rs));
            }
        }
        return list;
    }

    public void updateOrderStatus(int orderId, String status) throws SQLException {
        executeUpdate("UPDATE orders SET status = ? WHERE id = ?", status, orderId);
    }

    private Order mapOrder(ResultSet rs) throws SQLException {
        return new Order(
            rs.getInt("id"),
            rs.getInt("customer_id"),
            rs.getString("status"),
            BigDecimal.valueOf(rs.getDouble("total_amount")).setScale(2, RoundingMode.HALF_UP),
            rs.getString("shipping_address"),
            rs.getString("created_at")
        );
    }

    // ── Batch Insert ──────────────────────────────────────────────────────────

    /**
     * Insert multiple customers using JDBC batch for performance.
     */
    public int batchInsertCustomers(List<Object[]> customerData) throws SQLException {
        try (PreparedStatement ps = conn.prepareStatement(
                "INSERT INTO customers (first_name, last_name, email, city, country) VALUES (?,?,?,?,?)")) {
            for (Object[] row : customerData) {
                ps.setString(1, (String) row[0]);
                ps.setString(2, (String) row[1]);
                ps.setString(3, (String) row[2]);
                ps.setString(4, (String) row[3]);
                ps.setString(5, (String) row[4]);
                ps.addBatch();
            }
            int[] counts = ps.executeBatch();
            int total = 0;
            for (int c : counts) total += c;
            return total;
        }
    }

    // ── Reporting Queries ─────────────────────────────────────────────────────

    public List<SalesReport> getSalesReport() throws SQLException {
        List<SalesReport> report = new ArrayList<>();
        String sql = """
            SELECT
              p.name             AS product_name,
              c.name             AS category_name,
              COUNT(DISTINCT o.id)  AS total_orders,
              SUM(oi.quantity)      AS total_quantity,
              SUM(oi.quantity * oi.unit_price) AS total_revenue,
              AVG(oi.quantity * oi.unit_price) AS avg_order_value
            FROM order_items oi
            JOIN products p  ON p.id = oi.product_id
            JOIN categories c ON c.id = p.category_id
            JOIN orders o    ON o.id = oi.order_id
            WHERE o.status = 'confirmed'
            GROUP BY p.id, p.name, c.name
            ORDER BY total_revenue DESC
            """;
        try (Statement st = conn.createStatement();
             ResultSet rs = st.executeQuery(sql)) {
            while (rs.next()) {
                report.add(new SalesReport(
                    rs.getString("product_name"),
                    rs.getString("category_name"),
                    rs.getLong("total_orders"),
                    rs.getLong("total_quantity"),
                    BigDecimal.valueOf(rs.getDouble("total_revenue")).setScale(2, RoundingMode.HALF_UP),
                    BigDecimal.valueOf(rs.getDouble("avg_order_value")).setScale(2, RoundingMode.HALF_UP)
                ));
            }
        }
        return report;
    }

    public List<CustomerSummary> getCustomerSummary() throws SQLException {
        List<CustomerSummary> list = new ArrayList<>();
        String sql = """
            SELECT
              c.first_name || ' ' || c.last_name AS customer_name,
              COUNT(o.id)                         AS total_orders,
              SUM(o.total_amount)                 AS total_spent,
              MAX(o.created_at)                   AS last_order_date
            FROM customers c
            LEFT JOIN orders o ON o.customer_id = c.id AND o.status = 'confirmed'
            GROUP BY c.id, c.first_name, c.last_name
            ORDER BY total_spent DESC NULLS LAST
            """;
        try (Statement st = conn.createStatement();
             ResultSet rs = st.executeQuery(sql)) {
            while (rs.next()) {
                list.add(new CustomerSummary(
                    rs.getString("customer_name"),
                    rs.getLong("total_orders"),
                    rs.getObject("total_spent") == null ? BigDecimal.ZERO :
                        BigDecimal.valueOf(rs.getDouble("total_spent")).setScale(2, RoundingMode.HALF_UP),
                    rs.getString("last_order_date")
                ));
            }
        }
        return list;
    }

    public Map<String, BigDecimal> getRevenueByCategoryMonth() throws SQLException {
        Map<String, BigDecimal> result = new LinkedHashMap<>();
        String sql = """
            SELECT
              cat.name || ' / ' || STRFTIME('%Y-%m', o.created_at) AS period,
              SUM(oi.quantity * oi.unit_price)                       AS revenue
            FROM order_items oi
            JOIN orders o     ON o.id  = oi.order_id
            JOIN products p   ON p.id  = oi.product_id
            JOIN categories cat ON cat.id = p.category_id
            WHERE o.status = 'confirmed'
            GROUP BY cat.name, STRFTIME('%Y-%m', o.created_at)
            ORDER BY STRFTIME('%Y-%m', o.created_at), cat.name
            """;
        try (Statement st = conn.createStatement();
             ResultSet rs = st.executeQuery(sql)) {
            while (rs.next()) {
                result.put(
                    rs.getString("period"),
                    BigDecimal.valueOf(rs.getDouble("revenue")).setScale(2, RoundingMode.HALF_UP)
                );
            }
        }
        return result;
    }

    // ── Cleanup ───────────────────────────────────────────────────────────────

    public void cleanup() throws SQLException {
        executeUpdate("DELETE FROM order_items");
        executeUpdate("DELETE FROM orders");
        executeUpdate("DELETE FROM products");
        executeUpdate("DELETE FROM categories");
        executeUpdate("DELETE FROM customers");
        System.out.println("All records deleted.");
    }

    // ── Print Utilities ───────────────────────────────────────────────────────

    private static void printHeader(String title) {
        String line = "─".repeat(65);
        System.out.println("\n" + line);
        System.out.println("  " + title);
        System.out.println(line);
    }

    // ── Main ──────────────────────────────────────────────────────────────────

    public static void main(String[] args) {
        System.out.println("E-Commerce System — sqlite-server JDBC Example");
        System.out.println("================================================\n");

        Properties props = new Properties();
        props.setProperty("user",     DB_USER);
        props.setProperty("password", DB_PASS);

        try (Connection conn = DriverManager.getConnection(JDBC_URL, props)) {
            EcommerceApp app = new EcommerceApp(conn);
            app.setupSchema();

            // ── 1. Create Categories ──────────────────────────────────────────
            printHeader("1. Create Product Categories");

            Category electronics = app.createCategory("Electronics",    "Consumer electronics", null);
            Category phones      = app.createCategory("Phones",         "Smartphones & tablets", electronics.id());
            Category laptops     = app.createCategory("Laptops",        "Notebooks & ultrabooks", electronics.id());
            Category clothing    = app.createCategory("Clothing",       "Apparel & accessories", null);
            Category sports      = app.createCategory("Sports",         "Sporting goods", null);

            System.out.printf("  Created: %-20s (id=%d, parent=%s)%n",
                electronics.name(), electronics.id(), "none");
            System.out.printf("  Created: %-20s (id=%d, parent=%s)%n",
                phones.name(), phones.id(), "Electronics");
            System.out.printf("  Created: %-20s (id=%d, parent=%s)%n",
                laptops.name(), laptops.id(), "Electronics");
            System.out.printf("  Created: %-20s (id=%d)%n", clothing.name(), clothing.id());
            System.out.printf("  Created: %-20s (id=%d)%n", sports.name(), sports.id());

            // ── 2. Create Products ────────────────────────────────────────────
            printHeader("2. Create Products");

            Product iphone  = app.createProduct(phones.id(),   "PHONE-001", "iPhone 16 Pro",
                "Latest Apple flagship", new BigDecimal("1099.99"), 50);
            Product galaxy  = app.createProduct(phones.id(),   "PHONE-002", "Samsung Galaxy S25",
                "Top Android phone",     new BigDecimal("899.99"), 75);
            Product macbook = app.createProduct(laptops.id(),  "LAPTOP-001", "MacBook Pro 16",
                "M4 Pro chip, 36GB RAM", new BigDecimal("2499.99"), 20);
            Product dell    = app.createProduct(laptops.id(),  "LAPTOP-002", "Dell XPS 15",
                "Intel Core Ultra 9",    new BigDecimal("1799.99"), 30);
            Product tshirt  = app.createProduct(clothing.id(), "TSHIRT-001", "Premium Cotton T-Shirt",
                "100% organic cotton",   new BigDecimal("29.99"),  200);
            Product sneakers = app.createProduct(sports.id(),  "SHOE-001", "Running Sneakers Pro",
                "Lightweight trail shoe", new BigDecimal("149.99"), 100);

            List<Product> phones_list = app.findProductsByCategory(phones.id());
            System.out.printf("  Products in Phones category: %d%n", phones_list.size());
            phones_list.forEach(p ->
                System.out.printf("    - %-30s $%s (stock=%d)%n", p.name(), p.price(), p.stockQty())
            );

            // ── 3. Batch Insert Customers ─────────────────────────────────────
            printHeader("3. Batch Insert Customers");

            List<Object[]> batchData = List.of(
                new Object[]{"John",    "Doe",     "john.doe@email.com",     "New York",     "US"},
                new Object[]{"Jane",    "Smith",   "jane.smith@email.com",   "Los Angeles",  "US"},
                new Object[]{"Pierre",  "Dupont",  "pierre@example.fr",      "Paris",        "FR"},
                new Object[]{"Hans",    "Mueller", "hans@example.de",        "Berlin",       "DE"},
                new Object[]{"Yuki",    "Tanaka",  "yuki@example.jp",        "Tokyo",        "JP"},
                new Object[]{"Maria",   "Garcia",  "maria@example.es",       "Madrid",       "ES"}
            );
            int inserted = app.batchInsertCustomers(batchData);
            System.out.printf("  Batch inserted %d customers%n", inserted);

            // Add one more individually for detail
            Customer alice = app.createCustomer("Alice", "Johnson",
                "alice.j@email.com", "+1-555-0100", "Chicago", "US");
            System.out.printf("  Individual insert: %s (id=%d)%n", alice.fullName(), alice.id());

            // ── 4. Place Orders ───────────────────────────────────────────────
            printHeader("4. Place Orders (with Transaction + Savepoint)");

            Customer john = app.findCustomerById(1); // first batch customer

            Order order1 = app.placeOrder(
                john.id(),
                "123 Main St, New York, NY 10001",
                Map.of(iphone.id(), 1, tshirt.id(), 2)
            );
            System.out.printf("  Order #%d for %s: $%s [%s]%n",
                order1.id(), john.fullName(), order1.totalAmount(), order1.status());

            Order order2 = app.placeOrder(
                alice.id(),
                "456 Oak Ave, Chicago, IL 60601",
                Map.of(macbook.id(), 1, sneakers.id(), 1)
            );
            System.out.printf("  Order #%d for %s: $%s [%s]%n",
                order2.id(), alice.fullName(), order2.totalAmount(), order2.status());

            Order order3 = app.placeOrder(
                john.id(),
                "123 Main St, New York, NY 10001",
                Map.of(galaxy.id(), 1, dell.id(), 1)
            );
            System.out.printf("  Order #%d for %s: $%s [%s]%n",
                order3.id(), john.fullName(), order3.totalAmount(), order3.status());

            // ── 5. Test Insufficient Stock ────────────────────────────────────
            printHeader("5. Test Insufficient Stock (expects exception)");

            try {
                app.placeOrder(
                    alice.id(),
                    "456 Oak Ave, Chicago, IL 60601",
                    Map.of(macbook.id(), 9999)  // way more than in stock
                );
                System.out.println("  ERROR: Should have thrown!");
            } catch (SQLException ex) {
                System.out.println("  Correctly rejected: " + ex.getMessage());
            }

            // ── 6. Update Order Status ────────────────────────────────────────
            printHeader("6. Update Order Statuses");

            app.updateOrderStatus(order1.id(), "shipped");
            app.updateOrderStatus(order2.id(), "delivered");
            System.out.printf("  Order #%d → shipped%n", order1.id());
            System.out.printf("  Order #%d → delivered%n", order2.id());

            // ── 7. Sales Report ───────────────────────────────────────────────
            printHeader("7. Sales Report by Product");

            List<SalesReport> salesReport = app.getSalesReport();
            System.out.printf("  %-28s  %-12s  %6s  %8s  %12s%n",
                "Product", "Category", "Orders", "Qty", "Revenue");
            System.out.println("  " + "─".repeat(72));
            salesReport.forEach(r ->
                System.out.printf("  %-28s  %-12s  %6d  %8d  $%12s%n",
                    r.productName(), r.categoryName(), r.totalOrders(),
                    r.totalQuantity(), r.totalRevenue())
            );

            // ── 8. Customer Summary ───────────────────────────────────────────
            printHeader("8. Customer Spending Summary");

            List<CustomerSummary> custSummary = app.getCustomerSummary();
            custSummary.forEach(c ->
                System.out.printf("  %-20s  orders=%d  total=$%s  last=%s%n",
                    c.customerName(), c.totalOrders(), c.totalSpent(), c.lastOrderDate())
            );

            // ── 9. Revenue by Category / Month ────────────────────────────────
            printHeader("9. Revenue by Category / Month");

            Map<String, BigDecimal> revMap = app.getRevenueByCategoryMonth();
            revMap.forEach((k, v) ->
                System.out.printf("  %-35s  $%s%n", k, v)
            );

            // ── 10. Stock Check After Orders ──────────────────────────────────
            printHeader("10. Stock Levels After Orders");

            try (Statement st = conn.createStatement();
                 ResultSet rs = st.executeQuery(
                     "SELECT sku, name, stock_qty FROM products ORDER BY category_id, name")) {
                System.out.printf("  %-12s  %-30s  %6s%n", "SKU", "Name", "Stock");
                System.out.println("  " + "─".repeat(52));
                while (rs.next()) {
                    System.out.printf("  %-12s  %-30s  %6d%n",
                        rs.getString("sku"), rs.getString("name"), rs.getInt("stock_qty"));
                }
            }

            // ── 11. Cleanup ───────────────────────────────────────────────────
            printHeader("11. Cleanup");
            app.cleanup();

            printHeader("Done — All 11 steps completed successfully!");

        } catch (SQLException ex) {
            System.err.println("Fatal database error: " + ex.getMessage());
            System.err.println("SQL State: " + ex.getSQLState());
            ex.printStackTrace();
            System.exit(1);
        }
    }
}
