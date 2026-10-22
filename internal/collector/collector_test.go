package collector

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"dlink-snmp-exporter/internal/config"
	"dlink-snmp-exporter/internal/snmp"
	"dlink-snmp-exporter/internal/snmp/snmptest"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Fixtures recorded from real switches (see testdata/mkfixture.py). Neither had
// LLDP neighbors, so the DGS-3120 fixture is combined with synthetic ones.
var models = map[string][]string{
	"dgs3120-24tc": {"../../testdata/dgs3120-24tc.walk", "../../testdata/lldp-synthetic.walk"},
	"dgs1500-28":   {"../../testdata/dgs1500-28.walk"},
}

// fixedNow pins timestamps derived from sysUpTime.
var fixedNow = func() time.Time { return time.Unix(1_790_000_000, 0) }

// module returns a validated module with every collector enabled.
func module(t *testing.T, extra string) *config.Module {
	t.Helper()
	c, err := config.Parse([]byte("auths: {a: {community: x}}\nmodules:\n  m: {collectors: [" +
		strings.Join(config.AllCollectors, ", ") + "]" + extra + "}\n"))
	if err != nil {
		t.Fatal(err)
	}
	return c.Modules["m"]
}

func newTarget(t *testing.T, model string, mod *config.Module, files ...string) *Target {
	t.Helper()
	if files == nil {
		files = models[model]
	}
	fx, err := snmptest.Load(files...)
	if err != nil {
		t.Fatal(err)
	}
	if mod == nil {
		mod = module(t, "")
	}
	return &Target{Client: fx, Module: mod, Logger: slog.New(slog.DiscardHandler), Now: fixedNow}
}

// single exposes one collector as a prometheus.Collector.
type single struct {
	c   Collector
	t   *Target
	err error
}

func (s *single) Describe(chan<- *prometheus.Desc) {}
func (s *single) Collect(ch chan<- prometheus.Metric) {
	s.err = s.c.Collect(context.Background(), s.t, ch)
}

func gatherErr(t *testing.T, name string, tgt *Target) ([]*dto.MetricFamily, error) {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	s := &single{c: registry[name], t: tgt}
	reg.MustRegister(s)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather %s: %v", name, err)
	}
	return mfs, s.err
}

func gather(t *testing.T, name string, tgt *Target) []*dto.MetricFamily {
	t.Helper()
	mfs, err := gatherErr(t, name, tgt)
	if err != nil {
		t.Fatalf("collect %s: %v", name, err)
	}
	return mfs
}

func TestGolden(t *testing.T) {
	for model := range models {
		for _, name := range config.AllCollectors {
			t.Run(model+"/"+name, func(t *testing.T) {
				var buf bytes.Buffer
				enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
				for _, mf := range gather(t, name, newTarget(t, model, nil)) {
					if err := enc.Encode(mf); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join("testdata", model, name+".prom")
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run go test -update)", err)
				}
				if !bytes.Equal(buf.Bytes(), want) {
					t.Errorf("output differs from %s; run `go test ./internal/collector -update` and review the diff", path)
				}
			})
		}
	}
}

// value finds a sample by metric name and label subset.
func value(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	metrics:
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, l := range m.GetLabel() {
				got[l.GetName()] = l.GetValue()
			}
			for k, v := range labels {
				if got[k] != v {
					continue metrics
				}
			}
			switch {
			case m.Gauge != nil:
				return m.Gauge.GetValue()
			case m.Counter != nil:
				return m.Counter.GetValue()
			}
		}
	}
	t.Fatalf("no sample %s%v", name, labels)
	return 0
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// count returns the number of samples of name matching the label subset.
func count(mfs []*dto.MetricFamily, name string, labels map[string]string) int {
	n := 0
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	metrics:
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, l := range m.GetLabel() {
				got[l.GetName()] = l.GetValue()
			}
			for k, v := range labels {
				if got[k] != v {
					continue metrics
				}
			}
			n++
		}
	}
	return n
}

