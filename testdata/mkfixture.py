#!/usr/bin/env python3
"""Cut a full `snmpbulkwalk -On -Oe -Ot -Cc <switch> .1` dump down to the branches the
exporter reads and anonymize it for committing as a test fixture.

    python3 testdata/mkfixture.py full.walk > testdata/<model>.walk

Anonymization is deterministic: every MAC address (in values and in FDB table indexes)
is replaced by 02:00:00:00:HH:LL numbered in order of first appearance, so the same
station keeps the same fake MAC across tables. Serial numbers and sysName are replaced.
"""
import re
import sys

KEEP = [
    "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0",
    "1.3.6.1.2.1.2.2.1.",            # ifTable
    "1.3.6.1.2.1.31.1.1.1.",         # ifXTable
    "1.3.6.1.2.1.10.7.2.1.",         # dot3StatsTable
    "1.3.6.1.2.1.16.1.1.1.",         # RMON etherStatsTable
    "1.3.6.1.2.1.17.1.1.0",          # dot1dBaseBridgeAddress
    "1.3.6.1.2.1.17.1.4.1.",         # dot1dBasePortTable
    "1.3.6.1.2.1.17.4.3.1.",         # dot1dTpFdbTable
    "1.3.6.1.2.1.17.7.1.2.2.1.",     # dot1qTpFdbTable
    "1.0.8802.1.1.2.1.3.7.1.",       # lldpLocPortTable
    "1.0.8802.1.1.2.1.4.1.1.",       # lldpRemTable
    "1.3.6.1.4.1.171.12.1.1.6.",     # agentCPUutilization
    "1.3.6.1.4.1.171.12.1.1.9.",     # agentDRAMutilization
    "1.3.6.1.4.1.171.12.1.1.10.",    # agentFLASHutilization
    "1.3.6.1.4.1.171.12.1.1.12.0",   # agentSerialNumber
    "1.3.6.1.4.1.171.12.1.1.13.0",   # agentFirmwareType
    "1.3.6.1.4.1.171.12.11.1.6.",    # swPowerTable
    "1.3.6.1.4.1.171.12.11.1.7.",    # swFanTable
    "1.3.6.1.4.1.171.12.11.1.8.",    # swTemperatureTable
    "1.3.6.1.4.1.171.12.11.1.9.4.",  # swUnitMgmtTable (stack unit info)
    "1.3.6.1.4.1.171.12.72.2.",      # ddmStatus
    "1.3.6.1.4.1.171.12.72.3.",      # ddmThreshold
    "1.3.6.1.4.1.171.10.126.2.1.1.2.0",  # DGS-1500 hardware version
    "1.3.6.1.4.1.171.10.126.2.1.1.3.0",  # DGS-1500 firmware version
]

# Tables indexed by a MAC address in the last six sub-identifiers.
MAC_INDEXED = ["1.3.6.1.2.1.17.4.3.1.", "1.3.6.1.2.1.17.7.1.2.2.1."]

macs = {}


def fake_mac(raw: bytes) -> bytes:
    if raw not in macs:
        n = len(macs) + 1
        macs[raw] = bytes([2, 0, 0, 0, n >> 8, n & 0xFF])
    return macs[raw]


def hexstr(b: bytes) -> str:
    return " ".join(f"{x:02X}" for x in b) + " "


def anonymize(oid: str, val: str) -> tuple[str, str]:
    for p in MAC_INDEXED:
        if oid.startswith(p):
            parts = oid.split(".")
            mac = fake_mac(bytes(int(x) for x in parts[-6:]))
            oid = ".".join(parts[:-6] + [str(x) for x in mac])
    m = re.fullmatch(r"Hex-STRING: ((?:[0-9A-F]{2} ){6})", val)
    if m:
        val = "Hex-STRING: " + hexstr(fake_mac(bytes.fromhex(m.group(1).replace(" ", ""))))
    m = re.fullmatch(r'STRING: "?([0-9A-F]{2}(?:-[0-9A-F]{2}){5})"?', val)
    if m:
        val = 'STRING: "' + "-".join(f"{x:02X}" for x in fake_mac(bytes.fromhex(m.group(1).replace("-", "")))) + '"'
    if oid in ("1.3.6.1.4.1.171.12.1.1.12.0",) or oid.startswith("1.3.6.1.4.1.171.12.11.1.9.4.1.17."):
        val = 'STRING: "SN0000000001"'
    if oid == "1.3.6.1.2.1.1.5.0" and val.strip() != "STRING:":
        val = "STRING: switch-01"
    return oid, val


def main() -> None:
    keep = False
    for line in open(sys.argv[1], encoding="latin-1"):
        line = line.rstrip("\n")
        if not line.startswith("."):
            if keep:  # continuation of a wrapped Hex-STRING
                print(line)
            continue
        oid, sep, val = line[1:].partition(" = ")
        keep = bool(sep) and any(oid == k or (k.endswith(".") and oid.startswith(k)) for k in KEEP)
        if keep:
            oid, val = anonymize(oid, val)
            print(f".{oid} = {val}")


if __name__ == "__main__":
    main()
