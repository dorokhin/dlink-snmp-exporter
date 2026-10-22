# dlink-snmp-exporter

**English** | [Русский](README.Ru.md)

Prometheus exporter for D-Link DES/DGS switches. It collects data over SNMP (v2c or v3) and works in multi-target
mode like `snmp_exporter`: one process polls any number of switches, and Prometheus passes the switch address in
`/probe?target=`.

Tested on:

| Model | Firmware | Available |
|---|---|---|
| **DGS-3120-24TC** (managed) | 3.12.R020 | everything: interfaces, EtherLike, RMON, CPU/memory/temperature/fans/PSU, SFP DDM, MAC table, LLDP |
| **DGS-1500-28** (smart) | 2.51.B022 | interfaces, EtherLike, MAC table, LLDP, model and firmware. No CPU, temperature or DDM over SNMP |

Other managed DES/DGS-3xxx models (DES-3200, DGS-3000, DGS-3420, DGS-3620...) implement the same common D-Link MIBs
(`AGENT-GENERAL-MIB`, `EQUIPMENT-MIB`, DDM), so they are likely to work, but have not been tested. The exporter
detects what a switch supports: when a table is not implemented, the collector exports nothing and still succeeds.

## What it collects

| Collector    | Source                                          | Provides |
|--------------|-------------------------------------------------|----------|
| `system`     | SNMPv2-MIB, AGENT-GENERAL-MIB, EQUIPMENT-MIB     | model, serial number, firmware, hardware, bootrom, MAC, uptime |
| `interfaces` | IF-MIB ifTable/ifXTable                          | status, speed, MTU, all traffic counters (64-bit): bytes, unicast/multicast/broadcast, errors, discards, unknown protocols |
| `etherlike`  | EtherLike-MIB dot3StatsTable                     | duplex and Ethernet errors by type: FCS, alignment, collisions (including late), symbol... |
| `rmon`       | RMON-MIB etherStatsTable                         | frame size histogram, undersize/oversize/fragments/jabbers, CRC, drop events |
| `health`     | AGENT-GENERAL-MIB, EQUIPMENT-MIB                 | CPU (5s/1m/5m), DRAM/flash, temperature and thresholds, fans, power supplies |
| `ddm`        | D-Link DDM `171.12.72`                           | SFP: temperature, voltage, bias current, Tx/Rx (dBm), alarm/warning thresholds |
| `lldp`       | LLDP-MIB lldpRemTable                            | neighbors per port |
| `fdb`        | Q-BRIDGE-MIB / BRIDGE-MIB                        | **MAC address table**: MAC, VLAN, port, status, plus the number of MACs per port |

`fdb` is not part of the `default` module. The MAC table is scraped through a separate `fdb` module at its own
interval, because every entry becomes a series.

## Quick start

```bash
make build
./bin/dlink_exporter --config.file=config.example.yml
curl 'http://localhost:9841/probe?target=192.0.2.10'
curl 'http://localhost:9841/probe?target=192.0.2.10&module=fdb'
```

### Docker Compose: exporter + Prometheus + Grafana

`deploy/` holds a complete stack: the exporter, Prometheus, and Grafana with the dashboard provisioned
(it opens as the Grafana home page).

```bash
cd deploy
cp .env.example .env                                                  # optional
cp prometheus/targets/dlink.yml.example prometheus/targets/dlink.yml  # list your switches
docker compose up -d
```

Grafana http://localhost:3000, Prometheus http://localhost:9090, exporter http://localhost:9841. Prometheus picks up
changes to `prometheus/targets/dlink.yml` by itself. The exporter config is `deploy/dlink/config.yml`. The stack
has a fixed project name, `dlink-snmp-exporter`, so it does not clash with other compose stacks started from a
`deploy` directory. Ports 9090 and 3000 are still shared: if another Prometheus and Grafana already run on the host,
set different `PROMETHEUS_PORT` and `GRAFANA_PORT`.

Every variable is optional; the defaults are in `docker-compose.yml`:

| Variable | Default | Purpose |
|---|---|---|
| `DLINK_EXPORTER_TAG` | `1.0.0` | tag of `andrewpinkerton/dlink-snmp-exporter` |
| `EXPORTER_LOG_LEVEL` | `info` | exporter log level |
| `SNMP_COMMUNITY` | `public` | SNMPv2c community, substituted into `dlink/config.yml` |
| `SNMP_AUTH_PASSWORD`, `SNMP_PRIV_PASSWORD` | empty | SNMPv3 passwords (the v3 block in `dlink/config.yml` is commented out) |
| `GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASSWORD` | `admin` / `admin` | Grafana administrator; change the password before exposing Grafana |
| `EXPORTER_PORT`, `PROMETHEUS_PORT`, `GRAFANA_PORT` | `9841`, `9090`, `3000` | published ports |
| `PROMETHEUS_RETENTION` | `30d` | how long Prometheus keeps data |
| `TZ` | `UTC` | Grafana time zone |

