package acp

import (
	"encoding/json"
	"testing"
)

func TestInitialize(t *testing.T) {
	s := &server{}
	result, rpcErr := s.initialize(json.RawMessage(`{"protocolVersion":1}`))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	response := result.(initializeResponse)
	if response.ProtocolVersion != 1 || !response.AgentCapabilities.LoadSession || !response.AgentCapabilities.PromptCapabilities.Image {
		t.Fatalf("initialize response = %#v", response)
	}
	if _, rpcErr := s.initialize(json.RawMessage(`{"protocolVersion":1}`)); rpcErr == nil {
		t.Fatal("duplicate initialize was accepted")
	}
}

func TestInitializeClientBooleanConfig(t *testing.T) {
	withBoolean := &server{}
	if _, rpcErr := withBoolean.initialize(json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"session":{"configOptions":{"boolean":{}}}}}`)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if !withBoolean.supportsBooleanConfig() {
		t.Fatal("boolean config support was not recorded")
	}

	withoutBoolean := &server{}
	if _, rpcErr := withoutBoolean.initialize(json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"session":{}}}`)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if withoutBoolean.supportsBooleanConfig() {
		t.Fatal("boolean config support was recorded without advertisement")
	}
}

func TestInitializeToleratesMalformedClientCapabilities(t *testing.T) {
	// The schema marks capability fields x-deserialize-default-on-error, so
	// a schema-invalid value must degrade to "not advertised" instead of
	// failing the handshake.
	s := &server{}
	if _, rpcErr := s.initialize(json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"session":{"configOptions":{"boolean":false}}}}`)); rpcErr != nil {
		t.Fatalf("handshake rejected: %v", rpcErr)
	}
	if s.supportsBooleanConfig() {
		t.Fatal("malformed boolean advertisement must not count as support")
	}
}
