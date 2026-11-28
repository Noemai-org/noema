# testdata

`derive-golden.json` is the output of `derive(fixture)` from `benchmark/fixtures/standin.py` at noema#161 head `78248024fa01a023cb15a2c2b67c51e32134e097`
(sorted keys, indent 2). SYNTHETIC. The fixture (`fixture_id` `fx_golden`):

| ref | owner | amount_minor | age_minutes | ledger | switch | destination | settlement |
|---|---|---|---|---|---|---|---|
| TX1 | usr_aa | 250000 | 150 | POSTED | APPROVED | CREDITED | SETTLED |
| TX2 | usr_aa | 500000 | 30 | POSTED | TIMED_OUT | PENDING | PENDING |
| TX3 | usr_aa | 250000 | 600 | UNAVAILABLE | APPROVED | CREDITED | SETTLED |
| TX9 | usr_bb | 999900 | 45 | POSTED | APPROVED | CREDITED | SETTLED |

One beneficiary (`ben_0001`, owner `usr_aa`); principals `usr_aa` and `usr_bb`. Regenerate it only by running `derive` on that fixture; never edit it by hand.
If `derive` changes, the contract changed: update this file and the service together.