To build the exporter from source instead of pulling the image:
`docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build`.

## Configuration

```yaml
auths:
  public_v2:
    version: 2
    community: public
  secure_v3:
    version: 3
    username: monitor
    security_level: authPriv        # noAuthNoPriv | authNoPriv | authPriv
    auth_protocol: SHA              # MD5 | SHA | SHA224 | SHA256 | SHA384 | SHA512
    auth_password: ${SNMP_AUTH_PASSWORD}
    priv_protocol: AES              # DES | AES | AES192 | AES256 | AES192C | AES256C
    priv_password: ${SNMP_PRIV_PASSWORD}

modules:
  default:                          # system, interfaces, etherlike, rmon, health, ddm, lldp
    timeout: 5s                     # per SNMP request
    retries: 1
    max_repetitions: 25
    interface_types: [6, 117, 161]  # ifType: ethernetCsmacd, gigabitEthernet, ieee8023adLag; [] = all
  fdb:
    collectors: [fdb]
    fdb_max_entries: 20000          # above this the collector fails and no MACs are exported
```

- `auth` may be omitted when the config defines only one auth. `module` defaults to `default`.
- `interface_types` exists because D-Link lists VLAN interfaces (`802.1Q Encapsulation Tag 0001`) and a `System`
  interface in ifTable. The filter applies to `interfaces`, `etherlike` and `rmon`.
- `fdb_max_entries` protects Prometheus from a MAC flood: above the limit only the per-port MAC counts are exported,
  and `dlink_collector_success{collector="fdb"}` becomes 0.
- Secrets are read from the environment only in the `${VAR}` form (a lone `$` in a password is left alone).
  If the variable is not set, the config fails to load.
- Reload the config with `SIGHUP` or `POST /-/reload`. Validate it without starting: `--config.check`.

### Flags

| Flag                          | Default          | Description |
|-------------------------------|------------------|-------------|
| `--config.file`               | `config.yml`     | configuration file |
| `--web.listen-address`        | `:9841`          | HTTP address |
| `--web.config.file`           |                  | TLS/basic auth ([exporter-toolkit](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)) |
| `--scrape.timeout-offset`     | `500ms`          | subtracted from `X-Prometheus-Scrape-Timeout-Seconds` |
| `--log.level`, `--log.format` | `info`, `logfmt` | |

### Prometheus

```yaml
scrape_configs:
  - job_name: dlink
    scrape_interval: 30s
    scrape_timeout: 15s
    metrics_path: /probe
    params:
      module: [default]
    static_configs:
      - targets: [192.0.2.10, 192.0.2.11]
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: dlink-snmp-exporter:9841

  - job_name: dlink_fdb               # MAC table
    scrape_interval: 1m
    scrape_timeout: 30s
    metrics_path: /probe
    params:
      module: [fdb]
    static_configs:
      - targets: [192.0.2.10, 192.0.2.11]
    relabel_configs: # same as above
```

A complete example with file_sd is in `deploy/prometheus/prometheus.yml`.

## Metrics

Every per-port metric carries `ifindex`, `ifname`, `description` (ifAlias) and `port`. `port` is written the way
the D-Link CLI shows it: `1:24` on DGS-3xxx (unit:port), `24` on DGS-1500 (`Slot0/24`), `po1` for a LAG.

### Interfaces

| Metric | Type | Description |
|---|---|---|
| `dlink_interface_oper_up`, `dlink_interface_admin_up` | gauge | port state |
| `dlink_interface_speed_bytes` | gauge | speed in bytes per second |
| `dlink_interface_mtu_bytes` | gauge | MTU |
| `dlink_interface_last_change_timestamp_seconds` | gauge | time of the last status change |
| `dlink_interface_{receive,transmit}_bytes_total` | counter | bytes (ifHC*, falling back to 32-bit counters) |
| `dlink_interface_{receive,transmit}_{unicast,multicast,broadcast}_packets_total` | counter | packets by type |
| `dlink_interface_{receive,transmit}_{errors,discards}_total` | counter | errors and discards |
| `dlink_interface_receive_unknown_protocol_packets_total` | counter | packets dropped for an unknown protocol |
| `dlink_interface_duplex{duplex}` | gauge | state set: unknown, half, full |
| `dlink_interface_ethernet_errors_total{type}` | counter | alignment, fcs, single_collision, multiple_collision, sqe_test, deferred, late_collision, excessive_collision, internal_mac_transmit, carrier_sense, frame_too_long, internal_mac_receive, symbol |
| `dlink_rmon_packets_total{size}` | counter | frames by size: 64, 65_127, 128_255, 256_511, 512_1023, 1024_1518 |
| `dlink_rmon_{drop_events,crc_align_errors,undersize_packets,oversize_packets,fragments,jabbers,collisions}_total` | counter | RMON error counters |

