using System;
using System.Collections.Generic;
using System.Text;
using System.Threading.Tasks;
using Npgsql;

/// <summary>
/// Example 06 — C# + Npgsql
/// Application: HR Management System
///
/// Demonstrates:
///  - NpgsqlConnection / NpgsqlCommand / NpgsqlDataReader
///  - Async/await with ConfigureAwait(false)
///  - Typed C# records for all entities
///  - Repository pattern with async methods
///  - Transaction management (BeginTransactionAsync)
///  - Parameterized queries preventing SQL injection
///  - RETURNING clause to fetch inserted rows
///  - Aggregate / reporting queries
///  - Nullable reference types (C# 8+)
///  - Pattern matching on query results
///
/// Prerequisites:
///   dotnet run
///
/// Server must be running:
///   ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- hr.db
/// </summary>

// ─── Connection String ────────────────────────────────────────────────────────

const string CONNECTION_STRING =
    "Host=127.0.0.1;Port=5432;Username=admin;Password=secret;Database=hr;" +
    "Timeout=10;Command Timeout=30";

// ─── Domain Records ───────────────────────────────────────────────────────────

record Department(
    int    Id,
    string Name,
    string Location,
    int?   ManagerId,
    string CreatedAt
);

record Employee(
    int     Id,
    int     DepartmentId,
    string  FirstName,
    string  LastName,
    string  Email,
    string  JobTitle,
    decimal Salary,
    string  HireDate,
    bool    IsActive,
    int?    ManagerId
)
{
    public string FullName => $"{FirstName} {LastName}";
}

record LeaveRequest(
    int    Id,
    int    EmployeeId,
    string LeaveType,
    string StartDate,
    string EndDate,
    int    Days,
    string Status,
    string Reason
);

record PerformanceReview(
    int    Id,
    int    EmployeeId,
    int    ReviewerId,
    string ReviewPeriod,
    int    Rating,
    string Comments,
    string ReviewDate
);

// ─── Report View Models ───────────────────────────────────────────────────────

record DepartmentSummary(
    string  DepartmentName,
    string  Location,
    int     HeadCount,
    decimal AvgSalary,
    decimal TotalPayroll,
    decimal MinSalary,
    decimal MaxSalary
);

record EmployeeWithDept(
    int     EmployeeId,
    string  FullName,
    string  JobTitle,
    decimal Salary,
    string  DepartmentName,
    string  ManagerName
);

record LeaveBalance(
    string  EmployeeName,
    int     AnnualUsed,
    int     SickUsed,
    int     TotalDays
);

// ─── Database Helper ──────────────────────────────────────────────────────────

static class DbHelper
{
    public static async Task<T?> QueryOneAsync<T>(
        NpgsqlConnection conn,
        string sql,
        Func<NpgsqlDataReader, T> map,
        Action<NpgsqlCommand>? bind = null)
    {
        await using var cmd = new NpgsqlCommand(sql, conn);
        bind?.Invoke(cmd);
        await using var reader = await cmd.ExecuteReaderAsync().ConfigureAwait(false);
        return await reader.ReadAsync().ConfigureAwait(false) ? map(reader) : default;
    }

    public static async Task<List<T>> QueryManyAsync<T>(
        NpgsqlConnection conn,
        string sql,
        Func<NpgsqlDataReader, T> map,
        Action<NpgsqlCommand>? bind = null)
    {
        var list = new List<T>();
        await using var cmd = new NpgsqlCommand(sql, conn);
        bind?.Invoke(cmd);
        await using var reader = await cmd.ExecuteReaderAsync().ConfigureAwait(false);
        while (await reader.ReadAsync().ConfigureAwait(false))
            list.Add(map(reader));
        return list;
    }

    public static async Task<int> ExecuteAsync(
        NpgsqlConnection conn,
        string sql,
        Action<NpgsqlCommand>? bind = null)
    {
        await using var cmd = new NpgsqlCommand(sql, conn);
        bind?.Invoke(cmd);
        return await cmd.ExecuteNonQueryAsync().ConfigureAwait(false);
    }

