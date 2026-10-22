package collector

import "strings"

// Quirks are per-family firmware workarounds, selected by sysObjectID. Anything
// a switch simply does not implement is detected from empty walks instead.
type Quirks struct {
	// SkipSQE drops dot3StatsSQETestErrors. DGS-1500-28 2.51.B022 returns the
	// same meaningless value (~1.1e9) on every port.
	SkipSQE bool
	// HardwareSuffix and FirmwareSuffix locate hardware/firmware version
	// scalars below sysObjectID, for smart switches without the stack unit
	// table of EQUIPMENT-MIB.
	HardwareSuffix string
	FirmwareSuffix string
}

var quirkTable = []struct {
	prefix string
	q      Quirks
}{
	// DGS-1500 family (1.3.6.1.4.1.171.10.126.<model>.1): private MIB rooted at sysObjectID.
	{"1.3.6.1.4.1.171.10.126.", Quirks{SkipSQE: true, HardwareSuffix: "1.2.0", FirmwareSuffix: "1.3.0"}},
}

func quirksFor(objectID string) Quirks {
	for _, e := range quirkTable {
		if strings.HasPrefix(objectID, e.prefix) {
			return e.q
		}
	}
	return Quirks{}
}
