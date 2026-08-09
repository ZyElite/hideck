package imscore

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

func splitRegistrarCandidates(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	parts := strings.Split(spec, ";")
	candidates := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			candidates = append(candidates, part)
		}
	}
	if len(candidates) < 2 {
		return candidates
	}
	seen := make(map[string]struct{}, len(candidates))
	unique := candidates[:0]
	for _, candidate := range candidates {
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func registrarSpecMatchesFamily(spec string, preferIPv6 bool) bool {
	for _, candidate := range splitRegistrarCandidates(spec) {
		host, _, err := sipkit.ParseHostPortWithDefault(candidate, defaultSIPPort)
		if err != nil {
			continue
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if ip == nil || (preferIPv6 == (ip.To4() == nil)) {
			return true
		}
	}
	return false
}

func resolveRegisterAttemptCandidates(cfg IMSConfig, existing []string) []string {
	if len(existing) > 0 {
		return append([]string(nil), existing...)
	}
	if configured := strings.TrimSpace(cfg.Registrar); configured != "" {
		return splitRegistrarCandidates(configured)
	}
	return splitRegistrarCandidates(pickRegistrar(cfg))
}

func selectRegisterAttemptRegistrar(
	cfg IMSConfig,
	existing []string,
	index int,
) (string, []string, int, error) {
	candidates := resolveRegisterAttemptCandidates(cfg, existing)
	if len(candidates) == 0 {
		return "", nil, 0, errors.New("registrar 为空")
	}
	if index < 0 || index >= len(candidates) {
		index = 0
	}
	selected := strings.TrimSpace(candidates[index])
	if selected == "" {
		return "", candidates, index, errors.New("registrar 为空")
	}
	return selected, candidates, index, nil
}

func pickRegistrar(cfg IMSConfig) string {
	preferIPv6 := net.ParseIP(strings.Trim(cfg.LocalAddr, "[]")) != nil &&
		net.ParseIP(strings.Trim(cfg.LocalAddr, "[]")).To4() == nil
	if registrar := strings.TrimSpace(cfg.Registrar); registrar != "" && registrarSpecMatchesFamily(registrar, preferIPv6) {
		return registrar
	}
	if pcscf := strings.TrimSpace(cfg.PCSCF); pcscf != "" && registrarSpecMatchesFamily(pcscf, preferIPv6) {
		return pcscf
	}
	if domain := strings.TrimSpace(cfg.Domain); domain != "" {
		return discoverRegistrar(domain, cfg.Transport, preferIPv6)
	}
	return ""
}

func discoverRegistrar(domain, transport string, preferIPv6 bool) string {
	protocols := []string{"tcp", "udp"}
	switch policy.NormalizeIMSTransport(transport) {
	case "udp":
		protocols = []string{"udp"}
	case "tcp":
		protocols = []string{"tcp", "udp"}
	}
	for _, protocol := range protocols {
		_, records, err := net.LookupSRV("sip", protocol, domain)
		if err != nil || len(records) == 0 {
			continue
		}
		host := strings.TrimSuffix(records[0].Target, ".")
		if ip, err := resolveHostIP(host, preferIPv6); err == nil {
			host = ip.String()
		}
		return net.JoinHostPort(host, strconv.Itoa(int(records[0].Port)))
	}
	return ""
}

func (s *Service) registrationCandidates(ctx context.Context, transport string) ([]string, string, error) {
	preferIPv6 := s.cfg.LocalIP != nil && s.cfg.LocalIP.To4() == nil
	if spec := strings.TrimSpace(s.cfg.Registrar); spec != "" && registrarSpecMatchesFamily(spec, preferIPv6) {
		return splitRegistrarCandidates(spec), "registrar", nil
	}
	if spec := strings.TrimSpace(s.cfg.PCSCF); spec != "" && registrarSpecMatchesFamily(spec, preferIPv6) {
		return splitRegistrarCandidates(spec), "pcscf", nil
	}
	discovered, err := s.discoverRegistrar(ctx, transport)
	if err != nil {
		return nil, "dns", err
	}
	return splitRegistrarCandidates(discovered), "dns", nil
}

func (s *Service) selectRegistrarCandidate(ctx context.Context, transport string) (string, error) {
	s.mu.RLock()
	existing := append([]string(nil), s.registrarCandidates...)
	index := s.registrarIndex
	source := s.registrarSource
	s.mu.RUnlock()
	if len(existing) == 0 {
		var err error
		existing, source, err = s.registrationCandidates(ctx, transport)
		if err != nil {
			return "", err
		}
	}
	selected, candidates, index, err := selectRegisterAttemptRegistrar(*s.cfg, existing, index)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.registrar = selected
	s.registrarCandidates = candidates
	s.registrarIndex = index
	s.registrarSource = source
	s.mu.Unlock()
	return selected, nil
}

func (s *Service) advanceRegistrarForNextRetry(reason string) bool {
	if s == nil || reason == "min_expires" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.registrarCandidates) < 2 {
		return false
	}
	next := (s.registrarIndex + 1) % len(s.registrarCandidates)
	selected := strings.TrimSpace(s.registrarCandidates[next])
	if selected == "" || selected == s.registrar {
		return false
	}
	s.registrarIndex = next
	s.registrar = selected
	return true
}