    public static T Get<T>(NpgsqlDataReader r, string col)
        => r.IsDBNull(r.GetOrdinal(col)) ? default! : r.GetFieldValue<T>(r.GetOrdinal(col));

    public static T? GetNullable<T>(NpgsqlDataReader r, string col) where T : struct
        => r.IsDBNull(r.GetOrdinal(col)) ? null : r.GetFieldValue<T>(r.GetOrdinal(col));
}

// ─── Repository: Departments ──────────────────────────────────────────────────

class DepartmentRepository(NpgsqlConnection conn)
{
    private static Department Map(NpgsqlDataReader r) => new(
        DbHelper.Get<int>(r, "id"),
        DbHelper.Get<string>(r, "name"),
        DbHelper.Get<string>(r, "location"),
        DbHelper.GetNullable<int>(r, "manager_id"),
        DbHelper.Get<string>(r, "created_at")
    );

    public async Task<Department> CreateAsync(string name, string location)
    {
        var dept = await DbHelper.QueryOneAsync(conn,
            "INSERT INTO departments (name, location) VALUES ($1, $2) RETURNING *",
            Map,
            cmd => {
                cmd.Parameters.AddWithValue(name);
                cmd.Parameters.AddWithValue(location);
            });
        return dept!;
    }

    public async Task<Department?> FindByIdAsync(int id)
        => await DbHelper.QueryOneAsync(conn,
            "SELECT * FROM departments WHERE id = $1",
            Map,
            cmd => cmd.Parameters.AddWithValue(id));

    public async Task<List<Department>> FindAllAsync()
        => await DbHelper.QueryManyAsync(conn,
            "SELECT * FROM departments ORDER BY name", Map);

    public async Task<int> SetManagerAsync(int deptId, int managerId)
        => await DbHelper.ExecuteAsync(conn,
            "UPDATE departments SET manager_id = $1 WHERE id = $2",
            cmd => { cmd.Parameters.AddWithValue(managerId); cmd.Parameters.AddWithValue(deptId); });
}

// ─── Repository: Employees ────────────────────────────────────────────────────

class EmployeeRepository(NpgsqlConnection conn)
{
    private static Employee Map(NpgsqlDataReader r) => new(
        DbHelper.Get<int>(r, "id"),
        DbHelper.Get<int>(r, "department_id"),
        DbHelper.Get<string>(r, "first_name"),
        DbHelper.Get<string>(r, "last_name"),
        DbHelper.Get<string>(r, "email"),
        DbHelper.Get<string>(r, "job_title"),
        DbHelper.Get<decimal>(r, "salary"),
        DbHelper.Get<string>(r, "hire_date"),
        DbHelper.Get<bool>(r, "is_active"),
        DbHelper.GetNullable<int>(r, "manager_id")
    );

    public async Task<Employee> CreateAsync(
        int deptId, string firstName, string lastName, string email,
        string jobTitle, decimal salary, string hireDate, int? managerId = null)
    {
        var emp = await DbHelper.QueryOneAsync(conn,
            """
            INSERT INTO employees
              (department_id, first_name, last_name, email, job_title, salary, hire_date, manager_id)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
            RETURNING *
            """,
            Map,
            cmd => {
                cmd.Parameters.AddWithValue(deptId);
                cmd.Parameters.AddWithValue(firstName);
                cmd.Parameters.AddWithValue(lastName);
                cmd.Parameters.AddWithValue(email);
                cmd.Parameters.AddWithValue(jobTitle);
                cmd.Parameters.AddWithValue(salary);
                cmd.Parameters.AddWithValue(hireDate);
                cmd.Parameters.AddWithValue(managerId.HasValue ? (object)managerId.Value : DBNull.Value);
            });
        return emp!;
    }

    public async Task<Employee?> FindByIdAsync(int id)
        => await DbHelper.QueryOneAsync(conn,
            "SELECT * FROM employees WHERE id = $1", Map,
            cmd => cmd.Parameters.AddWithValue(id));

