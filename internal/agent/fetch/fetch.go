// Package fetch descarga instaladores del servidor con reanudación: lo
// descargado se guarda en <destino>.part y, si la descarga se corta, el
// siguiente intento pide solo el resto (Range). Antes de dar el fichero por
// bueno comprueba su tamaño y su SHA-256.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrChecksum = errors.New("el fichero descargado no coincide con su suma de comprobación")
	ErrNoSpace  = errors.New("no hay espacio suficiente en el disco")
)

type Getter struct {
	Base   string // https://servidor
	Bearer string
	HTTP   *http.Client
	// Free devuelve el espacio libre del disco de un directorio.
	Free func(dir string) (uint64, error)
}

// Get descarga el fichero con ese SHA-256 y tamaño en dst.
func (g Getter) Get(ctx context.Context, sha string, size int64, dst string) error {
	part := dst + ".part"
	var have int64
	if info, err := os.Stat(part); err == nil {
		have = info.Size()
	}
	if have > size {
		_ = os.Remove(part)
		have = 0
	}
	if g.Free != nil {
		free, err := g.Free(filepath.Dir(dst))
		need := uint64(float64(size-have) * 1.1)
		if err == nil && free < need {
			return fmt.Errorf("%w: hacen falta %d MB y hay %d MB libres", ErrNoSpace, need>>20, free>>20)
		}
	}
	if have < size {
		if err := g.download(ctx, sha, part, have); err != nil {
			return err
		}
	}
	info, err := os.Stat(part)
	if err != nil {
		return err
	}
	got, err := fileSHA(part)
	if err != nil {
		return err
	}
	if info.Size() != size || !strings.EqualFold(got, sha) {
		_ = os.Remove(part)
		_ = os.Remove(dst)
		return ErrChecksum
	}
	return os.Rename(part, dst)
}

func (g Getter) download(ctx context.Context, sha, part string, offset int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.Base+"/agent/v1/files/"+sha, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.Bearer)
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("descarga: %w", err)
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0 &&
		strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-"):
		flags |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		flags |= os.O_TRUNC // el servidor no admite el rango: desde el principio
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return errors.New("la descarga anterior no encaja con el fichero del servidor; se empezará de cero")
	default:
		return fmt.Errorf("el servidor respondió %d al descargar el instalador", resp.StatusCode)
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("descarga interrumpida (se reanudará): %w", err)
	}
	return nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
