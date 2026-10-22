package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

const (
	// dot1qTpFdbEntry (Q-BRIDGE-MIB), indexed by fid.<6 MAC octets>: 2 port, 3 status.
	// D-Link uses independent VLAN learning, so the fid is the VLAN ID.
	oidDot1qTpFdbEntry = "1.3.6.1.2.1.17.7.1.2.2.1"
	// dot1dTpFdbEntry (BRIDGE-MIB), indexed by <6 MAC octets>; fallback without VLANs.
	oidDot1dTpFdbEntry = "1.3.6.1.2.1.17.4.3.1"
)

var fdbStates = map[int64]string{1: "other", 2: "invalid", 3: "learned", 4: "self", 5: "mgmt"}

var (
	fdbEntryDesc = desc("fdb", "entry", "MAC address table entry; value is always 1.",
		append(portLabels, "vlan", "mac", "status")...)
	fdbEntriesDesc = desc("fdb", "entries", "Number of MAC address table entries on the port.", portLabels...)
)

type fdbEntry struct {
	vlan, mac, status string
	port              *Port
}

type fdbCollector struct{}

func (fdbCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	ports, err := t.Ports(ctx)
	if err != nil {
		return err
	}
	bridge, err := t.BridgePorts(ctx)
	if err != nil {
		return err
	}
	tbl, err := snmp.WalkColumns(ctx, t.Client, oidDot1qTpFdbEntry, 2, 3)
	if err != nil {
		return err
	}
	withVLAN := len(tbl) > 0
	if !withVLAN {
		if tbl, err = snmp.WalkColumns(ctx, t.Client, oidDot1dTpFdbEntry, 2, 3); err != nil {
			return err
		}
	}

	var entries []fdbEntry
	type portCount struct {
		port *Port
		n    int
	}
	perPort := map[int64]*portCount{} // by ifIndex; the CPU port is 0
	for _, idx := range tbl.Indexes() {
		vlan, mac, ok := splitFDBIndex(idx, withVLAN)
		if !ok {
			continue
		}
		row := tbl[idx]
		status, _ := cellInt(row, 3)
		if status == 2 { // invalid: being aged out
			continue
		}
		port := cpuPort
		if bp, ok := cellInt(row, 2); ok && bp != 0 {
			port = ports.Get(bridgePortIfIndex(bridge, bp))
		}
		name := fdbStates[status]
		if name == "" {
			name = "other"
		}
		entries = append(entries, fdbEntry{vlan: vlan, mac: mac, status: name, port: port})
		pc := perPort[port.IfIndex]
		if pc == nil {
			pc = &portCount{port: port}
			perPort[port.IfIndex] = pc
		}
		pc.n++
	}

	for _, pc := range perPort {
		gauge(ch, fdbEntriesDesc, float64(pc.n), pc.port.Labels()...)
	}
	if len(entries) > t.Module.FDBMaxEntries {
		return fmt.Errorf("MAC table has %d entries, more than fdb_max_entries (%d); entries not exported",
			len(entries), t.Module.FDBMaxEntries)
	}
	for _, e := range entries {
		gauge(ch, fdbEntryDesc, 1, append(e.port.Labels(), e.vlan, e.mac, e.status)...)
	}
	return nil
}

// splitFDBIndex parses "fid.m1.m2.m3.m4.m5.m6" (or the MAC alone without VLANs).
func splitFDBIndex(idx string, withVLAN bool) (vlan, mac string, ok bool) {
	parts := strings.Split(idx, ".")
	want := 6
	if withVLAN {
		want = 7
	}
	if len(parts) != want {
		return "", "", false
	}
	if withVLAN {
		vlan, parts = parts[0], parts[1:]
	}
	b := make([]byte, 6)
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return "", "", false
		}
		b[i] = byte(v)
	}
	return vlan, formatMAC(b), true
}
