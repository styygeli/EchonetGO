# ECHONET Lite Machine Readable Appendix (MRA)

The MRA contains the official, machine-readable definitions of all standard ECHONET Lite device classes and their properties (EPCs). It is published by the ECHONET Consortium.

## Download

Download from: https://echonet.jp/spec_mra_rr3/

Extract to `echonet_specs/MRA_v1.4.0/` (or the appropriate version directory).

## Usage

The `echonet-spec-architect` skill in `.agents/skills/echonet-spec-architect/` provides scripts to query, compare, and generate metric definitions from MRA data against the project's YAML specs:

```bash
# List all device classes in MRA v1.4.0
python3 .agents/skills/echonet-spec-architect/scripts/mra_sync.py --list

# Diff a YAML spec against MRA definition
python3 .agents/skills/echonet-spec-architect/scripts/mra_sync.py --diff etc/specs/home_ac.yaml

# Generate YAML metric snippets for an EOJ
python3 .agents/skills/echonet-spec-architect/scripts/mra_sync.py --gen 0x0130 0xBE 0xBA
```

## Note

The MRA data is not included in this repository due to copyright. It is gitignored under `echonet_specs/MRA_*/`.
