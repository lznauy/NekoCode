package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"nekocode/protocol"
)

func EvidenceHash(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// OutputHash hashes only the returned boundary value, never in-memory ReadID
// or the contents of the referenced artifact. Process/file completeness is a
// separate producer fact; this digest cannot establish it.
func OutputHash(output ToolOutput) string {
	data, _ := json.Marshal(struct {
		TextHash string
		Artifact protocol.ToolOutputArtifact
		Process  ProcessObservation
	}{EvidenceHash([]byte(output.Text)), output.Artifact, output.Process})
	return EvidenceHash(data)
}
