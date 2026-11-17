"""Behaviour of the CON-05 money rules (REQ-DATA-002; DEC-2026-10-16 money-and-value-lists M1, M4, M6).

Each case writes a tiny tree like the repository and lints it, so a rule that stops firing, or starts firing on allowed code, fails here. The canaries
under canaries/ prove each rule can fail on the real harness; these cases prove the edges. Run: python3 -I -m unittest discover -s tools/lint -p 'test_*.py'
"""
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

GO_MOD = "module github.com/Noemai-org/noema/services/core\n\ngo 1.24\n"


def lint(files: dict[str, str]) -> set[str]:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        for rel, text in files.items():
            path = root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        (root / "services/core").mkdir(parents=True, exist_ok=True)
        (root / "services/core/go.mod").write_text(GO_MOD)
        return {v[0] for v in run.lint(root)}


def go(body: str, imports: str = "") -> dict[str, str]:
    return {"services/core/edge/m.go": f"package edge\n{imports}\n{body}\n"}


def py(body: str) -> dict[str, str]:
    return {"services/model/noema_model/x.py": body}


def sql(body: str) -> dict[str, str]:
    return {"services/core/migrations/0001_x.sql": body}


class GoMultiplier(unittest.TestCase):
    def test_a_literal_power_of_ten_on_a_money_value_is_refused_in_every_form(self) -> None:
        for expr in ("amountMinor * 100", "amountMinor / 100", "amountMinor % 100", "100 * amountMinor", "(amountMinor) * 1000", "amountMinor * 100.0",
                     "amountMinor * 1e2", "amountMinor * 10", "feeMinor * 1_000", "amountMinor * math.Pow10(2)", "amountMinor * math.Pow(10, 2)", "price.Amount * 100"):
            with self.subTest(expr=expr):
                body = f"func f(amountMinor, feeMinor int64, price struct{{ Amount int64 }}) int64 {{ return {expr} }}"
                self.assertIn("money-literal-multiplier", lint(go(body, 'import "math"')))

    def test_arithmetic_that_is_not_a_power_of_ten_on_money_is_allowed(self) -> None:
        for expr in ("amountMinor * 2", "amountMinor + 100", "amountMinor - 100", "amountMinor * exponentFactor", "amountMinor * 11", "amountMinor / 3", "elapsed * 1000", "count / 100", "elapsed * math.Pow10(3)"):
            with self.subTest(expr=expr):
                body = f"func f(amountMinor, exponentFactor, elapsed, count int64) int64 {{ return {expr} }}"
                self.assertNotIn("money-literal-multiplier", lint(go(body, 'import "math"')))


class GoDecimal(unittest.TestCase):
    def test_a_money_field_of_a_decimal_or_arbitrary_precision_type_is_refused(self) -> None:
        for typ in ("Decimal", "*big.Rat", "big.Float", "decimal.Decimal"):
            with self.subTest(typ=typ):
                body = f"type Decimal struct{{}}\ntype Line struct{{ AmountMinor {typ} }}"
                self.assertIn("money-decimal-field", lint(go(body, 'import ("math/big"; "github.com/shopspring/decimal")\nvar _ = big.NewInt\nvar _ decimal.Decimal')))

    def test_a_rate_that_is_not_money_may_be_exact(self) -> None:
        body = "type Cfg struct{ ErrorRate *big.Rat }"
        self.assertNotIn("money-decimal-field", lint(go(body, 'import "math/big"')))


class PyMultiplier(unittest.TestCase):
    def test_a_literal_power_of_ten_on_a_money_value_is_refused(self) -> None:
        for expr in ("amount_minor * 100", "amount_minor // 100", "100 * amount_minor", "amount_minor / 1e2", "amount_minor * 10**2", "amount_minor * pow(10, 2)", "price.amount * 1000", "(amount_minor + 1) * 100"):
            with self.subTest(expr=expr):
                self.assertIn("py-money-multiplier", lint(py(f"def f(amount_minor, price):\n    return {expr}\n")))

    def test_other_arithmetic_is_allowed(self) -> None:
        for expr in ("amount_minor * 2", "amount_minor + 100", "amount_minor * factor", "elapsed * 1000", "count // 100", "elapsed * 10**3"):
            with self.subTest(expr=expr):
                self.assertNotIn("py-money-multiplier", lint(py(f"def f(amount_minor, factor, elapsed, count):\n    return {expr}\n")))


class SqlTypes(unittest.TestCase):
    def test_a_money_column_or_cast_of_an_inexact_type_is_refused(self) -> None:
        for text in ("CREATE TABLE t (amount_minor NUMERIC);", "CREATE TABLE t (fee_minor DOUBLE PRECISION);", "CREATE TABLE t (amount real);", "CREATE TABLE t (amount_total decimal(12,2));",
                     "ALTER TABLE t ALTER COLUMN amount_minor TYPE numeric;", "ALTER TABLE t ALTER COLUMN amount_minor SET DATA TYPE float8;", "SELECT amount_minor::numeric FROM t;",
                     "SELECT (amount_minor)::double precision FROM t;", "CREATE TABLE t (balance money);",
                     # Codex's P2 on noema#669: the standard CAST(... AS ...) form is a cast of a money value as well
                     "SELECT CAST(amount_minor AS NUMERIC) FROM t;", "SELECT cast(amount_minor as numeric) FROM t;", "SELECT CAST(t.amount_minor AS decimal(12,2)) FROM t;",
                     "SELECT CAST((amount_minor) AS double precision) FROM t;", "SELECT CAST( amount_minor\n   AS real ) FROM t;", "SELECT CAST(fee_minor + 1 AS float8) FROM t;",
                     "SELECT CAST(amount_minor AS money) FROM t;", "SELECT CAST ( amount_minor AS NUMERIC ) FROM t;"):
            with self.subTest(text=text):
                self.assertIn("money-sql-type", lint(sql(text)))

    def test_exact_money_and_inexact_non_money_are_allowed(self) -> None:
        for text in ("CREATE TABLE t (amount_minor BIGINT NOT NULL CHECK (amount_minor BETWEEN 0 AND 9007199254740991));", "CREATE TABLE t (ratio numeric, elapsed real);",
                     "-- amount_minor NUMERIC in a comment\nSELECT 1;", "SELECT 'amount_minor numeric';", "SELECT (r.json->>'attempt')::numeric FROM r;", "/* amount real */ SELECT 1;",
                     "SELECT CAST(amount_minor AS BIGINT) FROM t;", "SELECT CAST(r.attempt AS numeric) FROM r;", "SELECT CAST(elapsed AS real) FROM t;",
                     "SELECT CAST('amount_minor' AS numeric) FROM t;", "-- CAST(amount_minor AS NUMERIC)\nSELECT 1;", "SELECT CAST(amount_minor AS BIGINT) AS numeric FROM t;"):
            with self.subTest(text=text):
                self.assertNotIn("money-sql-type", lint(sql(text)))

    def test_the_line_number_is_the_line_of_the_column(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "services/core/migrations/0001_x.sql"
            path.parent.mkdir(parents=True)
            path.write_text("-- one\n/* two\nthree */\nCREATE TABLE t (\n    amount_minor numeric\n);\n")
            import pyrules
            self.assertEqual([(v[1], v[2]) for v in pyrules.money_sql(Path(tmp))], [("services/core/migrations/0001_x.sql", 5)])


if __name__ == "__main__":
    unittest.main()
