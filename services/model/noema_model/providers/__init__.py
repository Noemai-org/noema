"""The only place in the model service that talks to a model, speech or text-to-speech provider (GPT, Gemini, Claude; Aethex AI, Indigenius, N-ATLAS) behind neutral interfaces, with failover (BD-16; PERD §36.1). Provider SDKs and credentials live here and nowhere else (INV-29).

May import: noema_model.platform.

Must never: be imported by anything but understanding, explanation and voice; hold a credential for the core database, Temporal, an adapter or a grant key; depend on a single provider (BD-16).

Boundary package created by PLT-01. Its sub-packages are filled by the engine tracks; see the README beside this file for what exists.
"""
