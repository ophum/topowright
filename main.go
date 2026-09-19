package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Topology struct {
	Networks      map[string]*Network      `yaml:"networks"`
	Services      map[string]*Service      `yaml:"services"`
	ServiceGroups map[string]*ServiceGroup `yaml:"serviceGroups"`
}

type Network struct {
	IP       string     `yaml:"ip"`
	Conencts []*Connect `yaml:"connects"`
}
type Service struct {
	Listens  []*Listen  `yaml:"listens"`
	Connects []*Connect `yaml:"connects"`
}

type Listen struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	Port int    `yaml:"port"`
}

type Connect struct {
	ServiceName string `yaml:"serviceName"`
	PortName    string `yaml:"portName"`
}

type ServiceGroup struct {
	Hosts    map[string]*Host `yaml:"hosts"`
	Services []string         `yaml:"services"`
}

type Host struct {
	IP string `yaml:"ip"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func loadTopo(path string) (*Topology, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var topo Topology
	if err := yaml.NewDecoder(f).Decode(&topo); err != nil {
		return nil, err
	}
	return &topo, nil
}

func formatAcceptRule(fromHost, fromName, portName string, listen *Listen) (string, error) {
	switch listen.Type {
	case "icmp":
		return fmt.Sprintf("  iptables -A INPUT -s %s -p icmp -j ACCEPT -m comment --comment \"from %s to %s\"", fromHost, fromName, portName), nil
	case "", "tcp":
		return fmt.Sprintf("  iptables -A INPUT -s %s -p tcp --dport %d -j ACCEPT -m comment --comment \"from %s to %s\"", fromHost, listen.Port, fromName, portName), nil
	case "udp":
		return fmt.Sprintf("  iptables -A INPUT -s %s -p udp --dport %d -j ACCEPT -m comment --comment \"from %s to %s\"", fromHost, listen.Port, fromName, portName), nil
	default:
		return "", fmt.Errorf("unsupported listen type %q", listen.Type)
	}
}

type FirewallRule struct {
	SourceIP string
	Command  string
}

func ruleCommandsForHost(rules []FirewallRule, hostIP string) []string {
	commands := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.SourceIP == hostIP {
			continue
		}
		commands = append(commands, rule.Command)
	}
	return commands
}

func run(ctx context.Context) error {
	topo, err := loadTopo("topology.yml")
	if err != nil {
		return err
	}

	svcListens := map[string]map[string]*Listen{}
	for name, svc := range topo.Services {
		for _, ln := range svc.Listens {
			if _, ok := svcListens[name]; !ok {
				svcListens[name] = map[string]*Listen{}
			}
			svcListens[name][ln.Name] = ln
		}
	}
	hostIPs := map[string]string{}
	ipHosts := map[string]string{}
	for _, group := range topo.ServiceGroups {
		for hname, host := range group.Hosts {
			hostIPs[hname] = host.IP
			ipHosts[host.IP] = hname
		}
	}

	connected := map[string]map[string]string{}
	for name, net := range topo.Networks {
		ipHosts[net.IP] = name
		for _, conn := range net.Conencts {
			if _, ok := connected[conn.ServiceName]; !ok {
				connected[conn.ServiceName] = map[string]string{}
			}
			connected[conn.ServiceName][conn.PortName] = name
		}
	}
	for name, svc := range topo.Services {
		for _, conn := range svc.Connects {
			if _, ok := connected[conn.ServiceName]; !ok {
				connected[conn.ServiceName] = map[string]string{}
			}
			connected[conn.ServiceName][conn.PortName] = name
		}
	}

	svcIPTables := map[string][]FirewallRule{}
	for name, portFrom := range connected {
		for port, from := range portFrom {
			listen, ok := svcListens[name][port]
			if !ok {
				return fmt.Errorf("service %q has no listen named %q", name, port)
			}
			fromHosts := []string{}
			for _, group := range topo.ServiceGroups {
				if !slices.Contains(group.Services, from) {
					continue
				}
				for host := range group.Hosts {
					fromHosts = append(fromHosts, hostIPs[host])
				}
			}
			for _, net := range topo.Networks {
				if !slices.ContainsFunc(net.Conencts, func(a *Connect) bool {
					return a.ServiceName == name
				}) {
					continue
				}

				fromHosts = append(fromHosts, net.IP)
			}

			for _, fromHost := range fromHosts {
				if _, ok := svcIPTables[name]; !ok {
					svcIPTables[name] = []FirewallRule{}
				}
				rule, err := formatAcceptRule(fromHost, ipHosts[fromHost], port, listen)
				if err != nil {
					return fmt.Errorf("service %q listen %q: %w", name, port, err)
				}
				svcIPTables[name] = append(svcIPTables[name], FirewallRule{
					SourceIP: fromHost,
					Command:  rule,
				})
			}
		}
	}

	for _, group := range topo.ServiceGroups {
		for hostName, host := range group.Hosts {
			fmt.Println(hostName, host.IP)

			for _, svc := range group.Services {
				fmt.Println(strings.Join(ruleCommandsForHost(svcIPTables[svc], host.IP), "\n"))
			}
			fmt.Println("  iptables -A INPUT -j DROP")
		}
	}

	return nil
}