RMON octets are not exported: the counter is 32-bit and wraps in about 34 s at 1 Gbit/s. Use
`dlink_interface_*_bytes_total` for bytes.

### MAC table (`fdb` module)

| Metric | Description |
|---|---|
| `dlink_fdb_entry{<port>,vlan,mac,status}` | one MAC table entry, always 1. `status`: learned, mgmt, self, other. The switch's own MAC is on `port="cpu"` |
| `dlink_fdb_entries{<port>}` | number of MACs on the port |

Example queries: find the port of a MAC with `dlink_fdb_entry{mac=~"00:24:97.*"}`; find ports with more than one
MAC (an unmanaged switch or a loop) with `dlink_fdb_entries{port!="cpu"} > 1`.

### System and health

| Metric | Description |
|---|---|
| `dlink_device_info{model,serial,hardware,firmware,bootrom,firmware_type,mac,sysname,sysobjectid}` | device identification |
| `dlink_uptime_seconds` | agent uptime |
| `dlink_cpu_utilization_ratio{period="5s\|1m\|5m"}` | CPU load |
| `dlink_memory_{total,used}_bytes{unit,type="dram\|flash"}` | memory |
| `dlink_temperature_celsius{unit}`, `dlink_temperature_threshold_celsius{unit,type="high\|low"}` | temperature and thresholds |
| `dlink_fan_status{unit,fan,position,status}` | state set: other, working, fail, speed_0, speed_low, speed_middle, speed_high (`speed_0` means the fan is stopped because it is cool) |
| `dlink_fan_failed{unit,fan,position}`, `dlink_fan_speed_rpm{...}` | fan failure and speed |
| `dlink_power_supply_status{unit,psu,status}` | state set: other, low_voltage, over_current, working, fail, connect, disconnect |

### SFP / DDM

| Metric | Description |
|---|---|
| `dlink_sfp_temperature_celsius`, `dlink_sfp_voltage_volts`, `dlink_sfp_bias_current_amperes`, `dlink_sfp_tx_power_dbm`, `dlink_sfp_rx_power_dbm` | DDM readings (only ports with a DDM-capable module) |
| `dlink_sfp_threshold{param,level}` | module thresholds: `level` = high_alarm, high_warning, low_warning, low_alarm; `param` is the metric name above |

Zero optical power (no light) is exported as −40 dBm.

### LLDP

`dlink_lldp_neighbor_info{<port>,chassis_id,port_id,port_description,system_name,system_description}` = 1.

### Exporter

`dlink_probe_success`, `dlink_probe_duration_seconds`, `dlink_collector_success{collector}`,
`dlink_collector_duration_seconds{collector}`. On the exporter's own `/metrics`:
`dlink_exporter_probes_total{module,result}`, `dlink_exporter_config_last_reload_successful`, `dlink_exporter_build_info`.

## OID map and firmware quirks

| Data | OID | Note |
|---|---|---|
| CPU | `1.3.6.1.4.1.171.12.1.1.6.{1,2,3}.0` | AGENT-GENERAL-MIB, % over 5s/1m/5m |
| Memory | `1.3.6.1.4.1.171.12.1.1.{9,10}.1.{2,3}.<unit>` | DRAM/flash, total/used in KB |
| Serial number, firmware type | `1.3.6.1.4.1.171.12.1.1.{12,13}.0` | |
| Power supplies | `1.3.6.1.4.1.171.12.11.1.6.1.3.<unit>.<psu>` | EQUIPMENT-MIB swPowerStatus |
| Fans | `1.3.6.1.4.1.171.12.11.1.7.1.{3,4,5,6}.<unit>.<id>` | status, position, number, rpm |
| Temperature | `1.3.6.1.4.1.171.12.11.1.8.1.{2,3,4}.<unit>` | current, high and low threshold |
| Unit identity | `1.3.6.1.4.1.171.12.11.1.9.4.1.{2,9,10,11,12,17}.<unit>` | MAC, model, boot, fw, hw, serial |
| DDM readings | `1.3.6.1.4.1.171.12.72.2.1.1.1.{2..6}.<port>` | strings: °C, V, mA, mW, mW |
| DDM thresholds | `1.3.6.1.4.1.171.12.72.3.1.1.1.{2..5}.<port>.<param>` | high alarm, low alarm, high warning, low warning |
| DGS-1500 hardware, firmware | `<sysObjectID>.1.2.0`, `<sysObjectID>.1.3.0` | smart series private branch |
| MAC table | `1.3.6.1.2.1.17.7.1.2.2.1.{2,3}.<vlan>.<mac>` | Q-BRIDGE; falls back to `17.4.3.1` when empty |

