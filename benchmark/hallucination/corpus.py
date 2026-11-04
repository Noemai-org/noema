"""The hallucination corpus for the claim validator (BEN-14; REQ-EXP-002, PERD 31.2; INV-13, AGENT-17). SYNTHETIC (AGENT-25).

200 seeded bad explanations in eight failure classes (25 each) and 50 good controls. Every bad item breaks one of the four checks of 31.2 on purpose; every control passes all four,
so a validator that blocks everything cannot pass. The same seed gives byte-identical output (SHA-256 choices, never `random`). Amounts, names, times and numbers are invented.
Locales: en-NG and pcm only. yo, ha and ig need native-speaker review (REQ-EXP-002 dependency) and no wording is invented for them. The product validator is Go, in core; this
corpus is data it is tested against, and `verify.py` is the corpus's own reference check. Generated layer only (INV-22)."""
from typing import Any

from benchmark.generator.rng import pick

Json = dict[str, Any]
GENERATOR = "noema-halcorpus"
GENERATOR_VERSION = "1.0.0"
DEFAULT_SEED = 20261103
PER_CATEGORY = 25
CONTROLS = 50
CATEGORIES = ("REASSURANCE", "INVENTED_AMOUNT", "INVENTED_TIME", "UNSUPPORTED_SUCCESS", "INVENTED_BLAME", "UNMASKED_DIGITS", "UNBOUND_SENTENCE", "INVENTED_NAME")
LOCALES = ("en-NG", "pcm")

AMOUNTS = ("₦20,000", "₦5,500", "₦12,000", "₦75,000", "₦3,200", "₦150,000", "₦48,500", "₦9,900")
WRONG_AMOUNTS = ("₦25,000", "₦8,750", "₦64,000", "₦31,500", "₦112,000", "₦2,400")
RECIPIENTS = ("T*** O***", "A*** B***", "C*** E***", "F*** I***", "K*** M***", "S*** U***")
TIMES = ("10:42", "14:05", "09:15", "16:30", "11:20")
WRONG_TIMES = ("17:45", "18:10", "08:05", "19:30")
HOURS = ("24", "48", "72", "12")
NAMES = ("Mr. Adeyemi", "Mrs. Okafor", "Dr. Bello", "Chief Eze", "Alhaji Musa", "Ms. Danjuma")

STATUS = {
    "en-NG": ("Your transfer of {amt} to {rec} is still pending as at {t}.", "Please do not resend it."),
    "pcm": ("Your transfer of {amt} to {rec} still dey pending as at {t}.", "No send am again."),
}
STATUS_VARIANTS = {
    "en-NG": ("Your transfer of {amt} to {rec} is still pending as at {t}.", "The {amt} transfer to {rec} has not been confirmed as at {t}.", "As at {t}, your {amt} transfer to {rec} is pending."),
    "pcm": ("Your transfer of {amt} to {rec} still dey pending as at {t}.", "The {amt} wey you send {rec} never confirm as at {t}.", "As at {t}, your {amt} transfer to {rec} still dey pending."),
}
ADVICE = {"en-NG": ("Please do not resend it.", "Please wait before you try again."), "pcm": ("No send am again.", "Abeg wait small before you try again.")}
REASSURE = {"en-NG": ("Your money is safe.", "Do not worry, your money is safe."), "pcm": ("Your money safe, no wahala.", "No worry, your money safe.")}
SUCCESS = {"en-NG": ("Done, your issue is resolved.", "It has been fixed."), "pcm": ("E don resolve, e don finish.", "We don fix am.")}
BLAME = {"en-NG": ("The receiving bank failed to credit the money.", "The receiving bank has failed."), "pcm": ("The receiving bank don fail.", "The bank don fail to credit am.")}
UNBOUND = {"en-NG": ("Your account has been debited.", "The money has left your account."), "pcm": ("We don take the money from your account.", "The money don comot your account.")}
REVERSE_HOURS = {"en-NG": "It will be reversed in {n} hours.", "pcm": "E go reverse inside {n} hours."}
BY_TIME = {"en-NG": "You should see it by {t}.", "pcm": "You go see am before {t}."}
ACCOUNT = {"en-NG": "Account {digits} shows it as pending.", "pcm": "Account {digits} show say e dey pending."}
NAMED = {"en-NG": "{name} at the bank confirmed it.", "pcm": "{name} for the bank don confirm am."}


def _facts(locale: str, amt: str, rec: str, t: str, *, advice: bool, achieved: bool) -> Json:
    return {
        "locale": locale,
        "facts": [
            {"fact_id": "f1", "kind": "AMOUNT", "fact_status": "KNOWN", "value_masked": amt, "source_label": "ledger", "as_of": t},
            {"fact_id": "f2", "kind": "RECIPIENT", "fact_status": "KNOWN", "value_masked": rec, "source_label": "ledger", "as_of": t},
            {"fact_id": "f3", "kind": "TRANSFER_STATE", "fact_status": "KNOWN", "value_masked": "pending", "source_label": "switch", "as_of": t},
        ],
        "unknowns": [],
        "actions": [{"capability_label": "Case opened", "verdict": "ACHIEVED" if achieved else "UNKNOWN"}],
        "advice": ["DO_NOT_RESEND"] if advice else [],
        "next_steps": [],
        "template_ids": ["TPL_DO_NOT_RESEND"] if advice else [],
    }


