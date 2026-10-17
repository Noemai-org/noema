# contracts/fixtures/invalid

Documents each schema must reject: `invalid/<schema-name>/<case>.json`. The folder's `expectations.json` maps each file to the reason it must be rejected (a JSON Schema keyword such as `required`, `enum`, `pattern`, `type`, or `decoder` for malformed JSON and duplicate keys). A fixture rejected for any other reason fails the validator. All data is synthetic.
