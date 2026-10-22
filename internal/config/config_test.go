package config

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte(`
auths:
  public_v2:
    community: public
`))
	if err != nil {
		t.Fatal(err)
	}
	m := c.Modules["default"]
	if m == nil {
		t.Fatal("default module not created")
	}
	if !slices.Equal(m.Collectors, DefaultCollectors) || m.Timeout != 5*time.Second || *m.Retries != 1 || m.MaxRepetitions != 25 {
		t.Fatalf("unexpected defaults: %+v", m)
	}
	if slices.Contains(m.Collectors, CollectorFDB) {
		t.Fatal("fdb must not be in the default collector set")
	}
	if m.FDBMaxEntries != DefaultFDBMaxEntries {
		t.Fatalf("fdb_max_entries default %d", m.FDBMaxEntries)
	}
	if !m.WantInterface(6) || !m.WantInterface(161) || m.WantInterface(135) || m.WantInterface(53) {
		t.Fatalf("default interface types %v", m.InterfaceTypes)
	}
	p, err := c.Auths["public_v2"].Params()
	if err != nil || p.Version != gosnmp.Version2c || p.Community != "public" {
		t.Fatalf("params %+v, %v", p, err)
	}
}

func TestInterfaceTypesEmptyMeansAll(t *testing.T) {
	c, err := Parse([]byte(`
auths: {a: {community: x}}
modules:
  all: {interface_types: []}
  lag: {interface_types: [161]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Modules["all"].WantInterface(135) {
		t.Error("interface_types: [] must export every interface")
	}
	if m := c.Modules["lag"]; !m.WantInterface(161) || m.WantInterface(6) {
		t.Errorf("interface_types: [161] gives %v", m.InterfaceTypes)
	}
}

func TestParseV3WithEnv(t *testing.T) {
	t.Setenv("RC_AUTH", "auth$ecret")
	t.Setenv("RC_PRIV", "privsecret")
	c, err := Parse([]byte(`
auths:
  v3:
    version: 3
    username: monitor
    security_level: authPriv
    auth_protocol: SHA256
    auth_password: ${RC_AUTH}
    priv_protocol: AES
    priv_password: ${RC_PRIV}
`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Auths["v3"].Params()
	if err != nil {
		t.Fatal(err)
	}
	if p.AuthPassword != "auth$ecret" || p.PrivPassword != "privsecret" || p.AuthProtocol != gosnmp.SHA256 || p.SecurityLevel != gosnmp.AuthPriv {
		t.Fatalf("unexpected params %+v", p)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no auths":           `modules: {}`,
		"unknown collector":  "auths: {a: {community: x}}\nmodules: {m: {collectors: [cpu]}}",
		"missing community":  "auths: {a: {version: 2}}",
		"bad version":        "auths: {a: {version: 4, community: x}}",
		"v3 no user":         "auths: {a: {version: 3}}",
		"v3 bad auth proto":  "auths: {a: {version: 3, username: u, security_level: authNoPriv, auth_protocol: CRC, auth_password: p}}",
		"unknown field":      "auths: {a: {community: x, comunity: y}}",
		"missing env":        "auths: {a: {community: '${RC_SURELY_UNSET_VAR}'}}",
		"negative fdb limit": "auths: {a: {community: x}}\nmodules: {m: {fdb_max_entries: -1}}",
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSecretNotMarshalled(t *testing.T) {
	v, _ := Secret("hunter2").MarshalYAML()
	if strings.Contains(v.(string), "hunter2") {
		t.Fatal("secret leaked")
	}
}

func TestExampleConfig(t *testing.T) {
	if _, err := Load("../../config.example.yml"); err != nil {
		t.Fatal(err)
	}
}
