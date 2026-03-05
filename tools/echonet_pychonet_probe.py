#!/usr/bin/env python3
"""Pychonet-like ECHONET Lite probe.

This script intentionally mimics pychonet transport behavior:
- One long-lived UDP socket (optional)
- Source port 3610 by default
- SO_REUSEADDR enabled
- Route-aware local interface selection via UDP connect(host, 80)
- TID-based filtering with stale-frame logging

Use this to compare "pychonet-like" behavior vs per-request socket behavior.
"""

from __future__ import annotations

import argparse
import random
import socket
import struct
import sys
import time
from dataclasses import dataclass
from typing import Iterable


EHD1 = 0x10
EHD2 = 0x81
ESV_GET = 0x62
ESV_GET_RES = 0x72
ECHONET_PORT = 3610
MCAST_ADDR = "224.0.23.0"

SEOJ_CONTROLLER = (0x05, 0xFF, 0x01)
DEOJ_NODE_PROFILE = (0x0E, 0xF0, 0x01)


@dataclass(frozen=True)
class ProbeCase:
    name: str
    deoj: tuple[int, int, int]
    epcs: tuple[int, ...]


def probe_cases(ac_instance: int) -> list[ProbeCase]:
    return [
        ProbeCase("node_profile_instance_list", DEOJ_NODE_PROFILE, (0xD6,)),
        ProbeCase("node_profile_getmap", DEOJ_NODE_PROFILE, (0x9F,)),
        ProbeCase("node_profile_identity", DEOJ_NODE_PROFILE, (0x83, 0x8A, 0x8C)),
        ProbeCase("ac_operation_status", (0x01, 0x30, ac_instance), (0x80,)),
        ProbeCase("ac_getmap", (0x01, 0x30, ac_instance), (0x9F,)),
    ]


def route_local_ip_for_target(target_ip: str) -> str:
    tmp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        tmp.connect((target_ip, 80))
        local_ip, _ = tmp.getsockname()
        return local_ip
    finally:
        tmp.close()


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
        out.append(f"0x{edt[base]:02x}{edt[base + 1]:02x}{edt[base + 2]:02x}")
    return out


def create_socket(target_ip: str, timeout: float, source_port: int, pychonet_like: bool) -> socket.socket:
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)

    bind_port = source_port if source_port >= 0 else ECHONET_PORT
    sock.bind(("0.0.0.0", bind_port))

    if pychonet_like:
        local_ip = route_local_ip_for_target(target_ip)
        try:
            sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_IF, socket.inet_aton(local_ip))
        except OSError:
            pass
        try:
            mreq = struct.pack("=4s4s", socket.inet_aton(MCAST_ADDR), socket.inet_aton(local_ip))
            sock.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP, mreq)
        except OSError:
            # For unicast probing this is not critical.
            pass
    return sock


def receive_for_tid(
    sock: socket.socket,
    target_ip: str,
    tid: int,
    timeout: float,
    case_name: str,
    verbose: bool,
) -> tuple[bool, str]:
    deadline = time.monotonic() + timeout
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return False, f"[{case_name}] timeout after {timeout:.1f}s"
        sock.settimeout(remaining)
        try:
            data, addr = sock.recvfrom(4096)
        except socket.timeout:
            return False, f"[{case_name}] timeout after {timeout:.1f}s"
        except OSError as exc:
            return False, f"[{case_name}] socket error: {exc}"

        src_ip, src_port = addr[0], addr[1]
        if src_ip != target_ip:
            if verbose:
                print(f"[{case_name}] ignored frame from unexpected host {src_ip}:{src_port}")
            continue
        try:
            resp_tid, esv, props = parse_get_res(data)
        except ValueError as exc:
            if verbose:
                print(f"[{case_name}] invalid response from {src_ip}:{src_port}: {exc}")
            continue
        if resp_tid != tid:
            if verbose:
                print(
                    f"[{case_name}] stale frame from {src_ip}:{src_port}: "
                    f"expected tid=0x{tid:04x} got=0x{resp_tid:04x}"
                )
            continue

        lines = [
            f"[{case_name}] reply tid=0x{resp_tid:04x} esv=0x{esv:02x} props={len(props)} bytes={len(data)}"
        ]
        if esv != ESV_GET_RES:
            lines.append(f"[{case_name}] unexpected ESV, expected 0x{ESV_GET_RES:02x}")
            return False, "\n".join(lines)
        for epc, edt in props:
            if case_name == "node_profile_instance_list" and epc == 0xD6:
                lines.append(f"[{case_name}] D6 instances={decode_instance_list(edt)}")
            else:
                lines.append(f"[{case_name}] EPC 0x{epc:02x} EDT {edt.hex()}")
        return True, "\n".join(lines)


