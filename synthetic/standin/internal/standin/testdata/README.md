# testdata

`derive-golden.json` is the output of `derive(fixture)` from `benchmark/fixtures/standin.py` at noema#161 head `78248024fa01a023cb15a2c2b67c51e32134e097`
(sorted keys, indent 2). SYNTHETIC. The fixture (`fixture_id` `fx_golden`):

| ref | owner | amount_minor | age_minutes | ledger | switch | destination | settlement |
|---|---|---|---|---|---|---|---|
| fin.transaction:TX1 | usr_a | 250000 | 150 | POSTED | APPROVED | CREDITED | SETTLED |
| fin.transaction:TX2 | usr_a | 500000 | 30 | POSTED | TIMED_OUT | PENDING | PENDING |
| fin.transaction:TX3 | usr_a | 250000 | 600 | UNAVAILABLE | APPROVED | CREDITED | SETTLED |
| fin.transaction:TX9 | usr_b | 999900 | 45 | POSTED | APPROVED | CREDITED | SETTLED |

One beneficiary (`ben_0001`, owner `usr_a`); principals `usr_a` and `usr_b`. Regenerate it only by running `derive` on that fixture; never edit it by hand.
If `derive` changes, the contract changed: update this file and the service together.
