package collector

import (
	"context"
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

// lldpRemEntry (LLDP-MIB), indexed by timeMark.localPortNum.remIndex.
const oidLldpRemEntry = "1.0.8802.1.1.2.1.4.1.1"

const (
	lldpRemChassisIDSubtype = 4
	lldpRemChassisID        = 5
	lldpRemPortIDSubtype    = 6
	lldpRemPortID           = 7
	lldpRemPortDesc         = 8
	lldpRemSysName          = 9
	lldpRemSysDesc          = 10
)

// Subtypes whose ID is a MAC address.
const (
	chassisIDMAC = 4
	portIDMAC    = 3
)

var lldpNeighborDesc = desc("lldp", "neighbor_info", "Device seen via LLDP on the port; value is always 1.",
	append(portLabels, "chassis_id", "port_id", "port_description", "system_name", "system_description")...)

type lldpCollector struct{}

func (lldpCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	tbl, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidLldpRemEntry,
		lldpRemChassisIDSubtype, lldpRemChassisID, lldpRemPortIDSubtype, lldpRemPortID,
		lldpRemPortDesc, lldpRemSysName, lldpRemSysDesc)
	if err != nil {
		return err
	}
	if len(tbl) == 0 {
		return nil
	}
	ports, err := t.Ports(ctx)
	if err != nil {
		return err
	}
	bridge, err := t.BridgePorts(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, idx := range tbl.Indexes() {
		parts := strings.Split(idx, ".")
		if len(parts) != 3 {
			continue
		}
		localPort, ok := snmp.LastIndex(parts[1])
		if !ok {
			continue
		}
		row := tbl[idx]
		chassisSubtype, _ := cellInt(row, lldpRemChassisIDSubtype)
		portSubtype, _ := cellInt(row, lldpRemPortIDSubtype)
		// D-Link numbers LLDP ports like bridge ports.
		labels := append(ports.Get(bridgePortIfIndex(bridge, localPort)).Labels(),
			lldpID(row[lldpRemChassisID], chassisSubtype == chassisIDMAC),
			lldpID(row[lldpRemPortID], portSubtype == portIDMAC),
			row[lldpRemPortDesc].String(),
			row[lldpRemSysName].String(),
			row[lldpRemSysDesc].String(),
		)
		// The same neighbor can be listed under several timeMarks while the
		// agent ages out an old entry; export it once.
		key := strings.Join(labels, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		gauge(ch, lldpNeighborDesc, 1, labels...)
	}
	return nil
}

// lldpID renders a chassis or port ID: MAC subtypes as aa:bb:..., printable
// text as is, anything else as hex.
func lldpID(p snmp.PDU, isMAC bool) string {
	b := p.Bytes()
	if isMAC && len(b) == 6 {
		return formatMAC(b)
	}
	if s := snmp.CleanString(b); s != "" && strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) < 0 {
		return s
	}
	return hex.EncodeToString(b)
}
