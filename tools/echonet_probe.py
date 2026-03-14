#!/usr/bin/env python3
"""Unified ECHONET Lite probe utility.

This combines:
- Legacy quick probe mode (`--mode`)
- Advanced pychonet-like transport checks:
  - fixed source port or ephemeral source port
  - shared socket or per-request socket
  - route-aware multicast interface setup
  - TID-based stale-frame logging
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


def node_profile_cases() -> list[ProbeCase]:
    return [
        ProbeCase("node_profile_instance_list", DEOJ_NODE_PROFILE, (0xD6,)),
        ProbeCase("node_profile_getmap", DEOJ_NODE_PROFILE, (0x9F,)),
        ProbeCase("node_profile_identity", DEOJ_NODE_PROFILE, (0x83, 0x8A, 0x8C)),
    ]


def instance_cases(eoj: tuple[int, int, int]) -> list[ProbeCase]:
    label = f"{eoj[0]:02x}{eoj[1]:02x}{eoj[2]:02x}"
    return [
        ProbeCase(f"{label}_operation_status", eoj, (0x80,)),
        ProbeCase(f"{label}_getmap", eoj, (0x9F,)),
        ProbeCase(f"{label}_identity", eoj, (0x83, 0x8A)),
    ]


def parse_eoj_from_hex(s: str) -> tuple[int, int, int]:
    """Parse '026b01' or '0x026b01' into (0x02, 0x6b, 0x01)."""
    if s.startswith(("0x", "0X")):
        s = s[2:]
    if len(s) != 6:
        raise ValueError(f"EOJ must be exactly 6 hex digits, got '{s}'")
    b = bytes.fromhex(s)
    return (b[0], b[1], b[2])


def parse_epcs_from_arg(s: str) -> tuple[int, ...]:
    """Parse '0x80,0xB0,0xB2' into (0x80, 0xB0, 0xB2)."""
    out: list[int] = []
    for p in s.split(","):
        p = p.strip()
        if not p:
            continue
        if p.startswith(("0x", "0X")):
            p = p[2:]
        out.append(int(p, 16))
    return tuple(out)


def extract_instances_from_edt(edt: bytes) -> list[tuple[int, int, int]]:
    """Parse D6 instance list EDT into (group, class, instance) tuples."""
    if not edt:
        return []
    count = min(edt[0], (len(edt) - 1) // 3)
    out: list[tuple[int, int, int]] = []
    for i in range(count):
        base = 1 + i * 3
        out.append((edt[base], edt[base + 1], edt[base + 2]))
    return out


def format_eoj(eoj: tuple[int, int, int]) -> str:
    return f"0x{eoj[0]:02x}{eoj[1]:02x}{eoj[2]:02x}"


def route_local_ip_for_target(target_ip: str) -> str:
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as tmp:
        tmp.connect((target_ip, 80))
        return tmp.getsockname()[0]


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


def create_socket(target_ip: str, timeout: float, source_port: int, pychonet_like: bool) -> socket.socket:
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.bind(("0.0.0.0", source_port))

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
            pass
    return sock


def receive_for_tid(
    sock: socket.socket,
    target_ip: str,
    tid: int,
    timeout: float,
    case_name: str,
    verbose: bool,
) -> tuple[bool, str, list[tuple[int, bytes]]]:
    deadline = time.monotonic() + timeout
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return False, f"[{case_name}] timeout after {timeout:.1f}s", []
        sock.settimeout(remaining)
        try:
            data, addr = sock.recvfrom(4096)
        except socket.timeout:
            return False, f"[{case_name}] timeout after {timeout:.1f}s", []
        except OSError as exc:
            return False, f"[{case_name}] socket error: {exc}", []

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
        # 0x72 = Get_Res, 0x52 = Get_SNA (service not available) - still print props when present
        if esv not in (ESV_GET_RES, 0x52):
            lines.append(f"[{case_name}] unexpected ESV, expected 0x72 or 0x52")
            return False, "\n".join(lines), []
        if esv == 0x52:
            lines.append(f"[{case_name}] Get_SNA (partial/not available) - showing returned props")
        for epc, edt in props:
            if epc == 0xD6:
                lines.append(f"[{case_name}] D6 instances={[format_eoj(e) for e in extract_instances_from_edt(edt)]}")
            else:
                lines.append(f"[{case_name}] EPC 0x{epc:02x} EDT {edt.hex()}")
        return True, "\n".join(lines), props


def run_case(
    sock: socket.socket, target_ip: str, timeout: float, case: ProbeCase, verbose: bool,
) -> tuple[bool, list[tuple[int, bytes]]]:
    tid, payload = build_get(case.deoj, case.epcs)
    local_ip, local_port = sock.getsockname()
    print(f"[{case.name}] {local_ip}:{local_port} -> {target_ip}:{ECHONET_PORT} tid=0x{tid:04x}")
    try:
        sock.sendto(payload, (target_ip, ECHONET_PORT))
    except OSError as exc:
        print(f"[{case.name}] send error: {exc}")
        return False, []
    ok, message, props = receive_for_tid(sock, target_ip, tid, timeout, case.name, verbose)
    print(message)
    return ok, props


def _run_case_list(
    args: argparse.Namespace, source_port: int, cases: list[ProbeCase],
) -> tuple[bool, list[tuple[int, bytes]]]:
    """Run a list of probe cases respecting socket-mode. Returns (any_success, collected_props)."""
    success = False
    all_props: list[tuple[int, bytes]] = []
    shared = args.socket_mode == "shared"
    shared_sock: socket.socket | None = None
    if shared:
        try:
            shared_sock = create_socket(args.target_ip, args.timeout, source_port, args.pychonet_like)
        except OSError as exc:
            print(f"[setup] socket error: {exc}")
            return False, []
    try:
        for case in cases:
            for attempt in range(1, args.retries + 1):
                if shared:
                    sock = shared_sock
                else:
                    try:
                        sock = create_socket(args.target_ip, args.timeout, source_port, args.pychonet_like)
                    except OSError as exc:
                        print(f"[setup] socket error: {exc}")
                        return success, all_props
                try:
                    ok, props = run_case(sock, args.target_ip, args.timeout, case, args.verbose)
                    if ok:
                        success = True
                        all_props.extend(props)
                        break
                finally:
                    if not shared:
                        sock.close()
                if attempt < args.retries:
                    print(f"[{case.name}] retry {attempt + 1}/{args.retries}")
    finally:
        if shared_sock:
            shared_sock.close()
    return success, all_props


def run_port_mode(
    args: argparse.Namespace, source_port: int, eoj_override: tuple[int, int, int] | None,
) -> bool:
    mode_label = "bind3610" if source_port == ECHONET_PORT else "ephemeral"
    print(f"\n=== mode: {mode_label}, socket: {args.socket_mode}, pychonet_like: {args.pychonet_like} ===")

    np_success, np_props = _run_case_list(args, source_port, node_profile_cases())

    target_eoj = eoj_override
    if target_eoj is None:
        for epc, edt in np_props:
            if epc == 0xD6:
                instances = extract_instances_from_edt(edt)
                if instances:
                    target_eoj = instances[0]
                    print(f"[auto-detect] targeting first discovered instance: {format_eoj(target_eoj)}")
                break

    if target_eoj is None:
        if eoj_override is None:
            print("[auto-detect] no instances discovered; skipping instance probes")
        return np_success

    inst_success, _ = _run_case_list(args, source_port, instance_cases(target_eoj))
    return np_success or inst_success


def auto_detect_eoj(args: argparse.Namespace) -> tuple[int, int, int] | None:
    """Quick single-probe to discover first instance EOJ."""
    _, props = _run_case_list(args, ECHONET_PORT, [ProbeCase("auto_detect", DEOJ_NODE_PROFILE, (0xD6,))])
    for epc, edt in props:
        if epc == 0xD6:
            instances = extract_instances_from_edt(edt)
            if instances:
                return instances[0]
    return None


def send_and_recv(
    sock: socket.socket, target_ip: str, deoj: tuple[int, int, int], epcs: tuple[int, ...], timeout: float,
) -> list[tuple[int, bytes]]:
    """Send a GET and return the response properties, or [] on timeout."""
    tid, payload = build_get(deoj, epcs)
    try:
        sock.sendto(payload, (target_ip, ECHONET_PORT))
    except OSError:
        return []
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        sock.settimeout(max(deadline - time.monotonic(), 0.01))
        try:
            data, addr = sock.recvfrom(4096)
        except (socket.timeout, OSError):
            break
        if addr[0] != target_ip:
            continue
        try:
            resp_tid, _, props = parse_get_res(data)
        except ValueError:
            continue
        if resp_tid == tid:
            return props
    return []


def watch_loop(args: argparse.Namespace, eoj: tuple[int, int, int], epcs: tuple[int, ...]) -> None:
    """Poll targeted EPCs every --interval seconds; print one line per poll with timestamp."""
    try:
        sock = create_socket(args.target_ip, args.timeout, ECHONET_PORT, args.pychonet_like)
    except OSError as exc:
        print(f"watch: failed to create socket: {exc}", file=sys.stderr)
        sys.exit(1)
    epc_list = " ".join(f"0x{e:02x}" for e in epcs)
    print(f"Watching {format_eoj(eoj)} EPCs {epc_list} every {args.interval}s (Ctrl+C to stop)", file=sys.stderr)
    try:
        while True:
            props = send_and_recv(sock, args.target_ip, eoj, epcs, args.timeout)
            by_epc = {epc: edt for epc, edt in props}
            parts = [time.strftime("%H:%M:%S")]
            for epc in epcs:
                edt = by_epc.get(epc)
                parts.append(f"0x{epc:02x}={edt.hex() if edt else '-'}")
            print(" ".join(parts))
            time.sleep(args.interval)
    except KeyboardInterrupt:
        pass
    finally:
        sock.close()


def resolve_source_ports(args: argparse.Namespace) -> list[int]:
    if args.source_port is not None:
        return [args.source_port]
    if args.mode == "ephemeral":
        return [0]
    if args.mode == "bind3610":
        return [ECHONET_PORT]
    return [0, ECHONET_PORT]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Unified local ECHONET probe")
    parser.add_argument("target_ip", help="Target device IP, e.g. 192.168.3.86")
    parser.add_argument("--timeout", type=float, default=3.0, help="Timeout in seconds (default: 3)")
    parser.add_argument(
        "--mode",
        choices=("both", "ephemeral", "bind3610"),
        default="both",
        help="Compatibility mode selection for source port behavior",
    )
    parser.add_argument(
        "--source-port",
        type=int,
        default=None,
        help="Explicit source port override. Use 0 for ephemeral.",
    )
    parser.add_argument(
        "--socket-mode",
        choices=("shared", "per-request"),
        default="per-request",
        help="Use one long-lived socket or recreate a socket per request",
    )
    parser.add_argument(
        "--pychonet-like",
        action="store_true",
        help="Enable pychonet-like route/multicast socket setup",
    )
    parser.add_argument(
        "--eoj",
        default=None,
        help="Explicit target EOJ in hex, e.g. 026b01 or 0x013001. Auto-detected from node profile if omitted.",
    )
    parser.add_argument("--retries", type=int, default=1, help="Retries per probe case (default: 1)")
    parser.add_argument("--verbose", action="store_true", help="Show stale/ignored frames")
    parser.add_argument(
        "--watch",
        action="store_true",
        help="Poll EPCs every --interval seconds; print one line per poll (Ctrl+C to stop)",
    )
    parser.add_argument(
        "--epcs",
        default=None,
        help="Comma-separated hex EPCs for --watch mode, e.g. 0x80,0xB0,0xB2. Default: 0x80,0x9F",
    )
    parser.add_argument(
        "--interval",
        type=float,
        default=1.0,
        help="Seconds between polls in --watch mode (default: 1.0)",
    )
    args = parser.parse_args()

    if args.retries < 1:
        parser.error("--retries must be >= 1")
    if args.source_port is not None and not (0 <= args.source_port <= 65535):
        parser.error("--source-port must be in range 0..65535")
    if args.watch and args.interval <= 0:
        parser.error("--interval must be positive")

    args.eoj_tuple = None
    if args.eoj is not None:
        try:
            args.eoj_tuple = parse_eoj_from_hex(args.eoj)
        except ValueError as exc:
            parser.error(f"--eoj: {exc}")

    args.epcs_tuple = None
    if args.epcs is not None:
        try:
            args.epcs_tuple = parse_epcs_from_arg(args.epcs)
        except ValueError as exc:
            parser.error(f"--epcs: {exc}")
        if not args.epcs_tuple:
            parser.error("--epcs: no valid EPCs given")

    return args


def main() -> int:
    args = parse_args()
    if args.watch:
        eoj = args.eoj_tuple
        if eoj is None:
            eoj = auto_detect_eoj(args)
            if eoj is None:
                print("error: could not auto-detect EOJ; use --eoj to specify", file=sys.stderr)
                return 1
            print(f"[auto-detect] watching instance {format_eoj(eoj)}")
        epcs = args.epcs_tuple or (0x80, 0x9F)
        watch_loop(args, eoj, epcs)
        return 0

    success = False
    for source_port in resolve_source_ports(args):
        success = run_port_mode(args, source_port, args.eoj_tuple) or success
    print("\nRESULT:", "reachable (at least one successful probe)" if success else "no successful probes")
    return 0 if success else 1


if __name__ == "__main__":
    raise SystemExit(main())
