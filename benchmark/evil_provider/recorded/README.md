# recorded

Recorded request and response pairs of the real OpenAI Decisions API, as `<name>.request.json` and `<name>.response.json`.
**None exists yet**: no vendor key exists (decision-model scope update) and no documentation is in the repository.
`decisions_format.PROVISIONAL` is `True` for as long as this folder holds no pair. When a pair is added, `test_recorded.py`
fails until `PROVISIONAL` is set to `False` and `decisions_format.py` matches the recorded shape, so the evil provider and the
harness adapter (session D) are pinned to the same recorded truth.

Synthetic inputs only. Never record a secret, a key or a real customer string.
