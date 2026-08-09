package imscore

import (
	"context"
	"errors"
	"net"
	"testing"
)

type registrarDiscoveryNetwork struct {
	*SystemIMSNetwork
	calls []string
}

func (network *registrarDiscoveryNetwork) LookupSRV(
	_ context.Context,
	service, proto, name string,
) (string, uint16, error) {
	network.calls = append(network.calls, service+":"+proto+":"+name)
	if proto == "tcp" {
		return "pcscf.ims.example.", 5070, nil
	}
	return "", 0, errors.New("not found")
}

func TestDiscoverRegistrarUsesDNSModuleInProductionPath(t *testing.T) {
	network := &registrarDiscoveryNetwork{SystemIMSNetwork: NewSystemIMSNetwork(net.ParseIP("192.0.2.10"))}
	service := &Service{cfg: &IMSConfig{Domain: "ims.example", IMSNetwork: network}}
	registrar, err := service.discoverRegistrar(context.Background(), "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if registrar != "pcscf.ims.example:5070" {
		t.Fatalf("registrar = %q", registrar)
	}
	if len(network.calls) != 1 || network.calls[0] != "sip:tcp:ims.example" {
		t.Fatalf("calls = %v", network.calls)
	}
}

type networkWithoutSRV struct {
	IMSNetwork
}

func TestDiscoverRegistrarRejectsNetworkWithoutSRV(t *testing.T) {
	network := &networkWithoutSRV{IMSNetwork: NewSystemIMSNetwork(net.ParseIP("192.0.2.10"))}
	service := &Service{cfg: &IMSConfig{Domain: "ims.example", IMSNetwork: network}}
	if _, err := service.discoverRegistrar(context.Background(), "udp"); err == nil {
		t.Fatal("expected missing SRV capability error")
	}
}
