package collector

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

// D-Link DDM-MGMT-MIB (1.3.6.1.4.1.171.12.72). Values are DisplayStrings such as
// "47.7396"; ports without a DDM-capable module report "  -". The MIB is not
// published; column meanings were derived from the data and checked against the
// thresholds the module reports.
const (
	// ddmStatusEntry, indexed by port: 2 temperature °C, 3 voltage V, 4 bias mA,
	// 5 Tx power mW, 6 Rx power mW.
	oidDDMStatusEntry = "1.3.6.1.4.1.171.12.72.2.1.1.1"
	// ddmThresholdEntry, indexed by port.param (param = status column - 1):
	// 2 high alarm, 3 low alarm, 4 high warning, 5 low warning.
	oidDDMThresholdEntry = "1.3.6.1.4.1.171.12.72.3.1.1.1"
)

// minDBm stands in for zero optical power (no light), which has no dBm value.
const minDBm = -40

type ddmParam struct {
	name    string // also the "param" label of thresholds
	convert func(float64) float64
	desc    *prometheus.Desc
}

// ddmParams is keyed by parameter number (1..5); the status column is param+1.
var ddmParams = map[int]ddmParam{
	1: {"temperature_celsius", identity, desc("sfp", "temperature_celsius", "Module temperature.", portLabels...)},
	2: {"voltage_volts", identity, desc("sfp", "voltage_volts", "Module supply voltage.", portLabels...)},
	3: {"bias_current_amperes", milli, desc("sfp", "bias_current_amperes", "Laser bias current.", portLabels...)},
	4: {"tx_power_dbm", mwToDBm, desc("sfp", "tx_power_dbm", "Transmit optical power.", portLabels...)},
	5: {"rx_power_dbm", mwToDBm, desc("sfp", "rx_power_dbm", "Receive optical power.", portLabels...)},
}

var ddmLevels = map[int]string{2: "high_alarm", 3: "low_alarm", 4: "high_warning", 5: "low_warning"}

var sfpThresholdDesc = desc("sfp", "threshold", "DDM alarm/warning threshold, in the unit of the metric named by param.",
	append(portLabels, "param", "level")...)

func identity(v float64) float64 { return v }
func milli(v float64) float64    { return v / 1000 }

func mwToDBm(mw float64) float64 {
	if mw <= 0 {
		return minDBm
	}
	return math.Max(10*math.Log10(mw), minDBm)
}

// parseDDM parses a DDM DisplayString; "-" and empty strings mean no reading.
func parseDDM(p snmp.PDU) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(p.String()), 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}

type ddmCollector struct{}

func (ddmCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	status, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidDDMStatusEntry, 2, 3, 4, 5, 6)
	if err != nil {
		return err
	}
	if len(status) == 0 {
		return nil // no DDM support
	}
	ports, err := t.Ports(ctx)
	if err != nil {
		return err
	}
	// Thresholds are only meaningful for ports that report readings.
	withDDM := map[string]bool{}
	for _, idx := range status.Indexes() {
		port, ok := ddmPort(ports, idx)
		if !ok {
			continue
		}
		row := status[idx]
		for param := 1; param <= 5; param++ {
			pd := ddmParams[param]
			if v, ok := parseDDM(row[param+1]); ok {
				gauge(ch, pd.desc, pd.convert(v), port.Labels()...)
				withDDM[idx] = true
			}
		}
	}
	if len(withDDM) == 0 {
		return nil
	}

	thr, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidDDMThresholdEntry, 2, 3, 4, 5)
	if err != nil {
		return err
	}
	for _, idx := range thr.Indexes() {
		portIdx, paramStr, ok := strings.Cut(idx, ".")
		if !ok || !withDDM[portIdx] {
			continue
		}
		param, _ := strconv.Atoi(paramStr)
		pd, ok := ddmParams[param]
		if !ok {
			continue
		}
		port, _ := ddmPort(ports, portIdx)
		for col, level := range ddmLevels {
			if v, ok := parseDDM(thr[idx][col]); ok {
				gauge(ch, sfpThresholdDesc, pd.convert(v), append(port.Labels(), pd.name, level)...)
			}
		}
	}
	return nil
}

// ddmPort resolves a DDM port index. On standalone switches it equals ifIndex.
func ddmPort(ports *PortMap, idx string) (*Port, bool) {
	n, err := strconv.ParseInt(idx, 10, 64)
	if err != nil {
		return nil, false
	}
	return ports.Get(n), true
}