def run_case(sock: socket.socket, target_ip: str, timeout: float, case: ProbeCase, verbose: bool) -> bool:
    tid, payload = build_get(case.deoj, case.epcs)
    local_ip, local_port = sock.getsockname()
    print(f"[{case.name}] {local_ip}:{local_port} -> {target_ip}:{ECHONET_PORT} tid=0x{tid:04x}")
    try:
        sock.sendto(payload, (target_ip, ECHONET_PORT))
    except OSError as exc:
        print(f"[{case.name}] send error: {exc}")
        return False
    ok, message = receive_for_tid(sock, target_ip, tid, timeout, case.name, verbose)
    print(message)
    return ok


def run_probe(args: argparse.Namespace) -> int:
    success = False
    cases = probe_cases(args.ac_instance)

    # Shared socket (pychonet-like event-loop style behavior).
    if args.socket_mode == "shared":
        sock = create_socket(args.target_ip, args.timeout, args.source_port, args.pychonet_like)
        try:
            for case in cases:
                for attempt in range(1, args.retries + 1):
                    if run_case(sock, args.target_ip, args.timeout, case, args.verbose):
                        success = True
                        break
                    if attempt < args.retries:
                        print(f"[{case.name}] retry {attempt + 1}/{args.retries}")
        finally:
            sock.close()
    else:
        # Per-request socket mode, useful for A/B with shared mode.
        for case in cases:
            for attempt in range(1, args.retries + 1):
                sock = create_socket(args.target_ip, args.timeout, args.source_port, args.pychonet_like)
                try:
                    if run_case(sock, args.target_ip, args.timeout, case, args.verbose):
                        success = True
                        break
                finally:
                    sock.close()
                if attempt < args.retries:
                    print(f"[{case.name}] retry {attempt + 1}/{args.retries}")

    print("\nRESULT:", "reachable (at least one successful probe)" if success else "no successful probes")
    return 0 if success else 1


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Pychonet-like ECHONET probe")
    parser.add_argument("target_ip", help="Target device IP, e.g. 192.168.3.86")
    parser.add_argument("--timeout", type=float, default=3.0, help="Timeout in seconds (default: 3)")
    parser.add_argument(
        "--source-port",
        type=int,
        default=3610,
        help="UDP source port to bind. Use 0 for ephemeral (default: 3610)",
    )
    parser.add_argument(
        "--socket-mode",
        choices=("shared", "per-request"),
        default="shared",
        help="Use one long-lived socket (shared) or recreate each request",
    )
    parser.add_argument(
        "--pychonet-like",
        action="store_true",
        help="Enable pychonet-like route/multicast socket setup",
    )
    parser.add_argument("--ac-instance", type=int, default=1, help="Home AC EOJ instance (default: 1)")
    parser.add_argument("--retries", type=int, default=1, help="Retries per probe case (default: 1)")
    parser.add_argument("--verbose", action="store_true", help="Show stale/ignored frames")
    args = parser.parse_args()

    if not (0 <= args.ac_instance <= 0xFF):
        parser.error("--ac-instance must be in range 0..255")
    if args.source_port < 0 or args.source_port > 65535:
        parser.error("--source-port must be in range 0..65535")
    if args.retries < 1:
        parser.error("--retries must be >= 1")
    return args


def main() -> int:
    args = parse_args()
    return run_probe(args)


if __name__ == "__main__":
    raise SystemExit(main())
