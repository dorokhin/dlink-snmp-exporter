package collector

import (
	"context"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

// AGENT-GENERAL-MIB and EQUIPMENT-MIB, shared by managed DES/DGS-3xxx switches.
// Smart switches (DGS-1500) implement neither; the collector then exports nothing.
const (
	// agentCPUutilizationIn{5sec,1min,5min}.0, percent.
	oidAgentCPU = "1.3.6.1.4.1.171.12.1.1.6"
	// agent{DRAM,FLASH}utilizationEntry, indexed by unit: 2 total KB, 3 used KB.
	oidAgentDRAMEntry  = "1.3.6.1.4.1.171.12.1.1.9.1"
	oidAgentFLASHEntry = "1.3.6.1.4.1.171.12.1.1.10.1"
	// swPowerEntry, indexed by unit.psu: 3 status.
	oidSwPowerEntry = "1.3.6.1.4.1.171.12.11.1.6.1"
	// swFanEntry, indexed by unit.fanID: 3 status, 4 position, 5 number, 6 speed (rpm).
	oidSwFanEntry = "1.3.6.1.4.1.171.12.11.1.7.1"
	// swTemperatureEntry, indexed by unit: 2 current, 3 high threshold, 4 low threshold (°C).
	oidSwTemperatureEntry = "1.3.6.1.4.1.171.12.11.1.8.1"
)

var cpuPeriods = map[int64]string{1: "5s", 2: "1m", 3: "5m"}

var fanStates = map[int64]string{
	0: "other", 1: "working", 2: "fail", 3: "speed_0", 4: "speed_low", 5: "speed_middle", 6: "speed_high",
}

var fanPositions = map[int64]string{1: "other", 2: "left", 3: "right", 4: "back", 5: "cpu"}

var powerStates = map[int64]string{
	0: "other", 1: "low_voltage", 2: "over_current", 3: "working", 4: "fail", 5: "connect", 6: "disconnect",
}

var (
	cpuDesc         = desc("cpu", "utilization_ratio", "CPU utilization averaged over period.", "period")
	memTotalDesc    = desc("memory", "total_bytes", "Memory size by type (dram, flash).", "unit", "type")
	memUsedDesc     = desc("memory", "used_bytes", "Memory in use by type (dram, flash).", "unit", "type")
	tempDesc        = desc("", "temperature_celsius", "Shelf temperature.", "unit")
	tempThreshDesc  = desc("", "temperature_threshold_celsius", "Shelf temperature alarm thresholds.", "unit", "type")
	fanStatusDesc   = desc("fan", "status", "Fan status; state set, 1 for the current state (speed_0: idle because it is cool).", "unit", "fan", "position", "status")
	fanFailedDesc   = desc("fan", "failed", "1 when the fan reports a failure.", "unit", "fan", "position")
	fanSpeedDesc    = desc("fan", "speed_rpm", "Fan speed.", "unit", "fan", "position")
	powerStatusDesc = desc("power_supply", "status",
		"Power supply status; state set, 1 for the current state. An absent redundant supply usually reports fail.", "unit", "psu", "status")
)

type healthCollector struct{}

func (healthCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	cpu, err := t.Client.Walk(ctx, oidAgentCPU)
	if err != nil {
		return err
	}
	for _, p := range cpu {
		// agentCPUutilizationIn5sec is ...6.1.0: the period is the second-to-last component.
		period, ok := cpuPeriods[secondLast(p.OID)]
		if !ok {
			continue
		}
		if v, ok := p.Float(); ok {
			gauge(ch, cpuDesc, v/100, period)
		}
	}

	for _, m := range []struct{ oid, typ string }{{oidAgentDRAMEntry, "dram"}, {oidAgentFLASHEntry, "flash"}} {
		tbl, err := snmp.WalkColumnsIfPresent(ctx, t.Client, m.oid, 2, 3)
		if err != nil {
			return err
		}
		for _, unit := range tbl.Indexes() {
			if v, ok := cellFloat(tbl[unit], 2, 1024); ok {
				gauge(ch, memTotalDesc, v, unit, m.typ)
			}
			if v, ok := cellFloat(tbl[unit], 3, 1024); ok {
				gauge(ch, memUsedDesc, v, unit, m.typ)
			}
		}
	}

	temps, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidSwTemperatureEntry, 2, 3, 4)
	if err != nil {
		return err
	}
	for _, unit := range temps.Indexes() {
		row := temps[unit]
		if v, ok := cellFloat(row, 2, 1); ok {
			gauge(ch, tempDesc, v, unit)
		}
		if v, ok := cellFloat(row, 3, 1); ok {
			gauge(ch, tempThreshDesc, v, unit, "high")
		}
		if v, ok := cellFloat(row, 4, 1); ok {
			gauge(ch, tempThreshDesc, v, unit, "low")
		}
	}

	fans, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidSwFanEntry, 3, 4, 5, 6)
	if err != nil {
		return err
	}
	for _, idx := range fans.Indexes() {
		row := fans[idx]
		unit, fanID, _ := strings.Cut(idx, ".")
		// swFanNumber (1, 2, ...) matches "show environment"; swFanID is an internal id.
		fan := fanID
		if n, ok := cellInt(row, 5); ok {
			fan = strconv.FormatInt(n, 10)
		}
		pos, _ := cellInt(row, 4)
		position := fanPositions[pos]
		if position == "" {
			position = "other"
		}
		if v, ok := cellInt(row, 3); ok {
			stateSet(ch, fanStatusDesc, fanStates, v, unit, fan, position)
			gauge(ch, fanFailedDesc, boolFloat(v == 2), unit, fan, position)
		}
		if v, ok := cellFloat(row, 6, 1); ok {
			gauge(ch, fanSpeedDesc, v, unit, fan, position)
		}
	}

	psus, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidSwPowerEntry, 3)
	if err != nil {
		return err
	}
	for _, idx := range psus.Indexes() {
		unit, psu, _ := strings.Cut(idx, ".")
		if v, ok := cellInt(psus[idx], 3); ok {
			stateSet(ch, powerStatusDesc, powerStates, v, unit, psu)
		}
	}
	return nil
}

// secondLast returns the second-to-last OID component, or -1.
func secondLast(oid string) int64 {
	parts := strings.Split(oid, ".")
	if len(parts) < 2 {
		return -1
	}
	v, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	if err != nil {
		return -1
	}
	return v
}