    public async Task<List<Employee>> FindByDepartmentAsync(int deptId)
        => await DbHelper.QueryManyAsync(conn,
            "SELECT * FROM employees WHERE department_id = $1 AND is_active = TRUE ORDER BY last_name",
            Map, cmd => cmd.Parameters.AddWithValue(deptId));

    public async Task<List<Employee>> FindByManagerAsync(int managerId)
        => await DbHelper.QueryManyAsync(conn,
            "SELECT * FROM employees WHERE manager_id = $1 AND is_active = TRUE ORDER BY last_name",
            Map, cmd => cmd.Parameters.AddWithValue(managerId));

    public async Task<int> UpdateSalaryAsync(int id, decimal newSalary)
        => await DbHelper.ExecuteAsync(conn,
            "UPDATE employees SET salary = $1 WHERE id = $2",
            cmd => { cmd.Parameters.AddWithValue(newSalary); cmd.Parameters.AddWithValue(id); });

    public async Task<int> TerminateAsync(int id)
        => await DbHelper.ExecuteAsync(conn,
            "UPDATE employees SET is_active = FALSE WHERE id = $1",
            cmd => cmd.Parameters.AddWithValue(id));

    public async Task<List<EmployeeWithDept>> FindWithDeptAsync()
        => await DbHelper.QueryManyAsync(conn,
            """
            SELECT
              e.id          AS employee_id,
              e.first_name || ' ' || e.last_name AS full_name,
              e.job_title,
              e.salary,
              d.name        AS department_name,
              COALESCE(m.first_name || ' ' || m.last_name, 'N/A') AS manager_name
            FROM employees e
            JOIN departments d ON d.id = e.department_id
            LEFT JOIN employees m ON m.id = e.manager_id
            WHERE e.is_active = TRUE
            ORDER BY d.name, e.last_name
            """,
            r => new EmployeeWithDept(
                DbHelper.Get<int>(r, "employee_id"),
                DbHelper.Get<string>(r, "full_name"),
                DbHelper.Get<string>(r, "job_title"),
                DbHelper.Get<decimal>(r, "salary"),
                DbHelper.Get<string>(r, "department_name"),
                DbHelper.Get<string>(r, "manager_name")
            ));
}

// ─── Repository: Leave Requests ───────────────────────────────────────────────

class LeaveRepository(NpgsqlConnection conn)
{
    private static LeaveRequest Map(NpgsqlDataReader r) => new(
        DbHelper.Get<int>(r, "id"),
        DbHelper.Get<int>(r, "employee_id"),
        DbHelper.Get<string>(r, "leave_type"),
        DbHelper.Get<string>(r, "start_date"),
        DbHelper.Get<string>(r, "end_date"),
        DbHelper.Get<int>(r, "days"),
        DbHelper.Get<string>(r, "status"),
        DbHelper.Get<string>(r, "reason")
    );

    public async Task<LeaveRequest> RequestAsync(
        int empId, string type, string start, string end, int days, string reason)
    {
        var req = await DbHelper.QueryOneAsync(conn,
            "INSERT INTO leave_requests (employee_id,leave_type,start_date,end_date,days,reason) VALUES ($1,$2,$3,$4,$5,$6) RETURNING *",
            Map,
            cmd => {
                cmd.Parameters.AddWithValue(empId);
                cmd.Parameters.AddWithValue(type);
                cmd.Parameters.AddWithValue(start);
                cmd.Parameters.AddWithValue(end);
                cmd.Parameters.AddWithValue(days);
                cmd.Parameters.AddWithValue(reason);
            });
        return req!;
    }

    public async Task<int> ApproveAsync(int id)
        => await DbHelper.ExecuteAsync(conn,
            "UPDATE leave_requests SET status = 'approved' WHERE id = $1",
            cmd => cmd.Parameters.AddWithValue(id));

    public async Task<int> RejectAsync(int id)
        => await DbHelper.ExecuteAsync(conn,
            "UPDATE leave_requests SET status = 'rejected' WHERE id = $1",
            cmd => cmd.Parameters.AddWithValue(id));

