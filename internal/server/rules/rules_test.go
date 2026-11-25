package rules

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func cond(field, op string, value any) Condition {
	b, _ := json.Marshal(value)
	return Condition{Field: field, Op: op, Value: b}
}

func facts() Facts {
	return Facts{
		Platform: "windows", Group: "g1", OSName: "Windows 11 Pro", OSVersion: "10.0.26100", AgentVersion: "1.1.0",
		Hostname: "PC-CONTA-07", Manufacturer: "Lenovo", Model: "ThinkPad T14", AssignedUser: "ana.garcia", Domain: "EMPRESA",
		Compliance: "noncompliant", Status: "online", Tags: []string{"vip", "planta-2"},
		Software: []string{"Mozilla Firefox (x64 es-ES)", "7-Zip 24.08"}, LastSeenDays: 3,
	}
}

func TestMatches_EachFieldAndOp(t *testing.T) {
	cases := []struct {
		c    Condition
		want bool
	}{
		{cond("platform", "eq", "windows"), true}, {cond("platform", "ne", "windows"), false},
		{cond("group", "eq", "g1"), true}, {cond("group", "ne", "g2"), true},
		{cond("os_name", "contains", "windows 11"), true}, {cond("os_name", "starts_with", "Windows 10"), false},
		{cond("hostname", "eq", "pc-conta-07"), true}, {cond("hostname", "starts_with", "pc-"), true}, {cond("hostname", "ne", "PC-CONTA-07"), false},
		{cond("manufacturer", "contains", "leno"), true}, {cond("model", "eq", "ThinkPad X1"), false},
		{cond("assigned_user", "starts_with", "ana"), true}, {cond("domain", "eq", "empresa"), true},
		{cond("os_version", "gte", "10.0.22631"), true}, {cond("os_version", "lt", "10.0.22631"), false},
		{cond("os_version", "eq", "10.0.26100"), true}, {cond("os_version", "lte", "10.0.26100"), true}, {cond("os_version", "gt", "10.0.26100"), false},
		{cond("agent_version", "lt", "1.2.0"), true},
		{cond("tag", "has", "VIP"), true}, {cond("tag", "not_has", "vip"), false}, {cond("tag", "has", "ventas"), false},
		{cond("software", "installed", "firefox"), true}, {cond("software", "not_installed", "firefox"), false}, {cond("software", "installed", "chrome"), false},
		{cond("compliance", "eq", "noncompliant"), true}, {cond("compliance", "ne", "compliant"), true},
		{cond("status", "eq", "offline"), false},
		{cond("last_seen_days", "lt", 7), true}, {cond("last_seen_days", "gt", 7), false},
	}
	for _, c := range cases {
		r := Rule{Match: "all", Conditions: []Condition{c.c}}
		if err := Validate(r); err != nil {
			t.Errorf("%+v: regla inválida: %v", c.c, err)
			continue
		}
		if got := Matches(r, facts()); got != c.want {
			t.Errorf("%s %s %s → %v, se esperaba %v", c.c.Field, c.c.Op, c.c.Value, got, c.want)
		}
	}
}

func TestMatches_AllVsAny(t *testing.T) {
	yes, no := cond("platform", "eq", "windows"), cond("platform", "eq", "linux")
	if Matches(Rule{Match: "all", Conditions: []Condition{yes, no}}, facts()) {
		t.Error("all con una falsa debe ser falso")
	}
	if !Matches(Rule{Match: "any", Conditions: []Condition{yes, no}}, facts()) {
		t.Error("any con una verdadera debe ser verdadero")
	}
}

func TestMatches_EmptyFields(t *testing.T) {
	f := Facts{Platform: "linux"}
	if Matches(Rule{Match: "all", Conditions: []Condition{cond("os_version", "gte", "1")}}, f) {
		t.Error("versión vacía no es ≥ 1")
	}
	if !Matches(Rule{Match: "all", Conditions: []Condition{cond("tag", "not_has", "vip")}}, f) {
		t.Error("sin etiquetas, not_has debe cumplirse")
	}
	if Matches(Rule{Match: "all", Conditions: []Condition{cond("group", "eq", "g1")}}, f) {
		t.Error("sin grupo no es g1")
	}
}

func TestValidate(t *testing.T) {
	many := make([]Condition, 21)
	for i := range many {
		many[i] = cond("platform", "eq", "linux")
	}
	bad := []Rule{
		{Match: "all"},
		{Match: "all", Conditions: many},
		{Match: "todas", Conditions: []Condition{cond("platform", "eq", "linux")}},
		{Match: "all", Conditions: []Condition{cond("color", "eq", "rojo")}},
		{Match: "all", Conditions: []Condition{cond("os_version", "contains", "10")}},
		{Match: "all", Conditions: []Condition{cond("last_seen_days", "lt", "x")}},
		{Match: "all", Conditions: []Condition{cond("last_seen_days", "lt", -1)}},
		{Match: "all", Conditions: []Condition{cond("hostname", "eq", "")}},
		{Match: "all", Conditions: []Condition{cond("platform", "eq", "macos")}},
		{Match: "all", Conditions: []Condition{cond("status", "eq", "dormido")}},
		{Match: "all", Conditions: []Condition{cond("hostname", "eq", 5)}},
		{Match: "all", Conditions: []Condition{cond("os_version", "gt", "abc")}},
	}
	for i, r := range bad {
		err := Validate(r)
		if err == nil {
			t.Errorf("regla %d debería ser inválida: %+v", i, r)
			continue
		}
		if !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("regla %d: el mensaje debe ser una frase: %v", i, err)
		}
	}
	ok := Rule{Match: "any", Conditions: []Condition{cond("hostname", "contains", "conta"), cond("last_seen_days", "gt", 30)}}
	if err := Validate(ok); err != nil {
		t.Errorf("regla válida rechazada: %v", err)
	}
}

func TestValidate_ErrorNamesTheCondition(t *testing.T) {
	r := Rule{Match: "all", Conditions: []Condition{cond("platform", "eq", "linux"), cond("color", "eq", "x")}}
	if err := Validate(r); err == nil || !strings.Contains(err.Error(), fmt.Sprint(2)) {
		t.Fatalf("el error debe indicar la condición 2: %v", err)
	}
}

func TestDepartmentRule(t *testing.T) {
	r := Rule{Match: "all", Conditions: []Condition{{Field: "department", Op: "eq", Value: json.RawMessage(`"finanzas"`)}}}
	if err := Validate(r); err != nil {
		t.Fatal(err)
	}
	if !Matches(r, Facts{Department: "Finanzas"}) || Matches(r, Facts{Department: "Compras"}) {
		t.Fatal("departamento sin distinguir mayúsculas")
	}
}
