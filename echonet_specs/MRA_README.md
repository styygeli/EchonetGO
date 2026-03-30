# ECHONET Lite Machine Readable Appendix (MRA)

The MRA contains the official, machine-readable definitions of all standard ECHONET Lite device classes and their properties (EPCs). It is published by the ECHONET Consortium.

## Download

Download from: https://echonet.jp/spec_mra_rr3/

Extract to `echonet_specs/MRA_v1.3.1/` (or the appropriate version directory).

## Usage

The `mra-reader` skill in `.ai/skills/mra-reader/` provides scripts to query and compare MRA data against the project's YAML specs:

```bash
node .ai/skills/mra-reader/scripts/lookup_device.cjs --list
node .ai/skills/mra-reader/scripts/lookup_device.cjs 0x0130
node .ai/skills/mra-reader/scripts/compare_spec.cjs 0x0130 etc/specs/home_ac.yaml
```

## Note

The MRA data is not included in this repository due to copyright. It is gitignored under `echonet_specs/MRA_*/`.