func TestSystemValues(t *testing.T) {
	mfs := gather(t, config.CollectorSystem, newTarget(t, "dgs3120-24tc", nil))
	near(t, "3120 info", value(t, mfs, "dlink_device_info", map[string]string{
		"model": "DGS-3120-24TC", "serial": "SN0000000001", "hardware": "A1", "firmware": "3.12.R020",
		"bootrom": "2.00.003", "firmware_type": "EI", "mac": "02:00:00:00:00:1b", "sysname": "switch-01",
		"sysobjectid": "1.3.6.1.4.1.171.10.117.1.1",
	}), 1)

	// DGS-1500: no AGENT/EQUIPMENT MIB, details come from sysDescr and the private branch.
	mfs = gather(t, config.CollectorSystem, newTarget(t, "dgs1500-28", nil))
	near(t, "1500 info", value(t, mfs, "dlink_device_info", map[string]string{
		"model": "DGS-1500-28", "serial": "", "hardware": "A1", "firmware": "2.51.B022",
		"mac": "02:00:00:00:00:01", "sysobjectid": "1.3.6.1.4.1.171.10.126.2.1",
	}), 1)
	if v := value(t, mfs, "dlink_uptime_seconds", nil); v < 100*86400 {
		t.Errorf("uptime %v", v)
	}
}

