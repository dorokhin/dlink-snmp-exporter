package collector

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

const (
	oidIfEntry  = "1.3.6.1.2.1.2.2.1"
	oidIfXEntry = "1.3.6.1.2.1.31.1.1.1"
)

// ifTable columns
const (
	ifType            = 3
	ifMtu             = 4
	ifSpeed           = 5
	ifAdminStatus     = 7
	ifOperStatus      = 8
	ifLastChange      = 9
	ifInOctets        = 10
	ifInUcastPkts     = 11
	ifInDiscards      = 13
	ifInErrors        = 14
	ifInUnknownProtos = 15
	ifOutOctets       = 16
	ifOutUcastPkts    = 17
	ifOutDiscards     = 19
	ifOutErrors       = 20
)

// ifXTable columns
const (
	ifInMulticastPkts   = 2
	ifInBroadcastPkts   = 3
	ifOutMulticastPkts  = 4
	ifOutBroadcastPkts  = 5
	ifHCInOctets        = 6
	ifHCInUcastPkts     = 7
	ifHCInMulticastPkts = 8
	ifHCInBroadcastPkts = 9
	ifHCOutOctets       = 10
	ifHCOutUcastPkts    = 11
	ifHCOutMulticastPkt = 12
	ifHCOutBroadcastPkt = 13
	ifHighSpeed         = 15
)

var (
	ifOperUpDesc     = desc("interface", "oper_up", "1 when ifOperStatus is up.", portLabels...)
	ifAdminUpDesc    = desc("interface", "admin_up", "1 when ifAdminStatus is up.", portLabels...)
	ifSpeedDesc      = desc("interface", "speed_bytes", "Negotiated speed in bytes per second (0 when down).", portLabels...)
	ifMTUDesc        = desc("interface", "mtu_bytes", "Largest frame the interface can send or receive (ifMtu).", portLabels...)
	ifLastChangeDesc = desc("interface", "last_change_timestamp_seconds",
		"Unix time of the last oper status change (boot time if unchanged since boot).", portLabels...)
)

// cell addresses one column: table 0 = ifTable, 1 = ifXTable.
type cell struct{ table, col int }

// ifCounter is exported from the first cell present, so 64-bit counters win
// and agents without them fall back to the 32-bit ones.
type ifCounter struct {
	cells []cell
	desc  *prometheus.Desc
}

var ifCounters = []ifCounter{
	{[]cell{{1, ifHCInOctets}, {0, ifInOctets}}, desc("interface", "receive_bytes_total", "Received octets.", portLabels...)},
	{[]cell{{1, ifHCOutOctets}, {0, ifOutOctets}}, desc("interface", "transmit_bytes_total", "Transmitted octets.", portLabels...)},
	{[]cell{{1, ifHCInUcastPkts}, {0, ifInUcastPkts}}, desc("interface", "receive_unicast_packets_total", "Received unicast packets.", portLabels...)},
	{[]cell{{1, ifHCOutUcastPkts}, {0, ifOutUcastPkts}}, desc("interface", "transmit_unicast_packets_total", "Transmitted unicast packets.", portLabels...)},
	{[]cell{{1, ifHCInMulticastPkts}, {1, ifInMulticastPkts}}, desc("interface", "receive_multicast_packets_total", "Received multicast packets.", portLabels...)},
	{[]cell{{1, ifHCOutMulticastPkt}, {1, ifOutMulticastPkts}}, desc("interface", "transmit_multicast_packets_total", "Transmitted multicast packets.", portLabels...)},
	{[]cell{{1, ifHCInBroadcastPkts}, {1, ifInBroadcastPkts}}, desc("interface", "receive_broadcast_packets_total", "Received broadcast packets.", portLabels...)},
	{[]cell{{1, ifHCOutBroadcastPkt}, {1, ifOutBroadcastPkts}}, desc("interface", "transmit_broadcast_packets_total", "Transmitted broadcast packets.", portLabels...)},
	{[]cell{{0, ifInErrors}}, desc("interface", "receive_errors_total", "Inbound packets with errors.", portLabels...)},
	{[]cell{{0, ifOutErrors}}, desc("interface", "transmit_errors_total", "Outbound packets that could not be sent due to errors.", portLabels...)},
	{[]cell{{0, ifInDiscards}}, desc("interface", "receive_discards_total", "Inbound packets discarded without error.", portLabels...)},
	{[]cell{{0, ifOutDiscards}}, desc("interface", "transmit_discards_total", "Outbound packets discarded without error.", portLabels...)},
	{[]cell{{0, ifInUnknownProtos}}, desc("interface", "receive_unknown_protocol_packets_total",
		"Inbound packets discarded because of an unknown or unsupported protocol.", portLabels...)},
}

