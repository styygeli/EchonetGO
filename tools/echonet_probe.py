#!/usr/bin/env python3
"""Minimal local ECHONET Lite probe utility.

Use this to quickly verify whether a host can reach an ECHONET device directly.
No third-party dependencies.
"""

from __future__ import annotations

import argparse
import random
import socket
import struct
import sys
from dataclasses import dataclass
from typing import Iterable


EHD1 = 0x10
EHD2 = 0x81
ESV_GET = 0x62
ESV_GET_RES = 0x72
ECHONET_PORT = 3610

SEOJ_CONTROLLER = (0x05, 0xFF, 0x01)
DEOJ_NODE_PROFILE = (0x0E, 0xF0, 0x01)


@dataclass
class ProbeCase:
    name: str
    deoj: tuple[int, int, int]
    epcs: tuple[int, ...]


PROBE_CASES = (
    ProbeCase("node_profile_instance_list", DEOJ_NODE_PROFILE, (0xD6,)),
    ProbeCase("node_profile_getmap", DEOJ_NODE_PROFILE, (0x9F,)),
    ProbeCase("node_profile_identity", DEOJ_NODE_PROFILE, (0x83, 0x8A, 0x8C)),
)


def build_get(deoj: tuple[int, int, int], epcs: Iterable[int]) -> tuple[int, bytes]:
    tid = random.randint(0, 0xFFFF)
    epcs = tuple(epcs)
    msg = bytearray()
    msg.extend((EHD1, EHD2))
    msg.extend(struct.pack(">H", tid))
    msg.extend(SEOJ_CONTROLLER)
    msg.extend(deoj)
    msg.extend((ESV_GET, len(epcs)))
    for epc in epcs:
        msg.extend((epc, 0x00))
    return tid, bytes(msg)


def parse_get_res(data: bytes) -> tuple[int, int, list[tuple[int, bytes]]]:
    if len(data) < 12:
        raise ValueError(f"frame too short: {len(data)}")
    if data[0] != EHD1 or data[1] != EHD2:
        raise ValueError(f"invalid EHD: {data[0]:02x} {data[1]:02x}")
    tid = int.from_bytes(data[2:4], "big")
    esv = data[10]
    opc = data[11]
    pos = 12
    props: list[tuple[int, bytes]] = []
    for _ in range(opc):
        if pos + 2 > len(data):
            break
        epc = data[pos]
        pdc = data[pos + 1]
        pos += 2
        if pos + pdc > len(data):
            break
        edt = data[pos : pos + pdc]
        pos += pdc
        props.append((epc, edt))
    return tid, esv, props


def decode_instance_list(edt: bytes) -> list[str]:
    if not edt:
        return []
    count = min(edt[0], (len(edt) - 1) // 3)
    out: list[str] = []
    for i in range(count):
        base = 1 + i * 3
        out.append(f"0x{edt[base]:02x}{edt[base+1]:02x}{edt[base+2]:02x}")
    return out


def run_case(target_ip: str, source_port: int | None, timeout: float, case: ProbeCase) -> bool:
    tid, payload = build_get(case.deoj, case.epcs)

    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    if source_port is not None:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(("0.0.0.0", source_port))
    else:
        sock.bind(("0.0.0.0", 0))

    sock.connect((target_ip, ECHONET_PORT))
    local_ip, local_port = sock.getsockname()
    print(f"[{case.name}] {local_ip}:{local_port} -> {target_ip}:{ECHONET_PORT} tid=0x{tid:04x}")

    try:
        sock.send(payload)
        data = sock.recv(2048)
    except socket.timeout:
        print(f"[{case.name}] timeout after {timeout:.1f}s")
        return False
    except OSError as exc:
        print(f"[{case.name}] socket error: {exc}")
        return False
    finally:
        sock.close()

    try:
        resp_tid, esv, props = parse_get_res(data)
    except ValueError as exc:
        print(f"[{case.name}] invalid response: {exc}")
        return False

    print(f"[{case.name}] reply tid=0x{resp_tid:04x} esv=0x{esv:02x} props={len(props)} bytes={len(data)}")
    if esv != ESV_GET_RES:
        print(f"[{case.name}] unexpected ESV, expected 0x{ESV_GET_RES:02x}")
        return False

    for epc, edt in props:
        if case.name == "node_profile_instance_list" and epc == 0xD6:
            print(f"[{case.name}] D6 instances={decode_instance_list(edt)}")
        else:
            print(f"[{case.name}] EPC 0x{epc:02x} EDT {edt.hex()}")
    return True


def run_mode(target_ip: str, timeout: float, source_port: int | None, ac_instance: int) -> bool:
    mode_label = "bind3610" if source_port == ECHONET_PORT else "ephemeral"
    print(f"\n=== mode: {mode_label} ===")
    ok = False

    cases = list(PROBE_CASES)
    cases.append(ProbeCase("ac_operation_status", (0x01, 0x30, ac_instance), (0x80,)))
    cases.append(ProbeCase("ac_getmap", (0x01, 0x30, ac_instance), (0x9F,)))

    for case in cases:
        if run_case(target_ip, source_port, timeout, case):
            ok = True
    return ok


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Lightweight local ECHONET probe")
    p.add_argument("target_ip", help="Target device IP, e.g. 192.168.3.86")
    p.add_argument("--timeout", type=float, default=3.0, help="Receive timeout in seconds (default: 3)")
    p.add_argument(
        "--mode",
        choices=("both", "ephemeral", "bind3610"),
        default="both",
        help="Probe using ephemeral source port, source port 3610, or both",
    )
    p.add_argument("--ac-instance", type=int, default=1, help="Home AC EOJ instance byte (default: 1)")
    return p.parse_args()


def main() -> int:
    args = parse_args()
    if not (0 <= args.ac_instance <= 0xFF):
        print("ac-instance must be in range 0..255", file=sys.stderr)
        return 2

    success = False
    if args.mode in ("both", "ephemeral"):
        success = run_mode(args.target_ip, args.timeout, None, args.ac_instance) or success
    if args.mode in ("both", "bind3610"):
        success = run_mode(args.target_ip, args.timeout, ECHONET_PORT, args.ac_instance) or success

    print("\nRESULT:", "reachable (at least one successful probe)" if success else "no successful probes")
    return 0 if success else 1


if __name__ == "__main__":
    raise SystemExit(main())
