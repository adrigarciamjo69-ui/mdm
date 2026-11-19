package auth

import "fmt"

type Role string

const (
	RoleReadonly Role = "readonly"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

var roleRank = map[Role]int{RoleReadonly: 1, RoleOperator: 2, RoleAdmin: 3}

// AtLeast informa de si el rol alcanza el mínimo pedido. Un rol desconocido,
// en cualquiera de los dos lados, nunca lo alcanza.
func (r Role) AtLeast(min Role) bool {
	have, ok1 := roleRank[r]
	need, ok2 := roleRank[min]
	return ok1 && ok2 && have >= need
}

func ParseRole(s string) (Role, error) {
	r := Role(s)
	if _, ok := roleRank[r]; !ok {
		return "", fmt.Errorf("rol desconocido: %q", s)
	}
	return r, nil
}
