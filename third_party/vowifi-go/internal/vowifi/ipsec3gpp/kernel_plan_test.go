package ipsec3gpp

import (
	"errors"
	"fmt"
	"testing"
)

type recordingKernelOperations struct {
	operations []string
	fail       string
}

func (operations *recordingKernelOperations) record(operation string) error {
	operations.operations = append(operations.operations, operation)
	if operation == operations.fail {
		return errors.New("injected XFRM failure")
	}
	return nil
}

func (operations *recordingKernelOperations) deleteState(state kernelState) error {
	return operations.record(fmt.Sprintf("delete-state-%08x", state.spi))
}

func (operations *recordingKernelOperations) addState(state kernelState) error {
	return operations.record(fmt.Sprintf("add-state-%08x", state.spi))
}

func (operations *recordingKernelOperations) deletePolicy(policy kernelPolicy) error {
	return operations.record(fmt.Sprintf("delete-policy-%08x", policy.spi))
}

func (operations *recordingKernelOperations) addPolicy(policy kernelPolicy) error {
	return operations.record(fmt.Sprintf("add-policy-%08x", policy.spi))
}

func TestBuildKernelPlanCreatesFourStatesAndPolicies(t *testing.T) {
	policy := testPolicy(nil, nil, EncryptionAES)
	policy.FlowS.AuthAlg = "not-used-by-kernel"
	policy.FlowS.EncAlg = "not-used-by-kernel"
	policy.FlowS.CK = nil
	policy.FlowS.IK = nil
	plan, err := buildKernelPlan(policy)
	if err != nil {
		t.Fatalf("buildKernelPlan: %v", err)
	}
	if len(plan.states) != 4 || len(plan.policies) != 4 {
		t.Fatalf("plan has %d states and %d policies", len(plan.states), len(plan.policies))
	}
	wantSPIs := []uint32{0x44444444, 0x11111111, 0x33333333, 0x22222222}
	for index, spi := range wantSPIs {
		if plan.states[index].spi != spi || plan.policies[index].spi != spi {
			t.Fatalf("entry %d SPI = %08x/%08x, want %08x", index, plan.states[index].spi, plan.policies[index].spi, spi)
		}
	}
	if plan.policies[0].sourcePort != 41000 || plan.policies[0].destinationPort != 51001 || plan.policies[0].inbound {
		t.Fatalf("outbound C policy = %+v", plan.policies[0])
	}
	if plan.states[0].sourcePort != 41000 || plan.states[0].destinationPort != 51001 ||
		plan.states[1].sourcePort != 51001 || plan.states[1].destinationPort != 41000 {
		t.Fatalf("C state selectors = outbound:%+v inbound:%+v", plan.states[0], plan.states[1])
	}
	if plan.policies[1].sourcePort != 51001 || plan.policies[1].destinationPort != 41000 || !plan.policies[1].inbound {
		t.Fatalf("inbound C policy = %+v", plan.policies[1])
	}
	for index, state := range plan.states {
		if state.auth.name != "hmac(sha1)" || state.crypt.name != "cbc(aes)" {
			t.Fatalf("state %d algorithms = %s/%s", index, state.auth.name, state.crypt.name)
		}
	}
}

func TestInstallKernelPlanRollsBackStateFailure(t *testing.T) {
	plan, err := buildKernelPlan(testPolicy(nil, nil, EncryptionAES))
	if err != nil {
		t.Fatalf("buildKernelPlan: %v", err)
	}
	operations := &recordingKernelOperations{fail: "add-state-11111111"}
	cleanup, err := installKernelPlan(operations, plan)
	if err == nil || cleanup != nil {
		t.Fatalf("install = has_cleanup:%t err:%v", cleanup != nil, err)
	}
	if got := operations.operations[len(operations.operations)-1]; got != "delete-state-44444444" {
		t.Fatalf("last rollback operation = %s", got)
	}
}

func TestInstallKernelPlanRollsBackPolicyFailure(t *testing.T) {
	plan, err := buildKernelPlan(testPolicy(nil, nil, EncryptionAES))
	if err != nil {
		t.Fatalf("buildKernelPlan: %v", err)
	}
	operations := &recordingKernelOperations{fail: "add-policy-11111111"}
	cleanup, err := installKernelPlan(operations, plan)
	if err == nil || cleanup != nil {
		t.Fatalf("install = has_cleanup:%t err:%v", cleanup != nil, err)
	}
	wantTail := []string{
		"delete-policy-44444444",
		"delete-state-44444444", "delete-state-11111111", "delete-state-33333333", "delete-state-22222222",
	}
	assertOperationTail(t, operations.operations, wantTail)
}

func TestInstallKernelPlanCleanupDeletesPoliciesBeforeStates(t *testing.T) {
	plan, err := buildKernelPlan(testPolicy(nil, nil, EncryptionAES))
	if err != nil {
		t.Fatalf("buildKernelPlan: %v", err)
	}
	operations := &recordingKernelOperations{}
	cleanup, err := installKernelPlan(operations, plan)
	if err != nil {
		t.Fatalf("installKernelPlan: %v", err)
	}
	operations.operations = nil
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(operations.operations) != 8 || operations.operations[0] != "delete-policy-44444444" ||
		operations.operations[4] != "delete-state-44444444" {
		t.Fatalf("cleanup operations = %v", operations.operations)
	}
}

func assertOperationTail(t *testing.T, operations, want []string) {
	t.Helper()
	if len(operations) < len(want) {
		t.Fatalf("operations = %v", operations)
	}
	got := operations[len(operations)-len(want):]
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("operation tail = %v, want %v", got, want)
		}
	}
}