func TestPortNumber(t *testing.T) {
	cases := map[string]string{
		"1/24": "1:24", "2/3": "2:3", "Slot0/24": "24", "po1": "po1",
		"802.1Q Encapsulation Tag 0001": "802.1Q Encapsulation Tag 0001", "": "7",
	}
	for name, want := range cases {
		if got := portNumber(name, 7); got != want {
			t.Errorf("portNumber(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestInterfaceFilter(t *testing.T) {
	mfs := gather(t, config.CollectorInterfaces, newTarget(t, "dgs3120-24tc", nil))
	if n := count(mfs, "dlink_interface_oper_up", nil); n != 26 {
		t.Errorf("DGS-3120: %d interfaces, want 26 ports (VLAN and System interfaces filtered)", n)
	}
	if count(mfs, "dlink_interface_oper_up", map[string]string{"ifindex": "5121"}) != 0 {
		t.Error("System interface not filtered")
	}
	near(t, "port label", value(t, mfs, "dlink_interface_mtu_bytes", map[string]string{"ifindex": "24", "port": "1:24", "ifname": "1/24"}), 1500)

	all := gather(t, config.CollectorInterfaces, newTarget(t, "dgs3120-24tc", module(t, ", interface_types: []")))
	if n := count(all, "dlink_interface_oper_up", nil); n != 30 {
		t.Errorf("interface_types: [] gave %d interfaces, want 30", n)
	}

	mfs = gather(t, config.CollectorInterfaces, newTarget(t, "dgs1500-28", nil))
	if n := count(mfs, "dlink_interface_oper_up", nil); n != 28 {
		t.Errorf("DGS-1500: %d interfaces, want 28", n)
	}
	near(t, "1500 port label", value(t, mfs, "dlink_interface_admin_up", map[string]string{"port": "27", "ifname": "Slot0/27"}), 1)
	if v := value(t, mfs, "dlink_interface_receive_bytes_total", map[string]string{"port": "27"}); v < 1e9 {
		t.Errorf("uplink rx bytes %v: expected the 64-bit counter", v)
	}
}

func TestEtherLike(t *testing.T) {
	mfs := gather(t, config.CollectorEtherLike, newTarget(t, "dgs1500-28", nil))
	if count(mfs, "dlink_interface_ethernet_errors_total", map[string]string{"type": "sqe_test"}) != 0 {
		t.Error("DGS-1500 must not export the broken SQE counter")
	}
	if count(mfs, "dlink_interface_ethernet_errors_total", map[string]string{"type": "fcs"}) != 28 {
		t.Error("expected FCS errors for every port")
	}
	near(t, "duplex full", value(t, mfs, "dlink_interface_duplex", map[string]string{"port": "27", "duplex": "full"}), 1)
	near(t, "duplex half", value(t, mfs, "dlink_interface_duplex", map[string]string{"port": "27", "duplex": "half"}), 0)

	mfs = gather(t, config.CollectorEtherLike, newTarget(t, "dgs3120-24tc", nil))
	if count(mfs, "dlink_interface_ethernet_errors_total", map[string]string{"type": "sqe_test"}) != 26 {
		t.Error("DGS-3120 should export SQE errors")
	}
}

func TestRMON(t *testing.T) {
	mfs := gather(t, config.CollectorRMON, newTarget(t, "dgs3120-24tc", nil))
	near(t, "64-byte packets", value(t, mfs, "dlink_rmon_packets_total", map[string]string{"port": "1:1", "size": "64"}), 370633130)
	if n := count(mfs, "dlink_rmon_packets_total", map[string]string{"size": "1024_1518"}); n != 26 {
		t.Errorf("%d ports with RMON stats, want 26", n)
	}
	mfs = gather(t, config.CollectorRMON, newTarget(t, "dgs1500-28", nil))
	if len(mfs) != 0 {
		t.Errorf("DGS-1500 has no RMON, got %d families", len(mfs))
	}
}

func TestHealth(t *testing.T) {
	mfs := gather(t, config.CollectorHealth, newTarget(t, "dgs3120-24tc", nil))
	near(t, "cpu 5s", value(t, mfs, "dlink_cpu_utilization_ratio", map[string]string{"period": "5s"}), 0.19)
	near(t, "cpu 5m", value(t, mfs, "dlink_cpu_utilization_ratio", map[string]string{"period": "5m"}), 0.26)
	near(t, "dram", value(t, mfs, "dlink_memory_total_bytes", map[string]string{"unit": "1", "type": "dram"}), 131072*1024)
	near(t, "flash used", value(t, mfs, "dlink_memory_used_bytes", map[string]string{"type": "flash"}), 13668*1024)
	near(t, "temperature", value(t, mfs, "dlink_temperature_celsius", map[string]string{"unit": "1"}), 31)
	near(t, "temp high", value(t, mfs, "dlink_temperature_threshold_celsius", map[string]string{"type": "high"}), 79)
	near(t, "temp low", value(t, mfs, "dlink_temperature_threshold_celsius", map[string]string{"type": "low"}), 11)
	fan2 := map[string]string{"unit": "1", "fan": "2", "position": "right"}
	near(t, "fan speed", value(t, mfs, "dlink_fan_speed_rpm", fan2), 3000)
	near(t, "fan failed", value(t, mfs, "dlink_fan_failed", fan2), 0)
	near(t, "fan speed_low", value(t, mfs, "dlink_fan_status", map[string]string{"fan": "2", "status": "speed_low"}), 1)
	near(t, "fan working", value(t, mfs, "dlink_fan_status", map[string]string{"fan": "2", "status": "working"}), 0)
	near(t, "psu1", value(t, mfs, "dlink_power_supply_status", map[string]string{"psu": "1", "status": "working"}), 1)
	near(t, "psu2 (no RPS)", value(t, mfs, "dlink_power_supply_status", map[string]string{"psu": "2", "status": "fail"}), 1)

	mfs = gather(t, config.CollectorHealth, newTarget(t, "dgs1500-28", nil))
	if len(mfs) != 0 {
		t.Errorf("DGS-1500 has no health MIBs, got %d families", len(mfs))
	}
}

func TestDDM(t *testing.T) {
	mfs := gather(t, config.CollectorDDM, newTarget(t, "dgs3120-24tc", nil))
	p22 := map[string]string{"port": "1:22", "ifindex": "22"}
	near(t, "temperature", value(t, mfs, "dlink_sfp_temperature_celsius", p22), 47.7396)
	near(t, "voltage", value(t, mfs, "dlink_sfp_voltage_volts", p22), 3.2744)
	near(t, "bias", value(t, mfs, "dlink_sfp_bias_current_amperes", p22), 0.012272)
	near(t, "tx", value(t, mfs, "dlink_sfp_tx_power_dbm", p22), 10*math.Log10(0.3802))
	near(t, "rx", value(t, mfs, "dlink_sfp_rx_power_dbm", p22), 10*math.Log10(0.1615))
	near(t, "rx low alarm", value(t, mfs, "dlink_sfp_threshold",
		map[string]string{"port": "1:22", "param": "rx_power_dbm", "level": "low_alarm"}), 10*math.Log10(0.0079))
	near(t, "temp high warning", value(t, mfs, "dlink_sfp_threshold",
		map[string]string{"port": "1:22", "param": "temperature_celsius", "level": "high_warning"}), 75)
	near(t, "bias high alarm", value(t, mfs, "dlink_sfp_threshold",
		map[string]string{"port": "1:22", "param": "bias_current_amperes", "level": "high_alarm"}), 0.09)
	if n := count(mfs, "dlink_sfp_temperature_celsius", nil); n != 1 {
		t.Errorf("%d ports with DDM, want only port 22", n)
	}
	if n := count(mfs, "dlink_sfp_threshold", nil); n != 20 {
		t.Errorf("%d thresholds, want 5 params x 4 levels", n)
	}

	mfs = gather(t, config.CollectorDDM, newTarget(t, "dgs1500-28", nil))
	if len(mfs) != 0 {
		t.Errorf("DGS-1500 has no DDM, got %d families", len(mfs))
	}
}

func TestParseDDM(t *testing.T) {
	str := func(s string) snmp.PDU { return snmp.PDU{Value: []byte(s)} }
	for in, want := range map[string]float64{"47.7396": 47.7396, "-10.0000": -10, " 0.0000": 0} {
		if v, ok := parseDDM(str(in)); !ok || v != want {
			t.Errorf("parseDDM(%q) = %v, %v", in, v, ok)
		}
	}
	for _, in := range []string{"  -", "", "N/A", "NaN"} {
		if _, ok := parseDDM(str(in)); ok {
			t.Errorf("parseDDM(%q) should fail", in)
		}
	}
	near(t, "0 mW", mwToDBm(0), minDBm)
	near(t, "1 mW", mwToDBm(1), 0)
	near(t, "tiny", mwToDBm(1e-9), minDBm)
}

func TestFDB(t *testing.T) {
	mfs := gather(t, config.CollectorFDB, newTarget(t, "dgs3120-24tc", nil))
	if n := count(mfs, "dlink_fdb_entry", nil); n != 190 {
		t.Errorf("%d FDB entries, want 190", n)
	}
	near(t, "uplink count", value(t, mfs, "dlink_fdb_entries", map[string]string{"port": "1:22"}), 105)
	near(t, "cpu entry", value(t, mfs, "dlink_fdb_entry", map[string]string{"port": "cpu", "status": "self", "vlan": "854"}), 1)
	if n := count(mfs, "dlink_fdb_entry", map[string]string{"port": "1:22", "vlan": "854", "status": "learned"}); n == 0 {
		t.Error("no learned entries in VLAN 854 on the uplink")
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "mac" && len(l.GetValue()) != 17 {
					t.Errorf("bad mac label %q", l.GetValue())
				}
			}
		}
	}

	mfs = gather(t, config.CollectorFDB, newTarget(t, "dgs1500-28", nil))
	near(t, "1500 uplink count", value(t, mfs, "dlink_fdb_entries", map[string]string{"port": "27", "ifname": "Slot0/27"}), 123)
}

func TestFDBLimit(t *testing.T) {
	mfs, err := gatherErr(t, config.CollectorFDB, newTarget(t, "dgs1500-28", module(t, ", fdb_max_entries: 100")))
	if err == nil {
		t.Fatal("expected an error above fdb_max_entries")
	}
	if count(mfs, "dlink_fdb_entry", nil) != 0 {
		t.Error("entries exported despite the limit")
	}
	near(t, "counts still exported", value(t, mfs, "dlink_fdb_entries", map[string]string{"port": "27"}), 123)
}

func TestFDBWithoutQBridge(t *testing.T) {
	// Keep only dot1dTpFdbTable and the port tables from the DGS-3120 dump.
	src, err := os.ReadFile(models["dgs3120-24tc"][0])
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(string(src), "\n") {
		if !strings.HasPrefix(line, ".1.3.6.1.2.1.17.7.") {
			out = append(out, line)
		}
	}
	path := filepath.Join(t.TempDir(), "dot1d.walk")
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	mfs := gather(t, config.CollectorFDB, newTarget(t, "", nil, path))
	if n := count(mfs, "dlink_fdb_entry", map[string]string{"vlan": "", "port": "1:22", "status": "learned"}); n != 2 {
		t.Errorf("dot1d fallback gave %d entries, want 2", n)
	}
}

func TestLLDP(t *testing.T) {
	mfs := gather(t, config.CollectorLLDP, newTarget(t, "dgs3120-24tc", nil))
	if n := count(mfs, "dlink_lldp_neighbor_info", nil); n != 2 {
		t.Errorf("%d neighbors, want 2 (an entry repeated under another timeMark is exported once)", n)
	}
	near(t, "switch neighbor", value(t, mfs, "dlink_lldp_neighbor_info", map[string]string{
		"port": "1:22", "chassis_id": "02:00:00:00:10:01", "port_id": "1/26", "system_name": "core-sw",
	}), 1)
	near(t, "ap neighbor", value(t, mfs, "dlink_lldp_neighbor_info", map[string]string{
		"port": "1:24", "chassis_id": "ap-lobby", "port_id": "02:00:00:00:10:02", "port_description": "eth0",
	}), 1)

	mfs = gather(t, config.CollectorLLDP, newTarget(t, "dgs1500-28", nil))
	if len(mfs) != 0 {
		t.Error("DGS-1500 fixture has no LLDP neighbors")
	}
}

func TestProbeUnsupportedCollectorsSucceed(t *testing.T) {
	p := &Probe{Ctx: context.Background(), Target: newTarget(t, "dgs1500-28", nil)}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(p)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !p.Success {
		t.Error("probe of a switch without private MIBs must succeed")
	}
	for _, name := range config.AllCollectors {
		near(t, name+" success", value(t, mfs, "dlink_collector_success", map[string]string{"collector": name}), 1)
	}
}

// timeoutClient fails every request like an unreachable agent.
type timeoutClient struct{ calls int }

func (c *timeoutClient) Get(context.Context, ...string) (map[string]snmp.PDU, error) {
	c.calls++
	return nil, errors.New("request timeout (after 1 retries)")
}

func (c *timeoutClient) Walk(context.Context, string) ([]snmp.PDU, error) {
	c.calls++
	return nil, errors.New("request timeout (after 1 retries)")
}

func TestProbeAbortsAfterTimeout(t *testing.T) {
	cl := &timeoutClient{}
	p := &Probe{Ctx: context.Background(), Target: &Target{
		Client: cl, Module: &config.Module{Collectors: config.AllCollectors}, Logger: slog.New(slog.DiscardHandler),
	}}
	reg := prometheus.NewRegistry()
	reg.MustRegister(p)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if p.Success {
		t.Error("probe reported success")
	}
	if cl.calls != 1 {
		t.Errorf("agent contacted %d times, want 1 (remaining collectors must be skipped)", cl.calls)
	}
	for _, name := range config.AllCollectors {
		near(t, name+" success", value(t, mfs, "dlink_collector_success", map[string]string{"collector": name}), 0)
	}
}
