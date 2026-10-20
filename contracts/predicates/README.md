# Predicate language (SoT 43.7)

A small, deterministic expression language over evidence fields: boolean logic, comparisons, `exists`, `count`, field access on evidence entities and time arithmetic. No loops, no calls except the three below, no model. Pack predicates (outcome success predicates, policy conditions, capability and graph pre- and postconditions) are parsed and type-checked at pack load; a predicate that names an unknown entity, field or variable fails the pack.

```
expr    = or
or      = and { "or" and }
and     = not { "and" not }
not     = "not" not | compare
compare = sum [ ("==" | "!=" | "<" | "<=" | ">" | ">=") sum | ["not"] "in" "[" literal { "," literal } "]" ]
sum     = atom { ("+" | "-") atom }
atom    = literal | path | "(" expr ")" | "now" "(" ")"
        | ("exists" | "count") "(" entity "where" expr ")"
path    = name { "." name }
literal = 'text' | integer | integer("s"|"m"|"h"|"d") | "true" | "false" | "null"
```

**Types.** Boolean, String, Integer, Timestamp, Duration, Reference (an opaque ID), Null, and Enum (a closed list of upper-case values). `exists` is Boolean and `count` is Integer; `exists(case where ...)` binds `case` to the evidence entity of that name, and inside it `case.field` is a field of the entity. Outside, a name is a declared variable (a dotted name such as `subject_resource.status` is one variable).

**Typing rules.** `and`, `or`, `not` need Boolean. `==` and `!=` need compatible types (Reference compares with Reference or text; Enum compares with the same Enum or a text literal that is one of its values; `null` compares with anything). `<`, `<=`, `>`, `>=` need Integer, Timestamp or Duration. `Integer +/- Integer` and `Duration +/- Duration` give the same type; `Timestamp +/- Duration` gives Timestamp; `Timestamp - Timestamp` gives Duration. A `where` condition is Boolean. A predicate used by a pack must be Boolean.

**Error codes.** `SYNTAX`, `UNKNOWN_NAME`, `UNKNOWN_ENTITY`, `UNKNOWN_FIELD`, `UNKNOWN_ENUM_VALUE`, `TYPE_MISMATCH`, `NOT_BOOLEAN`.

**One corpus, two implementations.** `tests/corpus.json` lists expressions with the type each must have or the error code each must fail with, over a scope written in the pack's own `evidence_descriptors` and `predicate_variables` shapes. The Python reference (`tools/packcheck/predicate.py`) and the Go evaluator (`services/core/internal/predicate`, T6.1) both run the whole corpus, so they cannot disagree on what is accepted.

Open decisions (AGENT-02): the field vocabulary (`evidence_descriptors`) is not in the SoT or PERD; the null and duration literals are the smallest additions that the SoT examples and PERD section 23.5 need.
