#!/usr/bin/env python3
"""Generates grafana/dlink.json:  python3 grafana/gen_dashboard.py > grafana/dlink.json"""
import json, sys

DS = {"type": "prometheus", "uid": "${datasource}"}
I = 'instance="$instance"'
panels = []
_id = [0]


def nid():
    _id[0] += 1
    return _id[0]


def tgt(expr, legend="", ref="A", instant=False, fmt=None):
    t = {"datasource": DS, "expr": expr, "legendFormat": legend, "refId": ref}
    if instant:
        t["instant"] = True
        t["range"] = False
    if fmt:
        t["format"] = fmt
    return t


def thresholds(*steps):
    out = [{"color": steps[0], "value": None}]
    for i in range(1, len(steps), 2):
        out.append({"color": steps[i + 1], "value": steps[i]})
    return {"mode": "absolute", "steps": out}


def row(title, y, collapsed=False):
    panels.append({"type": "row", "title": title, "id": nid(), "collapsed": collapsed,
                   "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "panels": []})


def panel(ptype, title, x, y, w, h, targets, unit=None, desc=None, defaults=None, options=None, overrides=None,
          transformations=None):
    fd = {"unit": unit} if unit else {}
    fd.update(defaults or {})
    p = {"type": ptype, "title": title, "id": nid(), "datasource": DS,
         "gridPos": {"h": h, "w": w, "x": x, "y": y}, "targets": targets,
         "fieldConfig": {"defaults": fd, "overrides": overrides or []}, "options": options or {}}
    if desc:
        p["description"] = desc
    if transformations:
        p["transformations"] = transformations
    panels.append(p)
    return p


# Shown instead of "No data" for metrics some models do not report at all
# (DGS-1500 smart switches have no CPU, memory, temperature, fan, PSU or DDM objects).
NA = "not reported by this model"
NA_DESC = " Smart switches such as the DGS-1500 do not report this over SNMP."


def stat(title, x, y, w, h, targets, unit=None, th=None, mappings=None, desc=None, color_mode="value", na=False):
    d = {"color": {"mode": "fixed", "fixedColor": "text"}}
    if na:
        d["noValue"] = NA
        desc = (desc or "") + NA_DESC
    if th:
        d["thresholds"] = th
        d["color"] = {"mode": "thresholds"}
    if mappings:
        d["mappings"] = mappings
    return panel("stat", title, x, y, w, h, targets, unit, desc, d,
                 {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                  "colorMode": color_mode, "graphMode": "none", "textMode": "auto", "justifyMode": "auto"})


def ts(title, x, y, w, h, targets, unit=None, stack=False, desc=None, overrides=None, fill=10, minv=None, na=False):
    custom = {"drawStyle": "line", "lineWidth": 1, "fillOpacity": fill, "showPoints": "never",
              "spanNulls": True, "stacking": {"mode": "normal" if stack else "none", "group": "A"}}
    d = {"custom": custom}
    if na:
        d["noValue"] = NA
        desc = (desc or "") + NA_DESC
    if minv is not None:
        d["min"] = minv
    return panel("timeseries", title, x, y, w, h, targets, unit, desc, d,
                 {"legend": {"displayMode": "table", "placement": "right", "calcs": ["lastNotNull", "max"]},
                  "tooltip": {"mode": "multi", "sort": "desc"}}, overrides)


def timeline(title, x, y, w, h, targets, mappings, desc=None):
    return panel("state-timeline", title, x, y, w, h, targets, None, desc,
                 {"mappings": mappings, "color": {"mode": "thresholds"},
                  "thresholds": thresholds("red", 1, "green"), "custom": {"fillOpacity": 80, "lineWidth": 0}},
                 {"showValue": "never", "mergeValues": True, "rowHeight": 0.9, "alignValue": "left",
                  "legend": {"showLegend": False}})


def value_map(**kv):
    """Value mappings from {"value": (text, color)}; color None keeps the default text color."""
    opts = {}
    for i, (k, (t, c)) in enumerate(kv.items()):
        opts[k] = {"text": t, "index": i}
        if c:
            opts[k]["color"] = c
    return [{"type": "value", "options": opts}]


UPDOWN = value_map(**{"0": ("down", "red"), "1": ("up", "green")})
DUPLEX = value_map(**{"0": ("unknown", "transparent"), "1": ("HALF", "orange"), "2": ("full", "transparent")})


def cells(kind):
    return {"id": "custom.cellOptions", "value": {"type": kind}}


def by_name(name, *props):
    return {"matcher": {"id": "byName", "options": name}, "properties": list(props)}


# Table panels join several instant queries with the "merge" transformation,
# which needs identical label sets: aggregate to the port labels only.
PORT_BY = "ifindex, port, ifname, description"
HIDE = {"Time": True, "instance": True, "job": True, "site": True, "__name__": True}
PORT_RENAME = {"port": "Port", "ifname": "Interface", "description": "Description"}
# Port labels look like "1:24" (DGS-3xxx) or "24" (DGS-1500): sort by ifindex instead.
SORT_BY_IFINDEX = [
    {"id": "convertFieldType", "options": {"conversions": [{"targetField": "ifindex", "destinationType": "number"}]}},
    {"id": "sortBy", "options": {"sort": [{"field": "ifindex"}]}},
]

y = 0
# ---------------------------------------------------------------- overview
row("Overview", y); y += 1
panel("table", "Device", 0, y, 9, 4,
      [tgt(f"dlink_device_info{{{I}}}", ref="A", instant=True, fmt="table")],
      options={"showHeader": True, "cellHeight": "sm"},
      transformations=[{"id": "organize", "options": {
          "excludeByName": {**HIDE, "Value": True, "sysobjectid": True, "firmware_type": True},
          "indexByName": {"sysname": 0, "model": 1, "firmware": 2, "serial": 3, "mac": 4, "hardware": 5,
                          "bootrom": 6}}}])
stat("Probe", 9, y, 2, 4, [tgt(f"min(dlink_probe_success{{{I}}})")],
     th=thresholds("red", 1, "green"),
     mappings=[{"type": "value", "options": {"0": {"text": "FAIL"}, "1": {"text": "OK"}}}], color_mode="background")
stat("Uptime", 11, y, 3, 4, [tgt(f"dlink_uptime_seconds{{{I}}}")], unit="s")
stat("CPU (1m)", 14, y, 2, 4, [tgt(f'dlink_cpu_utilization_ratio{{{I},period="1m"}}')], unit="percentunit",
     th=thresholds("green", 0.7, "orange", 0.9, "red"), na=True)
stat("Temperature", 16, y, 2, 4, [tgt(f"max(dlink_temperature_celsius{{{I}}})")], unit="celsius",
     th=thresholds("green", 60, "orange", 70, "red"), na=True)
stat("Failed fans", 18, y, 2, 4, [tgt(f"sum(dlink_fan_failed{{{I}}})")],
     th=thresholds("green", 1, "red"), na=True)
stat("Power supplies", 20, y, 4, 4,
     [tgt(f'max by (unit, psu) (dlink_power_supply_status{{{I},status="working"}})', "PSU {{psu}}")],
     mappings=value_map(**{"0": ("not working", "orange"), "1": ("working", "green")}),
     desc="A switch without a redundant power supply connected usually reports the second PSU as failed.", na=True)
y += 4

stat("Ports up", 0, y, 4, 3, [tgt(f"sum(dlink_interface_oper_up{{{I}}})")])
stat("MAC addresses", 4, y, 4, 3, [tgt(f"sum(dlink_fdb_entries{{{I}}})")],
     desc="Entries in the MAC address table (fdb job, scraped every minute).")
stat("LLDP neighbors", 8, y, 4, 3, [tgt(f"count(dlink_lldp_neighbor_info{{{I}}}) or vector(0)")])
stat("Half-duplex ports", 12, y, 4, 3,
     [tgt(f'sum(dlink_interface_duplex{{{I},duplex="half"}} and on(instance, ifindex) dlink_interface_oper_up == 1) or vector(0)')],
     th=thresholds("green", 1, "orange"), color_mode="background",
     desc="Ports that are up in half duplex: usually a duplex mismatch with the connected device.")
stat("Errors (1h)", 16, y, 4, 3,
     [tgt(f"sum(increase(dlink_interface_receive_errors_total{{{I}}}[1h])) + sum(increase(dlink_interface_transmit_errors_total{{{I}}}[1h]))")],
     th=thresholds("green", 1, "orange"), desc="Inbound + outbound errors on all ports over the last hour.")
stat("Discards (1h)", 20, y, 4, 3,
     [tgt(f"sum(increase(dlink_interface_receive_discards_total{{{I}}}[1h])) + sum(increase(dlink_interface_transmit_discards_total{{{I}}}[1h]))")],
     th=thresholds("green", 1, "orange"))
y += 3

# ---------------------------------------------------------------- ports
row("Ports", y); y += 1
port_q = [
    ("A", f"max by ({PORT_BY}) (dlink_interface_oper_up{{{I}}})", "Link"),
    ("B", f"max by ({PORT_BY}) (dlink_interface_admin_up{{{I}}})", "Admin"),
    ("C", f"max by ({PORT_BY}) (dlink_interface_speed_bytes{{{I}}}) * 8", "Speed"),
    ("D", f'max by ({PORT_BY}) (dlink_interface_duplex{{{I},duplex="full"}}) * 2 + max by ({PORT_BY}) (dlink_interface_duplex{{{I},duplex="half"}})', "Duplex"),
    ("E", f"max by ({PORT_BY}) (rate(dlink_interface_receive_bytes_total{{{I}}}[5m])) * 8", "In"),
    ("F", f"max by ({PORT_BY}) (rate(dlink_interface_transmit_bytes_total{{{I}}}[5m])) * 8", "Out"),
    ("G", f"max by ({PORT_BY}) (rate(dlink_interface_receive_errors_total{{{I}}}[5m]) + rate(dlink_interface_transmit_errors_total{{{I}}}[5m]))", "Errors/s"),
    ("H", f"max by ({PORT_BY}) (rate(dlink_interface_receive_discards_total{{{I}}}[5m]) + rate(dlink_interface_transmit_discards_total{{{I}}}[5m]))", "Discards/s"),
    ("I", f'max by ({PORT_BY}) (dlink_fdb_entries{{{I},port!="cpu"}})', "MACs"),
    ("J", f"max by ({PORT_BY}) (dlink_interface_last_change_timestamp_seconds{{{I}}}) * 1000", "Last change"),
]
port_over = [
    by_name("Link", {"id": "mappings", "value": UPDOWN}, cells("color-background")),
    by_name("Admin", {"id": "mappings", "value": value_map(**{"0": ("disabled", "orange"), "1": ("enabled", None)})},
            cells("color-text")),
    by_name("Speed", {"id": "unit", "value": "bps"}),
    by_name("Duplex", {"id": "mappings", "value": DUPLEX}, cells("color-background")),
    by_name("In", {"id": "unit", "value": "bps"}, cells("gauge"), {"id": "max", "value": 1e9},
            {"id": "color", "value": {"mode": "continuous-GrYlRd"}}),
    by_name("Out", {"id": "unit", "value": "bps"}, cells("gauge"), {"id": "max", "value": 1e9},
            {"id": "color", "value": {"mode": "continuous-GrYlRd"}}),
    by_name("Errors/s", {"id": "decimals", "value": 2}, cells("color-text"),
            {"id": "thresholds", "value": thresholds("text", 0.001, "red")}, {"id": "color", "value": {"mode": "thresholds"}}),
    by_name("Discards/s", {"id": "decimals", "value": 2}, cells("color-text"),
            {"id": "thresholds", "value": thresholds("text", 0.001, "orange")}, {"id": "color", "value": {"mode": "thresholds"}}),
    by_name("Last change", {"id": "unit", "value": "dateTimeFromNow"}),
]
panel("table", "Ports", 0, y, 24, 14,
      [tgt(q, ref=r, instant=True, fmt="table") for r, q, _ in port_q],
      defaults={"custom": {"align": "auto", "filterable": True}},
      options={"showHeader": True, "cellHeight": "sm"}, overrides=port_over,
      desc="In/Out are 5-minute averages; MACs comes from the fdb job.",
      transformations=[{"id": "merge", "options": {}}] + SORT_BY_IFINDEX + [
          {"id": "organize", "options": {
              "excludeByName": {**HIDE, "ifindex": True},
              "indexByName": {"port": 0, "ifname": 1, "description": 2},
              "renameByName": {**PORT_RENAME, **{f"Value #{r}": n for r, _, n in port_q}}}}])
y += 14

# ---------------------------------------------------------------- traffic
row("Traffic", y); y += 1
up = f"and on(instance, ifindex) dlink_interface_oper_up{{{I}}} == 1"
ts("Traffic (bits/s)", 0, y, 24, 9,
   [tgt(f"rate(dlink_interface_receive_bytes_total{{{I}}}[$__rate_interval]) * 8 {up}", "{{port}} {{description}} in", "A"),
    tgt(f"rate(dlink_interface_transmit_bytes_total{{{I}}}[$__rate_interval]) * 8 {up}", "{{port}} {{description}} out", "B")],
   "bps", desc="Ports that are up. Outbound traffic is drawn below zero.",
   overrides=[{"matcher": {"id": "byFrameRefID", "options": "B"},
               "properties": [{"id": "custom.transform", "value": "negative-Y"}]}])
y += 9
for i, kind in enumerate(["unicast", "multicast", "broadcast"]):
    ts(f"{kind.capitalize()} packets/s", i * 8, y, 8, 8,
       [tgt(f"rate(dlink_interface_receive_{kind}_packets_total{{{I}}}[$__rate_interval]) {up}", "{{port}} in", "A"),
        tgt(f"rate(dlink_interface_transmit_{kind}_packets_total{{{I}}}[$__rate_interval]) {up}", "{{port}} out", "B")],
       "pps", overrides=[{"matcher": {"id": "byFrameRefID", "options": "B"},
                          "properties": [{"id": "custom.transform", "value": "negative-Y"}]}],
       desc="A broadcast/multicast storm shows up here first." if kind != "unicast" else None)
y += 8
ts("Errors & discards (packets/s)", 0, y, 12, 8,
   [tgt(f"rate(dlink_interface_receive_errors_total{{{I}}}[$__rate_interval]) > 0", "{{port}} rx errors", "A"),
    tgt(f"rate(dlink_interface_transmit_errors_total{{{I}}}[$__rate_interval]) > 0", "{{port}} tx errors", "B"),
    tgt(f"rate(dlink_interface_receive_discards_total{{{I}}}[$__rate_interval]) > 0", "{{port}} rx discards", "C"),
    tgt(f"rate(dlink_interface_transmit_discards_total{{{I}}}[$__rate_interval]) > 0", "{{port}} tx discards", "D"),
    tgt(f"rate(dlink_interface_receive_unknown_protocol_packets_total{{{I}}}[$__rate_interval]) > 0", "{{port}} unknown proto", "E")],
   "pps", minv=0)
ts("Ethernet errors by type", 12, y, 12, 8,
   [tgt(f"rate(dlink_interface_ethernet_errors_total{{{I}}}[$__rate_interval]) > 0", "{{port}} {{type}}")],
   "pps", minv=0, desc="EtherLike-MIB: FCS, alignment, collisions, symbol errors... Late collisions point to a duplex mismatch.")
y += 8
timeline("Link state", 0, y, 12, 9, [tgt(f"dlink_interface_oper_up{{{I}}}", "{{port}} {{description}}")], UPDOWN)
ts("Frame sizes (RMON, packets/s)", 12, y, 12, 9,
   [tgt(f'sum by (size) (rate(dlink_rmon_packets_total{{{I}}}[$__rate_interval]))', "{{size}} bytes")],
   "pps", stack=True, minv=0, desc="All ports, by frame size.", na=True)
y += 9

# ---------------------------------------------------------------- sfp
row("SFP / DDM", y); y += 1
dashed = [{"id": "custom.lineStyle", "value": {"fill": "dash", "dash": [10, 10]}}, {"id": "custom.fillOpacity", "value": 0}]
ts("Rx power", 0, y, 12, 8,
   [tgt(f"dlink_sfp_rx_power_dbm{{{I}}}", "{{port}} {{description}}", "A"),
    tgt(f'dlink_sfp_threshold{{{I},param="rx_power_dbm",level="low_alarm"}}', "{{port}} low alarm", "B")],
   "dBm", desc="Dashed lines are the module's low-alarm thresholds.",
   overrides=[{"matcher": {"id": "byFrameRefID", "options": "B"}, "properties": dashed}], fill=0, na=True)
ts("Tx power", 12, y, 12, 8, [tgt(f"dlink_sfp_tx_power_dbm{{{I}}}", "{{port}} {{description}}")], "dBm", fill=0, na=True)
y += 8
sfp_q = [("A", "dlink_sfp_temperature_celsius", "Temperature", "celsius"),
         ("B", "dlink_sfp_rx_power_dbm", "Rx", "dBm"),
         ("C", "dlink_sfp_tx_power_dbm", "Tx", "dBm"),
         ("D", "dlink_sfp_bias_current_amperes", "Bias", "amp"),
         ("E", "dlink_sfp_voltage_volts", "Voltage", "volt")]
sfp_targets = [tgt(f"max by ({PORT_BY}) ({m}{{{I}}})", ref=r, instant=True, fmt="table") for r, m, _, _ in sfp_q]
sfp_targets += [
    tgt(f'max by ({PORT_BY}) (dlink_sfp_threshold{{{I},param="rx_power_dbm",level="low_alarm"}})', ref="F", instant=True, fmt="table"),
    tgt(f'max by ({PORT_BY}) (dlink_sfp_threshold{{{I},param="rx_power_dbm",level="high_alarm"}})', ref="G", instant=True, fmt="table"),
]
sfp_names = {r: n for r, _, n, _ in sfp_q} | {"F": "Rx low alarm", "G": "Rx high alarm"}
sfp_over = [by_name(n, {"id": "unit", "value": u}, {"id": "decimals", "value": 2}) for _, _, n, u in sfp_q]
sfp_over += [by_name(n, {"id": "unit", "value": "dBm"}, {"id": "decimals", "value": 2}) for n in ("Rx low alarm", "Rx high alarm")]
panel("table", "Transceivers", 0, y, 24, 6, sfp_targets,
      options={"showHeader": True, "cellHeight": "sm"}, overrides=sfp_over,
      transformations=[{"id": "merge", "options": {}}] + SORT_BY_IFINDEX + [
          {"id": "organize", "options": {
              "excludeByName": {**HIDE, "ifindex": True},
              "indexByName": {"port": 0, "ifname": 1, "description": 2},
              "renameByName": {**PORT_RENAME, **{f"Value #{r}": n for r, n in sfp_names.items()}}}}])
y += 6

# ---------------------------------------------------------------- MAC table
row("MAC address table", y); y += 1
fdb_sel = f'{I},port=~"$port",mac=~"(?i).*$mac.*",vlan=~"$vlan"'
panel("table", "MAC addresses", 0, y, 14, 14,
      [tgt(f"dlink_fdb_entry{{{fdb_sel}}}", ref="A", instant=True, fmt="table")],
      defaults={"custom": {"align": "auto", "filterable": True}},
      options={"showHeader": True, "cellHeight": "sm"},
      desc="Filter with the Port, VLAN and MAC variables at the top (MAC accepts a fragment, colon-separated).",
      transformations=SORT_BY_IFINDEX + [
          {"id": "organize", "options": {
              "excludeByName": {**HIDE, "ifindex": True, "Value": True},
              "indexByName": {"port": 0, "ifname": 1, "description": 2, "vlan": 3, "mac": 4, "status": 5},
              "renameByName": {**PORT_RENAME, "vlan": "VLAN", "mac": "MAC", "status": "Status"}}}])
panel("bargauge", "MACs per port", 14, y, 10, 14,
      [tgt(f'sort_desc(dlink_fdb_entries{{{I},port=~"$port"}})', "{{port}} {{description}}", instant=True)],
      defaults={"color": {"mode": "continuous-BlYlRd"}, "min": 0},
      options={"displayMode": "basic", "orientation": "horizontal", "showUnfilled": True, "valueMode": "text",
               "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}},
      desc="Many MACs on an access port: an unmanaged switch or a loop.")
y += 14
ts("MACs per port over time", 0, y, 24, 7, [tgt(f'dlink_fdb_entries{{{I},port=~"$port"}}', "{{port}} {{description}}")],
   minv=0, fill=0)
y += 7

# ---------------------------------------------------------------- LLDP
row("LLDP neighbors", y); y += 1
panel("table", "LLDP neighbors", 0, y, 24, 6,
      [tgt(f"dlink_lldp_neighbor_info{{{I}}}", ref="A", instant=True, fmt="table")],
      options={"showHeader": True, "cellHeight": "sm"},
      transformations=SORT_BY_IFINDEX + [
          {"id": "organize", "options": {
              "excludeByName": {**HIDE, "ifindex": True, "Value": True},
              "indexByName": {"port": 0, "ifname": 1, "description": 2, "system_name": 3, "port_id": 4,
                              "port_description": 5, "chassis_id": 6, "system_description": 7},
              "renameByName": {**PORT_RENAME, "system_name": "Neighbor", "port_id": "Neighbor port",
                               "port_description": "Neighbor port description", "chassis_id": "Chassis ID",
                               "system_description": "Neighbor system"}}}])
y += 6

# ---------------------------------------------------------------- system
row("System", y); y += 1
ts("CPU", 0, y, 6, 7, [tgt(f"dlink_cpu_utilization_ratio{{{I}}}", "{{period}}")], "percentunit", minv=0, fill=0, na=True)
ts("Memory used", 6, y, 6, 7,
   [tgt(f"dlink_memory_used_bytes{{{I}}} / dlink_memory_total_bytes{{{I}}}", "unit {{unit}} {{type}}")],
   "percentunit", minv=0, fill=0, na=True, desc="DGS-3120 firmware reports DRAM as ~99% used at all times; watch the trend, not the level.")
ts("Temperature", 12, y, 6, 7,
   [tgt(f"dlink_temperature_celsius{{{I}}}", "unit {{unit}}", "A"),
    tgt(f'dlink_temperature_threshold_celsius{{{I},type="high"}}', "unit {{unit}} high threshold", "B")], "celsius",
   overrides=[{"matcher": {"id": "byFrameRefID", "options": "B"}, "properties": dashed}], fill=0, na=True)
ts("Fans", 18, y, 6, 7, [tgt(f"dlink_fan_speed_rpm{{{I}}}", "unit {{unit}} fan {{fan}}")], "rotrpm", minv=0, fill=0, na=True)
y += 7
ts("Exporter: collector duration", 0, y, 12, 7,
   [tgt(f"dlink_collector_duration_seconds{{{I}}}", "{{collector}}", "A"),
    tgt(f"dlink_probe_duration_seconds{{{I}}}", "probe {{job}}", "B")], "s", minv=0, fill=0)
timeline("Exporter: collector success", 12, y, 12, 7,
         [tgt(f"dlink_collector_success{{{I}}}", "{{collector}}")],
         [{"type": "value", "options": {"0": {"text": "fail", "color": "red"}, "1": {"text": "ok", "color": "green"}}}])


def query_var(name, label, query, multi=True):
    return {"name": name, "type": "query", "label": label, "datasource": DS,
            "query": {"query": query, "refId": name}, "definition": query, "refresh": 2, "sort": 3,
            "current": {"text": "All", "value": "$__all"} if multi else {}, "hide": 0,
            "includeAll": multi, "multi": multi, "allValue": ".*" if multi else None}


dash = {
    "title": "D-Link switch",
    "uid": "dlink-switch",
    "tags": ["dlink", "network"],
    "timezone": "browser",
    "schemaVersion": 39,
    "version": 1,
    "editable": True,
    "graphTooltip": 1,
    "refresh": "30s",
    "time": {"from": "now-6h", "to": "now"},
    "templating": {"list": [
        {"name": "datasource", "type": "datasource", "query": "prometheus", "label": "Data source",
         "current": {}, "hide": 0, "refresh": 1},
        query_var("instance", "Switch", "label_values(dlink_device_info, instance)", multi=False),
        query_var("port", "Port", 'label_values(dlink_fdb_entries{instance="$instance"}, port)'),
        query_var("vlan", "VLAN", 'label_values(dlink_fdb_entry{instance="$instance"}, vlan)'),
        {"name": "mac", "type": "textbox", "label": "MAC", "query": "", "current": {"text": "", "value": ""},
         "hide": 0, "description": "Fragment of a MAC address, e.g. 00:24:97"},
    ]},
    "annotations": {"list": []},
    "panels": panels,
}
json.dump(dash, sys.stdout, indent=2, ensure_ascii=False)
sys.stdout.write("\n")
