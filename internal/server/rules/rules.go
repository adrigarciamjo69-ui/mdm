// Package rules evalúa las reglas de los grupos dinámicos sobre los datos de
// un equipo. Es lógica pura: no accede a la base de datos.
package rules

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Rule struct {
	Match      string      `json:"match"`
	Conditions []Condition `json:"conditions"`
}

type Condition struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

// Facts son los datos de un equipo sobre los que se evalúan las reglas.
type Facts struct {
	Platform, Group, OSName, OSVersion, AgentVersion    string
	Hostname, Manufacturer, Model, AssignedUser, Domain string
	Compliance, Status                                  string
	Tags                                                []string
	Software                                            []string
	LastSeenDays                                        int
}

const MaxConditions = 20

type fieldKind int

const (
	kindEnum fieldKind = iota
	kindText
	kindVersion
	kindTag
	kindSoftware
	kindDays
	kindID
)

var fields = map[string]struct {
	kind   fieldKind
	ops    []string
	values []string // valores admitidos en los campos enumerados
}{
	"platform":       {kindEnum, []string{"eq", "ne"}, []string{"windows", "linux", "android"}},
	"group":          {kindID, []string{"eq", "ne"}, nil},
	"os_name":        {kindText, []string{"eq", "ne", "contains", "starts_with"}, nil},
	"hostname":       {kindText, []string{"eq", "ne", "contains", "starts_with"}, nil},
	"manufacturer":   {kindText, []string{"eq", "ne", "contains", "starts_with"}, nil},
	"model":          {kindText, []string{"eq", "ne", "contains", "starts_with"}, nil},
	"assigned_user":  {kindText, []string{"eq", "ne", "contains", "starts_with"}, nil},
	"domain":         {kindText, []string{"eq", "ne", "contains", "starts_with"}, nil},
	"os_version":     {kindVersion, []string{"eq", "lt", "lte", "gt", "gte"}, nil},
	"agent_version":  {kindVersion, []string{"eq", "lt", "lte", "gt", "gte"}, nil},
	"tag":            {kindTag, []string{"has", "not_has"}, nil},
	"software":       {kindSoftware, []string{"installed", "not_installed"}, nil},
	"compliance":     {kindEnum, []string{"eq", "ne"}, []string{"compliant", "noncompliant", "unknown"}},
	"status":         {kindEnum, []string{"eq", "ne"}, []string{"online", "offline", "inactive", "revoked"}},
	"last_seen_days": {kindDays, []string{"lt", "gt"}, nil},
}

var versionRe = regexp.MustCompile(`^[0-9A-Za-z]+([._\-+~][0-9A-Za-z]+)*$`)

// Validate comprueba la regla y devuelve un mensaje en español listo para mostrarse.
func Validate(r Rule) error {
	if r.Match != "all" && r.Match != "any" {
		return fmt.Errorf("La regla debe combinar las condiciones con «todas» o «cualquiera».")
	}
	if len(r.Conditions) == 0 || len(r.Conditions) > MaxConditions {
		return fmt.Errorf("La regla debe tener entre 1 y %d condiciones.", MaxConditions)
	}
	for i, c := range r.Conditions {
		if err := validateCondition(c); err != nil {
			return fmt.Errorf("Condición %d: %s", i+1, err.Error())
		}
	}
	return nil
}

func validateCondition(c Condition) error {
	f, ok := fields[c.Field]
	if !ok {
		return fmt.Errorf("el campo «%s» no existe.", c.Field)
	}
	allowed := false
	for _, op := range f.ops {
		allowed = allowed || op == c.Op
	}
	if !allowed {
		return fmt.Errorf("el operador «%s» no se puede usar con «%s».", c.Op, c.Field)
	}
	if f.kind == kindDays {
		var n int
		if err := json.Unmarshal(c.Value, &n); err != nil || n < 0 || n > 3650 {
			return fmt.Errorf("el número de días debe ser un entero entre 0 y 3650.")
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(c.Value, &s); err != nil || strings.TrimSpace(s) == "" {
		return fmt.Errorf("falta el valor.")
	}
	switch f.kind {
	case kindEnum:
		for _, v := range f.values {
			if v == s {
				return nil
			}
		}
		return fmt.Errorf("«%s» no es un valor válido para «%s».", s, c.Field)
	case kindVersion:
		if !versionRe.MatchString(s) || !strings.ContainsAny(s, "0123456789") {
			return fmt.Errorf("«%s» no es una versión válida.", s)
		}
	}
	return nil
}

// Matches evalúa la regla. Supone una regla validada.
func Matches(r Rule, f Facts) bool {
	if r.Match == "any" {
		for _, c := range r.Conditions {
			if matchCondition(c, f) {
				return true
			}
		}
		return false
	}
	for _, c := range r.Conditions {
		if !matchCondition(c, f) {
			return false
		}
	}
	return true
}

func (f Facts) text(field string) string {
	switch field {
	case "platform":
		return f.Platform
	case "group":
		return f.Group
	case "os_name":
		return f.OSName
	case "hostname":
		return f.Hostname
	case "manufacturer":
		return f.Manufacturer
	case "model":
		return f.Model
	case "assigned_user":
		return f.AssignedUser
	case "domain":
		return f.Domain
	case "os_version":
		return f.OSVersion
	case "agent_version":
		return f.AgentVersion
	case "compliance":
		return f.Compliance
	case "status":
		return f.Status
	}
	return ""
}

func matchCondition(c Condition, f Facts) bool {
	def := fields[c.Field]
	if def.kind == kindDays {
		var n int
		_ = json.Unmarshal(c.Value, &n)
		if c.Op == "lt" {
			return f.LastSeenDays < n
		}
		return f.LastSeenDays > n
	}
	var want string
	_ = json.Unmarshal(c.Value, &want)
	lw := strings.ToLower(want)
	switch def.kind {
	case kindTag:
		has := false
		for _, t := range f.Tags {
			has = has || strings.EqualFold(t, want)
		}
		return has == (c.Op == "has")
	case kindSoftware:
		has := false
		for _, s := range f.Software {
			has = has || strings.Contains(strings.ToLower(s), lw)
		}
		return has == (c.Op == "installed")
	case kindVersion:
		have := f.text(c.Field)
		if have == "" {
			return false
		}
		cmp := CompareVersions(have, want)
		switch c.Op {
		case "eq":
			return cmp == 0
		case "lt":
			return cmp < 0
		case "lte":
			return cmp <= 0
		case "gt":
			return cmp > 0
		case "gte":
			return cmp >= 0
		}
		return false
	}
	have := strings.ToLower(f.text(c.Field))
	switch c.Op {
	case "eq":
		return have == lw
	case "ne":
		return have != lw
	case "contains":
		return strings.Contains(have, lw)
	case "starts_with":
		return strings.HasPrefix(have, lw)
	}
	return false
}