    public async Task<List<LeaveBalance>> GetLeaveBalancesAsync()
        => await DbHelper.QueryManyAsync(conn,
            """
            SELECT
              e.first_name || ' ' || e.last_name AS employee_name,
              SUM(CASE WHEN lr.leave_type='annual' AND lr.status='approved' THEN lr.days ELSE 0 END) AS annual_used,
              SUM(CASE WHEN lr.leave_type='sick'   AND lr.status='approved' THEN lr.days ELSE 0 END) AS sick_used,
              SUM(CASE WHEN lr.status='approved'   THEN lr.days ELSE 0 END) AS total_days
            FROM employees e
            LEFT JOIN leave_requests lr ON lr.employee_id = e.id
            WHERE e.is_active = TRUE
            GROUP BY e.id, e.first_name, e.last_name
            ORDER BY total_days DESC
            """,
            r => new LeaveBalance(
                DbHelper.Get<string>(r, "employee_name"),
                (int)DbHelper.Get<long>(r, "annual_used"),
                (int)DbHelper.Get<long>(r, "sick_used"),
                (int)DbHelper.Get<long>(r, "total_days")
            ));
}

// ─── Repository: Performance Reviews ─────────────────────────────────────────

class ReviewRepository(NpgsqlConnection conn)
{
    public async Task<PerformanceReview> CreateAsync(
        int empId, int reviewerId, string period, int rating, string comments)
    {
        var rev = await DbHelper.QueryOneAsync(conn,
            "INSERT INTO performance_reviews (employee_id,reviewer_id,review_period,rating,comments) VALUES ($1,$2,$3,$4,$5) RETURNING *",
            r => new PerformanceReview(
                DbHelper.Get<int>(r, "id"),
                DbHelper.Get<int>(r, "employee_id"),
                DbHelper.Get<int>(r, "reviewer_id"),
                DbHelper.Get<string>(r, "review_period"),
                DbHelper.Get<int>(r, "rating"),
                DbHelper.Get<string>(r, "comments"),
                DbHelper.Get<string>(r, "review_date")
            ),
            cmd => {
                cmd.Parameters.AddWithValue(empId);
                cmd.Parameters.AddWithValue(reviewerId);
                cmd.Parameters.AddWithValue(period);
                cmd.Parameters.AddWithValue(rating);
                cmd.Parameters.AddWithValue(comments);
            });
        return rev!;
    }

    public async Task<List<(string Name, double AvgRating)>> GetAverageRatingsAsync()
        => await DbHelper.QueryManyAsync(conn,
            """
            SELECT
              e.first_name || ' ' || e.last_name AS emp_name,
              AVG(CAST(pr.rating AS REAL))        AS avg_rating
            FROM performance_reviews pr
            JOIN employees e ON e.id = pr.employee_id
            GROUP BY e.id, e.first_name, e.last_name
            ORDER BY avg_rating DESC
            """,
            r => (DbHelper.Get<string>(r, "emp_name"), r.GetDouble(r.GetOrdinal("avg_rating"))));
}

// ─── Schema Setup ─────────────────────────────────────────────────────────────

