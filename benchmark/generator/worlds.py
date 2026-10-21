"""Scenario worlds: the principals, beneficiaries, transactions and cases of a scenario, built from a declarative spec.

An archetype describes its world with labels and tokens; `build_world` turns that into a benchmark-fixture document and a
table of names (labels to resource refs, first names, amounts, a time phrase) that the utterance and the expectations are
written against. Everything is derived from (seed, variant), so a world is reproducible. SYNTHETIC (AGENT-25).
"""
from dataclasses import dataclass, field
from typing import Any

from .rng import below, pick, pick_distinct

Json = dict[str, Any]

FIRST_NAMES = ("Tunde", "Bola", "Chidi", "Amina", "Emeka", "Funke", "Ibrahim", "Ngozi", "Seun", "Hauwa", "Kemi", "Obinna")
SURNAMES = ("Adeyemi", "Bakare", "Okafor", "Bello", "Eze", "Salami", "Danjuma", "Nwosu", "Ogunleye", "Musa", "Balogun", "Okonkwo")
CUSTOMER_NAMES = ("Ada", "Bayo", "Cheta", "Dayo", "Efe", "Gift", "Hassan", "Ife")
# (text as a customer writes it, whole minor units of NGN, text as a speaker says it)
AMOUNTS = (
    ("20k", 2_000_000, "twenty thousand naira"),
    ("5,000 naira", 500_000, "five thousand naira"),
    ("N50,000", 5_000_000, "fifty thousand naira"),
    ("10k", 1_000_000, "ten thousand naira"),
    ("N2,500", 250_000, "two thousand five hundred naira"),
    ("30k", 3_000_000, "thirty thousand naira"),
)
NEAR_DELTA_MINOR = 5_000  # N50: "said 20k, actual 20,050" (EC-A07)
SOURCES_OK = {"ledger": "POSTED", "switch": "APPROVED", "destination": "CREDITED", "settlement": "SETTLED"}


@dataclass(frozen=True)
class Ben:
    label: str
    first: str = "$N1"            # token for the first name
    alias: str | None = None      # a nickname the beneficiary list carries
    owner: str = "me"             # "me" or "other"
    surname_index: int = 0
    last4: str | None = None


@dataclass(frozen=True)
class Tx:
    label: str
    ben: str | None = "b1"
    amount: str = "$A"            # $A, $B, $A+ (the stated amount plus a small delta) or $Ahalf
    age: int = 150                # minutes before the case starts
    owner: str = "me"
    direction: str = "OUTGOING"
    states: tuple[tuple[str, str], ...] = ()


@dataclass(frozen=True)
class WorldSpec:
    bens: tuple[Ben, ...] = (Ben("b1"),)
    txs: tuple[Tx, ...] = (Tx("t1"),)
    cases: tuple[tuple[str, str, str], ...] = ()                   # (label, transaction label, status)
    strings: tuple[tuple[str, str, str], ...] = ()                 # (transaction label, field, text)
    extra_principals: tuple[tuple[str, tuple[str, ...]], ...] = ()  # (label, roles)


@dataclass
class World:
    fixture: Json
    refs: dict[str, str] = field(default_factory=dict)            # label -> resource ref ("t1", "case1", ...)
    tokens: dict[str, str] = field(default_factory=dict)          # "$N1" -> "Tunde", "$A" -> "20k", "$W" -> "this morning"
    principal: str = ""
    other_principal: str = ""
    amount_minor: dict[str, int] = field(default_factory=dict)    # "$A" -> 2000000


def when_phrase(age_minutes: int, pidgin: bool) -> str:
    if age_minutes < 720:
        return "dis morning" if pidgin else "this morning"
    if age_minutes < 1440:
        return "last night"
    if age_minutes < 4320:
        return "yesterday"
    return "last week"


def _amount(token: str, a: tuple[str, int, str], b: tuple[str, int, str]) -> int:
    if token == "$A":
        return a[1]
    if token == "$B":
        return b[1]
    if token == "$A+":
        return a[1] + NEAR_DELTA_MINOR
    if token == "$Y":    # "ẹgbẹ̀rún méjì": 2,000 naira
        return 200_000
    if token == "$Y5":   # "puku ise" (Igbo) 5,000 naira
        return 500_000
    if token == "$Y10":  # 20,000 naira, ten times $Y
        return 2_000_000
    if token == "$Y20":  # "dubu ashirin" (Hausa) 20,000 naira
        return 2_000_000
    raise ValueError(f"unknown amount token {token!r}")


