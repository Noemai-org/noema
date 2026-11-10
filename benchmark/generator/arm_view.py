"""The arm view: the projection of a generated case that an arm is shown, `benchmark-arm-case.v1` (noema#260; Security Reviewer F-14 on noema#307, findings of noema#246).

What an arm sees is what an applicant to the same service would see, identical for every arm (REQ-BEN-007). It holds no goal, no case id, no category, nothing expected and no oracle reference.
`case_ref` and `fixture_ref` are opaque: a digest of a run-private salt and the real id, so the mapping back is known only to the runner that holds the salt.

`permitted_capabilities` is the principal's ROLE-level set, the union over the roles of the map, the same list for every case of a role. It is never the case's own `permitted_capabilities`, which
the generator builds from what the case expects to write and so encodes the answer (F-14). The map is pack data (noema#359: `roles_allowed` without the prohibited capabilities); it is an argument,
so this module derives nothing from the pack and cannot drift from it. Pure: no I/O, no clock, the same case, map and salt give the same bytes. Generated layer only (INV-22)."""
import hashlib
from collections.abc import Mapping, Sequence
from typing import Any

Json = dict[str, Any]
ARM_CLOCK_START = "2026-10-01T08:40:00Z"  # the simulated clock start of benchmark.fixtures.standin.CLOCK_START (2026-10-01T09:40:00+01:00) in UTC, SYNTHETIC


def _digest(salt: str, kind: str, ident: str) -> str:
    return hashlib.sha256(f"{salt}\x00{kind}\x00{ident}".encode()).hexdigest()[:16]


def role_capabilities(roles: Sequence[str], role_map: Mapping[str, Sequence[str]]) -> list[str]:
    """The sorted union of the capabilities the map gives these roles. A role the map does not hold, or no role, is an error: an arm is never offered a guess."""
    if not roles:
        raise ValueError("a principal with no role has no permitted capabilities to show")
    out: set[str] = set()
    for role in roles:
        if role not in role_map:
            raise ValueError(f"the role map holds no role {role!r}")
        caps = list(role_map[role])
        if len(caps) != len(set(caps)):
            raise ValueError(f"the role map offers a capability twice to {role!r}")
        out.update(caps)
    return sorted(out)


def arm_case(case: Mapping[str, Any], role_map: Mapping[str, Sequence[str]], *, salt: str) -> Json:
    """The `benchmark-arm-case.v1` view of one generated case. `salt` is the run-private secret that makes the refs opaque; an empty one is refused."""
    if not salt:
        raise ValueError("the arm view needs a run-private salt, or its refs could be recomputed from the case id")
    principal = case["principal"]
    return {
        "schema": "benchmark-arm-case.v1",
        "case_ref": "arm_" + _digest(salt, "case", case["case_id"]),
        "received_at": ARM_CLOCK_START,
        "channel": case["channel"],
        "principal": {"principal_id": principal["principal_id"], "roles": list(principal["roles"])},
        "utterances": [{"turn": u["turn"], "text": u["text"]} for u in case["utterances"]],
        "permitted_capabilities": role_capabilities(principal["roles"], role_map),
        "fixture_ref": "fx_arm_" + _digest(salt, "fixture", case["known_facts_fixture"]),
    }
