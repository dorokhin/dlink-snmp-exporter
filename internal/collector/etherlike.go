package collector

import (
	"context"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

// dot3StatsEntry (EtherLike-MIB), indexed by ifIndex.
const oidDot3StatsEntry = "1.3.6.1.2.1.10.7.2.1"

const (
	dot3StatsSQETestErrors = 6
	dot3StatsDuplexStatus  = 19
)

// dot3ErrorColumns maps dot3StatsTable columns to the "type" label.
var dot3ErrorColumns = []struct {
	col  int
	name string
}{
	{2, "alignment"},
	{3, "fcs"},
	{4, "single_collision"},
	{5, "multiple_collision"},
	{dot3StatsSQETestErrors, "sqe_test"},
	{7, "deferred"},
	{8, "late_collision"},
	{9, "excessive_collision"},
	{10, "internal_mac_transmit"},
	{11, "carrier_sense"},
	{13, "frame_too_long"},
	{16, "internal_mac_receive"},
	{18, "symbol"},
}

var duplexStates = map[int64]string{1: "unknown", 2: "half", 3: "full"}

var (
	ethErrorsDesc = desc("interface", "ethernet_errors_total",
		"Ethernet-level errors from dot3StatsTable, by type.", append(portLabels, "type")...)
	ethDuplexDesc = desc("interface", "duplex",
		"Duplex mode (dot3StatsDuplexStatus); state set, 1 for the current mode.", append(portLabels, "duplex")...)
)

type etherLikeCollector struct{}

func (etherLikeCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	ports, err := t.Ports(ctx)
	if err != nil {
		return err
	}
	q, err := t.Quirks(ctx)
	if err != nil {
		return err
	}
	cols := []int{dot3StatsDuplexStatus}
	for _, c := range dot3ErrorColumns {
		if c.col == dot3StatsSQETestErrors && q.SkipSQE {
			continue
		}
		cols = append(cols, c.col)
	}
	tbl, err := snmp.WalkColumns(ctx, t.Client, oidDot3StatsEntry, cols...)
	if err != nil {
		return err
	}
	for _, idx := range tbl.Indexes() {
		ifIndex, err := strconv.ParseInt(idx, 10, 64)
		if err != nil {
			continue
		}
		port := ports.Get(ifIndex)
		if !t.Module.WantInterface(port.Type) {
			continue
		}
		labels := port.Labels()
		row := tbl[idx]
		for _, c := range dot3ErrorColumns {
			if v, ok := cellFloat(row, c.col, 1); ok {
				counter(ch, ethErrorsDesc, v, append(labels, c.name)...)
			}
		}
		if v, ok := cellInt(row, dot3StatsDuplexStatus); ok {
			stateSet(ch, ethDuplexDesc, duplexStates, v, labels...)
		}
	}
	return nil
}
