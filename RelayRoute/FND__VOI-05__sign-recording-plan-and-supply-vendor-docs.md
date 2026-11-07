# Waiting on: Founder (FND) · Task: VOI-05, VOI-01, VOI-04, VOI-14 · From: Voice Engineer (V)

**Matter.** Four voice items cannot move without a Founder. Everything buildable without them is built and in review.

**What I need, specifically.**
1. **Sign (or change) the recording plan**: `docs/voice/recording-plan-DRAFT.md` in noema#201. Decisions it lists in section 8: speaker count, recruiting and payment, the consent text after legal review, the storage location, the retention grace period and withdrawal deadline, the access list. Until it is signed, VOI-06 onward cannot start.
2. **Vendor documents and sample access** for Aethex AI, Indigenius and N-ATLAS (API documents, and either recorded samples or approval of one live call per provider). Without them no adapter can be written (DEBT-14, docs#118), OQ-07 stays open (VOI-04), and the recorded-sample tests of VOI-01 stay unmet.
3. **Set the correctness floor for the field gate.** PERD §45.4 says "P95 <= 2.5 s with the Intent Object correct" and gives no number. `voice/field.py` (noema#230) takes it as an argument and has no default, so no run can be called PASS until a Founder sets it.
4. **Devices and SIMs (PRE-05)** for the field protocol (draft in noema#219).

**When done:** reply on the PR named above or in this file; I will change this title to "Case Resolved".