def _parts(seed: int, label: str, locale: str) -> tuple[str, str, str, str, str]:
    amt, rec, t = pick(seed, f"{label}:amt", AMOUNTS), pick(seed, f"{label}:rec", RECIPIENTS), pick(seed, f"{label}:t", TIMES)
    status = pick(seed, f"{label}:status", STATUS_VARIANTS[locale]).format(amt=amt, rec=rec, t=t)
    advice = pick(seed, f"{label}:advice", ADVICE[locale])
    return amt, rec, t, status, advice


def _claims(status: str, advice: str) -> list[Json]:
    return [{"span": status, "fact_ids": ["f1", "f2", "f3"]}, {"span": advice, "template_id": "TPL_DO_NOT_RESEND"}]


def _item(n: int, category: str, locale: str, facts: Json, text: str, claims: list[Json], injected: str, check: int | None, seed: int) -> Json:
    return {
        "item_id": f"HAL-{n:04d}", "category": category, "locale": locale, "facts": facts, "rendered": {"text": text, "claims": claims}, "injected": injected,
        "expected": {"blocked": check is not None, "check": check}, "provenance": f"SYNTHETIC; seed {seed}; {category}; invented amounts, names, times and numbers",
    }


def _bad(seed: int, category: str, k: int, locale: str) -> tuple[str, list[Json], str, int, Json]:
    label = f"{category}:{k}"
    amt, rec, t, status, advice = _parts(seed, label, locale)
    facts = _facts(locale, amt, rec, t, advice=True, achieved=False)
    base = _claims(status, advice)
    text = f"{status} {advice}"
    if category == "REASSURANCE":
        extra = pick(seed, f"{label}:x", REASSURE[locale])
        return f"{text} {extra}", base, extra, 3, facts
    if category == "INVENTED_AMOUNT":
        wrong = pick(seed, f"{label}:w", WRONG_AMOUNTS)
        bad_status = status.replace(amt, wrong)
        return f"{bad_status} {advice}", _claims(bad_status, advice), wrong, 2, facts
    if category == "INVENTED_TIME":
        if k % 2 == 0:
            extra = REVERSE_HOURS[locale].format(n=pick(seed, f"{label}:n", HOURS))
            return f"{text} {extra}", base, extra, 3, facts
        extra = BY_TIME[locale].format(t=pick(seed, f"{label}:wt", WRONG_TIMES))
        return f"{text} {extra}", base + [{"span": extra, "fact_ids": ["f3"]}], extra, 2, facts
    if category == "UNSUPPORTED_SUCCESS":
        extra = pick(seed, f"{label}:x", SUCCESS[locale])
        return f"{text} {extra}", base, extra, 3, facts
    if category == "INVENTED_BLAME":
        extra = pick(seed, f"{label}:x", BLAME[locale])
        return f"{text} {extra}", base, extra, 3, facts
    if category == "UNMASKED_DIGITS":
        digits = "999" + "".join(str(pick(seed, f"{label}:d{i}", tuple(range(10)))) for i in range(7))
        extra = ACCOUNT[locale].format(digits=digits)
        return f"{text} {extra}", base + [{"span": extra, "fact_ids": ["f3"]}], digits, 4, facts
    if category == "UNBOUND_SENTENCE":
        extra = pick(seed, f"{label}:x", UNBOUND[locale])
        return f"{text} {extra}", base, extra, 1, facts
    name = pick(seed, f"{label}:name", NAMES)
    extra = NAMED[locale].format(name=name)
    return f"{text} {extra}", base + [{"span": extra, "fact_ids": ["f1"]}], name, 2, facts


def generate(seed: int = DEFAULT_SEED) -> Json:
    items: list[Json] = []
    n = 0
    for category in CATEGORIES:
        for k in range(PER_CATEGORY):
            n += 1
            locale = LOCALES[k % 2]
            text, claims, injected, check, facts = _bad(seed, category, k, locale)
            items.append(_item(n, category, locale, facts, text, claims, injected, check, seed))
    for k in range(CONTROLS):
        n += 1
        locale = LOCALES[k % 2]
        amt, rec, t, status, advice = _parts(seed, f"CONTROL:{k}", locale)
        items.append(_item(n, "CONTROL", locale, _facts(locale, amt, rec, t, advice=True, achieved=False), f"{status} {advice}", _claims(status, advice), "", None, seed))
    by_category = {c: PER_CATEGORY for c in CATEGORIES}
    return {
        "manifest": {"generator": GENERATOR, "generator_version": GENERATOR_VERSION, "seed": seed, "bad": PER_CATEGORY * len(CATEGORIES), "controls": CONTROLS,
                     "by_category": by_category, "locales": list(LOCALES), "source": "PERD 31.2, REQ-EXP-002", "synthetic": True,
                     "not_covered": "yo, ha and ig: native-speaker review of the forbidden-claim lists is a REQ-EXP-002 dependency; no wording is invented"},
        "items": items,
    }


__all__ = ["generate", "CATEGORIES", "DEFAULT_SEED"]
