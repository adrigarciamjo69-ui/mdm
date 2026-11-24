package protocol

import "testing"

func TestChassisFromSMBIOS(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{[]int{10}, ChassisLaptop}, {[]int{9}, ChassisLaptop}, {[]int{31}, ChassisLaptop},
		{[]int{3}, ChassisDesktop}, {[]int{35}, ChassisDesktop},
		{[]int{17}, ChassisServer}, {[]int{23}, ChassisServer},
		{[]int{2, 10}, ChassisLaptop}, {[]int{1}, ChassisUnknown}, {nil, ChassisUnknown},
	}
	for _, c := range cases {
		if got := ChassisFromSMBIOS(c.in); got != c.want {
			t.Errorf("ChassisFromSMBIOS(%v) = %s, se esperaba %s", c.in, got, c.want)
		}
	}
}
