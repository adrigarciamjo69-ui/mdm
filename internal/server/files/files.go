// Package files guarda los instaladores en disco nombrados por su SHA-256:
// <dir>/<sha[0:2]>/<sha>. La subida se escribe en streaming en un temporal
// del mismo directorio y solo se renombra al terminar bien.
package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var (
	ErrTooLarge = errors.New("el fichero supera el tamaño máximo")
	ErrDiskFull = errors.New("no queda espacio suficiente en el disco del servidor")
	shaRe       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// checkEvery es cada cuántos bytes se vuelve a comprobar el espacio libre.
const checkEvery = 64 << 20

type Store struct {
	dir     string
	minFree uint64
	// free devuelve los bytes libres del disco de dir (sustituible en pruebas).
	free func(dir string) (uint64, error)
}

// New crea el almacén. minFree es el espacio que debe quedar libre en el disco.
func New(dir string, minFree uint64) *Store {
	return &Store{dir: dir, minFree: minFree, free: diskFree}
}

func diskFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// ValidSHA informa de si s es un SHA-256 en hexadecimal.
func ValidSHA(s string) bool { return shaRe.MatchString(s) }

func (s *Store) path(sha string) string { return filepath.Join(s.dir, sha[:2], sha) }

func (s *Store) checkSpace() error {
	if s.minFree == 0 {
		return nil
	}
	free, err := s.free(s.dir)
	if err != nil {
		return fmt.Errorf("comprobar el espacio libre: %w", err)
	}
	if free < s.minFree {
		return ErrDiskFull
	}
	return nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}

// Put guarda el contenido de r y devuelve su SHA-256 y su tamaño. Ante
// cualquier error no deja nada en el almacén.
func (s *Store) Put(ctx context.Context, r io.Reader, maxSize int64) (string, int64, error) {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return "", 0, err
	}
	if err := s.checkSpace(); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(s.dir, ".subida-*")
	if err != nil {
		return "", 0, err
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	src := io.LimitReader(ctxReader{ctx, r}, maxSize+1)
	var n int64
	for {
		k, err := io.CopyN(w, src, checkEvery)
		n += k
		if n > maxSize {
			return "", 0, ErrTooLarge
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, err
		}
		if err := s.checkSpace(); err != nil {
			return "", 0, err
		}
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	dst := s.path(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(dst); err == nil {
		return sha, n, nil // ya estaba: el temporal se borra
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", 0, err
	}
	keep = true
	return sha, n, nil
}

// Open abre un fichero del almacén.
func (s *Store) Open(sha string) (*os.File, os.FileInfo, error) {
	if !ValidSHA(sha) {
		return nil, nil, os.ErrNotExist
	}
	f, err := os.Open(s.path(sha))
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// Remove borra un fichero del almacén. No es error que no exista.
func (s *Store) Remove(sha string) error {
	if !ValidSHA(sha) {
		return nil
	}
	err := os.Remove(s.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