static async Task SetupSchemaAsync(NpgsqlConnection conn)
{
    Console.WriteLine("Setting up schema...");

    var tables = new[]
    {
        """
        CREATE TABLE IF NOT EXISTS departments (
          id         INTEGER PRIMARY KEY AUTOINCREMENT,
          name       TEXT NOT NULL UNIQUE,
          location   TEXT NOT NULL DEFAULT 'HQ',
          manager_id INTEGER,
          created_at TEXT NOT NULL DEFAULT (DATETIME('now'))
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS employees (
          id            INTEGER PRIMARY KEY AUTOINCREMENT,
          department_id INTEGER NOT NULL REFERENCES departments(id),
          first_name    TEXT NOT NULL,
          last_name     TEXT NOT NULL,
          email         TEXT NOT NULL UNIQUE,
          job_title     TEXT NOT NULL,
          salary        REAL NOT NULL,
          hire_date     TEXT NOT NULL,
          is_active     INTEGER NOT NULL DEFAULT 1,
          manager_id    INTEGER REFERENCES employees(id)
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS leave_requests (
          id          INTEGER PRIMARY KEY AUTOINCREMENT,
          employee_id INTEGER NOT NULL REFERENCES employees(id),
          leave_type  TEXT NOT NULL,
          start_date  TEXT NOT NULL,
          end_date    TEXT NOT NULL,
          days        INTEGER NOT NULL,
          status      TEXT NOT NULL DEFAULT 'pending',
          reason      TEXT NOT NULL DEFAULT ''
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS performance_reviews (
          id           INTEGER PRIMARY KEY AUTOINCREMENT,
          employee_id  INTEGER NOT NULL REFERENCES employees(id),
          reviewer_id  INTEGER NOT NULL REFERENCES employees(id),
          review_period TEXT NOT NULL,
          rating       INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
          comments     TEXT NOT NULL DEFAULT '',
          review_date  TEXT NOT NULL DEFAULT (DATE('now'))
        )
        """
    };

    foreach (var ddl in tables)
    {
        await using var cmd = new NpgsqlCommand(ddl, conn);
        await cmd.ExecuteNonQueryAsync().ConfigureAwait(false);
    }

    Console.WriteLine("Schema ready.\n");
}

// ─── Utility ──────────────────────────────────────────────────────────────────

static void PrintHeader(string title)
{
    var line = new string('─', 65);
    Console.WriteLine($"\n{line}");
    Console.WriteLine($"  {title}");
    Console.WriteLine(line);
}

static async Task CleanupAsync(NpgsqlConnection conn)
{
    foreach (var tbl in new[] { "performance_reviews", "leave_requests", "employees", "departments" })
    {
        await using var cmd = new NpgsqlCommand($"DELETE FROM {tbl}", conn);
        await cmd.ExecuteNonQueryAsync().ConfigureAwait(false);
    }
    Console.WriteLine("  All records deleted.");
}

// ─── Main ─────────────────────────────────────────────────────────────────────

Console.WriteLine("HR Management System — sqlite-server Npgsql Example");
Console.WriteLine("=====================================================\n");

await using var conn = new NpgsqlConnection(CONNECTION_STRING);
await conn.OpenAsync().ConfigureAwait(false);
Console.WriteLine($"Connected to: {conn.Host}:{conn.Port}  (server version: {conn.ServerVersion})\n");

await SetupSchemaAsync(conn);

var deptRepo   = new DepartmentRepository(conn);
var empRepo    = new EmployeeRepository(conn);
var leaveRepo  = new LeaveRepository(conn);
var reviewRepo = new ReviewRepository(conn);

// ── 1. Create Departments ─────────────────────────────────────────────────────
PrintHeader("1. Create Departments");

var eng  = await deptRepo.CreateAsync("Engineering",  "Building A, Floor 3");
var hr   = await deptRepo.CreateAsync("Human Resources", "Building B, Floor 1");
var mkt  = await deptRepo.CreateAsync("Marketing",    "Building C, Floor 2");
var fin  = await deptRepo.CreateAsync("Finance",      "Building A, Floor 1");

Console.WriteLine($"  Created: {eng.Name} (id={eng.Id}, location={eng.Location})");
Console.WriteLine($"  Created: {hr.Name} (id={hr.Id})");
Console.WriteLine($"  Created: {mkt.Name} (id={mkt.Id})");
Console.WriteLine($"  Created: {fin.Name} (id={fin.Id})");

// ── 2. Create Employees (transaction) ─────────────────────────────────────────
PrintHeader("2. Create Employees (inside a transaction)");

Employee? cto, alice, bob, carol, dave, eve;

