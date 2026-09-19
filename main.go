package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"sort"
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
	plantUML := flag.Bool("plantuml", false, "generate a PlantUML diagram instead of iptables rules")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, *plantUML); err != nil {
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

func plantUMLLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func generatePlantUML(topo *Topology) string {
	var diagram strings.Builder
	diagram.WriteString("@startuml\n")
	diagram.WriteString("left to right direction\n")
	diagram.WriteString("skinparam componentStyle rectangle\n\n")

	networkNames := make([]string, 0, len(topo.Networks))
	for name := range topo.Networks {
		networkNames = append(networkNames, name)
	}
	sort.Strings(networkNames)

	serviceNames := make([]string, 0, len(topo.Services))
	for name := range topo.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	serviceGroupNames := make([]string, 0, len(topo.ServiceGroups))
	for name := range topo.ServiceGroups {
		serviceGroupNames = append(serviceGroupNames, name)
	}
	sort.Strings(serviceGroupNames)
	serviceGroupsByService := make(map[string][]string)
	for _, groupName := range serviceGroupNames {
		for _, serviceName := range topo.ServiceGroups[groupName].Services {
			serviceGroupsByService[serviceName] = append(serviceGroupsByService[serviceName], groupName)
		}
	}

	networkIDs := make(map[string]string, len(networkNames))
	for i, name := range networkNames {
		id := fmt.Sprintf("network_%d", i)
		networkIDs[name] = id
		network := topo.Networks[name]
		fmt.Fprintf(&diagram, "cloud \"%s\\n%s\" as %s\n", plantUMLLabel(name), plantUMLLabel(network.IP), id)
	}

	diagram.WriteString("\n")
	serviceIDs := make(map[string]string, len(serviceNames))
	for i, name := range serviceNames {
		id := fmt.Sprintf("service_%d", i)
		serviceIDs[name] = id
		label := plantUMLLabel(name)
		listens := append([]*Listen(nil), topo.Services[name].Listens...)
		sort.Slice(listens, func(i, j int) bool { return listens[i].Name < listens[j].Name })
		for _, listen := range listens {
			protocol := listen.Type
			if protocol == "" {
				protocol = "tcp"
			}
			if protocol == "icmp" {
				label += fmt.Sprintf("\\n%s: %s", plantUMLLabel(listen.Name), protocol)
			} else {
				label += fmt.Sprintf("\\n%s: %s/%d", plantUMLLabel(listen.Name), protocol, listen.Port)
			}
		}
		groups := serviceGroupsByService[name]
		sort.Strings(groups)
		if len(groups) > 0 {
			label += "\\nserviceGroups: " + plantUMLLabel(strings.Join(groups, ", "))
		}
		fmt.Fprintf(&diagram, "component \"%s\" as %s\n", label, id)
	}

	type edge struct {
		from, to, label string
	}
	edges := []edge{}
	for _, name := range networkNames {
		for _, connect := range topo.Networks[name].Conencts {
			if to, ok := serviceIDs[connect.ServiceName]; ok {
				edges = append(edges, edge{networkIDs[name], to, connect.PortName})
			}
		}
	}
	for _, name := range serviceNames {
		for _, connect := range topo.Services[name].Connects {
			if to, ok := serviceIDs[connect.ServiceName]; ok {
				edges = append(edges, edge{serviceIDs[name], to, connect.PortName})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		if edges[i].to != edges[j].to {
			return edges[i].to < edges[j].to
		}
		return edges[i].label < edges[j].label
	})

	diagram.WriteString("\n")
	for _, edge := range edges {
		fmt.Fprintf(&diagram, "%s --> %s : %s\n", edge.from, edge.to, plantUMLLabel(edge.label))
	}
	diagram.WriteString("@enduml\n")
	return diagram.String()
}

func run(ctx context.Context, plantUML bool) error {
	topo, err := loadTopo("topology.yml")
	if err != nil {
		return err
	}
	if plantUML {
		fmt.Print(generatePlantUML(topo))
		return nil
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
