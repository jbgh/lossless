package crypt

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
)

const (
	ChunkSize  = 1 << 20
	tagSize    = 16
	saltSize   = 16
	headerSize = 4 + 1 + saltSize // magic, version, salt = 21
	formatVer  = 1
)

var magic = []byte("LSBK")

var ErrCorrupt = errors.New("crypt: object corrupt, truncated, or wrong key")

// CipherLen is the exact ciphertext length for a plaintext of plainLen
// bytes: header, one 16-byte tag per chunk, and the plaintext itself.
func CipherLen(plainLen int64) int64 {
	n := plainLen / ChunkSize
	if plainLen%ChunkSize != 0 || plainLen == 0 {
		n++
	}
	return headerSize + n*tagSize + plainLen
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func fill(nonce, aad []byte, i uint32, last bool) {
	binary.BigEndian.PutUint32(nonce[8:], i)
	binary.BigEndian.PutUint32(aad[:4], i)
	aad[4] = 0
	if last {
		aad[4] = 1
	}
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func hexSum(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

// Encrypt streams src into dst as one object bound to relpath. It returns
// the plaintext sha256, the ciphertext sha256, and the ciphertext length,
// so the caller can sign the upload without a second pass.
func (k *Keys) Encrypt(dst io.Writer, src io.Reader, relpath string) (plainSHA, cipherSHA string, cipherLen int64, err error) {
	salt := make([]byte, saltSize)
	if _, err = rand.Read(salt); err != nil {
		return "", "", 0, err
	}
	key, err := k.objectKey(salt, relpath)
	if err != nil {
		return "", "", 0, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", "", 0, err
	}
	ph, ch := sha256.New(), sha256.New()
	cw := &countWriter{w: io.MultiWriter(dst, ch)}
	header := make([]byte, 0, headerSize)
	header = append(header, magic...)
	header = append(header, formatVer)
	header = append(header, salt...)
	if _, err = cw.Write(header); err != nil {
		return "", "", 0, err
	}
	br := bufio.NewReaderSize(src, ChunkSize)
	buf := make([]byte, ChunkSize)
	sealed := make([]byte, 0, ChunkSize+tagSize)
	nonce := make([]byte, 12)
	aad := make([]byte, 5)
	for i := uint32(0); ; i++ {
		n, rerr := io.ReadFull(br, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return "", "", 0, rerr
		}
		chunk := buf[:n]
		ph.Write(chunk)
		last := n < ChunkSize
		if !last {
			if _, perr := br.Peek(1); perr == io.EOF {
				last = true
			} else if perr != nil {
				return "", "", 0, perr
			}
		}
		fill(nonce, aad, i, last)
		sealed = aead.Seal(sealed[:0], nonce, chunk, aad)
		if _, err = cw.Write(sealed); err != nil {
			return "", "", 0, err
		}
		if last {
			break
		}
	}
	return hexSum(ph), hexSum(ch), cw.n, nil
}

// Decrypt streams an object produced by Encrypt for relpath into dst and
// returns the plaintext sha256. Any tampering, truncation, reordering,
// wrong path, or wrong key returns ErrCorrupt.
func (k *Keys) Decrypt(dst io.Writer, src io.Reader, relpath string) (plainSHA string, err error) {
	br := bufio.NewReaderSize(src, ChunkSize+tagSize)
	header := make([]byte, headerSize)
	if _, err = io.ReadFull(br, header); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return "", ErrCorrupt
		}
		return "", err
	}
	if !bytes.Equal(header[:4], magic) || header[4] != formatVer {
		return "", ErrCorrupt
	}
	key, err := k.objectKey(header[5:], relpath)
	if err != nil {
		return "", err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	ph := sha256.New()
	buf := make([]byte, ChunkSize+tagSize)
	plain := make([]byte, 0, ChunkSize)
	nonce := make([]byte, 12)
	aad := make([]byte, 5)
	for i := uint32(0); ; i++ {
		n, rerr := io.ReadFull(br, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return "", rerr
		}
		if n < tagSize {
			return "", ErrCorrupt
		}
		last := n < len(buf)
		if !last {
			if _, perr := br.Peek(1); perr == io.EOF {
				last = true
			} else if perr != nil {
				return "", perr
			}
		}
		fill(nonce, aad, i, last)
		plain, err = aead.Open(plain[:0], nonce, buf[:n], aad)
		if err != nil {
			return "", ErrCorrupt
		}
		ph.Write(plain)
		if _, err = dst.Write(plain); err != nil {
			return "", err
		}
		if last {
			break
		}
	}
	return hexSum(ph), nil
}