await using (var tx = await conn.BeginTransactionAsync())
{
    try
    {
        cto   = await empRepo.CreateAsync(eng.Id, "Sarah",  "Chen",     "sarah.chen@corp.com",   "CTO",                    180000m, "2018-01-15");
        alice = await empRepo.CreateAsync(eng.Id, "Alice",  "Johnson",  "alice.j@corp.com",      "Senior Engineer",        120000m, "2020-03-01", cto.Id);
        bob   = await empRepo.CreateAsync(eng.Id, "Bob",    "Smith",    "bob.s@corp.com",        "Backend Engineer",        95000m, "2021-06-15", cto.Id);
        carol = await empRepo.CreateAsync(hr.Id,  "Carol",  "Williams", "carol.w@corp.com",      "HR Manager",             90000m, "2019-09-01");
        dave  = await empRepo.CreateAsync(mkt.Id, "Dave",   "Brown",    "dave.b@corp.com",       "Marketing Director",     110000m, "2020-05-20");
        eve   = await empRepo.CreateAsync(fin.Id, "Eve",    "Davis",    "eve.d@corp.com",        "Financial Analyst",       85000m, "2022-01-10");

        await tx.CommitAsync().ConfigureAwait(false);
        Console.WriteLine($"  Committed 6 employees in one transaction.");
    }
    catch
    {
        await tx.RollbackAsync().ConfigureAwait(false);
        throw;
    }
}

// Set department managers
await deptRepo.SetManagerAsync(eng.Id, cto!.Id);
await deptRepo.SetManagerAsync(hr.Id,  carol!.Id);
await deptRepo.SetManagerAsync(mkt.Id, dave!.Id);
Console.WriteLine("  Department managers assigned.");

// ── 3. All Employees with Department ─────────────────────────────────────────
PrintHeader("3. All Employees with Department");

var withDept = await empRepo.FindWithDeptAsync();
Console.WriteLine($"  {"Name",-22}  {"Title",-25}  {"Department",-15}  {"Salary",10}  {"Manager",-18}");
Console.WriteLine("  " + new string('─', 95));
foreach (var e in withDept)
    Console.WriteLine($"  {e.FullName,-22}  {e.JobTitle,-25}  {e.DepartmentName,-15}  {e.Salary,10:C}  {e.ManagerName,-18}");

// ── 4. Salary Update ──────────────────────────────────────────────────────────
PrintHeader("4. Give Alice a 10% Raise");

decimal newSalary = alice!.Salary * 1.10m;
await empRepo.UpdateSalaryAsync(alice.Id, newSalary);
var aliceUpdated = await empRepo.FindByIdAsync(alice.Id);
Console.WriteLine($"  {aliceUpdated!.FullName}: ${alice.Salary:F2} → ${aliceUpdated.Salary:F2}");

// ── 5. Leave Requests ─────────────────────────────────────────────────────────
PrintHeader("5. Submit Leave Requests");

var leave1 = await leaveRepo.RequestAsync(alice.Id, "annual",  "2025-07-01", "2025-07-05", 5, "Summer vacation");
var leave2 = await leaveRepo.RequestAsync(bob!.Id,  "sick",    "2025-03-10", "2025-03-11", 2, "Flu");
var leave3 = await leaveRepo.RequestAsync(carol!.Id,"annual",  "2025-08-01", "2025-08-10", 8, "Family trip");
var leave4 = await leaveRepo.RequestAsync(alice.Id, "annual",  "2025-12-23", "2025-12-27", 3, "Christmas break");
var leave5 = await leaveRepo.RequestAsync(bob.Id,   "sick",    "2025-06-01", "2025-06-02", 1, "Doctor appointment");

Console.WriteLine($"  Submitted {5} leave requests.");

// ── 6. Approve / Reject ───────────────────────────────────────────────────────
PrintHeader("6. Process Leave Approvals");

await leaveRepo.ApproveAsync(leave1.Id);
await leaveRepo.ApproveAsync(leave2.Id);
await leaveRepo.ApproveAsync(leave3.Id);
await leaveRepo.ApproveAsync(leave4.Id);
await leaveRepo.RejectAsync(leave5.Id);

Console.WriteLine($"  Approved: leaves #{leave1.Id}, #{leave2.Id}, #{leave3.Id}, #{leave4.Id}");
Console.WriteLine($"  Rejected: leave #{leave5.Id} (Bob's doctor appointment — too short notice)");

// ── 7. Leave Balances ─────────────────────────────────────────────────────────
PrintHeader("7. Leave Balance Report");

