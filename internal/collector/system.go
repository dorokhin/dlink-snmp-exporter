package collector

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"dlink-snmp-exporter/internal/snmp"
)

const (
	// dot1dBaseBridgeAddress: the switch MAC on agents without the unit table.
	oidDot1dBaseBridgeAddress = "1.3.6.1.2.1.17.1.1.0"

	// AGENT-GENERAL-MIB (agentBasicInfo).
	oidAgentSerialNumber = "1.3.6.1.4.1.171.12.1.1.12.0"
	oidAgentFirmwareType = "1.3.6.1.4.1.171.12.1.1.13.0"

	// EQUIPMENT-MIB swUnitMgmtEntry, indexed by stack unit.
	oidUnitEntry = "1.3.6.1.4.1.171.12.11.1.9.4.1"
)

// swUnitMgmtEntry columns.
const (
	unitMAC      = 2
	unitModel    = 9
	unitBootPROM = 10
	unitFirmware = 11
	unitHardware = 12
	unitSerial   = 17
)

var (
	sysInfoDesc = desc("", "device_info", "Device identification; value is always 1.",
		"model", "serial", "hardware", "firmware", "bootrom", "firmware_type", "mac", "sysname", "sysobjectid")
	sysUptimeDesc = desc("", "uptime_seconds", "Time since the agent (re)started (sysUpTime).")
)

type systemCollector struct{}

func (systemCollector) Collect(ctx context.Context, t *Target, ch chan<- prometheus.Metric) error {
	objectID, err := t.ObjectID(ctx)
	if err != nil {
		return err
	}
	q := quirksFor(objectID)
	oids := []string{oidSysDescr, oidSysUpTime, oidSysName, oidDot1dBaseBridgeAddress, oidAgentSerialNumber, oidAgentFirmwareType}
	if q.HardwareSuffix != "" {
		oids = append(oids, objectID+"."+q.HardwareSuffix, objectID+"."+q.FirmwareSuffix)
	}
	res, err := t.Client.Get(ctx, oids...)
	if err != nil {
		return err
	}
	if len(res) == 0 {
		return fmt.Errorf("agent returned none of the system objects")
	}
	str := func(oid string) string { return res[oid].String() }

	info := deviceInfo{
		serial:       str(oidAgentSerialNumber),
		firmwareType: str(oidAgentFirmwareType),
		mac:          formatMAC(res[oidDot1dBaseBridgeAddress].Bytes()),
	}
	units, err := snmp.WalkColumnsIfPresent(ctx, t.Client, oidUnitEntry, unitMAC, unitModel, unitBootPROM, unitFirmware, unitHardware, unitSerial)
	if err != nil {
		return err
	}
	if idx := units.Indexes(); len(idx) > 0 {
		// A stack is identified by its lowest unit.
		row := units[idx[0]]
		info.model = row[unitModel].String()
		info.bootrom = row[unitBootPROM].String()
		info.firmware = row[unitFirmware].String()
		info.hardware = row[unitHardware].String()
		info.serial = firstNonEmpty(row[unitSerial].String(), info.serial)
		info.mac = firstNonEmpty(formatMAC(row[unitMAC].Bytes()), info.mac)
	}
	if q.HardwareSuffix != "" {
		info.hardware = firstNonEmpty(info.hardware, str(objectID+"."+q.HardwareSuffix))
		info.firmware = firstNonEmpty(info.firmware, str(objectID+"."+q.FirmwareSuffix))
	}
	// sysDescr: "DGS-3120-24TC Gigabit Ethernet Switch", "DGS-1500-28          2.51.B022".
	if f := strings.Fields(str(oidSysDescr)); len(f) > 0 {
		info.model = firstNonEmpty(info.model, f[0])
		if len(f) == 2 {
			info.firmware = firstNonEmpty(info.firmware, f[1])
		}
	}

	gauge(ch, sysInfoDesc, 1, info.model, info.serial, info.hardware, info.firmware, info.bootrom,
		info.firmwareType, info.mac, str(oidSysName), objectID)
	if p, ok := res[oidSysUpTime]; ok {
		v, _ := p.Float()
		v /= 100
		t.uptime = &v
		gauge(ch, sysUptimeDesc, v)
	}
	return nil
}

type deviceInfo struct {
	model, serial, hardware, firmware, bootrom, firmwareType, mac string
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func formatMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	return strings.ToLower(net.HardwareAddr(b).String())
}
