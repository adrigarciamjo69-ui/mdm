package rules

import "mdm/internal/protocol"

// CompareVersions compara dos versiones (ver protocol.CompareVersions, que la
// comparte con el agente).
func CompareVersions(a, b string) int { return protocol.CompareVersions(a, b) }
