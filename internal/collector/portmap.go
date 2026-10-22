package collector

import (
	"context"
	"strconv"
	"strings"

	"dlink-snmp-exporter/internal/snmp"
)

const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidIfDescr     = "1.3.6.1.2.1.2.2.1.2"
	oidIfType      = "1.3.6.1.2.1.2.2.1.3"
	oidIfName      = "1.3.6.1.2.1.31.1.1.1.1"
	oidIfAlias     = "1.3.6.1.2.1.31.1.1.1.18"

	// dot1dBasePortIfIndex: bridge port number → ifIndex.
	oidDot1dBasePortIfIndex = "1.3.6.1.2.1.17.1.4.1.2"
)

// portLabels are attached to every per-port metric.
var portLabels = []string{"ifindex", "port", "ifname", "description"}

// Port describes one interface.
type Port struct {
	IfIndex     int64
	Type        int64  // ifType, 0 when unknown
	Name        string // ifName: "1/24" (DGS-3xxx), "Slot0/24" (DGS-1500)
	Number      string // port as the D-Link CLI shows it: "1:24", "24"
	Description string // ifAlias
}

// Labels returns values for portLabels.
func (p *Port) Labels() []string {
	return []string{strconv.FormatInt(p.IfIndex, 10), p.Number, p.Name, p.Description}
}

// cpuPort stands for FDB entries that belong to the switch itself (bridge port 0).
var cpuPort = &Port{Number: "cpu"}

// PortMap resolves ifIndex values to ports.
type PortMap struct {
	byIndex map[int64]*Port
}

// Get returns the port for ifIndex; unknown indexes get a placeholder so
// metrics are still emitted.
func (m *PortMap) Get(ifIndex int64) *Port {
	if p, ok := m.byIndex[ifIndex]; ok {
		return p
	}
	return &Port{IfIndex: ifIndex, Number: strconv.FormatInt(ifIndex, 10)}
}

// Known reports whether ifIndex is listed in ifTable/ifXTable.
func (m *PortMap) Known(ifIndex int64) bool {
	_, ok := m.byIndex[ifIndex]
	return ok
}

// Len is the number of known interfaces.
func (m *PortMap) Len() int { return len(m.byIndex) }

func loadPortMap(ctx context.Context, c snmp.Client) (*PortMap, error) {
	m := &PortMap{byIndex: map[int64]*Port{}}
	port := func(idx int64) *Port {
		p := m.byIndex[idx]
		if p == nil {
			p = &Port{IfIndex: idx}
			m.byIndex[idx] = p
		}
		return p
	}
	for _, root := range []string{oidIfDescr, oidIfName} {
		pdus, err := c.Walk(ctx, root)
		if err != nil {
			return nil, err
		}
		for _, pdu := range pdus {
			if idx, ok := snmp.LastIndex(pdu.OID); ok {
				// ifName wins over ifDescr ("D-Link DGS-3120-24TC R3.12.R020 Port 1 on Unit 1").
				if name := pdu.String(); name != "" {
					port(idx).Name = name
				}
			}
		}
	}
	types, err := c.Walk(ctx, oidIfType)
	if err != nil {
		return nil, err
	}
	for _, pdu := range types {
		if idx, ok := snmp.LastIndex(pdu.OID); ok {
			port(idx).Type, _ = pdu.Int64()
		}
	}
	aliases, err := c.Walk(ctx, oidIfAlias)
	if err != nil {
		return nil, err
	}
	for _, pdu := range aliases {
		if idx, ok := snmp.LastIndex(pdu.OID); ok {
			if p, ok := m.byIndex[idx]; ok {
				p.Description = pdu.String()
			}
		}
	}
	for _, p := range m.byIndex {
		p.Number = portNumber(p.Name, p.IfIndex)
	}
	return m, nil
}

// portNumber renders a port the way the D-Link CLI does: "1/24" → "1:24"
// (unit:port on DGS-3xxx), "Slot0/24" → "24" (DGS-1500); other names (po1,
// vlan1) are kept, and an empty name falls back to the ifIndex.
func portNumber(name string, ifIndex int64) string {
	if name == "" {
		return strconv.FormatInt(ifIndex, 10)
	}
	head, tail, ok := strings.Cut(name, "/")
	if !ok || !isDigits(tail) {
		return name
	}
	if isDigits(head) {
		return head + ":" + tail
	}
	if strings.HasPrefix(head, "Slot") {
		return tail
	}
	return name
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func loadBridgePorts(ctx context.Context, c snmp.Client) (map[int64]int64, error) {
	pdus, err := c.Walk(ctx, oidDot1dBasePortIfIndex)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(pdus))
	for _, p := range pdus {
		bp, ok := snmp.LastIndex(p.OID)
		if !ok {
			continue
		}
		if ifIndex, ok := p.Int64(); ok {
			out[bp] = ifIndex
		}
	}
	return out, nil
}

// bridgePortIfIndex resolves a bridge port; agents without dot1dBasePortTable
// number bridge ports like ifIndex.
func bridgePortIfIndex(bridge map[int64]int64, bp int64) int64 {
	if ifIndex, ok := bridge[bp]; ok {
		return ifIndex
	}
	return bp
}
