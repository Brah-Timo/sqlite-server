"""
Example 10 — Python + SQLAlchemy ORM (Core + ORM)
Application: School Management System

Demonstrates:
 - SQLAlchemy 2.0 ORM with DeclarativeBase
 - Mapped[] type annotations (Python 3.10+)
 - relationship() with back_populates
 - Session.add() / Session.commit() / Session.rollback()
 - Session.execute(select()) — ORM queries
 - Hybrid property for computed fields
 - Association table (many-to-many: students ↔ courses)
 - Core text() queries alongside ORM
 - Connection string: postgresql+psycopg2://
 - create_engine() + sessionmaker()
 - Eager loading with selectinload() / joinedload()
 - Filtering, ordering, limiting with ORM
 - aggregate functions: func.count(), func.avg(), func.sum()
 - Complex JOIN queries in ORM style

Prerequisites:
  pip install -r requirements.txt
  python app.py

Server must be running:
  ./sqlite-server --addr 127.0.0.1:5432 --no-auth -- school.db
"""

from __future__ import annotations

import sys
from datetime import date, datetime
from typing import List, Optional

from sqlalchemy import (
    Column,
    Float,
    ForeignKey,
    Integer,
    String,
    Table,
    Text,
    create_engine,
    func,
    select,
    text,
    and_,
    or_,
)
from sqlalchemy.orm import (
    DeclarativeBase,
    Mapped,
    Session,
    mapped_column,
    relationship,
    sessionmaker,
    selectinload,
    joinedload,
)

# ─── Configuration ────────────────────────────────────────────────────────────

DATABASE_URL = "postgresql+psycopg2://admin:secret@127.0.0.1:5432/school"

# ─── Base & Association Tables ────────────────────────────────────────────────


class Base(DeclarativeBase):
    pass


# Many-to-many: students ↔ courses (enrollment table)
enrollment = Table(
    "enrollment",
    Base.metadata,
    Column("student_id", Integer, ForeignKey("students.id"), primary_key=True),
    Column("course_id",  Integer, ForeignKey("courses.id"),  primary_key=True),
    Column("enrolled_at", String(10), default=None),
    Column("grade", Float, nullable=True),   # final grade 0-100
)

# ─── ORM Models ───────────────────────────────────────────────────────────────


class Department(Base):
    __tablename__ = "departments"

    id:          Mapped[int]           = mapped_column(Integer, primary_key=True, autoincrement=True)
    name:        Mapped[str]           = mapped_column(String(100), unique=True, nullable=False)
    code:        Mapped[str]           = mapped_column(String(10),  unique=True, nullable=False)
    description: Mapped[Optional[str]] = mapped_column(Text, nullable=True)
    head_id:     Mapped[Optional[int]] = mapped_column(Integer, ForeignKey("teachers.id"), nullable=True)

    # Relationships
    teachers: Mapped[List["Teacher"]] = relationship(
        "Teacher", back_populates="department", foreign_keys="Teacher.department_id"
    )
    courses: Mapped[List["Course"]] = relationship("Course", back_populates="department")

    def __repr__(self) -> str:
        return f"<Department {self.code}: {self.name}>"


class Teacher(Base):
    __tablename__ = "teachers"

    id:            Mapped[int]           = mapped_column(Integer, primary_key=True, autoincrement=True)
    department_id: Mapped[int]           = mapped_column(Integer, ForeignKey("departments.id"), nullable=False)
    first_name:    Mapped[str]           = mapped_column(String(80), nullable=False)
    last_name:     Mapped[str]           = mapped_column(String(80), nullable=False)
    email:         Mapped[str]           = mapped_column(String(200), unique=True, nullable=False)
    title:         Mapped[str]           = mapped_column(String(50), default="Lecturer")
    hire_date:     Mapped[Optional[str]] = mapped_column(String(10), nullable=True)
    salary:        Mapped[float]         = mapped_column(Float, default=0.0)
    is_active:     Mapped[int]          = mapped_column(Integer, default=1)

    # Relationships
    department: Mapped["Department"]    = relationship("Department", back_populates="teachers", foreign_keys=[department_id])
    courses:    Mapped[List["Course"]]  = relationship("Course", back_populates="teacher")

    @property
    def full_name(self) -> str:
        return f"{self.first_name} {self.last_name}"

    def __repr__(self) -> str:
        return f"<Teacher {self.full_name} ({self.title})>"


