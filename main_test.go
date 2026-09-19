package main

import (
	"strings"
	"testing"
)

func TestFormatAcceptRule(t *testing.T) {
	tests := []struct {
		name   string
		listen *Listen
		want   string
	}{
		{
			name:   "icmp does not have a destination port",
			listen: &Listen{Name: "icmp", Type: "icmp"},
			want:   `  iptables -A INPUT -s 192.0.2.1 -p icmp -j ACCEPT -m comment --comment "from source.example.com to icmp"`,
		},
		{
			name:   "an omitted type defaults to tcp",
			listen: &Listen{Name: "https", Port: 443},
			want:   `  iptables -A INPUT -s 192.0.2.1 -p tcp --dport 443 -j ACCEPT -m comment --comment "from source.example.com to https"`,
		},
		{
			name:   "udp includes its destination port",
			listen: &Listen{Name: "dns", Type: "udp", Port: 53},
			want:   `  iptables -A INPUT -s 192.0.2.1 -p udp --dport 53 -j ACCEPT -m comment --comment "from source.example.com to dns"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatAcceptRule("192.0.2.1", "source.example.com", tt.listen.Name, tt.listen)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("formatAcceptRule() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatAcceptRuleRejectsUnsupportedType(t *testing.T) {
	_, err := formatAcceptRule("192.0.2.1", "source.example.com", "gre", &Listen{Name: "gre", Type: "gre"})
	if err == nil {
		t.Fatal("formatAcceptRule() returned no error for an unsupported type")
	}
}

func TestRuleCommandsForHostExcludesOwnSourceIP(t *testing.T) {
	rules := []FirewallRule{
		{SourceIP: "10.0.0.1", Command: "rule from self"},
		{SourceIP: "10.0.0.2", Command: "rule from peer"},
		{SourceIP: "0.0.0.0/0", Command: "rule from network"},
	}

	got := ruleCommandsForHost(rules, "10.0.0.1")
	want := []string{"rule from peer", "rule from network"}
	if len(got) != len(want) {
		t.Fatalf("ruleCommandsForHost() returned %d rules, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ruleCommandsForHost()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGeneratePlantUML(t *testing.T) {
	topo := &Topology{
		Networks: map[string]*Network{
			"public": {
				IP: "0.0.0.0/0",
				Conencts: []*Connect{
					{ServiceName: "api", PortName: "http"},
				},
			},
		},
		Services: map[string]*Service{
			"api": {
				Listens: []*Listen{{Name: "http", Port: 80}},
				Connects: []*Connect{
					{ServiceName: "db", PortName: "mysql"},
				},
			},
			"db": {
				Listens: []*Listen{{Name: "mysql", Type: "tcp", Port: 3306}},
			},
		},
		ServiceGroups: map[string]*ServiceGroup{
			"backend": {
				Hosts: map[string]*Host{
					"db1.example.com": {IP: "10.0.0.2"},
				},
				Services: []string{"db"},
			},
		},
	}

	got := generatePlantUML(topo)
	for _, want := range []string{
		`cloud "public\n0.0.0.0/0" as network_0`,
		`component "api\nhttp: tcp/80" as service_0`,
		`component "db\nmysql: tcp/3306\nserviceGroups: backend" as service_1`,
		`network_0 --> service_0 : http`,
		`service_0 --> service_1 : mysql`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generatePlantUML() does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "db1.example.com") || strings.Contains(got, "10.0.0.2") {
		t.Errorf("generatePlantUML() contains serviceGroup host information:\n%s", got)
	}
	if strings.Contains(got, "service_group_") || strings.Contains(got, ": provides") {
		t.Errorf("generatePlantUML() contains a separate serviceGroup node or edge:\n%s", got)
	}
}
