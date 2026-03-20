#!/usr/bin/env python3
"""Passive ECHONET Lite packet listener.

Listens on UDP port 3610 (with multicast join on 224.0.23.0) and prints
every received frame.  Designed to confirm that devices send unsolicited
INF (0x73) / INFC (0x74) property-value notifications.

No external dependencies -- stdlib only.
"""

from __future__ import annotations

import argparse
import socket
import struct
import sys
import time

ECHONET_PORT = 3610
MCAST_ADDR = "224.0.23.0"
EHD1 = 0x10
EHD2 = 0x81

ESV_NAMES: dict[int, str] = {
    0x60: "SetI",
    0x61: "SetC",
    0x62: "Get",
    0x63: "INF_REQ",
    0x6E: "SetGet",
    0x71: "Set_Res",
    0x72: "Get_Res",
    0x73: "INF",
    0x74: "INFC",
    0x7A: "INFC_Res",
    0x7E: "SetGet_Res",
    0x50: "SetI_SNA",
    0x51: "SetC_SNA",
    0x52: "Get_SNA",
    0x53: "INF_SNA",
    0x5E: "SetGet_SNA",
}

NOTIFICATION_ESVS = {0x73, 0x74, 0x53}

SEOJ_CONTROLLER = bytes([0x05, 0xFF, 0x01])


def detect_local_ip() -> str:
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
        s.connect((MCAST_ADDR, ECHONET_PORT))
        return s.getsockname()[0]


def format_eoj(data: bytes) -> str:
    return f"{data[0]:02x}.{data[1]:02x}.{data[2]:02x}"


def parse_frame(data: bytes) -> dict | None:
    if len(data) < 12:
        return None
    if data[0] != EHD1 or data[1] != EHD2:
        return None
    tid = int.from_bytes(data[2:4], "big")
    seoj = data[4:7]
    deoj = data[7:10]
    esv = data[10]
    opc = data[11]
    props: list[tuple[int, bytes]] = []
    pos = 12
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
    return {"tid": tid, "seoj": seoj, "deoj": deoj, "esv": esv, "opc": opc, "props": props}


def build_infc_res(frame: dict) -> bytes:
    """Build an INFC_Res (0x7A) acknowledging an INFC frame."""
    msg = bytearray()
    msg.extend((EHD1, EHD2))
    msg.extend(struct.pack(">H", frame["tid"]))
    msg.extend(SEOJ_CONTROLLER)
    msg.extend(frame["seoj"])
    msg.append(0x7A)
    msg.append(frame["opc"])
    for epc, edt in frame["props"]:
        msg.extend((epc, len(edt)))
        msg.extend(edt)
    return bytes(msg)


def print_frame(addr: tuple[str, int], frame: dict, verbose: bool) -> None:
    ts = time.strftime("%H:%M:%S")
    esv = frame["esv"]
    esv_name = ESV_NAMES.get(esv, f"0x{esv:02x}")
    is_notif = esv in NOTIFICATION_ESVS
    tag = " <<<" if is_notif else ""

    print(
        f"{ts}  {addr[0]}:{addr[1]}  "
        f"ESV={esv_name:<11s} "
        f"SEOJ={format_eoj(frame['seoj'])}  "
        f"DEOJ={format_eoj(frame['deoj'])}  "
        f"TID=0x{frame['tid']:04x}  "
        f"OPC={frame['opc']}{tag}"
    )
    if verbose or is_notif:
        for epc, edt in frame["props"]:
            print(f"         EPC=0x{epc:02x}  PDC={len(edt)}  EDT={edt.hex() if edt else '-'}")


def create_socket(local_ip: str) -> socket.socket:
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    if hasattr(socket, "SO_REUSEPORT"):
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEPORT, 1)
    sock.bind(("0.0.0.0", ECHONET_PORT))

    sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_IF, socket.inet_aton(local_ip))
    mreq = struct.pack("=4s4s", socket.inet_aton(MCAST_ADDR), socket.inet_aton(local_ip))
    sock.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP, mreq)
    return sock


def listen(args: argparse.Namespace) -> int:
    local_ip = args.interface or detect_local_ip()
    try:
        sock = create_socket(local_ip)
    except OSError as exc:
        print(f"error: could not bind socket: {exc}", file=sys.stderr)
        return 1

    mode = "INF/INFC only" if args.inf_only else "all frames"
    print(
        f"Listening on 0.0.0.0:{ECHONET_PORT}  multicast={MCAST_ADDR}  "
        f"interface={local_ip}  filter={mode}",
        file=sys.stderr,
    )
    print(f"respond-infc={args.respond_infc}  verbose={args.verbose}", file=sys.stderr)
    print("Press Ctrl+C to stop.\n", file=sys.stderr)

    try:
        while True:
            data, addr = sock.recvfrom(4096)
            frame = parse_frame(data)
            if frame is None:
                if args.verbose:
                    print(f"{time.strftime('%H:%M:%S')}  {addr[0]}:{addr[1]}  <invalid frame, {len(data)} bytes>")
                continue
            if args.inf_only and frame["esv"] not in NOTIFICATION_ESVS:
                continue
            print_frame(addr, frame, args.verbose)

            if args.respond_infc and frame["esv"] == 0x74:
                resp = build_infc_res(frame)
                sock.sendto(resp, addr)
                if args.verbose:
                    print(f"         -> sent INFC_Res to {addr[0]}:{addr[1]}")
    except KeyboardInterrupt:
        print("\nStopped.", file=sys.stderr)
    finally:
        sock.close()
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Passively listen for ECHONET Lite packets (especially INF notifications).",
    )
    parser.add_argument(
        "--interface",
        metavar="IP",
        default=None,
        help="Local IP of the interface to join multicast on (auto-detected if omitted)",
    )
    parser.add_argument(
        "--inf-only",
        action="store_true",
        help="Only show notification packets (INF 0x73, INFC 0x74, INF_SNA 0x53)",
    )
    parser.add_argument(
        "--respond-infc",
        action="store_true",
        help="Send INFC_Res (0x7A) back when an INFC (0x74) packet is received",
    )
    parser.add_argument(
        "--verbose", "-v",
        action="store_true",
        help="Show property details for all frames (not just notifications)",
    )
    return listen(parser.parse_args())


if __name__ == "__main__":
    raise SystemExit(main())