class Student(Base):
    __tablename__ = "students"

    id:            Mapped[int]           = mapped_column(Integer, primary_key=True, autoincrement=True)
    student_number: Mapped[str]          = mapped_column(String(20), unique=True, nullable=False)
    first_name:    Mapped[str]           = mapped_column(String(80), nullable=False)
    last_name:     Mapped[str]           = mapped_column(String(80), nullable=False)
    email:         Mapped[str]           = mapped_column(String(200), unique=True, nullable=False)
    date_of_birth: Mapped[Optional[str]] = mapped_column(String(10), nullable=True)
    enrollment_year: Mapped[int]         = mapped_column(Integer, default=datetime.now().year)
    gpa:           Mapped[float]         = mapped_column(Float, default=0.0)
    is_active:     Mapped[int]          = mapped_column(Integer, default=1)

    # Many-to-many
    courses: Mapped[List["Course"]] = relationship(
        "Course", secondary=enrollment, back_populates="students"
    )
    # One-to-many
    attendance: Mapped[List["Attendance"]] = relationship("Attendance", back_populates="student")

    @property
    def full_name(self) -> str:
        return f"{self.first_name} {self.last_name}"

    def __repr__(self) -> str:
        return f"<Student {self.student_number}: {self.full_name}>"


class Course(Base):
    __tablename__ = "courses"

    id:           Mapped[int]           = mapped_column(Integer, primary_key=True, autoincrement=True)
    department_id: Mapped[int]          = mapped_column(Integer, ForeignKey("departments.id"), nullable=False)
    teacher_id:   Mapped[Optional[int]] = mapped_column(Integer, ForeignKey("teachers.id"), nullable=True)
    code:         Mapped[str]           = mapped_column(String(20), unique=True, nullable=False)
    name:         Mapped[str]           = mapped_column(String(200), nullable=False)
    credits:      Mapped[int]           = mapped_column(Integer, default=3)
    max_students: Mapped[int]           = mapped_column(Integer, default=30)
    semester:     Mapped[str]           = mapped_column(String(20), default="Spring 2025")
    is_active:    Mapped[int]          = mapped_column(Integer, default=1)

    # Relationships
    department: Mapped["Department"]     = relationship("Department", back_populates="courses")
    teacher:    Mapped[Optional["Teacher"]] = relationship("Teacher", back_populates="courses")
    students:   Mapped[List["Student"]]  = relationship(
        "Student", secondary=enrollment, back_populates="courses"
    )
    attendance: Mapped[List["Attendance"]] = relationship("Attendance", back_populates="course")

    def __repr__(self) -> str:
        return f"<Course {self.code}: {self.name}>"


class Attendance(Base):
    __tablename__ = "attendance"

    id:         Mapped[int]  = mapped_column(Integer, primary_key=True, autoincrement=True)
    student_id: Mapped[int]  = mapped_column(Integer, ForeignKey("students.id"), nullable=False)
    course_id:  Mapped[int]  = mapped_column(Integer, ForeignKey("courses.id"), nullable=False)
    date:       Mapped[str]  = mapped_column(String(10), nullable=False)
    present:    Mapped[int] = mapped_column(Integer, default=1)
    notes:      Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Relationships
    student: Mapped["Student"] = relationship("Student", back_populates="attendance")
    course:  Mapped["Course"]  = relationship("Course", back_populates="attendance")

    def __repr__(self) -> str:
        status = "Present" if self.present == 1 else "Absent"
        return f"<Attendance {self.date} s={self.student_id} c={self.course_id} {status}>"


# ─── Helpers ──────────────────────────────────────────────────────────────────


def print_header(title: str) -> None:
    line = "─" * 65
    print(f"\n{line}\n  {title}\n{line}")