def build_world(spec: WorldSpec, seed: int, label: str, variant: int, pidgin: bool, voice: bool) -> World:
    """Instantiate a world. `label` keeps one archetype's worlds apart from another's."""
    scope = f"{label}:{variant}"
    number = 1 + below(seed, f"{scope}:principal", 180)
    principal = f"usr_synth{number:03d}"
    other = f"usr_synth{number + 600:03d}"
    names = pick_distinct(seed, f"{scope}:names", FIRST_NAMES, 4)
    amounts = pick_distinct(seed, f"{scope}:amounts", AMOUNTS, 2)
    a, b = amounts
    tokens: dict[str, str] = {f"$N{i + 1}": names[i] for i in range(4)}
    tokens["$Y"] = "ẹgbẹ̀rún méjì"
    tokens["$A"] = a[2] if voice else a[0]
    tokens["$B"] = b[2] if voice else b[0]
    amount_minor = {"$A": a[1], "$B": b[1], "$A+": a[1] + NEAR_DELTA_MINOR}
    surnames = pick_distinct(seed, f"{scope}:surnames", SURNAMES, 4)
    ids = pick_distinct(seed, f"{scope}:ids", range(1000, 9999), len(spec.txs) + len(spec.cases) + 1)
    last4 = pick_distinct(seed, f"{scope}:last4", range(1000, 9999), len(spec.bens))
    for index, digits in enumerate(last4):
        tokens[f"$D{index + 1}"] = str(digits)

    principals: list[Json] = [{"principal_id": principal, "roles": ["customer"], "display_name": pick(seed, f"{scope}:me", CUSTOMER_NAMES)},
                  {"principal_id": other, "roles": ["customer"], "display_name": pick(seed, f"{scope}:other", CUSTOMER_NAMES)}]
    for index, (extra, roles) in enumerate(spec.extra_principals):
        pid = f"usr_synth{number + 700 + index:03d}"
        principals.append({"principal_id": pid, "roles": list(roles), "display_name": extra})
    owners = {"me": principal, "other": other}

    beneficiaries: list[Json] = []
    ben_ids: dict[str, str] = {}
    for index, ben in enumerate(spec.bens):
        first = tokens.get(ben.first, ben.first)
        ben_ids[ben.label] = f"ben_{index + 1:04d}"
        beneficiaries.append({
            "beneficiary_id": ben_ids[ben.label],
            "owner": owners[ben.owner],
            "name": f"{first} {surnames[ben.surname_index % 4]}",
            "aliases": [first] + ([ben.alias] if ben.alias else []),
            "account_last4": ben.last4 or f"{last4[index]}",
        })

    refs: dict[str, str] = {}
    transactions: list[Json] = []
    for index, tx in enumerate(spec.txs):
        ref = f"fin.transaction:TX{ids[index]}"
        refs[tx.label] = ref
        tokens[f"$R_{tx.label}"] = ref.split(":", 1)[1]
        states = {**SOURCES_OK, **dict(tx.states)}
        transactions.append({
            "ref": ref, "owner": owners[tx.owner], "beneficiary": ben_ids.get(tx.ben) if tx.ben else None,
            "direction": tx.direction, "amount_minor": _amount(tx.amount, a, b), "currency": "NGN",
            "age_minutes": tx.age + below(seed, f"{scope}:age:{tx.label}", 8), **states,
        })
    cases: list[Json] = []
    for index, (case_label, tx_label, status) in enumerate(spec.cases):
        ref = f"fin.case:CS{ids[len(spec.txs) + index]}"
        refs[case_label] = ref
        cases.append({"ref": ref, "transaction": refs[tx_label], "status": status, "owner_queue": "SYNTH_QUEUE_L1"})
    strings = [{"target": refs[tx_label], "field": fld, "text": text} for tx_label, fld, text in spec.strings]

    target_tx = next((t for t in transactions if t["owner"] == principal), transactions[0] if transactions else None)
    tokens["$O"] = principals[1]["display_name"]
    tokens["$R_ghost"] = f"TX{ids[-1]}"
    tokens["$W"] = when_phrase(target_tx["age_minutes"] if target_tx else 150, pidgin)
    fixture = {
        "fixture_id": f"fx_{label}_{variant:04d}", "synthetic": True, "principals": principals, "beneficiaries": beneficiaries,
        "transactions": transactions, "cases": cases, "enterprise_strings": strings,
    }
    return World(fixture=fixture, refs=refs, tokens=tokens, principal=principal, other_principal=other, amount_minor=amount_minor)
