"""Strict loading of a workflow file for the CI safeguards (OD-P-95, closed 18 Nov 2026; ruling A283 of the Architect: stop reading YAML by lines).

The two workflow safeguards (check_workflow_permissions.py, check_checkout_credentials.py) read the PARSED document, so no spelling of a key or a value can hide from them (a quoted key, a flow
mapping, a space before the colon, a quoted `#`, a YAML escape in a key: each parses to the same thing as the plain spelling). PyYAML is the one parser, `safe_load` semantics only (the
SafeLoader: no Python object is ever built), pinned with hashes in tools/ci/requirements.lock and installed in the `safeguards` job only. This module adds what the parser alone does not:

  * a file over MAX_BYTES, a file with more than one document, and a file that is not valid YAML are refused (a refusal, never a pass);
  * an anchor or an alias is refused: a workflow does not need one, and an alias can expand a small file into a very large one when it is loaded;
  * a mapping that repeats a key is refused. PyYAML keeps the last of two equal keys without a word; GitHub's own parser refuses the file, so a checker that kept the last would read a
    different workflow than the one that runs (noema#697: a duplicate `persist-credentials` key took `main` red).

Never calls `yaml.load` with another loader, never reads a tag it was not given, and never prints the content of the file in a message.
"""
from typing import Any

try:  # the checks fail closed, with a message, when the parser is not installed (tools/ci/requirements.lock)
    import yaml
except ImportError:  # pragma: no cover - exercised by the install step of the job, not by a test
    yaml = None

MAX_BYTES = 1_048_576
MERGE_TAG = "tag:yaml.org,2002:merge"


class YamlRefused(Exception):
    """The file cannot be read as one plain YAML document. The message names the file and the reason, never its content."""


def available() -> bool:
    return yaml is not None


if yaml is not None:

    class _StrictLoader(yaml.SafeLoader):
        def construct_mapping(self, node, deep=False):  # type: ignore[no-untyped-def]
            if isinstance(node, yaml.MappingNode):
                seen: set[Any] = set()
                for key_node, _value_node in node.value:
                    if key_node.tag == MERGE_TAG:
                        continue
                    key = self.construct_object(key_node, deep=True)
                    try:
                        if key in seen:
                            raise yaml.constructor.ConstructorError(None, None, "a mapping repeats a key", key_node.start_mark)
                        seen.add(key)
                    except TypeError:
                        raise yaml.constructor.ConstructorError(None, None, "a mapping key that is not a plain value", key_node.start_mark) from None
            return super().construct_mapping(node, deep)


def load(text: str, name: str) -> Any:
    """The one document of a YAML file, or YamlRefused."""
    if yaml is None:
        raise YamlRefused(f"{name}: PyYAML is not installed, so the file cannot be read (tools/ci/requirements.lock)")
    if len(text.encode("utf-8")) > MAX_BYTES:
        raise YamlRefused(f"{name}: larger than {MAX_BYTES} bytes")
    try:
        for event in yaml.parse(text, Loader=yaml.SafeLoader):
            if isinstance(event, yaml.AliasEvent) or getattr(event, "anchor", None):
                raise YamlRefused(f"{name}: an anchor or an alias: write the value out")
        documents = list(yaml.load_all(text, Loader=_StrictLoader))  # noqa: S506 - a subclass of SafeLoader, never the full loader
    except YamlRefused:
        raise
    except yaml.YAMLError as exc:
        reason = "repeats a key" if "repeats a key" in str(exc) else "is not plain YAML"
        raise YamlRefused(f"{name}: {reason} ({exc.__class__.__name__})") from None
    if len(documents) > 1:
        raise YamlRefused(f"{name}: more than one YAML document")
    return documents[0] if documents else None