def print_table(rows: list, cols: list) -> None:
    if not rows:
        print("  (no rows)")
        return
    widths = {c: len(str(c)) for c in cols}
    for row in rows:
        for c in cols:
            v = str(row[c]) if isinstance(row, dict) else str(getattr(row, c, "NULL"))
            widths[c] = max(widths[c], len(v))
    header = "  " + "  ".join(str(c).ljust(widths[c]) for c in cols)
    sep    = "  " + "  ".join("─" * widths[c] for c in cols)
    print(header)
    print(sep)
    for row in rows:
        line = "  " + "  ".join(
            str(row[c] if isinstance(row, dict) else getattr(row, c, "NULL")).ljust(widths[c])
            for c in cols
        )
        print(line)


# ─── Main ─────────────────────────────────────────────────────────────────────


def main() -> None:
    print("School Management System — sqlite-server SQLAlchemy ORM Example")
    print("=================================================================\n")

    # Create engine & tables
    engine = create_engine(DATABASE_URL, echo=False, future=True)
    Base.metadata.create_all(engine)
    print("Schema created (all tables).\n")

    SessionLocal = sessionmaker(bind=engine, autocommit=False, autoflush=False)

    with SessionLocal() as session:

        # ── 1. Create Departments ─────────────────────────────────────────────
        print_header("1. Create Departments")

        dept_cs   = Department(name="Computer Science",  code="CS",   description="Software, algorithms, AI")
        dept_math = Department(name="Mathematics",       code="MATH", description="Pure and applied math")
        dept_phys = Department(name="Physics",           code="PHYS", description="Classical and modern physics")
        dept_eng  = Department(name="English Literature",code="ENG",  description="Language and literature")

        session.add_all([dept_cs, dept_math, dept_phys, dept_eng])
        session.commit()

        for d in [dept_cs, dept_math, dept_phys, dept_eng]:
            print(f"  [{d.code}] {d.name}")

        # ── 2. Create Teachers ────────────────────────────────────────────────
        print_header("2. Create Teachers")

        teachers_data = [
            dict(department_id=dept_cs.id,   first_name="Dr. Sarah",  last_name="Chen",      email="s.chen@school.edu",    title="Professor",        hire_date="2015-09-01", salary=95000),
            dict(department_id=dept_cs.id,   first_name="Prof. James",last_name="Miller",     email="j.miller@school.edu",  title="Associate Prof.",  hire_date="2018-01-15", salary=82000),
            dict(department_id=dept_math.id, first_name="Dr. Emily",  last_name="Johnson",    email="e.johnson@school.edu", title="Professor",        hire_date="2012-08-20", salary=98000),
            dict(department_id=dept_phys.id, first_name="Dr. Alan",   last_name="Rodriguez",  email="a.rodriguez@school.edu",title="Senior Lecturer", hire_date="2019-03-01", salary=78000),
            dict(department_id=dept_eng.id,  first_name="Ms. Rachel", last_name="Thompson",   email="r.thompson@school.edu",title="Lecturer",         hire_date="2021-09-01", salary=65000),
        ]

        teachers = []
        for td in teachers_data:
            t = Teacher(**td)
            session.add(t)
            teachers.append(t)
        session.commit()

        for t in teachers:
            print(f"  {t.full_name:<22}  {t.title:<20}  {t.department.name}")

        # Set department heads
        dept_cs.head_id   = teachers[0].id
        dept_math.head_id = teachers[2].id
        dept_phys.head_id = teachers[3].id
        session.commit()
        print("  Department heads assigned.")

        # ── 3. Create Students ────────────────────────────────────────────────
        print_header("3. Enroll Students")

        students_data = [
            dict(student_number="STU-001", first_name="Alice",   last_name="Williams", email="alice.w@student.edu",  date_of_birth="2002-05-15", enrollment_year=2022),
            dict(student_number="STU-002", first_name="Bob",     last_name="Anderson", email="bob.a@student.edu",    date_of_birth="2001-11-03", enrollment_year=2021),
            dict(student_number="STU-003", first_name="Carol",   last_name="Martinez", email="carol.m@student.edu",  date_of_birth="2003-02-28", enrollment_year=2023),
            dict(student_number="STU-004", first_name="Dave",    last_name="Taylor",   email="dave.t@student.edu",   date_of_birth="2002-08-14", enrollment_year=2022),
            dict(student_number="STU-005", first_name="Eve",     last_name="Brown",    email="eve.b@student.edu",    date_of_birth="2001-04-30", enrollment_year=2021),
            dict(student_number="STU-006", first_name="Frank",   last_name="Wilson",   email="frank.w@student.edu",  date_of_birth="2003-09-10", enrollment_year=2023),
            dict(student_number="STU-007", first_name="Grace",   last_name="Lee",      email="grace.l@student.edu",  date_of_birth="2002-01-25", enrollment_year=2022),
            dict(student_number="STU-008", first_name="Henry",   last_name="Davis",    email="henry.d@student.edu",  date_of_birth="2000-12-05", enrollment_year=2020),
        ]

        students = []
        for sd in students_data:
            s = Student(**sd)
            session.add(s)
            students.append(s)
        session.commit()

        for s in students:
            print(f"  {s.student_number}  {s.full_name:<20}  enrolled {s.enrollment_year}")

        # ── 4. Create Courses ─────────────────────────────────────────────────
        print_header("4. Create Courses")

        courses_data = [
            dict(department_id=dept_cs.id,   teacher_id=teachers[0].id, code="CS101",   name="Introduction to Programming",    credits=4, max_students=30),
            dict(department_id=dept_cs.id,   teacher_id=teachers[1].id, code="CS201",   name="Data Structures & Algorithms",   credits=4, max_students=25),
            dict(department_id=dept_cs.id,   teacher_id=teachers[0].id, code="CS301",   name="Machine Learning Fundamentals",  credits=3, max_students=20),
            dict(department_id=dept_math.id, teacher_id=teachers[2].id, code="MATH101", name="Calculus I",                     credits=4, max_students=35),
            dict(department_id=dept_math.id, teacher_id=teachers[2].id, code="MATH201", name="Linear Algebra",                 credits=3, max_students=30),
            dict(department_id=dept_phys.id, teacher_id=teachers[3].id, code="PHYS101", name="Classical Mechanics",            credits=4, max_students=30),
            dict(department_id=dept_eng.id,  teacher_id=teachers[4].id, code="ENG101",  name="Academic Writing",               credits=2, max_students=25),
        ]

        courses = []
        for cd in courses_data:
            c = Course(**cd)
            session.add(c)
            courses.append(c)
        session.commit()

        for c in courses:
            print(f"  [{c.code}]  {c.name:<40}  {c.credits} cr  max={c.max_students}")

        # ── 5. Enroll Students in Courses ─────────────────────────────────────
        print_header("5. Enroll Students in Courses")

        # Using append() on the ORM many-to-many relationship
        enrollments = [
            (students[0], [courses[0], courses[1], courses[3], courses[6]]),  # Alice → CS101, CS201, MATH101, ENG101
            (students[1], [courses[0], courses[1], courses[2], courses[3]]),  # Bob   → CS101, CS201, CS301, MATH101
            (students[2], [courses[3], courses[4], courses[5]]),              # Carol → MATH101, MATH201, PHYS101
            (students[3], [courses[0], courses[3], courses[5], courses[6]]),  # Dave  → CS101, MATH101, PHYS101, ENG101
            (students[4], [courses[1], courses[2], courses[3]]),              # Eve   → CS201, CS301, MATH101
            (students[5], [courses[0], courses[5], courses[6]]),              # Frank → CS101, PHYS101, ENG101
            (students[6], [courses[2], courses[3], courses[4]]),              # Grace → CS301, MATH101, MATH201
            (students[7], [courses[0], courses[1], courses[2]]),              # Henry → CS101, CS201, CS301
        ]

        for student, enrolled_courses in enrollments:
            for course in enrolled_courses:
                student.courses.append(course)
        session.commit()

        total_enrollments = sum(len(ec) for _, ec in enrollments)
        print(f"  Enrolled {len(students)} students into courses ({total_enrollments} total enrollments).")

        # ── 6. Add Grades (update enrollment table via Core) ──────────────────
        print_header("6. Record Final Grades")

        grade_data = [
            (students[0].id, courses[0].id, 92.5),
            (students[0].id, courses[1].id, 88.0),
            (students[1].id, courses[0].id, 78.5),
            (students[1].id, courses[1].id, 85.0),
            (students[1].id, courses[2].id, 91.0),
            (students[2].id, courses[3].id, 95.0),
            (students[3].id, courses[0].id, 72.0),
            (students[4].id, courses[1].id, 89.5),
            (students[4].id, courses[2].id, 93.0),
            (students[5].id, courses[0].id, 68.0),
            (students[6].id, courses[2].id, 96.5),
            (students[7].id, courses[0].id, 84.0),
            (students[7].id, courses[1].id, 79.0),
            (students[7].id, courses[2].id, 87.5),
        ]

        for sid, cid, grade in grade_data:
            session.execute(
                text(
                    "UPDATE enrollment SET grade = :g "
                    "WHERE student_id = :s AND course_id = :c"
                ),
                {"g": grade, "s": sid, "c": cid},
            )
        session.commit()
        print(f"  Updated {len(grade_data)} student grades.")

        # Update GPA (avg grade per student)
        for student in students:
            result = session.execute(
                text(
                    "SELECT AVG(grade) AS avg_grade FROM enrollment "
                    "WHERE student_id = :sid AND grade IS NOT NULL"
                ),
                {"sid": student.id},
            ).first()
            if result and result.avg_grade is not None:
                student.gpa = round(result.avg_grade, 2)
        session.commit()

        # ── 7. Record Attendance ──────────────────────────────────────────────
        print_header("7. Record Attendance")

        attendance_records = [
            Attendance(student_id=students[0].id, course_id=courses[0].id, date="2025-03-10", present=1),
            Attendance(student_id=students[0].id, course_id=courses[0].id, date="2025-03-12", present=1),
            Attendance(student_id=students[1].id, course_id=courses[0].id, date="2025-03-10", present=0, notes="Sick"),
            Attendance(student_id=students[1].id, course_id=courses[0].id, date="2025-03-12", present=1),
            Attendance(student_id=students[3].id, course_id=courses[0].id, date="2025-03-10", present=1),
            Attendance(student_id=students[3].id, course_id=courses[0].id, date="2025-03-12", present=0, notes="No reason"),
            Attendance(student_id=students[5].id, course_id=courses[0].id, date="2025-03-10", present=1),
            Attendance(student_id=students[7].id, course_id=courses[0].id, date="2025-03-10", present=1),
            Attendance(student_id=students[7].id, course_id=courses[0].id, date="2025-03-12", present=1),
        ]
        session.add_all(attendance_records)
        session.commit()
        print(f"  Added {len(attendance_records)} attendance records.")

        # ── 8. ORM Query — Students with Courses (selectinload) ───────────────
        print_header("8. Students with Their Enrolled Courses (ORM SELECT)")

        stmt = (
            select(Student)
            .options(selectinload(Student.courses))
            .where(Student.is_active == 1)
            .order_by(Student.last_name)
        )
        all_students = session.execute(stmt).scalars().all()

        for s in all_students:
            course_list = ", ".join(c.code for c in s.courses)
            print(f"  {s.full_name:<22}  GPA={s.gpa:.2f}  courses=[{course_list}]")

        # ── 9. Teacher → Courses (joinedload) ─────────────────────────────────
        print_header("9. Teachers and Their Courses")

        stmt = (
            select(Teacher)
            .options(joinedload(Teacher.courses), joinedload(Teacher.department))
            .where(Teacher.is_active == 1)
            .order_by(Teacher.last_name)
        )
        all_teachers = session.execute(stmt).unique().scalars().all()

        for t in all_teachers:
            course_names = ", ".join(c.code for c in t.courses)
            print(f"  {t.full_name:<25}  {t.department.code:<6}  courses=[{course_names or 'none'}]")

        # ── 10. Aggregate — Course Enrollment Stats ────────────────────────────
        print_header("10. Course Enrollment & Grade Statistics")

        stmt = (
            select(
                Course.code,
                Course.name,
                func.count(enrollment.c.student_id).label("enrolled"),
                func.avg(enrollment.c.grade).label("avg_grade"),
                func.min(enrollment.c.grade).label("min_grade"),
                func.max(enrollment.c.grade).label("max_grade"),
            )
            .select_from(Course)
            .outerjoin(enrollment, Course.id == enrollment.c.course_id)
            .group_by(Course.id, Course.code, Course.name)
            .order_by(func.count(enrollment.c.student_id).desc())
        )

        rows = session.execute(stmt).all()
        print(f"  {'Code':<10}  {'Name':<40}  {'Enr':>5}  {'Avg':>6}  {'Min':>6}  {'Max':>6}")
        print("  " + "─" * 78)
        for r in rows:
            avg = f"{r.avg_grade:.1f}" if r.avg_grade else "N/A"
            mn  = f"{r.min_grade:.1f}" if r.min_grade else "N/A"
            mx  = f"{r.max_grade:.1f}" if r.max_grade else "N/A"
            print(f"  {r.code:<10}  {r.name:<40}  {r.enrolled:>5}  {avg:>6}  {mn:>6}  {mx:>6}")

        # ── 11. Department Statistics ─────────────────────────────────────────
        print_header("11. Department Statistics")

        stmt = (
            select(
                Department.name.label("department"),
                func.count(Teacher.id.distinct()).label("teachers"),
                func.count(Course.id.distinct()).label("courses"),
                func.avg(Teacher.salary).label("avg_salary"),
            )
            .select_from(Department)
            .outerjoin(Teacher, and_(Teacher.department_id == Department.id, Teacher.is_active == 1))
            .outerjoin(Course, and_(Course.department_id == Department.id, Course.is_active == 1))
            .group_by(Department.id, Department.name)
            .order_by(func.count(Teacher.id.distinct()).desc())
        )

        rows = session.execute(stmt).all()
        print(f"  {'Department':<25}  {'Teachers':>9}  {'Courses':>8}  {'Avg Salary':>12}")
        print("  " + "─" * 60)
        for r in rows:
            salary = f"${r.avg_salary:,.0f}" if r.avg_salary else "N/A"
            print(f"  {r.department:<25}  {r.teachers:>9}  {r.courses:>8}  {salary:>12}")

        # ── 12. Top Students by GPA ────────────────────────────────────────────
        print_header("12. Top Students by GPA")

        stmt = (
            select(Student)
            .where(Student.gpa > 0)
            .order_by(Student.gpa.desc())
            .limit(5)
        )
        top_students = session.execute(stmt).scalars().all()

        for i, s in enumerate(top_students, 1):
            print(f"  #{i}  {s.full_name:<22}  GPA={s.gpa:.2f}  year={s.enrollment_year}")

        # ── 13. Attendance Report for CS101 ───────────────────────────────────
        print_header("13. Attendance Report — CS101 Introduction to Programming")

        stmt = (
            select(
                Student.first_name + " " + Student.last_name,
                func.count(Attendance.id).label("total"),
                func.sum(
                    func.cast(Attendance.present, Integer)
                ).label("present_count"),
            )
            .select_from(Student)
            .join(Attendance, Student.id == Attendance.student_id)
            .join(Course, and_(Course.id == Attendance.course_id, Course.code == "CS101"))
            .group_by(Student.id, Student.first_name, Student.last_name)
            .order_by(Student.last_name)
        )

        rows = session.execute(stmt).all()
        print(f"  {'Student':<22}  {'Classes':>8}  {'Present':>8}  {'Absent':>8}")
        print("  " + "─" * 54)
        for name, total, present in rows:
            absent = total - (present or 0)
            print(f"  {name:<22}  {total:>8}  {present or 0:>8}  {absent:>8}")

        # ── 14. Search Students by Name ────────────────────────────────────────
        print_header("14. Search Students Containing 'a' in Name")

        stmt = (
            select(Student)
            .where(
                or_(
                    func.lower(Student.first_name).contains("a"),
                    func.lower(Student.last_name).contains("a"),
                )
            )
            .order_by(Student.last_name)
        )
        found = session.execute(stmt).scalars().all()
        print(f"  Found {len(found)} student(s):")
        for s in found:
            print(f"    - {s.full_name} ({s.student_number})")

        # ── 15. Cleanup ───────────────────────────────────────────────────────
        print_header("15. Cleanup (drop all tables)")

        # Drop all to clean state
        Base.metadata.drop_all(engine)
        print("  All tables dropped.")

        print_header("Done — All 15 steps completed successfully!")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"\nFatal error: {exc}", file=sys.stderr)
        raise