Quirks handled in the code:

- **DDM values are strings** (`"0.1615"` mW). A port without a DDM-capable module returns `"  -"`. D-Link has not
  published a MIB for this branch: column meanings were derived from the data and checked against the module's
  thresholds.
- **VLAN interfaces and `System` in ifTable** on DGS-3120 are filtered out by `interface_types`.
- **DGS-1500 reports no CPU, memory, temperature, fans, PSU or DDM.** These objects exist neither in D-Link's
  official DGS-1500-28 MIB (2.50.002) nor in the agent's answers, and the DGS-1500-28 is fanless. The dashboard marks
  such panels "not reported by this model".
- **DGS-1500 `dot3StatsSQETestErrors`** holds the same meaningless number (~1.1e9) on every port, so it is not
  exported for the DGS-1500 family.
- **DGS-1500 LAGs (`po1…po14`)** exist only in ifXTable, without ifType and with zero counters. Not exported.
- **DGS-3120 DRAM is always ~99% used.** This is how the firmware reports it, not a leak. Watch the trend.
- **DGS-3120 second power supply in `fail`** means no redundant power supply (RPS) is connected.
- **The DGS-3120 MAC table holds entries from VLANs not configured on it** (for example 19, 60, 2573). They are
  exported as they are, with that VLAN ID.
- **Probe time:** about 1.5 s for a full probe of a DGS-3120, about 2 s for a DGS-1500 (every request to the
  DGS-1500 agent costs about 0.05–0.1 s). The `fdb` module takes 0.2–0.5 s. Tables a switch does not have cost a
  single request.
- **Unreachable switch:** after the first timeout the remaining collectors are skipped, so the probe stays within the
  scrape timeout.

## Security

- Use a dedicated read-only community restricted by ACL to the exporter's address, or better SNMPv3 `authPriv`.
- Review read-write communities and remove the ones you do not need: the exporter only reads. The tested DGS-1500
  has a `private` community with the `ReadWrite` view besides `public`.
- On a DGS-1500 the read-only community can read the community and user tables over SNMP. This is one more reason to
  restrict SNMP access to a list of addresses.
- The exporter's web interface can be protected with TLS and basic auth via `--web.config.file`.

## Deployment

- **Docker:** image `andrewpinkerton/dlink-snmp-exporter` (distroless, non-root, linux/amd64 and linux/arm64).
  Mount the config at `/etc/dlink_exporter/config.yml`:

  ```bash
  docker run -d -p 9841:9841 -v $PWD/config.yml:/etc/dlink_exporter/config.yml:ro \
    andrewpinkerton/dlink-snmp-exporter:1.0.0
  ```

  Versions follow SemVer (number in the `VERSION` file). Each release is published as a single tag with the exact
  version. `make docker` builds a local image, `make docker-push` pushes it, `make docker-release` builds
  amd64+arm64 and pushes right away. Pushing requires `docker login`.
- **systemd:** `deploy/systemd/dlink-snmp-exporter.service` (DynamicUser, hardening). Installation steps are in a
  comment inside the file. Put secrets in `/etc/default/dlink_exporter` with mode 0600.
- **Grafana:** dashboard `grafana/dlink.json`. It has a device overview, a port table (status, speed, duplex,
  traffic, errors, MAC count), traffic and error graphs, RMON, SFP, **MAC table search** (Port, VLAN and MAC
  variables), LLDP and system health. It is generated with `make dashboard`.

## Development

```bash
make test       # unit + golden tests on real dumps in testdata/*.walk
make lint       # gofmt + go vet
make golden     # regenerate internal/collector/testdata/<model>/*.prom after an intentional change
make mibcheck   # MIBs load in net-snmp and every private OID from the dumps resolves
```

Fixtures are taken with a full walk, then cut down to the branches the exporter reads and anonymized (MACs, serial
numbers, sysName):

```bash
snmpbulkwalk -v2c -c public -On -Oe -Ot -Cc <switch> .1 > full.walk
python3 testdata/mkfixture.py full.walk > testdata/<model>.walk
```

## MIBs

`mibs/` holds MIBs for the private objects the exporter reads:
- `AGENT-GENERAL-MIB`, `EQUIPMENT-MIB`, `DLINK-ID-REC-MIB`, `TIMERANGE-MIB`: D-Link MIBs (from LibreNMS);
- `DLINK-DDM-RECONSTRUCTED-MIB`: our reconstruction of the DDM branch `171.12.72`, because D-Link has not published
  a MIB for it. The object names are ours;
- `deps/`: standard dependencies (Q-BRIDGE-MIB and others) that net-snmp does not ship.

The exporter itself does not load MIBs; OIDs are compiled in.

## License

[MIT](LICENSE)