type interfacesCollector struct{}

func (interfacesCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	ports, err := t.Ports(ctx)
	if err != nil {
		return err
	}
	uptime, err := t.Uptime(ctx)
	if err != nil {
		return err
	}
	now := t.now()

	ift, err := snmp.WalkColumns(ctx, t.Client, oidIfEntry,
		ifType, ifMtu, ifSpeed, ifAdminStatus, ifOperStatus, ifLastChange,
		ifInOctets, ifInUcastPkts, ifInDiscards, ifInErrors, ifInUnknownProtos,
		ifOutOctets, ifOutUcastPkts, ifOutDiscards, ifOutErrors)
	if err != nil {
		return err
	}
	ifx, err := snmp.WalkColumns(ctx, t.Client, oidIfXEntry,
		ifInMulticastPkts, ifInBroadcastPkts, ifOutMulticastPkts, ifOutBroadcastPkts,
		ifHCInOctets, ifHCInUcastPkts, ifHCInMulticastPkts, ifHCInBroadcastPkts,
		ifHCOutOctets, ifHCOutUcastPkts, ifHCOutMulticastPkt, ifHCOutBroadcastPkt, ifHighSpeed)
	if err != nil {
		return err
	}
	tables := [2]snmp.Table{ift, ifx}

	// Rows come from ifTable: ifXTable-only rows (unused port-channels on
	// DGS-1500) have no ifType and carry no traffic of their own.
	for _, idx := range ift.Indexes() {
		ifIndex, err := strconv.ParseInt(idx, 10, 64)
		if err != nil {
			continue
		}
		row, xrow := ift[idx], ifx[idx]
		typ, _ := cellInt(row, ifType)
		if !t.Module.WantInterface(typ) {
			continue
		}
		labels := ports.Get(ifIndex).Labels()

		if v, ok := cellInt(row, ifOperStatus); ok {
			gauge(ch, ifOperUpDesc, boolFloat(v == 1), labels...)
		}
		if v, ok := cellInt(row, ifAdminStatus); ok {
			gauge(ch, ifAdminUpDesc, boolFloat(v == 1), labels...)
		}
		if v, ok := cellFloat(xrow, ifHighSpeed, 1e6/8); ok && v > 0 {
			gauge(ch, ifSpeedDesc, v, labels...)
		} else if v, ok := cellFloat(row, ifSpeed, 1.0/8); ok {
			gauge(ch, ifSpeedDesc, v, labels...)
		}
		if v, ok := cellFloat(row, ifMtu, 1); ok {
			gauge(ch, ifMTUDesc, v, labels...)
		}
		if v, ok := cellFloat(row, ifLastChange, 0.01); ok && v <= uptime {
			ts := now.Add(-time.Duration((uptime - v) * float64(time.Second)))
			gauge(ch, ifLastChangeDesc, float64(ts.Unix()), labels...)
		}
		for _, c := range ifCounters {
			for _, cl := range c.cells {
				if v, ok := cellFloat(tables[cl.table][idx], cl.col, 1); ok {
					counter(ch, c.desc, v, labels...)
					break
				}
			}
		}
	}
	return nil
}
