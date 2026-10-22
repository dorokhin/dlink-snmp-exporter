package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

// etherStatsEntry (RMON-MIB). Rows are bound to interfaces by etherStatsDataSource.
// etherStatsOctets (32 bit, wraps in ~34 s at 1 Gbit/s) is not exported: use
// the 64-bit interface byte counters instead.
const (
	oidEtherStatsEntry   = "1.3.6.1.2.1.16.1.1.1"
	etherStatsDataSource = 2
)

var rmonSizeColumns = []struct {
	col  int
	size string
}{
	{14, "64"},
	{15, "65_127"},
	{16, "128_255"},
	{17, "256_511"},
	{18, "512_1023"},
	{19, "1024_1518"},
}

var rmonCounters = []struct {
	col  int
	desc *prometheus.Desc
}{
	{3, desc("rmon", "drop_events_total", "Events in which packets were dropped for lack of resources (etherStatsDropEvents).", portLabels...)},
	{8, desc("rmon", "crc_align_errors_total", "Packets of valid length with a bad FCS or alignment (etherStatsCRCAlignErrors).", portLabels...)},
	{9, desc("rmon", "undersize_packets_total", "Well-formed packets shorter than 64 octets.", portLabels...)},
	{10, desc("rmon", "oversize_packets_total", "Well-formed packets longer than 1518 octets.", portLabels...)},
	{11, desc("rmon", "fragments_total", "Packets shorter than 64 octets with a bad FCS or alignment.", portLabels...)},
	{12, desc("rmon", "jabbers_total", "Packets longer than 1518 octets with a bad FCS or alignment.", portLabels...)},
	{13, desc("rmon", "collisions_total", "Best estimate of collisions on the segment.", portLabels...)},
}

var rmonPacketsDesc = desc("rmon", "packets_total",
	"Packets (including bad ones) by frame size in octets (etherStatsPkts*Octets).", append(portLabels, "size")...)

type rmonCollector struct{}

func (rmonCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	ports, err := t.Ports(ctx)
	if err != nil {
		return err
	}
	cols := []int{etherStatsDataSource}
	for _, c := range rmonCounters {
		cols = append(cols, c.col)
	}
	for _, c := range rmonSizeColumns {
		cols = append(cols, c.col)
	}
	tbl, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidEtherStatsEntry, cols...)
	if err != nil {
		return err
	}
	for _, idx := range tbl.Indexes() {
		row := tbl[idx]
		src, ok := row[etherStatsDataSource]
		if !ok {
			continue
		}
		// etherStatsDataSource is ifIndex.<n>, e.g. 1.3.6.1.2.1.2.2.1.1.5.
		ifIndex, ok := snmp.LastIndex(snmp.Normalize(src.String()))
		if !ok {
			continue
		}
		port := ports.Get(ifIndex)
		if !t.Module.WantInterface(port.Type) {
			continue
		}
		labels := port.Labels()
		for _, c := range rmonCounters {
			if v, ok := cellFloat(row, c.col, 1); ok {
				counter(ch, c.desc, v, labels...)
			}
		}
		for _, c := range rmonSizeColumns {
			if v, ok := cellFloat(row, c.col, 1); ok {
				counter(ch, rmonPacketsDesc, v, append(labels, c.size)...)
			}
		}
	}
	return nil
}