var balances = await leaveRepo.GetLeaveBalancesAsync();
Console.WriteLine($"  {"Employee",-22}  {"Annual (days)",14}  {"Sick (days)",12}  {"Total",8}");
Console.WriteLine("  " + new string('─', 60));
foreach (var b in balances)
    Console.WriteLine($"  {b.EmployeeName,-22}  {b.AnnualUsed,14}  {b.SickUsed,12}  {b.TotalDays,8}");

// ── 8. Performance Reviews ────────────────────────────────────────────────────
PrintHeader("8. Submit Performance Reviews (Q1 2025)");

await reviewRepo.CreateAsync(alice.Id, cto!.Id,   "Q1-2025", 5, "Outstanding technical leadership on the auth migration project.");
await reviewRepo.CreateAsync(bob.Id,   cto.Id,    "Q1-2025", 4, "Solid contributions. Great collaboration with frontend team.");
await reviewRepo.CreateAsync(carol.Id, cto.Id,    "Q1-2025", 4, "Excellent onboarding process improvements this quarter.");
await reviewRepo.CreateAsync(dave!.Id, cto.Id,    "Q1-2025", 3, "Campaign results met target but could improve data-driven decisions.");
await reviewRepo.CreateAsync(eve!.Id,  carol.Id,  "Q1-2025", 5, "Exceptional budget analysis; saved company $120K through renegotiations.");

Console.WriteLine("  5 performance reviews submitted.");

// ── 9. Average Ratings ────────────────────────────────────────────────────────
PrintHeader("9. Average Performance Ratings");

var ratings = await reviewRepo.GetAverageRatingsAsync();
Console.WriteLine($"  {"Employee",-22}  {"Avg Rating",12}  {"Stars",-10}");
Console.WriteLine("  " + new string('─', 50));
foreach (var (name, avg) in ratings)
{
    var stars = new string('★', (int)Math.Round(avg)) + new string('☆', 5 - (int)Math.Round(avg));
    Console.WriteLine($"  {name,-22}  {avg,12:F1}  {stars}");
}

// ── 10. Department Payroll Summary ────────────────────────────────────────────
PrintHeader("10. Department Payroll Summary");

var deptSummary = await DbHelper.QueryManyAsync(conn,
    """
    SELECT
      d.name                     AS department_name,
      d.location,
      COUNT(e.id)                AS head_count,
      AVG(e.salary)              AS avg_salary,
      SUM(e.salary)              AS total_payroll,
      MIN(e.salary)              AS min_salary,
      MAX(e.salary)              AS max_salary
    FROM departments d
    LEFT JOIN employees e ON e.department_id = d.id AND e.is_active = TRUE
    GROUP BY d.id, d.name, d.location
    ORDER BY total_payroll DESC NULLS LAST
    """,
    r => new DepartmentSummary(
        DbHelper.Get<string>(r, "department_name"),
        DbHelper.Get<string>(r, "location"),
        (int)DbHelper.Get<long>(r, "head_count"),
        DbHelper.Get<double>(r, "avg_salary") is double avg ? (decimal)avg : 0m,
        DbHelper.Get<double>(r, "total_payroll") is double tot ? (decimal)tot : 0m,
        DbHelper.Get<double>(r, "min_salary") is double mn ? (decimal)mn : 0m,
        DbHelper.Get<double>(r, "max_salary") is double mx ? (decimal)mx : 0m
    ));

Console.WriteLine($"  {"Department",-20}  {"Location",-22}  {"HC",3}  {"Avg Salary",12}  {"Total Payroll",14}");
Console.WriteLine("  " + new string('─', 80));
foreach (var d in deptSummary)
    Console.WriteLine(
        $"  {d.DepartmentName,-20}  {d.Location,-22}  {d.HeadCount,3}  {d.AvgSalary,12:C}  {d.TotalPayroll,14:C}");

// ── 11. Cleanup ───────────────────────────────────────────────────────────────
PrintHeader("11. Cleanup");
await CleanupAsync(conn);

PrintHeader("Done — All 11 steps completed successfully!");
