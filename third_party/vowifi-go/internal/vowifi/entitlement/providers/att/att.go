// Package att implements the AT&T-specific entitlement flow (E911 address
// update) built on the TS.43 primitives.
//
// Reconstructed from the decompiled internal/vowifi/entitlement/providers/att.
package att

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
)

// E911UpdateRequest is the input to the E911 address update.
type E911UpdateRequest struct {
	IMSI  string
	IMEI  string
	Phone string
	// Address is the emergency address.
	Address map[string]string
}

// CheckE911AddressUpdate checks whether an E911 address update is needed and
// performs it.
func CheckE911AddressUpdate(ctx context.Context, client *http.Client, url string, req E911UpdateRequest) error {
	if client == nil {
		client = http.DefaultClient
	}
	actions, err := buildInitialActions(req)
	if err != nil {
		return err
	}
	return runActions(ctx, client, url, actions)
}

// buildInitialActions builds the initial E911 update actions.
func buildInitialActions(req E911UpdateRequest) ([]map[string]interface{}, error) {
	payload := map[string]interface{}{
		"imsi":      req.IMSI,
		"imei":      req.IMEI,
		"phone":     req.Phone,
		"address":   req.Address,
		"action":    "e911_address_update",
	}
	return []map[string]interface{}{payload}, nil
}

// runActions runs the E911 update actions against the entitlement server.
func runActions(ctx context.Context, client *http.Client, url string, actions []map[string]interface{}) error {
	if len(actions) == 0 {
		return errors.New("att: no actions to run")
	}
	body, err := json.Marshal(map[string]interface{}{"actions": actions})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, jsonReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("att: e911 update failed with status %d", resp.StatusCode)
	}
	return nil
}

// ParseResponse parses an AT&T entitlement response.
func ParseResponse(statusCode int, data []byte) (*ts43.EntitlementResponse, error) {
	return ts43.ParseResponse(statusCode, data)
}

// jsonReader wraps a JSON body as an io.Reader.
func jsonReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}
