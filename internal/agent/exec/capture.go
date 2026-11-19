package exec

import "sync"

const truncatedMark = "[salida recortada]\n"

// capture guarda como mucho los últimos max bytes de lo escrito, con memoria
// acotada, y recuerda si descartó algo.
type capture struct {
	mu      sync.Mutex
	max     int
	buf     []byte
	dropped bool
}

func newCapture(max int) *capture { return &capture{max: max} }

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	if len(p) >= c.max {
		c.dropped = c.dropped || len(c.buf) > 0 || len(p) > c.max
		c.buf = append(c.buf[:0], p[len(p)-c.max:]...)
		return n, nil
	}
	c.buf = append(c.buf, p...)
	if len(c.buf) > 2*c.max { // compactar: conservar solo el final
		c.dropped = true
		copy(c.buf, c.buf[len(c.buf)-c.max:])
		c.buf = c.buf[:c.max]
	}
	return n, nil
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.buf
	dropped := c.dropped
	if len(b) > c.max {
		b = b[len(b)-c.max:]
		dropped = true
	}
	if !dropped {
		return string(b)
	}
	for len(b) > 0 && b[0]&0xC0 == 0x80 { // no empezar a mitad de un carácter
		b = b[1:]
	}
	return truncatedMark + string(b)
}
